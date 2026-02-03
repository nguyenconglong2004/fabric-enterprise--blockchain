package raft

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"

	netpkg "raft-order-service/internal/network"
	"raft-order-service/internal/types"
)

// RaftNode represents a node in the Raft-based order service
type RaftNode struct {
	Transport *netpkg.Transport

	// State
	mu              sync.RWMutex
	state           types.NodeState
	currentTerm     int64
	currentLeaderID peer.ID

	// Membership
	Membership *types.MembershipView
	joinTime   time.Time

	// Leader detection
	lastHeartbeat time.Time

	// Chờ node có priority cao nhất gửi I AM NEW LEADER (dùng cho follower không phải highest)
	expectedLeaderID     peer.ID
	expectedLeaderDeadline time.Time

	// Order log
	OrderLog *types.OrderLog

	// Channels
	MessageChan      chan types.Message
	stopChan         chan struct{}
	LeaderClaimAckChan chan types.Message // acks khi node đang claim leader (MsgLeaderClaimAck)
	BlockAckChan     chan types.Message
}

// NewRaftNode creates a new Raft node
func NewRaftNode(ctx context.Context, port int) (*RaftNode, error) {
	transport, err := netpkg.NewTransport(ctx, port)
	if err != nil {
		return nil, err
	}

	node := &RaftNode{
		Transport:       transport,
		state:           types.Follower,
		currentTerm:     0,
		Membership:    types.NewMembershipView(),
		joinTime:      time.Now(),
		lastHeartbeat: time.Now(),
		OrderLog:         types.NewOrderLog(),
		MessageChan:      make(chan types.Message, 100),
		stopChan:         make(chan struct{}),
		LeaderClaimAckChan: make(chan types.Message, 100),
		BlockAckChan:     make(chan types.Message, 100),
	}

	// Add self to membership
	node.Membership.AddMember(transport.ID(), node.joinTime)

	// Set stream handler
	transport.SetStreamHandler(node.handleStream)

	log.Printf("[%s] Node created with ID: %s", transport.ID().ShortString(), transport.ID())

	return node, nil
}

// Start begins the node's operation
func (rn *RaftNode) Start() {
	log.Printf("[%s] Starting node", rn.Transport.ID().ShortString())

	// Start message processor
	go rn.processMessages()

	// Start heartbeat monitor
	go rn.monitorHeartbeat()

	// If this is the first node (only member), become leader
	if len(rn.Membership.GetAliveMembers()) == 1 {
		rn.becomeLeader()
	}
}

// handleStream handles incoming streams
func (rn *RaftNode) handleStream(s network.Stream) {
	defer s.Close()

	decoder := json.NewDecoder(s)
	var msg types.Message

	if err := decoder.Decode(&msg); err != nil {
		log.Printf("[%s] Error decoding message: %v", rn.Transport.ID().ShortString(), err)
		return
	}

	rn.MessageChan <- msg
}

// ConnectToPeer connects to another peer
func (rn *RaftNode) ConnectToPeer(peerAddr string) error {
	addr, err := rn.Transport.Connect(peerAddr)
	if err != nil {
		return err
	}

	log.Printf("[%s] Connected to peer: %s", rn.Transport.ID().ShortString(), addr.ID.ShortString())

	// Request to join the membership
	rn.requestMembershipJoin(addr.ID)

	return nil
}

// requestMembershipJoin requests to join the membership view
func (rn *RaftNode) requestMembershipJoin(bootstrapPeer peer.ID) {
	proposal := types.MembershipProposal{
		PeerID:   rn.Transport.ID().String(),
		JoinTime: rn.joinTime,
		Version:  rn.Membership.Version,
	}

	msg := types.Message{
		Type:      types.MsgMembershipUpdate,
		Term:      rn.currentTerm,
		SenderID:  rn.Transport.ID().String(),
		Data:      proposal,
		Timestamp: time.Now(),
	}

	if err := rn.Transport.SendMessage(bootstrapPeer, msg); err != nil {
		log.Printf("[%s] Error sending membership request: %v",
			rn.Transport.ID().ShortString(), err)
	}
}

// GetAddress returns the node's address
func (rn *RaftNode) GetAddress() string {
	return rn.Transport.GetAddress()
}

// GetState returns the current state of the node
func (rn *RaftNode) GetState() types.NodeState {
	rn.mu.RLock()
	defer rn.mu.RUnlock()
	return rn.state
}

// GetLeaderID returns the current leader's ID
func (rn *RaftNode) GetLeaderID() peer.ID {
	rn.mu.RLock()
	defer rn.mu.RUnlock()
	return rn.currentLeaderID
}

// GetCurrentTerm returns the current term
func (rn *RaftNode) GetCurrentTerm() int64 {
	rn.mu.RLock()
	defer rn.mu.RUnlock()
	return rn.currentTerm
}

// IsLeader returns true if this node is the leader
func (rn *RaftNode) IsLeader() bool {
	rn.mu.RLock()
	defer rn.mu.RUnlock()
	return rn.state == types.Leader
}

// ID returns the node's peer ID
func (rn *RaftNode) ID() peer.ID {
	return rn.Transport.ID()
}

// GetMembershipViewForClient returns membership view with addresses for client
func (rn *RaftNode) GetMembershipViewForClient() []peer.AddrInfo {
	members := rn.Membership.GetAliveMembers()
	nodes := make([]peer.AddrInfo, 0, len(members))

	for _, member := range members {
		var addrs []string
		if member.PeerID == rn.Transport.ID() {
			hostAddrs := rn.Transport.Addrs()
			addrs = make([]string, 0, len(hostAddrs))
			for _, addr := range hostAddrs {
				addrs = append(addrs, addr.String())
			}
		} else {
			peerAddrs := rn.Transport.Peerstore().Addrs(member.PeerID)
			addrs = make([]string, 0, len(peerAddrs))
			for _, addr := range peerAddrs {
				addrs = append(addrs, addr.String())
			}
		}

		if len(addrs) > 0 {
			addrInfo := peer.AddrInfo{
				ID:    member.PeerID,
				Addrs: make([]multiaddr.Multiaddr, 0, len(addrs)),
			}
			for _, addrStr := range addrs {
				if addr, err := multiaddr.NewMultiaddr(addrStr); err == nil {
					addrInfo.Addrs = append(addrInfo.Addrs, addr)
				}
			}
			if len(addrInfo.Addrs) > 0 {
				nodes = append(nodes, addrInfo)
			}
		}
	}

	return nodes
}

// SendMessage sends a message to a peer
func (rn *RaftNode) SendMessage(peerID peer.ID, msg types.Message) error {
	return rn.Transport.SendMessage(peerID, msg)
}

// BroadcastMessage broadcasts a message to all alive members
func (rn *RaftNode) BroadcastMessage(msg types.Message) {
	rn.Transport.BroadcastMessage(msg, rn.Membership.GetAliveMembers(), nil)
}

// BroadcastMessageWithFailureHandler giống BroadcastMessage nhưng gọi onSendFailure(peerID) khi gửi tới peer thất bại
func (rn *RaftNode) BroadcastMessageWithFailureHandler(msg types.Message, onSendFailure func(peer.ID)) {
	rn.Transport.BroadcastMessage(msg, rn.Membership.GetAliveMembers(), onSendFailure)
}

// Stop gracefully stops the node
func (rn *RaftNode) Stop() {
	log.Printf("[%s] Stopping node", rn.Transport.ID().ShortString())
	close(rn.stopChan)
	rn.Transport.Close()
}
