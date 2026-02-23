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

// OrderClient represents a client that can submit orders to the order service
type OrderClient struct {
	Transport              *netpkg.Transport
	MembershipResponseChan chan types.Message

	// AutoMode suppresses per-order print and counts responses silently
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

// handleStream handles incoming streams (for order responses and membership responses)
func (oc *OrderClient) handleStream(s network.Stream) {
	defer s.Close()

	decoder := json.NewDecoder(s)
	var msg types.Message

	if err := decoder.Decode(&msg); err != nil {
		log.Printf("Error decoding message: %v", err)
		return
	}

	switch msg.Type {
	case types.MsgOrderResponse:
		data, err := json.Marshal(msg.Data)
		if err != nil {
			return
		}

		var order types.Order
		if err := json.Unmarshal(data, &order); err != nil {
			return
		}

		if oc.AutoMode {
			atomic.AddInt64(&oc.AutoRecvCount, 1)
		} else {
			fmt.Printf("\n Order submitted successfully!\n")
			fmt.Printf("  Order ID: %s\n", order.ID)
			fmt.Printf("  Data: %s\n", order.Data)
			fmt.Printf("  Status: %s\n", order.Status)
			fmt.Printf("  Timestamp: %s\n\n", order.Timestamp.Format("2006-01-02 15:04:05"))
		}

	case types.MsgMembershipResponse:
		// Store membership response for later use
		oc.MembershipResponseChan <- msg
	}
}

// ConnectToNode connects to a node in the cluster
func (oc *OrderClient) ConnectToNode(nodeAddr string) error {
	_, err := oc.Transport.Connect(nodeAddr)
	return err
}

// GetClusterNodes requests and gets list of all nodes from membership view
func (oc *OrderClient) GetClusterNodes(nodeID peer.ID) ([]peer.AddrInfo, error) {
	// Request membership view from node
	requestMsg := types.Message{
		Type:      types.MsgMembershipRequest,
		Term:      0,
		SenderID:  oc.Transport.ID().String(),
		Data:      nil,
		Timestamp: time.Now(),
	}

	// Send request
	s, err := oc.Transport.Host.NewStream(oc.Transport.Ctx, nodeID, protocol.ID(netpkg.ProtocolID))
	if err != nil {
		return nil, fmt.Errorf("failed to create stream: %w", err)
	}

	encoder := json.NewEncoder(s)
	if err := encoder.Encode(requestMsg); err != nil {
		s.Close()
		return nil, fmt.Errorf("failed to send membership request: %w", err)
	}

	// Wait for response (with timeout)
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

		// Skip if not alive
		isAlive, _ := memberMap["is_alive"].(bool)
		if !isAlive {
			continue
		}

		// Extract addresses
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

// SubmitOrder submits an order to all nodes in the cluster
func (oc *OrderClient) SubmitOrder(orderData string, allNodes []peer.AddrInfo) (string, error) {
	orderID := fmt.Sprintf("order-%d", time.Now().UnixNano())

	order := types.Order{
		ID:        orderID,
		Data:      orderData,
		Timestamp: time.Now(),
		Status:    "pending",
	}

	msg := types.Message{
		Type:      types.MsgOrderRequest,
		Term:      0, // Client doesn't have term, node will handle
		SenderID:  oc.Transport.ID().String(),
		Data:      order,
		Timestamp: time.Now(),
	}

	// Send message to all nodes
	successCount := 0
	for _, nodeInfo := range allNodes {
		// Ensure we're connected
		if err := oc.Transport.ConnectToAddrInfo(nodeInfo); err != nil {
			log.Printf("Warning: Failed to connect to node %s: %v", nodeInfo.ID.ShortString(), err)
			continue
		}

		// Send message to node
		s, err := oc.Transport.Host.NewStream(oc.Transport.Ctx, nodeInfo.ID, protocol.ID(netpkg.ProtocolID))
		if err != nil {
			log.Printf("Warning: Failed to create stream to %s: %v", nodeInfo.ID.ShortString(), err)
			continue
		}

		encoder := json.NewEncoder(s)
		if err := encoder.Encode(msg); err != nil {
			s.Close()
			log.Printf("Warning: Failed to send order to %s: %v", nodeInfo.ID.ShortString(), err)
			continue
		}
		s.Close()

		successCount++
		log.Printf("Order sent to node %s", nodeInfo.ID.ShortString())
	}

	if successCount == 0 {
		return "", fmt.Errorf("failed to send order to any node")
	}

	log.Printf("Order request sent to %d/%d nodes: %s", successCount, len(allNodes), orderID)

	// Wait a bit for response
	time.Sleep(1 * time.Second)

	return orderID, nil
}

// SubmitOrderFast submits an order without waiting for response (for high-frequency use)
func (oc *OrderClient) SubmitOrderFast(orderData string, allNodes []peer.AddrInfo) (string, error) {
	orderID := fmt.Sprintf("order-%d", time.Now().UnixNano())

	order := types.Order{
		ID:        orderID,
		Data:      orderData,
		Timestamp: time.Now(),
		Status:    "pending",
	}

	msg := types.Message{
		Type:      types.MsgOrderRequest,
		Term:      0,
		SenderID:  oc.Transport.ID().String(),
		Data:      order,
		Timestamp: time.Now(),
	}

	successCount := 0
	for _, nodeInfo := range allNodes {
		if err := oc.Transport.ConnectToAddrInfo(nodeInfo); err != nil {
			continue
		}

		s, err := oc.Transport.Host.NewStream(oc.Transport.Ctx, nodeInfo.ID, protocol.ID(netpkg.ProtocolID))
		if err != nil {
			continue
		}

		encoder := json.NewEncoder(s)
		if err := encoder.Encode(msg); err != nil {
			s.Close()
			continue
		}
		s.Close()
		successCount++
	}

	if successCount == 0 {
		return "", fmt.Errorf("failed to send order to any node")
	}

	return orderID, nil
}

// Stop stops the client
func (oc *OrderClient) Stop() {
	oc.Transport.Close()
}
