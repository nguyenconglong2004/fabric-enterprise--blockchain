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
	"coreservice/internal/crypto"
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

	// Generate or load key pair
	keyPair, err := crypto.GenerateKeyPair()
	if err != nil {
		fmt.Printf("❌ Lỗi tạo key pair: %v\n", err)
		return
	}

	fmt.Printf("📝 Public Key: %s\n", keyPair.PublicKey[:16]+"...")
	fmt.Printf("🔐 Private Key: %s\n", keyPair.PrivateKey[:16]+"...")

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

	// Libp2p multiaddr của bất kỳ orderer nào, ví dụ: /ip4/127.0.0.1/tcp/6000/p2p/12D3Koo...
	envPeer := strings.TrimSpace(os.Getenv("ORDER_SERVICE_PEER"))
	fmt.Println()
	fmt.Println("📮 Order Service (libp2p) — nhập multiaddr của một orderer trong cluster.")
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

	apiServer := &api.APIServer{
		Engine:             engine,
		KeyPair:            keyPair,
		Transport:          transport,
		OrderServicePeer:   orderServicePeer,
		DB:                 postgresDB,
	}

	http.HandleFunc("/api/tx/deploy", apiServer.HandleDeployContract)
	http.HandleFunc("/api/deploy-example", apiServer.HandleDeployExampleAsset)
	http.HandleFunc("/api/tx/submit", apiServer.HandleSubmitTx)
	http.HandleFunc("/api/contracts", apiServer.HandleListContracts)
	http.HandleFunc("/api/contract/schema", apiServer.HandleGetContractSchema)
	http.HandleFunc("/api/blocks", apiServer.HandleListCommittedBlocks)
	http.HandleFunc("/api/transactions", apiServer.HandleListCommittedTransactions)
	http.HandleFunc("/api/state", apiServer.HandleGetState)
	http.HandleFunc("/api/block", apiServer.HandleGetBlock)

	port := ":8080"
	fmt.Printf("🌐 Core Node API Server đang chạy tại http://localhost%s\n", port)

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	server := &http.Server{
		Addr: port,
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
