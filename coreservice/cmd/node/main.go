package main

import (
	"fmt"
	"net/http"

	"coreservice/internal/api"
	"coreservice/internal/state"
	"coreservice/internal/vm"
)

func main() {
	fmt.Println("🚀 Đang khởi động Core Node...")

	db := state.InitDB("./data")
	defer db.Close()

	engine := vm.NewWasmEngine(db)
	defer engine.Close()

	apiServer := &api.APIServer{Engine: engine}

	http.HandleFunc("/api/tx/deploy", apiServer.HandleDeployContract)
	http.HandleFunc("/api/tx/submit", apiServer.HandleSubmitTx)
	http.HandleFunc("/api/state", apiServer.HandleGetState)

	port := ":8080"
	fmt.Printf("🌐 Core Node API Server đang chạy tại http://localhost%s\n", port)

	err := http.ListenAndServe(port, nil)
	if err != nil {
		fmt.Printf("❌ Lỗi server: %v\n", err)
	}
}
