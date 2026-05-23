# Web UI & Orchestrator

Giao diện web trực quan hóa cluster Raft, cho phép tạo/quản lý node, theo dõi heartbeat, và chỉnh cấu hình runtime — tất cả trong một binary Go duy nhất.

## Kiến trúc tổng quan

```
Browser (React + Vite)
  │  HTTP REST  +  WebSocket (/ws/events)
  ▼
orchestrator (single Go binary, port 8080)
  ├── Static files  ← web/dist/ (embedded via embed.FS)
  ├── REST API      ← internal/orchestrator/http_api.go
  ├── WebSocket     ← internal/orchestrator/ws.go
  └── NodeManager   ← internal/orchestrator/manager.go
       └── map[port]*ManagedNode
             ├── *raft.RaftNode  (real libp2p host, binds TCP port)
             ├── *log.Logger     → WS event "log"
             └── *BusEmitter     → WS events (heartbeat, state, term…)
```

Mỗi `RaftNode` là một goroutine-set trong cùng process — không spawn child process. Mỗi node bind TCP port thật (6000, 6001, ...) và giao tiếp qua loopback giống như chạy trên máy khác.

## Build

```sh
# Bước 1 — build React frontend
cd source/web
npm install
npm run build          # output → source/web/dist/

# Bước 2 — build orchestrator (embed dist/ vào binary)
cd ..
go build -o orchestrator.exe ./cmd/orchestrator

# Hoặc build luôn hai bước (PowerShell)
cd source/web; npm run build; cd ..; go build -o orchestrator.exe ./cmd/orchestrator
```

> **Lưu ý:** Phải `npm run build` trước `go build` vì `//go:embed all:dist` trong `web/embed.go` cần thư mục `web/dist/` tồn tại.

## Chạy

### Production (một binary, self-contained)

```sh
./orchestrator.exe
# Mở trình duyệt: http://localhost:8080
```

Flags:
| Flag | Default | Mô tả |
|---|---|---|
| `--addr` | `:8080` | Địa chỉ HTTP server |
| `--dev` | false | Dev mode (proxy static → Vite) |
| `--static-proxy` | `http://localhost:5173` | URL Vite dev server (chỉ dùng khi `--dev`) |

### Dev mode (Hot Module Replacement)

```sh
# Terminal 1 — Go backend
cd source
go run ./cmd/orchestrator --dev

# Terminal 2 — Vite dev server
cd source/web
npm run dev
```

Trình duyệt mở `http://localhost:5173` (Vite). Vite proxy `/api` và `/ws` về `:8080`.

## REST API

| Method | Path | Body | Mô tả |
|---|---|---|---|
| `POST` | `/api/network` | `{"port":6000}` | Tạo cluster với 1 leader node |
| `GET` | `/api/nodes` | — | Danh sách nodes hiện tại |
| `POST` | `/api/nodes` | `{"port":6001}` | Thêm follower node, tự join leader |
| `DELETE` | `/api/nodes/:port` | — | Dừng node (cancel context) |
| `POST` | `/api/nodes/:port/cmd` | `{"cmd":"status"}` | Thực thi CLI command trên node |
| `PATCH` | `/api/nodes/:port/config` | xem bên dưới | Chỉnh config runtime |

### PATCH /api/nodes/:port/config

Body (tất cả optional — chỉ field có giá trị mới được apply):

```json
{
  "heartbeat_interval_ms": 2000,
  "heartbeat_timeout_ms": 5000,
  "detection_timeout_ms": 3000,
  "auto_propose_interval_ms": 500,
  "auto_propose_block_size": 20,
  "sync_discovery_window_ms": 2000,
  "sync_fetch_timeout_ms": 30000,
  "sync_shard_size": 64
}
```

Response: snapshot config sau khi apply (JSON).

### NodeInfo (response của GET /api/nodes)

```json
[
  {
    "port": 6000,
    "peerID": "12D3KooW...",
    "address": "/ip4/127.0.0.1/tcp/6000/p2p/12D3KooW...",
    "priority": 0,
    "state": "Leader",
    "term": 3,
    "alive": true
  }
]
```

