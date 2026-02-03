package types

import (
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
)

// MemberInfo stores information about a member node
type MemberInfo struct {
	PeerID   peer.ID
	JoinTime time.Time
	Priority int // Lower number = higher priority (earlier join)
	IsAlive  bool
}

// MembershipView maintains the list of all nodes with their priorities
type MembershipView struct {
	Mu      sync.RWMutex
	Members map[peer.ID]*MemberInfo
	Version int64 // Version for consensus
}

func NewMembershipView() *MembershipView {
	return &MembershipView{
		Members: make(map[peer.ID]*MemberInfo),
		Version: 0,
	}
}

func (mv *MembershipView) AddMember(peerID peer.ID, joinTime time.Time) {
	mv.Mu.Lock()
	defer mv.Mu.Unlock()

	if _, exists := mv.Members[peerID]; !exists {
		mv.Members[peerID] = &MemberInfo{
			PeerID:   peerID,
			JoinTime: joinTime,
			Priority: len(mv.Members), // Priority based on join order
			IsAlive:  true,
		}
		mv.Version++
	}
}

func (mv *MembershipView) MarkDead(peerID peer.ID) {
	mv.Mu.Lock()
	defer mv.Mu.Unlock()

	if member, exists := mv.Members[peerID]; exists {
		member.IsAlive = false
		mv.Version++
	}
}

func (mv *MembershipView) GetHighestPriorityAliveNode() *MemberInfo {
	mv.Mu.RLock()
	defer mv.Mu.RUnlock()

	var highest *MemberInfo
	for _, member := range mv.Members {
		if !member.IsAlive {
			continue
		}
		if highest == nil || member.Priority < highest.Priority {
			highest = member
		}
	}
	return highest
}

func (mv *MembershipView) GetAliveMembers() []*MemberInfo {
	mv.Mu.RLock()
	defer mv.Mu.RUnlock()

	var alive []*MemberInfo
	for _, member := range mv.Members {
		if member.IsAlive {
			alive = append(alive, member)
		}
	}
	return alive
}
