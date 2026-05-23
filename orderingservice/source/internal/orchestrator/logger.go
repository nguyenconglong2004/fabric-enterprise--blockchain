package orchestrator

import (
	"bytes"
	"log"
	"time"
)

// NewNodeLogger returns a *log.Logger whose output is forwarded as "log" events on the bus.
func NewNodeLogger(port int, bus *EventBus) *log.Logger {
	return log.New(&nodeLogWriter{port: port, bus: bus}, "", log.LstdFlags)
}

type nodeLogWriter struct {
	port int
	bus  *EventBus
}

func (w *nodeLogWriter) Write(p []byte) (n int, err error) {
	line := string(bytes.TrimRight(p, "\n\r"))
	w.bus.Publish(MakeEvent("log", map[string]interface{}{
		"port": w.port,
		"line": line,
		"ts":   time.Now().UnixMilli(),
	}))
	return len(p), nil
}