## WebSocket Events

Kết nối: `ws://localhost:8080/ws/events`

Mỗi message là JSON `{"type":"...", "data":{...}}`:

| type | data fields | Ý nghĩa |
|---|---|---|
| `node-added` | port, peerID, priority, address | Node mới được tạo/join |
| `node-removed` | port | Node bị stop |
| `state-changed` | port, from, to | Đổi trạng thái Raft |
| `term-changed` | port, term | Tăng term |
| `heartbeat-sent` | fromPort, toPort, ts | Leader gửi HB đến follower |
| `heartbeat-received` | port, fromPort, ts | Node nhận HB |
| `leader-claim` | port, term | Node bắt đầu claim leader |
| `claim-ack` | fromPort, toPort, accept | Phản hồi claim |
| `became-leader` | port, term | Node trở thành leader |
| `block-committed` | port, blockIndex, hash, txCount | Block được commit |
| `membership-changed` | members[] | Membership snapshot |
| `tx-pool` | port, size | Số tx trong pool của leader |
| `log` | port, line, ts | Dòng log realtime từ node |
| `cmd-output` | port, output | Kết quả CLI command |

## Internal Packages

### `internal/orchestrator/manager.go`

`NodeManager` quản lý vòng đời node:
- `CreateNetwork(port, cfg)` — tạo node đầu tiên (leader), lưu `peerToPort` map
- `AddNode(port, cfg)` — tạo node, gọi `ConnectToPeer(leaderAddr)` để join
- `RemoveNode(port)` — cancel context → tất cả goroutine của node dừng
- `PeerPort(peer.ID)` — map ngược `peer.ID → port` dùng trong BusEmitter

### `internal/orchestrator/managed_node.go`

`ManagedNode` wrapper quanh `*raft.RaftNode`:
- Giữ `Cancel` function để stop node
- `BusEmitter` implement `raft.EventEmitter` — mỗi emit gọi `bus.Publish()` non-blocking

### `internal/orchestrator/eventbus.go`

Pub/sub event bus:
- `Subscribe()` → trả `(id, <-chan Event)`
- `Publish(ev)` — non-blocking: nếu channel của subscriber đầy (buffer=64), event bị drop để không block consensus
- `Unsubscribe(id)` — đóng và xóa subscriber channel

### `internal/orchestrator/logger.go`

`NewNodeLogger(port, bus)` — tạo `*log.Logger` với writer emit WS event `log` cho mỗi dòng.

### `internal/orchestrator/cli_router.go`

`ExecCommand(mn, input)` — route CLI command đến method của `RaftNode`:
- `status` → `PrintStatus(buffer)`
- `connect <addr>` → `ConnectToPeer` + `RequestMembershipJoin`
- `delay <secs> <p1> [p2...]` → `SetHeartbeatDelay`
- `help` → static text

### `internal/orchestrator/ws.go`

Mỗi WS client có goroutine riêng nhận từ `EventBus` và ghi JSON ra WebSocket.

## Frontend Architecture

```
source/web/src/
├── api/
│   ├── rest.ts     # fetch wrappers → /api/...
│   └── ws.ts       # useWebSocket() hook, auto-reconnect với backoff
├── store/
│   └── cluster.ts  # zustand store: nodes, beams, selectedPort, logs
└── components/
    ├── NetworkTopology.tsx  # SVG ring layout + edges + heartbeat beams
    ├── NodeCircle.tsx       # circle + state color + countdown ring
    ├── HeartbeatBeam.tsx    # framer-motion animated pulse Leader→Follower
    ├── NodeTerminal.tsx     # xterm.js embed + log streaming + command input
    ├── ConfigPanel.tsx      # sliders → PATCH config
    ├── Sidebar.tsx          # panel cho selected node (terminal/config tabs)
    └── CreateNetworkModal.tsx  # modal tạo network và add node
```

### State Store (zustand)

