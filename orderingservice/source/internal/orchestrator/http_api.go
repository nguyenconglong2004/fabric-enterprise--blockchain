package orchestrator

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"raft-order-service/internal/raft"
)

// RegisterRoutes mounts all REST and WebSocket routes on mux.
func RegisterRoutes(mux *http.ServeMux, mgr *NodeManager, bus *EventBus) {
	mux.HandleFunc("/api/network", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		handleCreateNetwork(w, r, mgr)
	})

	mux.HandleFunc("/api/nodes", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			handleListNodes(w, r, mgr)
		case http.MethodPost:
			handleAddNode(w, r, mgr)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	// /api/nodes/:port  and  /api/nodes/:port/cmd  and  /api/nodes/:port/config
	mux.HandleFunc("/api/nodes/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/nodes/")
		parts := strings.SplitN(path, "/", 2)
		port, err := strconv.Atoi(parts[0])
		if err != nil {
			http.Error(w, "invalid port", http.StatusBadRequest)
			return
		}

		if len(parts) == 1 {
			// /api/nodes/:port
			if r.Method == http.MethodDelete {
				handleRemoveNode(w, r, mgr, port)
			} else {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			}
			return
		}

		switch parts[1] {
		case "cmd":
			if r.Method == http.MethodPost {
				handleExecCmd(w, r, mgr, bus, port)
			} else {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			}
		case "config":
			if r.Method == http.MethodPatch {
				handleUpdateConfig(w, r, mgr, port)
			} else {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			}
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	})

	mux.HandleFunc("/ws/events", func(w http.ResponseWriter, r *http.Request) {
		ServeWS(bus, w, r)
	})
}

type createNetworkReq struct {
	Port   int              `json:"port"`
	Config *raft.ConfigJSON `json:"config,omitempty"`
}

func handleCreateNetwork(w http.ResponseWriter, r *http.Request, mgr *NodeManager) {
	var req createNetworkReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Port <= 0 {
		req.Port = 6000
	}

	cfg := raft.DefaultConfig()
	if req.Config != nil {
		req.Config.ApplyTo(cfg)
	}

	mn, err := mgr.CreateNetwork(req.Port, cfg)
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}

	writeJSON(w, http.StatusCreated, nodeInfo(mn))
}

type addNodeReq struct {
	Port   int              `json:"port"`
	Config *raft.ConfigJSON `json:"config,omitempty"`
}

func handleAddNode(w http.ResponseWriter, r *http.Request, mgr *NodeManager) {
	var req addNodeReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Port <= 0 {
		http.Error(w, "port required", http.StatusBadRequest)
		return
	}

	cfg := raft.DefaultConfig()
	if req.Config != nil {
		req.Config.ApplyTo(cfg)
	}

	mn, err := mgr.AddNode(req.Port, cfg)
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}

	writeJSON(w, http.StatusCreated, nodeInfo(mn))
}

func handleListNodes(w http.ResponseWriter, r *http.Request, mgr *NodeManager) {
	writeJSON(w, http.StatusOK, mgr.GetNodes())
}

func handleRemoveNode(w http.ResponseWriter, r *http.Request, mgr *NodeManager, port int) {
	if err := mgr.RemoveNode(port); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type cmdReq struct {
	Cmd string `json:"cmd"`
}

func handleExecCmd(w http.ResponseWriter, r *http.Request, mgr *NodeManager, bus *EventBus, port int) {
	var req cmdReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	mn := mgr.GetNode(port)
	if mn == nil {
		http.Error(w, "node not found", http.StatusNotFound)
		return
	}
	output := ExecCommand(mn, req.Cmd)

	// Also broadcast as cmd-output event
	bus.Publish(MakeEvent("cmd-output", map[string]interface{}{
		"port":   port,
		"output": output,
		"ts":     time.Now().UnixMilli(),
	}))

	writeJSON(w, http.StatusOK, map[string]string{"output": output})
}

func handleUpdateConfig(w http.ResponseWriter, r *http.Request, mgr *NodeManager, port int) {
	mn := mgr.GetNode(port)
	if mn == nil {
		http.Error(w, "node not found", http.StatusNotFound)
		return
	}

	var patch raft.ConfigJSON
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	patch.ApplyTo(mn.Raft.Config)

	writeJSON(w, http.StatusOK, mn.Raft.Config.Snapshot())
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
