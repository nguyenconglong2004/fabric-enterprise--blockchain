package client

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync/atomic"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/multiformats/go-multiaddr"

	netpkg "raft-order-service/internal/network"
	"raft-order-service/internal/types"
)

// OrderClient represents a client that can submit transactions to the ordering service
type OrderClient struct {
	Transport              *netpkg.Transport
	MembershipResponseChan chan types.Message

	// AutoMode suppresses per-tx print and counts responses silently
	AutoMode      bool
	AutoRecvCount int64 // accessed atomically
}

// NewOrderClient creates a new order client
func NewOrderClient(ctx context.Context) (*OrderClient, error) {
	transport, err := netpkg.NewClientTransport(ctx)
	if err != nil {
		return nil, err
	}

	client := &OrderClient{
		Transport:              transport,
		MembershipResponseChan: make(chan types.Message, 10),
	}

	// Set stream handler for responses
	transport.SetStreamHandler(client.handleStream)

	log.Printf("Client created with ID: %s", transport.ID().ShortString())

	return client, nil
}

// handleStream handles incoming streams (for tx responses and membership responses)
func (oc *OrderClient) handleStream(s network.Stream) {
	defer s.Close()

	decoder := json.NewDecoder(s)
	var msg types.Message

	if err := decoder.Decode(&msg); err != nil {
		log.Printf("Error decoding message: %v", err)
		return
	}

	switch msg.Type {
	case types.MsgTxResponse:
		data, err := json.Marshal(msg.Data)
		if err != nil {
			return
		}

		var tx types.Transaction
		if err := json.Unmarshal(data, &tx); err != nil {
			return
		}

		if oc.AutoMode {
			atomic.AddInt64(&oc.AutoRecvCount, 1)
		} else {
			fmt.Printf("\n Transaction submitted successfully!\n")
			fmt.Printf("  TX ID:     %s\n", tx.ID)
			fmt.Printf("  Data:      %s\n", tx.Data)
			fmt.Printf("  Timestamp: %s\n\n", tx.Timestamp.Format("2006-01-02 15:04:05"))
		}

	case types.MsgMembershipResponse:
		oc.MembershipResponseChan <- msg
	}
}

// ConnectToNode connects to a node in the cluster
func (oc *OrderClient) ConnectToNode(nodeAddr string) error {
	_, err := oc.Transport.Connect(nodeAddr)
	return err
}

// GetClusterNodes requests and gets the list of all nodes from membership view
func (oc *OrderClient) GetClusterNodes(nodeID peer.ID) ([]peer.AddrInfo, error) {
	requestMsg := types.Message{
		Type:      types.MsgMembershipRequest,
		Term:      0,
		SenderID:  oc.Transport.ID().String(),
		Data:      nil,
		Timestamp: time.Now(),
	}

	s, err := oc.Transport.Host.NewStream(oc.Transport.Ctx, nodeID, protocol.ID(netpkg.ProtocolID))
	if err != nil {
		return nil, fmt.Errorf("failed to create stream: %w", err)
	}

	encoder := json.NewEncoder(s)
	if err := encoder.Encode(requestMsg); err != nil {
		s.Close()
		return nil, fmt.Errorf("failed to send membership request: %w", err)
	}

	select {
	case responseMsg := <-oc.MembershipResponseChan:
		s.Close()
		return oc.parseMembershipResponse(responseMsg)
	case <-time.After(5 * time.Second):
		s.Close()
		return nil, fmt.Errorf("timeout waiting for membership response")
	}
}

// parseMembershipResponse parses membership response and returns list of nodes
func (oc *OrderClient) parseMembershipResponse(msg types.Message) ([]peer.AddrInfo, error) {
	dataMap, ok := msg.Data.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid membership response format")
	}

	membersData, ok := dataMap["members"].([]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid members data format")
	}

	nodes := make([]peer.AddrInfo, 0)
	for _, memberData := range membersData {
		memberMap, ok := memberData.(map[string]interface{})
		if !ok {
			continue
		}

		peerIDStr, ok := memberMap["peer_id"].(string)
		if !ok {
			continue
		}

		peerID, err := peer.Decode(peerIDStr)
		if err != nil {
			continue
		}

		isAlive, _ := memberMap["is_alive"].(bool)
		if !isAlive {
			continue
		}

		var addrs []multiaddr.Multiaddr
		if addressesData, ok := memberMap["addresses"].([]interface{}); ok {
			for _, addr := range addressesData {
				if addrStr, ok := addr.(string); ok {
					if ma, err := multiaddr.NewMultiaddr(addrStr); err == nil {
						addrs = append(addrs, ma)
					}
				}
			}
		}

		if len(addrs) > 0 {
			nodes = append(nodes, peer.AddrInfo{
				ID:    peerID,
				Addrs: addrs,
			})
		}
	}

	return nodes, nil
}

// SubmitTransaction sends a transaction to a single node (preferably the leader).
// The node will forward to the leader if it is not the leader itself.
func (oc *OrderClient) SubmitTransaction(txData string, node peer.AddrInfo) (string, error) {
	txID := fmt.Sprintf("tx-%d", time.Now().UnixNano())

	tx := types.Transaction{
		ID:        txID,
		Data:      txData,
		Timestamp: time.Now(),
	}

	msg := types.Message{
		Type:      types.MsgTxRequest,
		Term:      0,
		SenderID:  oc.Transport.ID().String(),
		Data:      tx,
		Timestamp: time.Now(),
	}

	if err := oc.Transport.ConnectToAddrInfo(node); err != nil {
		return "", fmt.Errorf("failed to connect to node %s: %w", node.ID.ShortString(), err)
	}

	s, err := oc.Transport.Host.NewStream(oc.Transport.Ctx, node.ID, protocol.ID(netpkg.ProtocolID))
	if err != nil {
		return "", fmt.Errorf("failed to create stream to %s: %w", node.ID.ShortString(), err)
	}

	encoder := json.NewEncoder(s)
	if err := encoder.Encode(msg); err != nil {
		s.Close()
		return "", fmt.Errorf("failed to send tx to %s: %w", node.ID.ShortString(), err)
	}
	s.Close()

	log.Printf("Tx %s sent to node %s", txID, node.ID.ShortString())

	// Wait briefly for ack
	time.Sleep(500 * time.Millisecond)

	return txID, nil
}

// SubmitTransactionFast sends a transaction without waiting for a response (for high-frequency use)
func (oc *OrderClient) SubmitTransactionFast(txData string, node peer.AddrInfo) (string, error) {
	txID := fmt.Sprintf("tx-%d", time.Now().UnixNano())

	tx := types.Transaction{
		ID:        txID,
		Data:      txData,
		Timestamp: time.Now(),
	}

	msg := types.Message{
		Type:      types.MsgTxRequest,
		Term:      0,
		SenderID:  oc.Transport.ID().String(),
		Data:      tx,
		Timestamp: time.Now(),
	}

	if err := oc.Transport.ConnectToAddrInfo(node); err != nil {
		return "", fmt.Errorf("failed to connect to node: %w", err)
	}

	s, err := oc.Transport.Host.NewStream(oc.Transport.Ctx, node.ID, protocol.ID(netpkg.ProtocolID))
	if err != nil {
		return "", fmt.Errorf("failed to create stream: %w", err)
	}

	encoder := json.NewEncoder(s)
	if err := encoder.Encode(msg); err != nil {
		s.Close()
		return "", fmt.Errorf("failed to send tx: %w", err)
	}
	s.Close()

	return txID, nil
}

// Stop stops the client
func (oc *OrderClient) Stop() {
	oc.Transport.Close()
}
