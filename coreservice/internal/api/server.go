// File: internal/api/server.go
package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"coreservice/internal/core"
	"coreservice/internal/crypto"
	"coreservice/internal/network"
	"coreservice/internal/vm"
)

// APIServer bọc lấy WasmEngine để xử lý request
type APIServer struct {
	Engine           *vm.WasmEngine
	KeyPair          *crypto.KeyPair
	Transport        *network.Transport
	OrderServiceAddr string
}

func (s *APIServer) HandleSubmitTx(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Chỉ hỗ trợ phương thức POST", http.StatusMethodNotAllowed)
		return
	}

	var tx core.Transaction
	err := json.NewDecoder(r.Body).Decode(&tx)
	if err != nil {
		http.Error(w, "JSON gửi lên sai định dạng", http.StatusBadRequest)
		return
	}

	fmt.Printf("\n📥 [API] Nhận được giao dịch: %s gọi contract '%s'\n", tx.TxID, tx.ContractName)

	// Execute contract
	err = s.Engine.Execute(r.Context(), tx)

	w.Header().Set("Content-Type", "application/json")

	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "error",
			"message": err.Error(),
		})
		return
	}

	// Sign transaction with server's private key
	signature, err := crypto.SignTransaction(tx.TxID, tx.ContractName, tx.Payload, s.KeyPair.PrivateKey)
	if err != nil {
		fmt.Printf("❌ [API] Lỗi ký transaction: %v\n", err)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "error",
			"message": "Failed to sign transaction",
		})
		return
	}

	tx.Signature = signature
	tx.SenderPubKey = s.KeyPair.PublicKey // Endorser's public key
	tx.ClientPubKey = s.KeyPair.PublicKey // Client = endorser in this case

	fmt.Printf("✍️  [API] Đã ký transaction: %s\n", signature[:16]+"...")
	fmt.Printf("📌 [API] Public Key: %s\n", s.KeyPair.PublicKey[:16]+"...")

	// Verify signature
	isValid := crypto.VerifyTransaction(tx.TxID, tx.ContractName, tx.Payload, signature, s.KeyPair.PublicKey)
	if !isValid {
		fmt.Printf("❌ [API] Signature verification failed!\n")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "error",
			"message": "Signature verification failed",
		})
		return
	}

	fmt.Printf("✅ [API] Signature verified successfully\n")

	// Send transaction to Order Service via libp2p
	fmt.Printf("📤 [API] Fetching membership from Order Service...\n")

	if s.Transport == nil {
		fmt.Printf("❌ [API] ERROR: Transport is nil!\n")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "error",
			"message": "Transport not initialized",
		})
		return
	}

	// Fetch membership to find Order Service node
	membership, err := s.Transport.GetMembershipFromOrderService(s.OrderServiceAddr)
	if err != nil {
		fmt.Printf("⚠️  [API] Error fetching membership: %v\n", err)
		w.WriteHeader(http.StatusBadGateway)
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "error",
			"message": "Failed to fetch membership from Order Service",
		})
		return
	}

	fmt.Printf("📋 [API] Membership: Leader=%s, Members=%d\n", membership.LeaderID[:8], len(membership.Members))

	// Try to find and connect to leader first, then followers
	membersToTry := []network.MemberInfo{}
	for _, member := range membership.Members {
		if member.ID == membership.LeaderID {
			membersToTry = append([]network.MemberInfo{member}, membersToTry...) // leader first
		} else {
			membersToTry = append(membersToTry, member)
		}
	}

	sent := false
	for _, member := range membersToTry {
		fmt.Printf("🔄 [API] Attempting to send transaction to %s via libp2p...\n", member.ID[:8])

		// Send transaction via libp2p with member info (includes addresses)
		if err := s.Transport.SendTransaction(member, tx); err != nil {
			fmt.Printf("⚠️  [API] Failed to send to %s: %v\n", member.ID[:8], err)
			continue
		}

		fmt.Printf("✅ [API] Transaction sent successfully to %s\n", member.ID[:8])
		sent = true
		break
	}

	if !sent {
		fmt.Printf("❌ [API] Failed to send transaction to any node\n")
		w.WriteHeader(http.StatusBadGateway)
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "error",
			"message": "Failed to send transaction to Order Service",
		})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status":    "success",
		"tx_id":     tx.TxID,
		"signature": signature[:32] + "...",
	})
}

func (s *APIServer) HandleDeployContract(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Chỉ hỗ trợ phương thức POST", http.StatusMethodNotAllowed)
		return
	}

	err := r.ParseMultipartForm(10 << 20)
	if err != nil {
		http.Error(w, "Lỗi parse dữ liệu gửi lên", http.StatusBadRequest)
		return
	}

	contractName := r.FormValue("contract_name")
	if contractName == "" {
		http.Error(w, "Thiếu tham số 'contract_name'", http.StatusBadRequest)
		return
	}

	// 3. Lấy file .wasm đính kèm
	file, _, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "Thiếu file đính kèm (field 'file')", http.StatusBadRequest)
		return
	}
	defer file.Close()

	wasmBytes, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, "Lỗi đọc file nhị phân", http.StatusInternalServerError)
		return
	}

	err = s.Engine.GetDB().SaveContract(contractName, wasmBytes)
	if err != nil {
		http.Error(w, "Lỗi lưu vào LevelDB", http.StatusInternalServerError)
		return
	}

	fmt.Printf("📦 [API] Đã deploy Contract mới: '%s' (%d bytes)\n", contractName, len(wasmBytes))

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status":        "success",
		"message":       "Deploy Smart Contract thành công!",
		"contract_name": contractName,
	})
}

func (s *APIServer) HandleGetState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Chỉ hỗ trợ phương thức GET", http.StatusMethodNotAllowed)
		return
	}

	key := r.URL.Query().Get("key")
	if key == "" {
		http.Error(w, "Thiếu tham số 'key'", http.StatusBadRequest)
		return
	}

	val, err := s.Engine.GetDB().GetState(key)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "Không tìm thấy dữ liệu"})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(val)
}
