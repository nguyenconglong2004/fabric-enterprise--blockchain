package raft

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/peerstore"
	"github.com/multiformats/go-multiaddr"

	netpkg "raft-order-service/internal/network"
	"raft-order-service/internal/types"
)

const (
	AutoProposeBlockSize = 1000                    // max tx per auto-proposed block
	AutoProposeInterval  = 100 * time.Millisecond   // tick interval for auto-propose loop
)

// RaftNode represents a node in the Raft-based order service
type RaftNode struct {
	Transport *netpkg.Transport

	// Runtime-tunable config
	Config *Config

	// Event emitter for UI integration
	Emitter EventEmitter

	// Per-node logger (allows separate log output per node when embedded in orchestrator)
	Logger *log.Logger

	// State
	mu              sync.RWMutex
	state           types.NodeState
	currentTerm     int64
	currentLeaderID peer.ID

	// Membership
	Membership *types.MembershipView
	joinTime   time.Time

	// Leader detection
	lastHeartbeat     time.Time
	lastBlockSentTime time.Time // last time leader broadcast a block message (proposal or commit)

	// Chờ node có priority cao nhất gửi I AM NEW LEADER (dùng cho follower không phải highest)
	expectedLeaderID       peer.ID
	expectedLeaderDeadline time.Time

	// Transaction pool (leader only): pending transactions from clients
	TxPool   []types.Transaction
	TxPoolMu sync.Mutex

	// lastCommittedHash is the Hash of the most recently committed block,
	// used as PrevHash when creating new blocks to maintain the hash chain.
	lastCommittedHash []byte

	// Raft log: uncommitted log entries
	RaftLog *types.RaftLog

	// Ordering block: committed blocks
	OrderingBlock *types.OrderingBlock

	// Channels
	MessageChan        chan types.Message
	stopChan           chan struct{}
	LeaderClaimAckChan chan types.Message // acks khi node đang claim leader (MsgLeaderClaimAck)
	BlockAckChan       chan types.Message

	// Auto-propose block
	autoProposeMu        sync.Mutex
	autoProposeRunning   bool
	autoProposeStop      chan struct{}
	blockCommittedNotify chan struct{} // buffered(1): signals each time a block is committed

	// Deliver service: fan-out committed blocks to committing peers
	DeliverMgr *DeliverManager

	// Heartbeat delay simulation (testing only).
	delayMu              sync.Mutex
	delayedPriorities    map[int]bool
	heartbeatPausedUntil time.Time

	// Sync coordinator state (block/log catch-up khi first-join hoặc rejoin).
	syncMu         sync.Mutex
	syncing        bool
	SyncStatusChan chan types.Message // buffered: chứa MsgSyncStatusResponse trong cửa sổ discovery

	// JoinCluster: chứa MsgMembershipResponse khi đang query leader qua bootstrap peer.
	MembershipResponseChan chan types.Message
}

// NewRaftNode creates a new Raft node.
// config and emitter may be nil; defaults are used in that case.
// logger may be nil; log.Default() is used in that case.
func NewRaftNode(ctx context.Context, port int, config *Config, emitter EventEmitter, logger *log.Logger) (*RaftNode, error) {
	transport, err := netpkg.NewTransport(ctx, port)
	if err != nil {
		return nil, err
	}

	if config == nil {
		config = DefaultConfig()
	}
	if emitter == nil {
		emitter = NoopEmitter{}
	}
	if logger == nil {
		logger = log.New(os.Stderr, "", log.LstdFlags)
	}

	node := &RaftNode{
		Transport:            transport,
		Config:               config,
		Emitter:              emitter,
		Logger:               logger,
		state:                types.Follower,
		currentTerm:          0,
		Membership:           types.NewMembershipView(),
		joinTime:             time.Now(),
		lastHeartbeat:        time.Now(),
		TxPool:               make([]types.Transaction, 0),
		RaftLog:              types.NewRaftLog(),
		OrderingBlock:        types.NewOrderingBlock(),
		MessageChan:          make(chan types.Message, 100),
		stopChan:             make(chan struct{}),
		LeaderClaimAckChan:   make(chan types.Message, 100),
		BlockAckChan:         make(chan types.Message, 100),
		blockCommittedNotify: make(chan struct{}, 1),
		DeliverMgr:                NewDeliverManager(),
		delayedPriorities:         make(map[int]bool),
		SyncStatusChan:            make(chan types.Message, 100),
		MembershipResponseChan:    make(chan types.Message, 8),
	}

	// Add self to membership
	node.Membership.AddMember(transport.ID(), node.joinTime)

	// Set stream handler
	transport.SetStreamHandler(node.handleStream)

	node.Logger.Printf("[%s] Node created with ID: %s", transport.ID().ShortString(), transport.ID())

	return node, nil
}

