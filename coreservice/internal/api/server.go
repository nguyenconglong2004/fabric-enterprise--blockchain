// File: internal/api/server.go
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"coreservice/internal/core"
	"coreservice/internal/discovery"
	"coreservice/internal/network"
	"coreservice/internal/storage"
	"coreservice/internal/vm"
)

func apiVerbose() bool {
	return vm.Verbose()
}

// APIServer bọc lấy WasmEngine để xử lý request
type APIServer struct {
	Engine    *vm.WasmEngine
	Transport *network.Transport
	// OrderServicePeer is a libp2p multiaddr of any orderer node (e.g. /ip4/127.0.0.1/tcp/6000/p2p/12D3Koo...).
	OrderServicePeer string
	// OrderDiscovery resolves alive orderers (multi-bootstrap, cached membership).
	OrderDiscovery *discovery.Client
	DB               *storage.PostgresDB
	// CommitPeerMultiaddrs is comma-separated list of commit peer libp2p multiaddrs
	// (fallback: try each in order if one fails).
	// Format: addr1,addr2,addr3
	// Env: COMMIT_PEER_P2P.
	CommitPeerMultiaddrs string

	submitRecorder *storage.SubmitRecorder
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
	submittedAt := time.Now().UTC()

	if apiVerbose() {
		fmt.Printf("\n📥 [API] Nhận được giao dịch: %s gọi contract '%s'\n", tx.Txid, tx.ContractName)
	}

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

	if err := s.signTxViaCommitPeer(&tx); err != nil {
		if apiVerbose() {
			fmt.Printf("❌ [API] Commit peer signing failed: %v\n", err)
		}
		w.WriteHeader(http.StatusBadGateway)
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "error",
			"message": err.Error(),
		})
		return
	}

	sigPreview := tx.Signature
	if len(tx.Endorsements) > 0 {
		sigPreview = tx.Endorsements[len(tx.Endorsements)-1].Signature
	}
	if len(sigPreview) > 32 {
		sigPreview = sigPreview[:32] + "..."
	}
	if apiVerbose() {
		fmt.Printf("✍️  [API] Đã ký qua commit peer: %s (endorsements=%d)\n", sigPreview, len(tx.Endorsements))
		if len(tx.SenderPubKey) > 16 {
			fmt.Printf("📌 [API] Endorser pubkey (legacy / last): %s...\n", tx.SenderPubKey[:16])
		}
	}

	s.sendEndorsementAsync(tx)
	if s.submitRecorder != nil {
		s.submitRecorder.Record(tx.Txid, submittedAt)
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":            "success",
		"tx_id":             tx.Txid,
		"contract_name":     tx.ContractName,
		"function_name":     tx.FunctionName,
		"sender_pubkey":     tx.SenderPubKey,
		"client_pubkey":     tx.ClientPubKey,
		"signature":         sigPreview,
		"endorsement_count": len(tx.Endorsements),
	})
}

func (s *APIServer) sendEndorsementAsync(tx core.Transaction) {
	if s.OrderDiscovery == nil || s.Transport == nil {
		return
	}
	leaderOnly := endorseLeaderOnly()
	run := func() {
		if err := discovery.SendEndorsement(
			context.Background(), s.OrderDiscovery, s.Transport, tx, leaderOnly,
		); err != nil && apiVerbose() {
			fmt.Printf("⚠️  [API] async endorsement failed txid=%s: %v\n", tx.Txid, err)
		}
	}
	if asyncEndorse() {
		go run()
		return
	}
	run()
}

