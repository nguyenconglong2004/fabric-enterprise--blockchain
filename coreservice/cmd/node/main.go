package main

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"coreservice/internal/api"
	"coreservice/internal/discovery"
	"coreservice/internal/network"
	"coreservice/internal/state"
	"coreservice/internal/storage"
	"coreservice/internal/vm"
)

// redactPostgresURL hides password for logs.
func redactPostgresURL(conn string) string {
	u := strings.TrimPrefix(conn, "postgres://")
	if i := strings.Index(u, "@"); i > 0 {
		userpass := u[:i]
		if j := strings.Index(userpass, ":"); j > 0 {
			return "postgres://" + userpass[:j] + ":***@" + u[i+1:]
		}
	}
	return conn
}

func main() {
	fmt.Println("🚀 Đang khởi động Core Node...")

	// Create libp2p transport
	ctx := context.Background()
	transport, err := network.NewTransport(ctx)
	if err != nil {
		fmt.Printf("❌ Lỗi tạo Transport: %v\n", err)
		return
	}
	defer transport.Close()

	fmt.Printf("📡 P2P ID: %s\n", transport.ID().ShortString())

	// Initialize PostgreSQL (same DB as commit peer: commit_peer.* tables).
	dbConnStr := strings.TrimSpace(os.Getenv("POSTGRES_URL"))
	if dbConnStr == "" {
		dbConnStr = "postgres://fabric:fabric123@localhost:5432/blockchain?sslmode=disable"
	}

	var postgresDB *storage.PostgresDB
	var pgErr error
	for attempt := 1; attempt <= 10; attempt++ {
		postgresDB, pgErr = storage.NewPostgresDB(dbConnStr)
		if pgErr == nil {
			break
		}
		fmt.Printf("⚠️  PostgreSQL chưa sẵn sàng (lần %d/10): %v\n", attempt, pgErr)
		if attempt < 10 {
			time.Sleep(2 * time.Second)
		}
	}
	if postgresDB == nil {
		fmt.Println("⚠️  Không kết nối được PostgreSQL — API /api/block, /api/blocks, /api/transactions sẽ không đọc được ledger đã commit.")
		fmt.Println("   Kiểm tra POSTGRES_URL hoặc chờ DB rồi khởi động lại core node.")
	} else {
		defer postgresDB.Close()
		fmt.Printf("✅ PostgreSQL connected (%s)\n", redactPostgresURL(dbConnStr))
	}

	// Initialize LevelDB for state
	stateDB := state.InitDB("./data")
	defer stateDB.Close()

	engine := vm.NewWasmEngine(stateDB)
	defer engine.Close()
	fmt.Printf("⚡ WASM pool: %d sandbox/contract (WASM_POOL_SIZE, max 32)\n", vm.ModulePoolSize())

	// Libp2p multiaddr của bất kỳ orderer nào, ví dụ: /ip4/127.0.0.1/tcp/6000/p2p/12D3Koo...
	envPeer := strings.TrimSpace(os.Getenv("ORDER_SERVICE_PEER"))
	fmt.Println()
	fmt.Println("📮 Order Service (libp2p) — nhập multiaddr của một orderer trong cluster.")
	fmt.Println("   (Bất kỳ node nào cũng được; discovery sẽ lấy leader/members alive qua membership.)")
	fmt.Println("   Ví dụ: /ip4/127.0.0.1/tcp/6000/p2p/12D3Koo...")
	if envPeer != "" {
		fmt.Printf("   (Biến ORDER_SERVICE_PEER=%s — Enter trống sẽ dùng giá trị này)\n", envPeer)
	} else {
		fmt.Println("   (Enter trống = không gửi endorsement lên order service)")
	}
	fmt.Print("OrderServicePeer > ")
	sc := bufio.NewScanner(os.Stdin)
	orderServicePeer := ""
	if sc.Scan() {
		orderServicePeer = strings.TrimSpace(sc.Text())
	}
	if orderServicePeer == "" {
		orderServicePeer = envPeer
	}
	if orderServicePeer != "" {
		fmt.Printf("✅ Đã cấu hình OrderServicePeer: %s\n", orderServicePeer)
	} else {
		fmt.Println("ℹ️  Chưa cấu OrderServicePeer — endorsements sẽ không gửi tới orderers.")
	}

	var orderDiscovery *discovery.Client
	if orderServicePeer != "" {
		var discErr error
		orderDiscovery, discErr = discovery.NewClient(transport, []string{orderServicePeer})
		if discErr != nil {
			fmt.Printf("⚠️  Không tạo order discovery: %v\n", discErr)
		} else {
			orderDiscovery.StartRefreshLoop(ctx, 5*time.Second)
			fmt.Println("✅ Order discovery bật (refresh membership mỗi 5s)")
		}
	}

	// Commit Peer P2P address for transaction signing
	envCommitPeer := strings.TrimSpace(os.Getenv("COMMIT_PEER_P2P"))
	fmt.Println()
	fmt.Println("🔏 Commit Peer (tx-sign) — nhập multiaddr của committing peer để ký ghi chứng (endorsement).")
	fmt.Println("   Ví dụ: /ip4/127.0.0.1/tcp/12345/p2p/12D3Koo...")
	if envCommitPeer != "" {
		fmt.Printf("   (Biến COMMIT_PEER_P2P=%s — Enter trống sẽ dùng giá trị này)\n", envCommitPeer)
	} else {
		fmt.Println("   (Enter trống = không ký ghi chứng, /api/tx/submit sẽ thất bại)")
	}
	fmt.Print("CommitPeerP2P > ")
	commitPeerP2P := ""
	if sc.Scan() {
		commitPeerP2P = strings.TrimSpace(sc.Text())
	}
	if commitPeerP2P == "" {
		commitPeerP2P = envCommitPeer
	}
	if commitPeerP2P != "" {
		fmt.Printf("✅ Đã cấu hình CommitPeerP2P: %s\n", commitPeerP2P)
		firstCP := strings.TrimSpace(strings.Split(commitPeerP2P, ",")[0])
		if err := transport.WarmCommitPeer(firstCP); err != nil {
			fmt.Printf("⚠️  Warm commit peer dial: %v\n", err)
		} else if network.SignPoolEnabled() {
			fmt.Println("✅ Commit peer sign pool bật (warm conn, CORE_SIGN_POOL=0 để tắt)")
		} else {
			fmt.Println("✅ Commit peer P2P warmed (sign pool tắt)")
		}
	} else {
		fmt.Println("⚠️  Chưa cấu CommitPeerP2P — /api/tx/submit sẽ thất bại khi sign transaction.")
	}

	if orderDiscovery != nil {
		if _, err := orderDiscovery.Refresh(ctx); err != nil {
			fmt.Printf("⚠️  Order discovery initial refresh: %v\n", err)
		} else {
			fmt.Println("✅ Order discovery membership cached")
		}
	}

	apiServer := &api.APIServer{
		Engine:               engine,
		Transport:            transport,
		OrderServicePeer:     orderServicePeer,
		OrderDiscovery:       orderDiscovery,
		DB:                   postgresDB,
		CommitPeerMultiaddrs: commitPeerP2P,
	}
	apiServer.InitSubmitRecorder()
	defer apiServer.CloseSubmitRecorder()

	http.HandleFunc("/api/tx/deploy", apiServer.HandleDeployContract)
	http.HandleFunc("/api/deploy-example", apiServer.HandleDeployExampleAsset)
	http.HandleFunc("/api/tx/submit", apiServer.HandleSubmitTx)
	http.HandleFunc("/api/contracts", apiServer.HandleListContracts)
	http.HandleFunc("/api/contract/schema", apiServer.HandleGetContractSchema)
	http.HandleFunc("/api/blocks", apiServer.HandleListCommittedBlocks)
	http.HandleFunc("/api/transactions", apiServer.HandleListCommittedTransactions)
	http.HandleFunc("/api/state", apiServer.HandleGetState)
	http.HandleFunc("/api/block", apiServer.HandleGetBlock)
	http.HandleFunc("/api/metrics/throughput", apiServer.HandleThroughputMetrics)
	http.HandleFunc("/api/metrics/benchmark", apiServer.HandleBenchmarkMetrics)
	http.HandleFunc("/api/metrics/e2e", apiServer.HandleE2EMetrics)
	http.HandleFunc("/api/explorer/stream", apiServer.HandleExplorerStream)

	port := ":8080"
	fmt.Printf("🌐 Core Node API Server đang chạy tại http://localhost%s\n", port)
	if os.Getenv("CORE_RECORD_SUBMIT") == "0" {
		fmt.Println("📊 Submit recording tắt (CORE_RECORD_SUBMIT=0) — benchmark submit metrics = 0")
	} else {
		fmt.Println("📊 Submit recording bật — GET /api/metrics/benchmark (tắt: CORE_RECORD_SUBMIT=0)")
	}

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	server := &http.Server{
		Addr:              port,
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      120 * time.Second,
	}

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Printf("❌ Lỗi server: %v\n", err)
		}
	}()

	// Wait for shutdown signal
	<-sigChan
	fmt.Println("\n🛑 Đang tắt server...")
	server.Shutdown(context.Background())
	fmt.Println("✅ Server đã tắt")
}
