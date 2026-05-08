// File: internal/api/server.go
package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

	"coreservice/internal/core"
	"coreservice/internal/crypto"
	"coreservice/internal/network"
	"coreservice/internal/storage"
	"coreservice/internal/vm"
)

// APIServer bọc lấy WasmEngine để xử lý request
type APIServer struct {
	Engine           *vm.WasmEngine
	KeyPair          *crypto.KeyPair
	Transport        *network.Transport
	// OrderServicePeer is a libp2p multiaddr of any orderer node (e.g. /ip4/127.0.0.1/tcp/6000/p2p/12D3Koo...).
	OrderServicePeer string
	DB               *storage.PostgresDB
}

// resolveContractSchema: schema saved at deploy (LevelDB / Postgres) overrides builtin map in core/contract_schema.go.
func (s *APIServer) resolveContractSchema(contractName string) (*core.ContractSchema, string) {
	if s.Engine != nil {
		if ldb := s.Engine.GetDB(); ldb != nil {
			raw, err := ldb.GetContractMetaSchema(contractName)
			if err == nil && len(raw) > 0 {
				var sch core.ContractSchema
				if json.Unmarshal(raw, &sch) == nil {
					if sch.Name == "" {
						sch.Name = contractName
					}
					return &sch, "deployed"
				}
			}
		}
	}
	if s.DB != nil {
		raw, err := s.DB.GetContractPayloadSchema(contractName)
		if err == nil && len(raw) > 0 {
			var sch core.ContractSchema
			if json.Unmarshal(raw, &sch) == nil {
				if sch.Name == "" {
					sch.Name = contractName
				}
				return &sch, "deployed"
			}
		}
	}
	return core.GetContractSchema(contractName), "builtin"
}

// HandleListContracts returns all deployed contracts.
// GET /api/contracts
func (s *APIServer) HandleListContracts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Only GET supported", http.StatusMethodNotAllowed)
		return
	}

	var contracts []string

	// Prefer LevelDB (Engine DB) since DeployContract writes there.
	if s.Engine != nil {
		if db := s.Engine.GetDB(); db != nil {
			if names, err := db.ListContracts(); err == nil {
				contracts = names
			} else {
				fmt.Printf("⚠️  [API] ListContracts(LevelDB) error: %v\n", err)
			}
		}
	}

	// If no contracts in DB, use available contracts from schema
	if len(contracts) == 0 {
		availableContracts := core.ListAvailableContracts()
		for _, cs := range availableContracts {
			contracts = append(contracts, cs.Name)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":    "success",
		"contracts": contracts,
		"count":     len(contracts),
	})
}

