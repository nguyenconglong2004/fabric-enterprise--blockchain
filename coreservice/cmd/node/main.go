package main

import (
	"context"
	"fmt"
	"net/http"

	"coreservice/internal/api"
	"coreservice/internal/crypto"
	"coreservice/internal/network"
	"coreservice/internal/state"
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

	db := state.InitDB("./data")
	defer db.Close()

	engine := vm.NewWasmEngine(db)
	defer engine.Close()

	apiServer := &api.APIServer{
		Engine:           engine,
		KeyPair:          keyPair,
		Transport:        transport,
		OrderServiceAddr: "http://localhost:8081",
	}

	http.HandleFunc("/api/tx/deploy", apiServer.HandleDeployContract)
	http.HandleFunc("/api/tx/submit", apiServer.HandleSubmitTx)
	http.HandleFunc("/api/state", apiServer.HandleGetState)

	port := ":8080"
	fmt.Printf("🌐 Core Node API Server đang chạy tại http://localhost%s\n", port)

	err = http.ListenAndServe(port, nil)
	if err != nil {
		fmt.Printf("❌ Lỗi server: %v\n", err)
	}
}
