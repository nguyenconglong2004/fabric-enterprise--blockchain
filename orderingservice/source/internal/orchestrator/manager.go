package orchestrator

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

	"raft-order-service/internal/raft"
)

// NodeManager creates and tracks all RaftNode instances in the cluster.
type NodeManager struct {
	mu         sync.RWMutex
	nodes      map[int]*ManagedNode
	peerToPort map[peer.ID]int
	bus        *EventBus
}

func NewNodeManager(bus *EventBus) *NodeManager {
	return &NodeManager{
		nodes:      make(map[int]*ManagedNode),
		peerToPort: make(map[peer.ID]int),
		bus:        bus,
	}
}

// NodeInfo is the JSON-serializable description of a managed node.
type NodeInfo struct {
	Port     int    `json:"port"`
	PeerID   string `json:"peerID"`
	Address  string `json:"address"`
	Priority int    `json:"priority"`
	State    string `json:"state"`
	Term     int64  `json:"term"`
	Alive    bool   `json:"alive"`
}

// PeerPort resolves a peer.ID to its port (0 if unknown).
func (m *NodeManager) PeerPort(id peer.ID) int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.peerToPort[id]
}

// CreateNetwork creates the first (leader-candidate) node.
func (m *NodeManager) CreateNetwork(port int, cfg *raft.Config) (*ManagedNode, error) {
	m.mu.Lock()
	if _, exists := m.nodes[port]; exists {
		m.mu.Unlock()
		return nil, fmt.Errorf("port %d already in use", port)
	}
	m.mu.Unlock()

	mn, err := m.spawnNode(port, cfg)
	if err != nil {
		return nil, err
	}

	m.bus.Publish(MakeEvent("node-added", buildNodeAddedPayload(mn)))
	return mn, nil
}

// AddNode creates a follower node and connects it to the current leader.
func (m *NodeManager) AddNode(port int, cfg *raft.Config) (*ManagedNode, error) {
	m.mu.Lock()
	if _, exists := m.nodes[port]; exists {
		m.mu.Unlock()
		return nil, fmt.Errorf("port %d already in use", port)
	}
	m.mu.Unlock()

	mn, err := m.spawnNode(port, cfg)
	if err != nil {
		return nil, err
	}

	// Connect to any alive node to join the cluster
	leaderAddr := m.findAnyNodeAddress()
	if leaderAddr != "" {
		time.Sleep(300 * time.Millisecond) // let libp2p host come up
		if err := mn.Raft.ConnectToPeer(leaderAddr); err != nil {
			mn.Logger.Printf("[orchestrator] failed to connect to %s: %v", leaderAddr, err)
		}
	}

	m.bus.Publish(MakeEvent("node-added", buildNodeAddedPayload(mn)))
	return mn, nil
}

// RemoveNode stops a node and removes it from the manager.
func (m *NodeManager) RemoveNode(port int) error {
	m.mu.Lock()
	mn, ok := m.nodes[port]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("node on port %d not found", port)
	}
	delete(m.nodes, port)
	if pid := mn.Raft.ID(); pid != "" {
		delete(m.peerToPort, pid)
	}
	m.mu.Unlock()

	mn.Cancel()
	mn.Raft.Stop()

	m.bus.Publish(MakeEvent("node-removed", map[string]interface{}{"port": port}))
	return nil
}

// GetNodes returns a snapshot of all managed nodes.
func (m *NodeManager) GetNodes() []NodeInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]NodeInfo, 0, len(m.nodes))
	for _, mn := range m.nodes {
		out = append(out, nodeInfo(mn))
	}
	return out
}

// GetNode returns the managed node for a port, or nil.
func (m *NodeManager) GetNode(port int) *ManagedNode {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.nodes[port]
}

// spawnNode creates a new RaftNode with a BusEmitter + NodeLogger and registers it.
func (m *NodeManager) spawnNode(port int, cfg *raft.Config) (*ManagedNode, error) {
	if cfg == nil {
		cfg = raft.DefaultConfig()
	}
	ctx, cancel := context.WithCancel(context.Background())
	logger := NewNodeLogger(port, m.bus)
	emitter := NewBusEmitter(port, m.bus, m.PeerPort)

	node, err := raft.NewRaftNode(ctx, port, cfg, emitter, logger)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("create node port %d: %w", port, err)
	}

	mn := &ManagedNode{
		Port:   port,
		Raft:   node,
		Cancel: cancel,
		Logger: logger,
	}

	m.mu.Lock()
	m.nodes[port] = mn
	m.peerToPort[node.ID()] = port
	m.mu.Unlock()

	node.Start()
	return mn, nil
}

// findAnyNodeAddress returns the multiaddr of the first alive node.
func (m *NodeManager) findAnyNodeAddress() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, mn := range m.nodes {
		addr := mn.Raft.GetAddress()
		if addr != "" {
			return addr
		}
	}
	return ""
}

func buildNodeAddedPayload(mn *ManagedNode) map[string]interface{} {
	return map[string]interface{}{
		"port":    mn.Port,
		"peerID":  mn.Raft.ID().String(),
		"address": mn.Raft.GetAddress(),
	}
}

func nodeInfo(mn *ManagedNode) NodeInfo {
	return NodeInfo{
		Port:    mn.Port,
		PeerID:  mn.Raft.ID().String(),
		Address: mn.Raft.GetAddress(),
		State:   mn.Raft.GetState().String(),
		Term:    mn.Raft.GetCurrentTerm(),
		Alive:   true,
	}
}
