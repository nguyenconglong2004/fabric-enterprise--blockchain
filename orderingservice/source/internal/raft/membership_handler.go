package raft

import (
	"encoding/json"
	"log"

	"github.com/libp2p/go-libp2p/core/network"
)

// HandleMembershipStream handles membership queries from Core Service via P2P
// Protocol: /raft-order-service/membership/1.0.0
func (rn *RaftNode) HandleMembershipStream(s network.Stream) {
	defer s.Close()

	leaderID := rn.GetLeaderID()
	members := rn.Membership.GetAliveMembers()

	type MemberInfo struct {
		ID        string   `json:"id"`
		Addresses []string `json:"addresses"`
		Alive     bool     `json:"alive"`
	}

	memberInfos := make([]MemberInfo, len(members))
	for i, member := range members {
		addrs := rn.Transport.Peerstore().Addrs(member.PeerID)
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

	membership := map[string]interface{}{
		"leader_id": leaderID.String(),
		"members":   memberInfos,
	}

	encoder := json.NewEncoder(s)
	if err := encoder.Encode(membership); err != nil {
		log.Printf("[%s] membership: failed to encode: %v",
			rn.Transport.ID().ShortString(), err)
		return
	}

	log.Printf("[%s] membership: sent to %s",
		rn.Transport.ID().ShortString(), s.Conn().RemotePeer().ShortString())
}
