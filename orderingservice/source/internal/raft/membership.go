package raft

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/peerstore"

	"raft-order-service/internal/types"
)

// handleMembershipUpdate handles membership update requests
func (rn *RaftNode) handleMembershipUpdate(msg types.Message) {
	rn.Logger.Printf("[%s] Received membership update from %s",
		rn.Transport.ID().ShortString(), msg.SenderID)

	// Full membership broadcast from leader (contains "members" key)
	if dataMap, ok := msg.Data.(map[string]interface{}); ok {
		if _, hasMembers := dataMap["members"]; hasMembers {
			rn.mu.RLock()
			curTerm := rn.currentTerm
			rn.mu.RUnlock()
			if msg.Term < curTerm {
				rn.Logger.Printf("[%s] Ignoring stale membership broadcast from %s (term %d < current %d)",
					rn.Transport.ID().ShortString(), msg.SenderID, msg.Term, curTerm)
				return
			}
			rn.Logger.Printf("[%s] Applying membership update from leader", rn.Transport.ID().ShortString())
			rn.updateMembershipFromData(msg.Data)
			rn.Emitter.MembershipChanged(rn.Transport.ID(), rn.Membership.Version)
			return
		}
	}

	// Join request — only leader handles it
	if !rn.IsLeader() {
		rn.Logger.Printf("[%s] Not leader, ignoring join request", rn.Transport.ID().ShortString())
		return
	}

	data, err := json.Marshal(msg.Data)
	if err != nil {
		rn.Logger.Printf("[%s] Error marshaling membership data: %v", rn.Transport.ID().ShortString(), err)
		return
	}

	var proposal types.MembershipProposal
	if err := json.Unmarshal(data, &proposal); err != nil {
		rn.Logger.Printf("[%s] Error unmarshaling membership proposal: %v",
			rn.Transport.ID().ShortString(), err)
		return
	}

	peerID, err := peer.Decode(proposal.PeerID)
	if err != nil {
		rn.Logger.Printf("[%s] Error decoding peer ID: %v", rn.Transport.ID().ShortString(), err)
		return
	}

	rn.Membership.AddMember(peerID, proposal.JoinTime)
	rn.Membership.MarkAlive(peerID)
	rn.Logger.Printf("[%s] Added/restored member %s with priority %d",
		rn.Transport.ID().ShortString(),
		peerID.ShortString(),
		rn.Membership.Members[peerID].Priority)

	rn.broadcastMembershipView()
	rn.Emitter.MembershipChanged(rn.Transport.ID(), rn.Membership.Version)

	ackMsg := types.Message{
		Type:      types.MsgMembershipAck,
		Term:      rn.currentTerm,
		SenderID:  rn.Transport.ID().String(),
		Data:      rn.serializeMembershipView(),
		Timestamp: time.Now(),
	}

	if err := rn.Transport.SendMessage(peerID, ackMsg); err != nil {
		rn.Logger.Printf("[%s] Error sending membership ack: %v", rn.Transport.ID().ShortString(), err)
	}
}