// Start begins the node's operation
func (rn *RaftNode) Start() {
	rn.Logger.Printf("[%s] Starting node", rn.Transport.ID().ShortString())

	// Register deliver stream handler
	rn.Transport.SetDeliverStreamHandler(rn.HandleDeliverStream)

	// Register endorsement stream handler
	rn.Transport.SetEndorsementStreamHandler(rn.HandleEndorsementStream)

	// Register inter-node sync stream handler (block/log catch-up)
	rn.Transport.SetSyncStreamHandler(rn.HandleSyncStream)

	// Start message processor
	go rn.processMessages()

	// Start heartbeat monitor
	go rn.monitorHeartbeat()
}

// handleStream handles incoming streams
func (rn *RaftNode) handleStream(s network.Stream) {
	defer s.Close()

	decoder := json.NewDecoder(s)
	var msg types.Message

	if err := decoder.Decode(&msg); err != nil {
		rn.Logger.Printf("[%s] Error decoding message: %v", rn.Transport.ID().ShortString(), err)
		return
	}

	// Bypass MessageChan for consensus ACKs to avoid HOL blocking during tx floods.
	if msg.Type == types.MsgBlockProposalAck {
		select {
		case rn.BlockAckChan <- msg:
		default:
			rn.Logger.Printf("[%s] Block ACK channel full, dropping ACK", rn.Transport.ID().ShortString())
		}
		return
	}

	// Bypass MessageChan for tx ingest. Each stream already runs in its own
	// goroutine, so handling the tx inline (TxPool append is guarded by TxPoolMu)
	// keeps the shared MessageChan from filling up during high-TPS floods, which
	// would otherwise starve consensus ACK/commit/heartbeat processing.
	if msg.Type == types.MsgTxRequest {
		rn.HandleTxRequest(msg)
		return
	}

	rn.MessageChan <- msg
}

// BootstrapAsLeader makes this node the leader of a brand-new single-node cluster.
// Call this only when no other cluster exists (i.e., this is the very first node).
func (rn *RaftNode) BootstrapAsLeader() {
	rn.becomeLeader()
}

// JoinCluster connects to a bootstrap peer, discovers the current leader via
// MsgMembershipRequest/Response, then sends a join request directly to the leader.
// It retries up to 5 times with 2s backoff if the cluster has no leader yet (e.g.
// during election). Returns an error if the cluster remains leaderless after retries.
func (rn *RaftNode) JoinCluster(bootstrapAddr string) error {
	bootstrapInfo, err := rn.Transport.Connect(bootstrapAddr)
	if err != nil {
		return fmt.Errorf("connect to bootstrap peer: %w", err)
	}
	rn.Logger.Printf("[%s] Connected to bootstrap peer: %s",
		rn.Transport.ID().ShortString(), bootstrapInfo.ID.ShortString())

	const maxRetries = 5
	const retryInterval = 2 * time.Second
	const responseTimeout = 1500 * time.Millisecond

	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			rn.Logger.Printf("[%s] JoinCluster: no leader in response, retry %d/%d",
				rn.Transport.ID().ShortString(), attempt, maxRetries-1)
			time.Sleep(retryInterval)
		}

		// Drain stale responses from previous attempt.
	drain:
		for {
			select {
			case <-rn.MembershipResponseChan:
			default:
				break drain
			}
		}

		queryMsg := types.Message{
			Type:      types.MsgMembershipRequest,
			Term:      rn.GetCurrentTerm(),
			SenderID:  rn.Transport.ID().String(),
			Timestamp: time.Now(),
		}
		if err := rn.Transport.SendMessage(bootstrapInfo.ID, queryMsg); err != nil {
			rn.Logger.Printf("[%s] JoinCluster: failed to send membership query: %v",
				rn.Transport.ID().ShortString(), err)
			continue
		}

		var resp types.Message
		select {
		case resp = <-rn.MembershipResponseChan:
		case <-time.After(responseTimeout):
			rn.Logger.Printf("[%s] JoinCluster: timed out waiting for membership response",
				rn.Transport.ID().ShortString())
			continue
		}

		dataMap, ok := resp.Data.(map[string]interface{})
		if !ok {
			continue
		}
		leaderIDStr, _ := dataMap["leader_id"].(string)
		if leaderIDStr == "" {
			// Cluster has no leader yet (e.g., during election).
			continue
		}

		leaderID, err := peer.Decode(leaderIDStr)
		if err != nil {
			rn.Logger.Printf("[%s] JoinCluster: invalid leader_id %q: %v",
				rn.Transport.ID().ShortString(), leaderIDStr, err)
			continue
		}

		// If leader is a different peer, load its addresses into peerstore first.
		if leaderID != bootstrapInfo.ID {
			if members, ok := dataMap["members"].([]interface{}); ok {
				rn.loadLeaderAddrs(leaderID, leaderIDStr, members)
			}
		}

		rn.Logger.Printf("[%s] JoinCluster: sending join request to leader %s",
			rn.Transport.ID().ShortString(), leaderID.ShortString())
		rn.requestMembershipJoin(leaderID)
		return nil
	}

	return fmt.Errorf("no leader available after %d retries", maxRetries)
}