```ts
interface ClusterStore {
  nodes: Record<number, NodeState>   // keyed by port
  beams: HbBeam[]                    // heartbeat animations in flight
  selectedPort: number | null
  globalTerm: number
  logs: Record<number, string[]>     // log lines per port
  cmdOutputs: Record<number, string>
  connected: boolean                 // WS connection status
}
```

`handleEvent(event)` — dispatcher xử lý tất cả WS event types, cập nhật store tương ứng.

### Heartbeat Animation

1. Event `heartbeat-sent {fromPort, toPort}` → thêm `HbBeam` vào store
2. `HeartbeatBeam.tsx` render `motion.circle` di chuyển từ `(fromX,fromY)` → `(toX,toY)` trong 500ms
3. Khi animation xong → xóa beam khỏi store
4. Event `heartbeat-received {port}` → cập nhật `node.lastHbAt = now` → countdown ring reset

### Countdown Ring (follower)

SVG `<circle>` stroke-dasharray theo `fraction = elapsed / hbTimeoutMs`. Gradient xanh→vàng→đỏ. Cập nhật mỗi 500ms bằng `useAnimationTick`.

### Zoom & Pan (NetworkTopology)

`NetworkTopology` bọc toàn bộ content SVG trong một `<g transform="translate(panX panY) scale(zoom)">`. Tương tác:

- **Mouse wheel** trên vùng topology → zoom in/out toward cursor (clamp `[0.4, 3]`, factor `1.1`). Pan được điều chỉnh để giữ điểm dưới chuột cố định.
- **Drag background** (chuột trái trên SVG, không trúng node) → pan. Khi đang drag, cursor là `grabbing`; mặc định là `grab`.
- **Overlay controls** góc dưới-phải: `−` / `<percent>` / `+`. Click % giữa để reset (`zoom=1, pan=0`).

State (`zoom`, `pan`) là local component state — không persist qua reload.

### Resizable & Collapsible Sidebar

`App.tsx` quản lý `sidebarWidth` (default 320) và `sidebarCollapsed`. Giữa `<main>` và `<Sidebar>` có một thanh dọc 5px với `cursor: col-resize`:

- **Drag** thanh đó → thay đổi `sidebarWidth`, clamp `[220px, viewport - 300px]`. Trong khi drag, `document.body.style.cursor = 'col-resize'` + `userSelect: 'none'`.
- **Chevron ◀** trong sidebar header → collapse về strip 40px chỉ có nút ▶ để expand.
- Width hiện tại giữ lại khi expand lại; state không persist qua reload.

## Config Runtime

`Config` struct trong `internal/raft/config.go` có `sync.RWMutex`. Tất cả getter (`GetHeartbeatInterval()` v.v.) đọc qua RLock. Setter qua Lock.

Các goroutine (monitorHeartbeat, autoProposeLoop) sử dụng `time.After(cfg.Get...)` mỗi vòng lặp thay vì `time.NewTicker` cố định, nên config change có hiệu lực ngay vòng tiếp theo.

## Tương thích với CLI Server cũ

`cmd/server/main.go` vẫn build và chạy được:

```sh
cd source
go build -o server.exe ./cmd/server
./server.exe
```

CLI server dùng `raft.DefaultConfig()` + `raft.NoopEmitter{}` + redirect log output vào readline stdout. Hoàn toàn độc lập với orchestrator — có thể join cùng cluster qua libp2p.

## Troubleshooting

**`go build` lỗi `pattern all:dist: no matching files`**
→ Chưa build frontend. Chạy `cd source/web && npm run build` trước.

**WS events không đến trình duyệt**
→ Kiểm tra `connected` badge trên header. Nếu `disconnected`, frontend đang reconnect. Xem browser console.

**Node tạo ra nhưng không thấy trên UI**
→ Kiểm tra WS event `node-added` trong Network tab. Có thể WS disconnect giữa chừng.

**`stop node` không giải phóng port**
→ Node context đã cancel nhưng libp2p host có thể mất ~1s để close. Đợi 1-2s trước khi tái sử dụng port.