// HandleGetContractSchema returns the schema for a contract.
// GET /api/contract/schema?name=example_asset
func (s *APIServer) HandleGetContractSchema(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Only GET supported", http.StatusMethodNotAllowed)
		return
	}

	contractName := r.URL.Query().Get("name")
	if contractName == "" {
		http.Error(w, "Missing 'name' parameter", http.StatusBadRequest)
		return
	}

	schema, source := s.resolveContractSchema(contractName)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":        "success",
		"schema":        schema,
		"schema_source": source,
	})
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

	fmt.Printf("\n📥 [API] Nhận được giao dịch: %s gọi contract '%s'\n", tx.Txid, tx.ContractName)

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
	signature, err := crypto.SignTransaction(tx.Txid, tx.ContractName, tx.Payload, s.KeyPair.PrivateKey)
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
	isValid := crypto.VerifyTransaction(tx.Txid, tx.ContractName, tx.Payload, signature, s.KeyPair.PublicKey)
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

	// Send endorsement to Order Service over libp2p (endorsement protocol).
	fmt.Printf("🔍 [API] DEBUG: OrderServicePeer='%s', Transport=%v\n", s.OrderServicePeer, s.Transport != nil)

	if s.OrderServicePeer != "" {
		if s.Transport == nil {
			fmt.Printf("❌ [API] ERROR: Transport is nil!\n")
		} else {
			fmt.Printf("📤 [API] Lấy membership từ Order Service (libp2p)...\n")
			membership, err := s.Transport.GetMembershipFromBootstrapPeer(s.OrderServicePeer)
			if err != nil {
				fmt.Printf("⚠️  [API] Lỗi lấy membership (libp2p): %v\n", err)
			} else {
				leaderPrefix := membership.LeaderID
				if len(leaderPrefix) > 8 {
					leaderPrefix = leaderPrefix[:8]
				}
				fmt.Printf("📋 [API] Membership: Leader=%s..., Members=%d\n", leaderPrefix, len(membership.Members))

				sent := false
				// Prefer sending endorsement directly to the current leader.
				if membership.LeaderID != "" {
					for _, m := range membership.Members {
						if !m.Alive || m.ID != membership.LeaderID {
							continue
						}
						addrInfo, err := network.AddrInfoFromMember(m)
						if err != nil {
							fmt.Printf("⚠️  [API] Không build AddrInfo cho leader: %v\n", err)
							continue
						}
						if err := s.Transport.SendEndorsement(addrInfo, tx); err != nil {
							fmt.Printf("⚠️  [API] Gửi endorsement tới leader thất bại: %v\n", err)
						} else {
							short := m.ID
							if len(short) > 8 {
								short = short[:8]
							}
							fmt.Printf("✅ [API] Đã gửi endorsement tới leader %s (libp2p)\n", short)
							sent = true
						}
						break
					}
				}
				// Fallback: any alive member (order node forwards to leader if needed).
				if !sent {
					for _, m := range membership.Members {
						if !m.Alive {
							continue
						}
						addrInfo, err := network.AddrInfoFromMember(m)
						if err != nil {
							continue
						}
						if err := s.Transport.SendEndorsement(addrInfo, tx); err != nil {
							short := m.ID
							if len(short) > 8 {
								short = short[:8]
							}
							fmt.Printf("⚠️  [API] Gửi endorsement tới %s thất bại: %v\n", short, err)
							continue
						}
						short := m.ID
						if len(short) > 8 {
							short = short[:8]
						}
						fmt.Printf("✅ [API] Đã gửi endorsement tới %s (libp2p, có thể forward)\n", short)
						sent = true
						break
					}
				}
				if !sent {
					bootstrap, err := peer.AddrInfoFromString(s.OrderServicePeer)
					if err != nil {
						fmt.Printf("⚠️  [API] Bootstrap peer invalid: %v\n", err)
					} else if err := s.Transport.SendEndorsement(*bootstrap, tx); err != nil {
						fmt.Printf("⚠️  [API] Gửi endorsement tới bootstrap thất bại: %v\n", err)
					} else {
						fmt.Printf("✅ [API] Đã gửi endorsement tới bootstrap peer (libp2p)\n")
					}
				}
			}
		}
	} else {
		fmt.Printf("📤 [API] Không có ORDER_SERVICE_PEER — bỏ qua gửi endorsement tới order service\n")
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":        "success",
		"tx_id":         tx.Txid,
		"contract_name": tx.ContractName,
		"function_name": tx.FunctionName,
		"sender_pubkey": tx.SenderPubKey,
		"client_pubkey": tx.ClientPubKey,
		"signature":     signature[:32] + "...",
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

	var schemaBytes []byte
	schemaStr := strings.TrimSpace(r.FormValue("payload_schema"))
	if schemaStr != "" {
		var probe map[string]interface{}
		if err := json.Unmarshal([]byte(schemaStr), &probe); err != nil {
			http.Error(w, "payload_schema phải là JSON (gợi ý: {\"name\":\"...\",\"fields\":[{\"name\":\"x\",\"label\":\"X\",\"type\":\"string\",\"required\":true}]})", http.StatusBadRequest)
			return
		}
		schemaBytes = []byte(schemaStr)
	}

	err = s.Engine.GetDB().SaveContract(contractName, wasmBytes)
	if err != nil {
		http.Error(w, "Lỗi lưu vào LevelDB", http.StatusInternalServerError)
		return
	}

	if len(schemaBytes) > 0 {
		if err := s.Engine.GetDB().SaveContractMetaSchema(contractName, schemaBytes); err != nil {
			fmt.Printf("⚠️  [API] Lỗi lưu payload_schema vào LevelDB: %v\n", err)
		}
	}

	// Save to PostgreSQL if available
	if s.DB != nil {
		if err := s.DB.SaveContract(contractName, wasmBytes, schemaBytes); err != nil {
			fmt.Printf("⚠️  [API] Lỗi lưu contract vào PostgreSQL: %v\n", err)
			// Continue even if DB save fails
		} else {
			fmt.Printf("✅ [API] Contract saved to PostgreSQL\n")
		}
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

// HandleDeployExampleAsset deploys the pre-built example_asset contract
// POST /api/deploy-example
func (s *APIServer) HandleDeployExampleAsset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Chỉ hỗ trợ phương thức POST", http.StatusMethodNotAllowed)
		return
	}

	contractName := "example_asset"
	// Relative to coreservice/cmd/node when running `go run .` from there.
	wasmPath := "../contracts/example_asset/my_contract.wasm"

	// Read the pre-built WASM file
	wasmBytes, err := os.ReadFile(wasmPath)
	if err != nil {
		fmt.Printf("❌ [API] Lỗi đọc file WASM: %v\n", err)
		http.Error(w, fmt.Sprintf("Không tìm thấy file contract: %s", wasmPath), http.StatusInternalServerError)
		return
	}

	// Save to LevelDB
	err = s.Engine.GetDB().SaveContract(contractName, wasmBytes)
	if err != nil {
		http.Error(w, "Lỗi lưu vào LevelDB", http.StatusInternalServerError)
		return
	}

	// Save to PostgreSQL if available
	if s.DB != nil {
		if err := s.DB.SaveContract(contractName, wasmBytes, nil); err != nil {
			fmt.Printf("⚠️  [API] Lỗi lưu contract vào PostgreSQL: %v\n", err)
			// Continue even if DB save fails
		} else {
			fmt.Printf("✅ [API] Contract saved to PostgreSQL\n")
		}
	}

	fmt.Printf("📦 [API] Đã deploy Contract 'example_asset' (%d bytes)\n", len(wasmBytes))

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":        "success",
		"message":       "Deploy example_asset contract thành công!",
		"contract_name": contractName,
		"size_bytes":    len(wasmBytes),
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

// HandleGetBlock returns a block by hash
// GET /api/block?hash=<block_hash>
func (s *APIServer) HandleGetBlock(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Only GET supported", http.StatusMethodNotAllowed)
		return
	}

	blockHash := r.URL.Query().Get("hash")
	if blockHash == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "missing 'hash' parameter"})
		return
	}

	preview := blockHash
	if len(preview) > 16 {
		preview = preview[:16] + "..."
	}
	fmt.Printf("📦 [API] Querying block: %s\n", preview)

	if s.DB == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{
			"status": "error",
			"error":  "PostgreSQL not connected on core node; set POSTGRES_URL or restart after DB is up (same DB as commit peer)",
		})
		return
	}

	block, err := s.DB.GetCommittedBlockByHash(blockHash)
	if err == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "success",
			"block":  block,
		})
		return
	}
	fmt.Printf("⚠️  [API] Block not in DB: %v\n", err)

	w.WriteHeader(http.StatusNotFound)
	json.NewEncoder(w).Encode(map[string]string{
		"status": "error",
		"error":  "block not found",
	})
}

