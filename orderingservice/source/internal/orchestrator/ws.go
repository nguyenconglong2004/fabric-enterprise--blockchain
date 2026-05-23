package orchestrator

import (
	"log"
	"net/http"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// ServeWS upgrades the HTTP connection to WebSocket and streams events from the bus.
func ServeWS(bus *EventBus, w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("ws upgrade error: %v", err)
		return
	}
	defer conn.Close()

	id, ch := bus.Subscribe()
	defer bus.Unsubscribe(id)

	// Drain client messages (pings/close) in a goroutine so the writer doesn't block.
	go func() {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	for ev := range ch {
		if err := conn.WriteJSON(ev); err != nil {
			return
		}
	}
}