// handleMembershipAck handles membership acknowledgment
func (rn *RaftNode) handleMembershipAck(msg types.Message) {
	rn.Logger.Printf("[%s] Received membership ack from %s",
		rn.Transport.ID().ShortString(), msg.SenderID)

	rn.updateMembershipFromData(msg.Data)

	leaderID, err := peer.Decode(msg.SenderID)
	if err != nil {
		rn.Logger.Printf("[%s] Error decoding leader ID: %v", rn.Transport.ID().ShortString(), err)
		return
	}

	rn.mu.Lock()
	wasLeader := rn.state == types.Leader
	oldState := rn.state
	rn.currentLeaderID = leaderID
	rn.currentTerm = msg.Term
	rn.lastHeartbeat = time.Now()

	if wasLeader && leaderID != rn.Transport.ID() {
		highestPriority := rn.Membership.GetHighestPriorityAliveNode()
		if highestPriority != nil && highestPriority.PeerID == leaderID {
			rn.Logger.Printf("[%s] Stepping down, discovered higher priority leader: %s",
				rn.Transport.ID().ShortString(), leaderID.ShortString())
			rn.state = types.Follower
		}
	} else if rn.state != types.Leader {
		rn.state = types.Follower
	}
	newState := rn.state
	rn.mu.Unlock()

	if oldState != newState {
		rn.Emitter.StateChanged(rn.Transport.ID(), oldState, newState)
	}

	rn.Logger.Printf("[%s] Updated membership view, current leader: %s",
		rn.Transport.ID().ShortString(), leaderID.ShortString())

	rn.Emitter.MembershipChanged(rn.Transport.ID(), rn.Membership.Version)

	if rn.OrderingBlock.GetLastIndex() == 0 && len(rn.Membership.GetAliveMembers()) >= 2 {
		go rn.StartSync("first-join")
	}
}

// broadcastMembershipView broadcasts the current membership view to all members
func (rn *RaftNode) broadcastMembershipView() {
	msg := types.Message{
		Type:      types.MsgMembershipUpdate,
		Term:      rn.currentTerm,
		SenderID:  rn.Transport.ID().String(),
		Data:      rn.serializeMembershipView(),
		Timestamp: time.Now(),
	}

	rn.Transport.BroadcastMessage(msg, rn.Membership.GetAliveMembers(), nil)
}

// serializeMembershipView serializes the membership view
func (rn *RaftNode) serializeMembershipView() map[string]interface{} {
	rn.Membership.Mu.RLock()
	defer rn.Membership.Mu.RUnlock()

	members := make([]map[string]interface{}, 0)
	for _, member := range rn.Membership.Members {
		var addrStrings []string
		if member.PeerID == rn.Transport.ID() {
			addrs := rn.Transport.Addrs()
			addrStrings = make([]string, 0, len(addrs))
			for _, addr := range addrs {
				addrStrings = append(addrStrings, addr.String())
			}
		} else {
			addrs := rn.Transport.Peerstore().Addrs(member.PeerID)
			addrStrings = make([]string, 0, len(addrs))
			for _, addr := range addrs {
				addrStrings = append(addrStrings, addr.String())
			}
		}

		members = append(members, map[string]interface{}{
			"peer_id":   member.PeerID.String(),
			"join_time": member.JoinTime.Format(time.RFC3339Nano),
			"priority":  member.Priority,
			"is_alive":  member.IsAlive,
			"addresses": addrStrings,
		})
	}

	return map[string]interface{}{
		"members": members,
		"version": rn.Membership.Version,
	}
}

// handleMembershipRequest handles request for membership view from client
func (rn *RaftNode) handleMembershipRequest(msg types.Message) {
	rn.Logger.Printf("[%s] Received membership request from %s", rn.Transport.ID().ShortString(), msg.SenderID)

	data := rn.serializeMembershipView()
	if lid := rn.GetLeaderID(); lid != "" {
		data["leader_id"] = lid.String()
	} else {
		data["leader_id"] = ""
	}
	responseMsg := types.Message{
		Type:      types.MsgMembershipResponse,
		Term:      rn.currentTerm,
		SenderID:  rn.Transport.ID().String(),
		Data:      data,
		Timestamp: time.Now(),
	}

	senderID, err := peer.Decode(msg.SenderID)
	if err == nil {
		rn.Transport.SendMessage(senderID, responseMsg)
	}
}

