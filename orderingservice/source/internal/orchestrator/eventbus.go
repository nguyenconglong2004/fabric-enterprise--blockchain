package orchestrator

import (
	"encoding/json"
	"sync"
)

// Event is a JSON-serializable event pushed to WebSocket clients.
type Event struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data,omitempty"`
}

// EventBus is a simple pub/sub bus for broadcasting events to WS clients.
type EventBus struct {
	mu      sync.RWMutex
	clients map[int]chan Event
	nextID  int
}

func NewEventBus() *EventBus {
	return &EventBus{clients: make(map[int]chan Event)}
}

func (b *EventBus) Subscribe() (int, <-chan Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	id := b.nextID
	b.nextID++
	ch := make(chan Event, 256)
	b.clients[id] = ch
	return id, ch
}

func (b *EventBus) Unsubscribe(id int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if ch, ok := b.clients[id]; ok {
		close(ch)
		delete(b.clients, id)
	}
}

func (b *EventBus) Publish(ev Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, ch := range b.clients {
		select {
		case ch <- ev:
		default:
		}
	}
}

// MakeEvent marshals data into an Event. Marshal errors produce empty data.
func MakeEvent(typ string, data interface{}) Event {
	raw, _ := json.Marshal(data)
	return Event{Type: typ, Data: raw}
}
