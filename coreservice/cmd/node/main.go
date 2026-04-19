package main

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"

	"coreservice/internal/api"
	"coreservice/internal/crypto"
	"coreservice/internal/network"
	"coreservice/internal/state"
	"coreservice/internal/vm"
)

func main() {
	fmt.Println("🚀 Đang khởi động Core Node...")

	reader := bufio.NewReader(os.Stdin)
	fmt.Print("Enter Order Service P2P address (e.g., /ip4/127.0.0.1/tcp/6000/p2p/12D3Koo...): ")
	orderServiceAddr, _ := reader.ReadString('\n')
	orderServiceAddr = strings.TrimSpace(orderServiceAddr)

	if orderServiceAddr == "" {
		fmt.Printf("❌ Order Service address is required!\n")
		return
	}

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
		OrderServiceAddr: orderServiceAddr,
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