// HandleListCommittedBlocks returns latest committed blocks from DB.
// GET /api/blocks?limit=20
func (s *APIServer) HandleListCommittedBlocks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Only GET supported", http.StatusMethodNotAllowed)
		return
	}

	limit := 20
	if raw := r.URL.Query().Get("limit"); raw != "" {
		var parsed int
		if _, err := fmt.Sscanf(raw, "%d", &parsed); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	if s.DB == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "success",
			"blocks": []interface{}{},
			"count":  0,
		})
		return
	}

	blocks, err := s.DB.ListCommittedBlocks(limit)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"status": "error",
			"error":  err.Error(),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "success",
		"blocks": blocks,
		"count":  len(blocks),
	})
}

// HandleListCommittedTransactions returns latest committed transactions from DB.
// GET /api/transactions?limit=50
func (s *APIServer) HandleListCommittedTransactions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Only GET supported", http.StatusMethodNotAllowed)
		return
	}

	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		var parsed int
		if _, err := fmt.Sscanf(raw, "%d", &parsed); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	if s.DB == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":       "success",
			"transactions": []interface{}{},
			"count":        0,
		})
		return
	}

	txs, err := s.DB.ListCommittedTransactions(limit)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"status": "error",
			"error":  err.Error(),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":       "success",
		"transactions": txs,
		"count":        len(txs),
	})
}

// HandleExplorerStream streams explorer updates via SSE.
// GET /api/explorer/stream
func (s *APIServer) HandleExplorerStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Only GET supported", http.StatusMethodNotAllowed)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	sendEvent := func(eventType string, payload map[string]interface{}) {
		b, _ := json.Marshal(payload)
		fmt.Fprintf(w, "event: %s\n", eventType)
		fmt.Fprintf(w, "data: %s\n\n", string(b))
		flusher.Flush()
	}

	sendEvent("ready", map[string]interface{}{
		"status":  "connected",
		"message": "explorer stream ready",
	})

	if s.DB == nil {
		sendEvent("error", map[string]interface{}{
			"status":  "error",
			"message": "PostgreSQL is not connected",
		})
		<-r.Context().Done()
		return
	}

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	lastBlockHash := ""
	lastTxID := ""

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			changed := false
			eventPayload := map[string]interface{}{
				"updated_at": time.Now().UnixMilli(),
			}

			blocks, err := s.DB.ListCommittedBlocks(1)
			if err == nil && len(blocks) > 0 {
				hash := fmt.Sprintf("%v", blocks[0]["hash"])
				if hash != "" && hash != "<nil>" && hash != lastBlockHash {
					lastBlockHash = hash
					eventPayload["block_hash"] = hash
					eventPayload["latest_block"] = blocks[0]
					changed = true
				}
			}

			txs, err := s.DB.ListCommittedTransactions(1)
			if err == nil && len(txs) > 0 {
				txID := fmt.Sprintf("%v", txs[0]["txid"])
				if txID == "" || txID == "<nil>" {
					txID = fmt.Sprintf("%v", txs[0]["tx_id"])
				}
				if txID != "" && txID != "<nil>" && txID != lastTxID {
					lastTxID = txID
					eventPayload["txid"] = txID
					eventPayload["latest_tx"] = txs[0]
					changed = true
				}
			}

			if changed {
				sendEvent("ledger_update", eventPayload)
				continue
			}

			// Keep connection alive through proxies/load-balancers.
			fmt.Fprintf(w, ": ping %d\n\n", time.Now().Unix())
			flusher.Flush()
		}
	}
}