// updateMembershipFromData updates membership from received data
func (rn *RaftNode) updateMembershipFromData(data interface{}) {
	dataMap, ok := data.(map[string]interface{})
	if !ok {
		rn.Logger.Printf("[%s] Invalid membership data format", rn.Transport.ID().ShortString())
		return
	}

	membersData, ok := dataMap["members"].([]interface{})
	if !ok {
		rn.Logger.Printf("[%s] Invalid members data format", rn.Transport.ID().ShortString())
		return
	}

	rn.Logger.Printf("[%s] Updating membership with %d members", rn.Transport.ID().ShortString(), len(membersData))

	peersToConnect := make([]peer.AddrInfo, 0)

	for _, memberData := range membersData {
		memberMap, ok := memberData.(map[string]interface{})
		if !ok {
			rn.Logger.Printf("[%s] Skipping invalid member data", rn.Transport.ID().ShortString())
			continue
		}

		peerIDStr, ok := memberMap["peer_id"].(string)
		if !ok {
			rn.Logger.Printf("[%s] Missing peer_id in member data", rn.Transport.ID().ShortString())
			continue
		}

		peerID, err := peer.Decode(peerIDStr)
		if err != nil {
			rn.Logger.Printf("[%s] Error decoding peer ID %s: %v", rn.Transport.ID().ShortString(), peerIDStr, err)
			continue
		}

		joinTimeStr, ok := memberMap["join_time"].(string)
		if !ok {
			rn.Logger.Printf("[%s] Missing join_time for peer %s", rn.Transport.ID().ShortString(), peerID.ShortString())
			continue
		}

		joinTime, err := time.Parse(time.RFC3339Nano, joinTimeStr)
		if err != nil {
			rn.Logger.Printf("[%s] Error parsing join_time for peer %s: %v", rn.Transport.ID().ShortString(), peerID.ShortString(), err)
			continue
		}

		priority, _ := memberMap["priority"].(float64)
		isAlive, _ := memberMap["is_alive"].(bool)

		var addrs []string
		if addressesData, ok := memberMap["addresses"].([]interface{}); ok {
			addrs = make([]string, 0, len(addressesData))
			for _, addr := range addressesData {
				if addrStr, ok := addr.(string); ok {
					addrs = append(addrs, addrStr)
				}
			}
		}

		if peerID != rn.Transport.ID() && len(addrs) > 0 {
			addrInfos := make([]peer.AddrInfo, 0)
			for _, addrStr := range addrs {
				addr, err := peer.AddrInfoFromString(fmt.Sprintf("%s/p2p/%s", addrStr, peerIDStr))
				if err == nil {
					rn.Transport.Peerstore().AddAddrs(peerID, addr.Addrs, peerstore.PermanentAddrTTL)
					addrInfos = append(addrInfos, *addr)
				}
			}
			if len(addrInfos) > 0 {
				peersToConnect = append(peersToConnect, addrInfos[0])
			}
		}

		rn.Membership.Mu.Lock()
		rn.Membership.Members[peerID] = &types.MemberInfo{
			PeerID:   peerID,
			JoinTime: joinTime,
			Priority: int(priority),
			IsAlive:  isAlive,
		}
		rn.Membership.Mu.Unlock()

		rn.Logger.Printf("[%s] Updated member: %s (priority: %d, alive: %v, addresses: %d)",
			rn.Transport.ID().ShortString(), peerID.ShortString(), int(priority), isAlive, len(addrs))
	}

	if version, ok := dataMap["version"].(float64); ok {
		rn.Membership.Version = int64(version)
	}

	rn.Logger.Printf("[%s] Membership update complete, total members: %d",
		rn.Transport.ID().ShortString(), len(rn.Membership.Members))

	go func() {
		for _, addrInfo := range peersToConnect {
			if err := rn.Transport.ConnectToAddrInfo(addrInfo); err != nil {
				rn.Logger.Printf("[%s] Failed to connect to %s: %v (will retry later)",
					rn.Transport.ID().ShortString(), addrInfo.ID.ShortString(), err)
			} else {
				rn.Logger.Printf("[%s] Connected to peer %s for membership sync",
					rn.Transport.ID().ShortString(), addrInfo.ID.ShortString())
			}
		}
	}()
}
