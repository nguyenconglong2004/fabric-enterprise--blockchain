package deliver

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
)

// Order protocol IDs and message types (must match orderingservice).
const (
	orderProtocolMain          = "/raft-order-service/1.0.0"
	orderMsgMembershipRequest  = 6
	orderMsgMembershipResponse = 7
)

// MembershipView is the ordering cluster view returned to discovery clients.
type MembershipView struct {
	LeaderID string
	Members  []MemberInfo
}

// MemberInfo is one orderer in the cluster.
type MemberInfo struct {
	ID        string
	Addresses []string
	Alive     bool
	Priority  int
}

// FetchMembership connects to bootstrap, sends MsgMembershipRequest, waits for response.
func (c *Client) FetchMembership(ctx context.Context, bootstrap string) (*MembershipView, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	addrInfo, err := peer.AddrInfoFromString(bootstrap)
	if err != nil {
		return nil, fmt.Errorf("deliver: parse bootstrap: %w", err)
	}
	if err := c.host.Connect(ctx, *addrInfo); err != nil {
		return nil, fmt.Errorf("deliver: connect bootstrap: %w", err)
	}

	select {
	case <-c.membershipCh:
	default:
	}

	s, err := c.host.NewStream(ctx, addrInfo.ID, protocol.ID(orderProtocolMain))
	if err != nil {
		return nil, fmt.Errorf("deliver: open membership stream: %w", err)
	}
	defer s.Close()

	req := map[string]interface{}{
		"Type":      orderMsgMembershipRequest,
		"Term":      int64(0),
		"SenderID":  c.host.ID().String(),
		"Data":      nil,
		"Timestamp": time.Now(),
	}
	if err := json.NewEncoder(s).Encode(req); err != nil {
		return nil, fmt.Errorf("deliver: send membership request: %w", err)
	}

	select {
	case mv := <-c.membershipCh:
		return mv, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("deliver: membership request timeout: %w", ctx.Err())
	}
}

func parseMembershipData(data map[string]interface{}) (*MembershipView, error) {
	if data == nil {
		return nil, fmt.Errorf("empty membership data")
	}
	mv := &MembershipView{}
	if lid, ok := data["leader_id"].(string); ok {
		mv.LeaderID = lid
	}
	membersRaw, ok := data["members"].([]interface{})
	if !ok {
		return mv, nil
	}
	for _, m := range membersRaw {
		mm, ok := m.(map[string]interface{})
		if !ok {
			continue
		}
		pid, _ := mm["peer_id"].(string)
		alive, _ := mm["is_alive"].(bool)
		priority := 0
		if p, ok := mm["priority"].(float64); ok {
			priority = int(p)
		}
		var addrs []string
		if arr, ok := mm["addresses"].([]interface{}); ok {
			for _, a := range arr {
				if s, ok := a.(string); ok {
					addrs = append(addrs, s)
				}
			}
		}
		mv.Members = append(mv.Members, MemberInfo{
			ID:        pid,
			Addresses: addrs,
			Alive:     alive,
			Priority:  priority,
		})
	}
	return mv, nil
}
