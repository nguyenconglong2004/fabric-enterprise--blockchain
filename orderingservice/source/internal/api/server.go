package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"raft-order-service/internal/raft"
	"raft-order-service/internal/types"
)

// APIServer handles HTTP requests for the Ordering Service
type APIServer struct {
	node *raft.RaftNode
}

// NewAPIServer creates a new API server
func NewAPIServer(node *raft.RaftNode) *APIServer {
	return &APIServer{node: node}
}

// Start starts the HTTP API server on the given port
func (s *APIServer) Start(port int) error {
	mux := http.NewServeMux()

	// Public endpoints
	mux.HandleFunc("/api/leader", s.handleGetLeader)
	mux.HandleFunc("/api/membership", s.handleGetMembership)
	mux.HandleFunc("/api/submit-tx", s.handleSubmitTransaction)
	mux.HandleFunc("/api/endorsement", s.handleEndorsement)

	addr := fmt.Sprintf(":%d", port)
	log.Printf("[API] Starting HTTP server on port %d\n", port)

	go func() {
		if err := http.ListenAndServe(addr, mux); err != nil && err != http.ErrServerClosed {
			log.Printf("[API] Server error: %v\n", err)
		}
	}()

	return nil
}

// handleGetLeader returns the current leader's address
// GET /api/leader
// Response: {"leader_id": "12D3Koo...", "addresses": ["/ip4/127.0.0.1/tcp/6000/..."]}
func (s *APIServer) handleGetLeader(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Only GET supported", http.StatusMethodNotAllowed)
		return
	}

	leaderID := s.node.GetLeaderID()
	if leaderID == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "no leader available",
		})
		return
	}

	// Get leader's addresses from peerstore
	addrs := s.node.Transport.Peerstore().Addrs(leaderID)
	addrStrs := make([]string, len(addrs))
	for i, addr := range addrs {
		addrStrs[i] = addr.String()
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"leader_id": leaderID.String(),
		"addresses": addrStrs,
	})
}

// handleGetMembership returns all cluster members and their addresses
// GET /api/membership
// Response: {"leader_id": "...", "members": [{"id": "...", "addresses": [...], "alive": true}]}
func (s *APIServer) handleGetMembership(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Only GET supported", http.StatusMethodNotAllowed)
		return
	}

	leaderID := s.node.GetLeaderID()
	members := s.node.Membership.GetAliveMembers()

	type MemberInfo struct {
		ID        string   `json:"id"`
		Addresses []string `json:"addresses"`
		Alive     bool     `json:"alive"`
	}

	memberInfos := make([]MemberInfo, len(members))
	for i, member := range members {
		addrs := s.node.Transport.Peerstore().Addrs(member.PeerID)
		addrStrs := make([]string, len(addrs))
		for j, addr := range addrs {
			addrStrs[j] = addr.String()
		}

		memberInfos[i] = MemberInfo{
			ID:        member.PeerID.String(),
			Addresses: addrStrs,
			Alive:     true,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"leader_id": leaderID.String(),
		"members":   memberInfos,
	})
}

// handleSubmitTransaction accepts a transaction with endorsements
// POST /api/submit-tx
// Body: {"txid": "...", "payload": "...", "contract_name": "...", "endorsements": [...]}
func (s *APIServer) handleSubmitTransaction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Only POST supported", http.StatusMethodNotAllowed)
		return
	}

	var tx types.Transaction
	if err := json.NewDecoder(r.Body).Decode(&tx); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("invalid JSON: %v", err),
		})
		return
	}

	// Validate transaction
	if err := tx.Validate(); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("validation failed: %v", err),
		})
		return
	}

	// If not leader, forward to leader
	if !s.node.IsLeader() {
		leaderID := s.node.GetLeaderID()
		if leaderID == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]string{
				"error": "no leader available, please retry with leader address",
				"hint":  "GET /api/leader to find current leader",
			})
			return
		}

		// Forward to leader via message
		msg := types.Message{
			Type:      types.MsgTxRequest,
			Term:      s.node.GetCurrentTerm(),
			SenderID:  s.node.Transport.ID().String(),
			Data:      tx,
			Timestamp: time.Now(),
		}

		if err := s.node.SendMessage(leaderID, msg); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadGateway)
			json.NewEncoder(w).Encode(map[string]string{
				"error": fmt.Sprintf("failed to forward to leader: %v", err),
			})
			return
		}

		log.Printf("[API] Forwarded tx %s to leader %s\n", tx.Txid, leaderID.ShortString())

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{
			"status": "forwarded to leader",
			"tx_id":  tx.Txid,
		})
		return
	}

	// Leader: add to TxPool
	if _, err := s.node.SubmitTransaction(tx); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("failed to process transaction: %v", err),
		})
		return
	}

	log.Printf("[API] Accepted tx %s\n", tx.Txid)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status": "success",
		"tx_id":  tx.Txid,
		"note":   "transaction added to TxPool, will be included in next block",
	})
}

// handleEndorsement receives endorsements from Core Service
// POST /api/endorsement
// Body: transaction with signature
func (s *APIServer) handleEndorsement(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Only POST supported", http.StatusMethodNotAllowed)
		return
	}

	log.Printf("[API] 📥 Received endorsement request\n")

	var tx types.Transaction
	if err := json.NewDecoder(r.Body).Decode(&tx); err != nil {
		log.Printf("[API] ❌ JSON decode error: %v\n", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("invalid JSON: %v", err),
		})
		return
	}

	log.Printf("[API] ✅ Decoded endorsement for tx %s (field: Txid='%s')\n", tx.Txid, tx.Txid)

	// If not leader, forward to leader
	if !s.node.IsLeader() {
		leaderID := s.node.GetLeaderID()
		log.Printf("[API] 📤 Not leader, forwarding to leader %s\n", leaderID.ShortString())

		if leaderID != "" {
			// Forward via libp2p
			if err := s.node.Transport.SendEndorsement(leaderID, tx); err != nil {
				log.Printf("[API] ❌ Forward failed: %v\n", err)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadGateway)
				json.NewEncoder(w).Encode(map[string]string{
					"error": fmt.Sprintf("failed to forward to leader: %v", err),
				})
				return
			}
			log.Printf("[API] ✅ Forwarded to leader\n")
		}
	} else {
		// Leader: add to TxPool
		log.Printf("[API] 👑 I am leader, adding to TxPool\n")

		if _, err := s.node.SubmitTransaction(tx); err != nil {
			log.Printf("[API] ❌ Error submitting endorsement: %v\n", err)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{
				"error": fmt.Sprintf("failed to submit: %v", err),
			})
			return
		}
		log.Printf("[API] ✅ Endorsement tx %s added to TxPool\n", tx.Txid)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status": "success",
		"tx_id":  tx.Txid,
	})
}

// processTx is a wrapper around RaftNode.processTx to make it public (needed for API)
// This is already defined in raft/transaction.go but we need to access from API
