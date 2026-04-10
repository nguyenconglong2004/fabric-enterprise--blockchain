// File: internal/api/server.go
package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"coreservice/internal/core"
	"coreservice/internal/crypto"
	"coreservice/internal/vm"
)

// APIServer bọc lấy WasmEngine để xử lý request
type APIServer struct {
	Engine      *vm.WasmEngine
	NodePrivKey string
	NodeID      string
}

func (s *APIServer) HandleSubmitTx(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Chỉ hỗ trợ phương thức POST", http.StatusMethodNotAllowed)
		return
	}

	var txProposal core.TransactionProposal
	err := json.NewDecoder(r.Body).Decode(&txProposal)
	if err != nil {
		http.Error(w, "JSON gửi lên sai định dạng", http.StatusBadRequest)
		return
	}

	fmt.Printf("\n📥 [API] Nhận được giao dịch: %s gọi contract '%s'\n", txProposal.TxID, txProposal.ContractName)

	rwSet, err := s.Engine.Execute(r.Context(), &txProposal)

	w.Header().Set("Content-Type", "application/json")

	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "error",
			"message": err.Error(),
		})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "success",
		"tx_id":  txProposal.TxID,
		"rw_set": rwSet,
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
func (s *APIServer) HandleProposeTx(w http.ResponseWriter, r *http.Request) {
	var proposal core.TransactionProposal
	json.NewDecoder(r.Body).Decode(&proposal)

	isValid := crypto.Verify(proposal.ClientPubKey, proposal.Payload, proposal.Signature)
	if !isValid {
		http.Error(w, "Chữ ký Client không hợp lệ (Giả mạo!)", http.StatusUnauthorized)
		return
	}
	fmt.Printf("✅ [Crypto] Chữ ký của Client hợp lệ!\n")

	rwSet, err := s.Engine.Execute(r.Context(), &proposal)
	if err != nil {
		http.Error(w, fmt.Sprintf("Lỗi khi chạy nháp: %v", err), http.StatusInternalServerError)
		return
	}

	rwSetBytes, _ := json.Marshal(rwSet)
	peerSignature, err := crypto.Sign(s.NodePrivKey, rwSetBytes)
	if err != nil {
		http.Error(w, "Lỗi Server: Không thể ký RWSet", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(core.EndorsementResponse{
		TxID:       proposal.TxID,
		EndorserID: s.NodeID,
		RWSet:      rwSet,
		Status:     200,
		Message:    "Mô phỏng thành công, trả về RW Set",
		Signature:  peerSignature, // HÀNG REAL CHUẨN MẬT MÃ!
	})
}
