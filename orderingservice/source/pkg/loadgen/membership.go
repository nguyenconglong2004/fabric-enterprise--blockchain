package loadgen

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/multiformats/go-multiaddr"

	netpkg "raft-order-service/internal/network"
	"raft-order-service/internal/types"
)

// LeaderTarget is the libp2p dial target for the Raft leader.
type LeaderTarget struct {
	ID      peer.ID
	Addrs   []multiaddr.Multiaddr
	Leader  peer.ID
	AddrStr string // full multiaddr /ip4/.../p2p/...
}

// ResolveLeader connects to any orderer bootstrap and returns the current leader dial info.
func ResolveLeader(ctx context.Context, transport *netpkg.Transport, bootstrapMultiaddr string) (*LeaderTarget, error) {
	bootstrapMultiaddr = trim(bootstrapMultiaddr)
	if bootstrapMultiaddr == "" {
		return nil, fmt.Errorf("empty orderer multiaddr")
	}

	addrInfo, err := peer.AddrInfoFromString(bootstrapMultiaddr)
	if err != nil {
		return nil, fmt.Errorf("parse bootstrap multiaddr: %w", err)
	}
	if err := transport.Host.Connect(ctx, *addrInfo); err != nil {
		return nil, fmt.Errorf("connect bootstrap orderer: %w", err)
	}

	respCh := make(chan types.Message, 1)
	transport.Host.SetStreamHandler(protocol.ID(netpkg.ProtocolID), func(s network.Stream) {
		defer s.Close()
		var msg types.Message
		if err := json.NewDecoder(s).Decode(&msg); err != nil {
			return
		}
		if msg.Type != types.MsgMembershipResponse {
			return
		}
		select {
		case respCh <- msg:
		default:
		}
	})

	req := types.Message{
		Type:      types.MsgMembershipRequest,
		Term:      0,
		SenderID:  transport.ID().String(),
		Data:      nil,
		Timestamp: time.Now(),
	}
	s, err := transport.Host.NewStream(ctx, addrInfo.ID, protocol.ID(netpkg.ProtocolID))
	if err != nil {
		return nil, fmt.Errorf("open membership stream: %w", err)
	}
	if err := json.NewEncoder(s).Encode(req); err != nil {
		s.Close()
		return nil, fmt.Errorf("send membership request: %w", err)
	}
	s.Close()

	var resp types.Message
	select {
	case resp = <-respCh:
	case <-time.After(8 * time.Second):
		return nil, fmt.Errorf("timeout waiting for membership response")
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	dataMap, ok := resp.Data.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid membership response data")
	}

	leaderIDStr, _ := dataMap["leader_id"].(string)
	if leaderIDStr == "" {
		// Single-node or unknown leader — dial bootstrap directly.
		return addrInfoToTarget(addrInfo), nil
	}

	leaderID, err := peer.Decode(leaderIDStr)
	if err != nil {
		return nil, fmt.Errorf("decode leader_id: %w", err)
	}

	membersRaw, _ := dataMap["members"].([]interface{})
	for _, m := range membersRaw {
		mm, ok := m.(map[string]interface{})
		if !ok {
			continue
		}
		pidStr, _ := mm["peer_id"].(string)
		if pidStr != leaderIDStr {
			continue
		}
		alive, _ := mm["is_alive"].(bool)
		if !alive {
			break
		}
		var mas []multiaddr.Multiaddr
		if arr, ok := mm["addresses"].([]interface{}); ok {
			for _, a := range arr {
				if s, ok := a.(string); ok {
					if ma, err := multiaddr.NewMultiaddr(s); err == nil {
						mas = append(mas, ma)
					}
				}
			}
		}
		if len(mas) > 0 {
			return &LeaderTarget{
				ID:      leaderID,
				Addrs:   mas,
				Leader:  leaderID,
				AddrStr: fmt.Sprintf("%s/p2p/%s", mas[0], leaderID),
			}, nil
		}
	}

	// Leader known but no addresses in view — try bootstrap if it is the leader.
	if leaderID == addrInfo.ID {
		return addrInfoToTarget(addrInfo), nil
	}
	return nil, fmt.Errorf("leader %s has no dial addresses in membership view", leaderID.ShortString())
}

func addrInfoToTarget(ai *peer.AddrInfo) *LeaderTarget {
	addrStr := ""
	if len(ai.Addrs) > 0 {
		addrStr = fmt.Sprintf("%s/p2p/%s", ai.Addrs[0], ai.ID)
	}
	return &LeaderTarget{
		ID:      ai.ID,
		Addrs:   ai.Addrs,
		Leader:  ai.ID,
		AddrStr: addrStr,
	}
}

func trim(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t') {
		s = s[:len(s)-1]
	}
	return s
}