// loadLeaderAddrs adds the leader's multiaddresses into the local peerstore so that
// subsequent SendMessage calls can reach it without an explicit Connect.
func (rn *RaftNode) loadLeaderAddrs(leaderID peer.ID, leaderIDStr string, members []interface{}) {
	for _, m := range members {
		memberMap, ok := m.(map[string]interface{})
		if !ok {
			continue
		}
		if pid, _ := memberMap["peer_id"].(string); pid != leaderIDStr {
			continue
		}
		addrsRaw, _ := memberMap["addresses"].([]interface{})
		for _, addrRaw := range addrsRaw {
			addrStr, ok := addrRaw.(string)
			if !ok {
				continue
			}
			// Addresses in the snapshot may or may not already include the /p2p/ component.
			addrInfo, err := peer.AddrInfoFromString(fmt.Sprintf("%s/p2p/%s", addrStr, leaderIDStr))
			if err != nil {
				continue
			}
			rn.Transport.Peerstore().AddAddrs(leaderID, addrInfo.Addrs, peerstore.PermanentAddrTTL)
		}
		break
	}
}

// ConnectToPeer connects to another peer and sends a raw join request.
// Prefer JoinCluster for initial cluster join; use this for runtime peer connections.
func (rn *RaftNode) ConnectToPeer(peerAddr string) error {
	addr, err := rn.Transport.Connect(peerAddr)
	if err != nil {
		return err
	}

	rn.Logger.Printf("[%s] Connected to peer: %s", rn.Transport.ID().ShortString(), addr.ID.ShortString())

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
		rn.Logger.Printf("[%s] Error sending membership request: %v",
			rn.Transport.ID().ShortString(), err)
	}
}

// setState atomically changes the node state and emits a StateChanged event.
// Do NOT call while holding rn.mu.
func (rn *RaftNode) setState(newState types.NodeState) {
	rn.mu.Lock()
	old := rn.state
	rn.state = newState
	rn.mu.Unlock()
	if old != newState {
		rn.Emitter.StateChanged(rn.Transport.ID(), old, newState)
	}
}

// SetLogOutput redirects the node's logger to the given writer.
// Useful for the CLI server to redirect logs to readline.Stdout() after readline is initialized.
func (rn *RaftNode) SetLogOutput(w io.Writer) {
	rn.Logger.SetOutput(w)
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

// GetLeaderAddress returns the leader's address information
func (rn *RaftNode) GetLeaderAddress() (peer.AddrInfo, error) {
	leaderID := rn.GetLeaderID()
	if leaderID == "" {
		return peer.AddrInfo{}, fmt.Errorf("no leader known")
	}

	members := rn.Membership.GetAliveMembers()
	for _, member := range members {
		if member.PeerID == leaderID {
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
					return addrInfo, nil
				}
			}
		}
	}

	return peer.AddrInfo{}, fmt.Errorf("leader address not found")
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

// GetFollowerAddresses returns list of follower addresses (all except leader)
func (rn *RaftNode) GetFollowerAddresses() []peer.AddrInfo {
	members := rn.Membership.GetAliveMembers()
	leaderID := rn.GetLeaderID()
	nodes := make([]peer.AddrInfo, 0)

	for _, member := range members {
		// Skip leader
		if member.PeerID == leaderID {
			continue
		}

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

// BroadcastToAllMembers broadcasts a message to ALL known members (alive + dead).
func (rn *RaftNode) BroadcastToAllMembers(msg types.Message) {
	rn.Transport.BroadcastMessage(msg, rn.Membership.GetAllMembers(), nil)
}

// Stop gracefully stops the node
func (rn *RaftNode) Stop() {
	rn.Logger.Printf("[%s] Stopping node", rn.Transport.ID().ShortString())
	close(rn.stopChan)
	rn.Transport.Close()
}
