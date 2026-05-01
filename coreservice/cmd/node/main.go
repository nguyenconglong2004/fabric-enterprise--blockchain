package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"coreservice/internal/api"
	"coreservice/internal/crypto"
	"coreservice/internal/network"
	"coreservice/internal/state"
	"coreservice/internal/storage"
	"coreservice/internal/vm"
)

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

	// Initialize PostgreSQL connection
	dbConnStr := "postgres://fabric:fabric123@localhost:5432/blockchain?sslmode=disable"

	postgresDB, err := storage.NewPostgresDB(dbConnStr)
	if err != nil {
		fmt.Printf("⚠️  Warning: Could not connect to PostgreSQL: %v\n", err)
		fmt.Println("Continuing without database persistence...")
		postgresDB = nil
	} else {
		defer postgresDB.Close()
		fmt.Printf("✅ PostgreSQL connected\n")
	}

	// Initialize LevelDB for state
	stateDB := state.InitDB("./data")
	defer stateDB.Close()

	engine := vm.NewWasmEngine(stateDB)
	defer engine.Close()

	apiServer := &api.APIServer{
		Engine:           engine,
		KeyPair:          keyPair,
		Transport:        transport,
		OrderServiceAddr: "http://localhost:8081",
		DB:               postgresDB,
	}

	http.HandleFunc("/api/tx/deploy", apiServer.HandleDeployContract)
	http.HandleFunc("/api/deploy-example", apiServer.HandleDeployExampleAsset)
	http.HandleFunc("/api/tx/submit", apiServer.HandleSubmitTx)
	http.HandleFunc("/api/contracts", apiServer.HandleListContracts)
	http.HandleFunc("/api/contract/schema", apiServer.HandleGetContractSchema)
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
