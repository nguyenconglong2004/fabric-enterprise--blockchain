package types

import (
	"sync"
	"time"
)

// Order represents a client order
type Order struct {
	ID        string
	Data      string
	Timestamp time.Time
	Status    string
}

// OrderLog stores committed orders
type OrderLog struct {
	mu     sync.RWMutex
	Orders []Order
}

func NewOrderLog() *OrderLog {
	return &OrderLog{
		Orders: make([]Order, 0),
	}
}

func (ol *OrderLog) AppendOrder(order Order) {
	ol.mu.Lock()
	defer ol.mu.Unlock()
	ol.Orders = append(ol.Orders, order)
}

func (ol *OrderLog) GetOrders() []Order {
	ol.mu.RLock()
	defer ol.mu.RUnlock()
	return append([]Order{}, ol.Orders...)
}

// FindOrderByID finds an order by ID
func (ol *OrderLog) FindOrderByID(orderID string) *Order {
	ol.mu.RLock()
	defer ol.mu.RUnlock()

	for i := range ol.Orders {
		if ol.Orders[i].ID == orderID {
			return &ol.Orders[i]
		}
	}
	return nil
}

// GetPendingOrderIndices returns 1-based indices of up to maxCount pending orders
func (ol *OrderLog) GetPendingOrderIndices(maxCount int) []int {
	ol.mu.RLock()
	defer ol.mu.RUnlock()

	indices := make([]int, 0, maxCount)
	for i, order := range ol.Orders {
		if order.Status == "pending" {
			indices = append(indices, i+1) // 1-based
			if len(indices) >= maxCount {
				break
			}
		}
	}
	return indices
}

// CountPendingOrders returns the number of pending orders
func (ol *OrderLog) CountPendingOrders() int {
	ol.mu.RLock()
	defer ol.mu.RUnlock()

	count := 0
	for _, order := range ol.Orders {
		if order.Status == "pending" {
			count++
		}
	}
	return count
}

// UpdateOrderStatus updates the status of an order by ID
func (ol *OrderLog) UpdateOrderStatus(orderID string, newStatus string) bool {
	ol.mu.Lock()
	defer ol.mu.Unlock()

	for i := range ol.Orders {
		if ol.Orders[i].ID == orderID {
			ol.Orders[i].Status = newStatus
			return true
		}
	}
	return false
}