func (s *APIServer) signTxViaCommitPeer(tx *core.Transaction) error {
	addrsStr := strings.TrimSpace(s.CommitPeerMultiaddrs)
	if addrsStr == "" {
		return fmt.Errorf("COMMIT_PEER_P2P is not set (use comma-separated commit peer multiaddrs, e.g., addr1,addr2)")
	}

	// Parse comma-separated addresses
	addrs := strings.Split(addrsStr, ",")
	var lastErr error

	// Try each commit peer in order (fallback strategy)
	for i, addr := range addrs {
		addr = strings.TrimSpace(addr)
		if addr == "" {
			continue
		}

		if s.Transport == nil {
			return fmt.Errorf("libp2p transport not available")
		}

		if apiVerbose() {
			fmt.Printf("📞 [API] Trying commit peer %d/%d: %s\n", i+1, len(addrs), addr[:min(32, len(addr))]+"...")
		}
		err := s.Transport.SignTransactionViaCommitPeer(addr, tx)
		if err == nil {
			return nil
		}

		lastErr = err
		if apiVerbose() {
			fmt.Printf("⚠️  [API] Commit peer %d failed: %v, trying next...\n", i+1, err)
		}
	}

	if lastErr != nil {
		return fmt.Errorf("all commit peers failed (last error: %w)", lastErr)
	}
	return fmt.Errorf("no valid commit peer addresses found")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
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
	wasmPath := "../contracts/example_asset/my_contract.wasm"

	wasmBytes, err := os.ReadFile(wasmPath)
	if err != nil {
		fmt.Printf("❌ [API] Lỗi đọc file WASM: %v\n", err)
		http.Error(w, fmt.Sprintf("Không tìm thấy file contract: %s", wasmPath), http.StatusInternalServerError)
		return
	}

	if err := s.Engine.GetDB().SaveContract(contractName, wasmBytes); err != nil {
		http.Error(w, "Lỗi lưu vào LevelDB", http.StatusInternalServerError)
		return
	}

	if s.DB != nil {
		if err := s.DB.SaveContract(contractName, wasmBytes, nil); err != nil {
			fmt.Printf("⚠️  [API] Lỗi lưu contract vào PostgreSQL: %v\n", err)
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

// HandleThroughputMetrics returns tx/s from ledger commit times (Postgres mirror).
// GET /api/metrics/throughput?window=1&tx_prefix=k6-              (mode=latest, default)
// GET /api/metrics/throughput?mode=peak&lookback=60&window=1    (max tx/s bucket in lookback)
// GET /api/metrics/throughput?mode=window&since=...&until=...   (sustained over load window)
// GET /api/metrics/throughput?mode=since&since=...
func (s *APIServer) HandleThroughputMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Only GET supported", http.StatusMethodNotAllowed)
		return
	}

	if s.DB == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{
			"status": "error",
			"error":  "PostgreSQL not connected",
		})
		return
	}

	txPrefix := r.URL.Query().Get("tx_prefix")
	windowSec := 1
	if raw := r.URL.Query().Get("window"); raw != "" {
		var parsed int
		if _, err := fmt.Sscanf(raw, "%d", &parsed); err == nil && parsed > 0 {
			windowSec = parsed
		}
	}

	mode := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("mode")))
	var metrics *storage.ThroughputMetrics
	var err error

	switch {
	case mode == "window":
		since, okSince := parseTimeQuery(r.URL.Query().Get("since"))
		until, okUntil := parseTimeQuery(r.URL.Query().Get("until"))
		if !okSince || !okUntil {
			writeJSONError(w, http.StatusBadRequest, "mode=window requires since and until (RFC3339)")
			return
		}
		metrics, err = s.DB.GetThroughputWindow(since, until, txPrefix)

	case mode == "peak":
		lookbackSec := 60
		if raw := r.URL.Query().Get("lookback"); raw != "" {
			var parsed int
			if _, parseErr := fmt.Sscanf(raw, "%d", &parsed); parseErr == nil && parsed > 0 {
				lookbackSec = parsed
			}
		}
		metrics, err = s.DB.GetThroughputPeak(lookbackSec, windowSec, txPrefix)

	case mode == "since" || r.URL.Query().Get("since") != "":
		mode = "since"
		since := time.Now().Add(-time.Duration(windowSec) * time.Second)
		if raw := r.URL.Query().Get("since"); raw != "" {
			if t, parseErr := time.Parse(time.RFC3339, raw); parseErr == nil {
				since = t
			} else if t, parseErr := time.Parse(time.RFC3339Nano, raw); parseErr == nil {
				since = t
			} else if ms, parseErr := strconv.ParseInt(raw, 10, 64); parseErr == nil && ms > 0 {
				if ms > 1_000_000_000_000 {
					since = time.UnixMilli(ms)
				} else {
					since = time.Unix(ms, 0)
				}
			}
		}
		metrics, err = s.DB.GetThroughputSince(since, txPrefix)

	default:
		mode = "latest"
		metrics, err = s.DB.GetThroughputLatest(windowSec, txPrefix)
	}

	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"status": "error",
			"error":  err.Error(),
		})
		return
	}

	resp := map[string]interface{}{
		"status":           "success",
		"mode":             mode,
		"tx_prefix":        txPrefix,
		"window_seconds":   metrics.WindowSeconds,
		"tx_committed":     metrics.TxCommitted,
		"blocks_committed": metrics.BlocksCommitted,
		"tx_per_sec":       metrics.TxPerSec,
		"blocks_per_sec":   metrics.BlocksPerSec,
	}
	if metrics.WindowStart != nil {
		resp["window_start"] = metrics.WindowStart.UTC().Format(time.RFC3339Nano)
	}
	if metrics.WindowEnd != nil {
		resp["window_end"] = metrics.WindowEnd.UTC().Format(time.RFC3339Nano)
	}
	if metrics.LookbackSeconds > 0 {
		resp["lookback_seconds"] = metrics.LookbackSeconds
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
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
