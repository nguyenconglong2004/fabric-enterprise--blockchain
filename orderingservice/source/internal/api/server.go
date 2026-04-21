package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"raft-order-service/internal/raft"
)

type APIServer struct {
	node *raft.RaftNode
}

func NewAPIServer(node *raft.RaftNode) *APIServer {
	return &APIServer{node: node}
}

func (s *APIServer) Start(port int) error {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/membership", s.handleGetMembership)

	addr := fmt.Sprintf(":%d", port)
	log.Printf("[API] Starting HTTP server on port %d\n", port)

	go func() {
		if err := http.ListenAndServe(addr, mux); err != nil && err != http.ErrServerClosed {
			log.Printf("[API] Server error: %v\n", err)
		}
	}()

	return nil
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
