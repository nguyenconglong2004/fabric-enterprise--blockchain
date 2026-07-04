# Deploy `bench_ping` và chạy benchmark k6

Hướng dẫn setup stack tối thiểu, deploy contract **`bench_ping`** (tx nhẹ, không ghi state), và chạy k6 để đo throughput / latency.

Tài liệu metric: [BENCHMARK_METRICS.md](./BENCHMARK_METRICS.md).

---

## 1. Tổng quan pipeline

```
k6  ──HTTP──►  Core :8080  ──P2P──►  Orderer (Raft)
                    │                      │
                    │                      ▼
                    └── sign tx ◄──  Commit Peer ──► Postgres (ledger mirror)
```

Thứ tự khởi động: **Postgres → Orderer → Commit peer → Core → deploy contract → k6**.

---

## 2. Yêu cầu

| Công cụ | Dùng cho |
|---------|----------|
| Docker + Docker Compose | Postgres |
| Go 1.21+ | Orderer, commit peer, Core |
| [TinyGo](https://tinygo.org/getting-started/install/) | Build WASM `bench_ping` |
| [k6](https://grafana.com/docs/k6/latest/set-up/install-k6/) | Load test |
| Node.js 18+ | Build frontend orchestrator |
| PowerShell 7+ hoặc Git Bash | Chạy lệnh trên Windows (xem mục 2.1) |

Trên **macOS/Linux**, trước benchmark tăng file descriptor:

```bash
ulimit -n 65536
```

Trên **Windows** không cần `ulimit` (mục 2.1).

---

## 2.1 Chạy trên Windows

Hướng dẫn dùng **PowerShell** từ thư mục gốc repo (vd. `C:\...\fabric-enterprise--blockchain`). Có thể dùng **Git Bash** cho các lệnh `bash`/`./script.sh` nếu quen hơn.

### Cài đặt

| Công cụ | Cách cài (gợi ý) |
|---------|------------------|
| Docker Desktop | [docker.com/products/docker-desktop](https://www.docker.com/products/docker-desktop/) — bật WSL2 backend |
| Go | [go.dev/dl](https://go.dev/dl/) — MSI Windows amd64 |
| Node.js | [nodejs.org](https://nodejs.org/) LTS |
| TinyGo | [tinygo.org/getting-started/install/windows](https://tinygo.org/getting-started/install/windows/) — thêm `tinygo` vào `PATH` |
| k6 | `winget install k6` hoặc `choco install k6` |
| jq (tùy chọn) | `winget install jqlang.jq` — không bắt buộc, có thể đọc JSON thô |

Kiểm tra nhanh:

```powershell
docker compose version
go version
node -v
tinygo version
k6 version
```

### Cửa sổ terminal

Chạy **song song** (mỗi dịch vụ một tab Windows Terminal):

| Tab | Dịch vụ | Ghi chú |
|-----|---------|---------|
| 1 | Postgres (Docker) | Chạy một lần, để nền |
| 2 | Orderer orchestrator | UI + API orderer |
| 3 | Commit peer | Prompt nhập multiaddr orderer |
| 4 | Core Service | HTTP API cho k6 |
| 5 | k6 | Chạy sau khi deploy contract |

### Xung đột cổng `:8080`

Orchestrator và Core **cùng mặc định cổng 8080**. Khi chạy full stack trên một máy:

- **Orchestrator** → cổng khác, vd. `:9090`
- **Core** → giữ `:8080` (k6 gọi `BASE_URL=http://localhost:8080`)

```powershell
cd orderingservice\source
.\orchestrator.exe --addr :9090
# UI orderer: http://localhost:9090
```

Tune auto-propose (nếu cần) qua API orderer:

```powershell
curl.exe -X PATCH http://localhost:9090/api/nodes/6000/config `
  -H "Content-Type: application/json" `
  -d '{\"auto_propose_interval_ms\": 100, \"auto_propose_block_size\": 1000}'
```

### Postgres — migration trên Windows

Thay `docker exec ... < file` (bash) bằng pipe PowerShell:

```powershell
Get-Content migrations\002_tx_submit_times.sql -Raw |
  docker exec -i fabric-postgres psql -U fabric -d blockchain
```

### Ordering Service

```powershell
cd orderingservice\source\web
npm install
npm run build
cd ..
go build -o orchestrator.exe .\cmd\orchestrator
.\orchestrator.exe --addr :9090
```

Tạo node trên UI (`http://localhost:9090`), ghi multiaddr P2P đầy đủ.

### Committing Peer

```powershell
cd commitingpeer\source
go mod tidy
go build -o ..\peer.exe .\cmd\peer
cd ..
.\peer.exe
```

### Core Service — biến môi trường (PowerShell)

```powershell
$env:ORDER_SERVICE_PEER = "/ip4/127.0.0.1/tcp/6000/p2p/12D3KooW..."
$env:COMMIT_PEER_P2P    = "/ip4/127.0.0.1/tcp/12345/p2p/12D3KooW..."
$env:POSTGRES_URL       = "postgres://fabric:fabric123@localhost:5432/blockchain?sslmode=disable"

cd coreservice\cmd\node
go run main.go
```

Tắt ghi submit time: `$env:CORE_RECORD_SUBMIT = "0"`.

**Git Bash** (tương đương `export`):

```bash
export ORDER_SERVICE_PEER="/ip4/127.0.0.1/tcp/6000/p2p/12D3KooW..."
export COMMIT_PEER_P2P="/ip4/127.0.0.1/tcp/12345/p2p/12D3KooW..."
export POSTGRES_URL="postgres://fabric:fabric123@localhost:5432/blockchain?sslmode=disable"
cd coreservice/cmd/node && go run main.go
```

### Build WASM `bench_ping`

**Cách 1 — TinyGo trực tiếp (PowerShell):**

```powershell
cd coreservice\contracts
tinygo build -o bench_ping/my_contract.wasm -target wasi -no-debug -scheduler=none ./bench_ping
```

**Cách 2 — Git Bash** (build cả 3 contract):

```bash
cd coreservice/contracts
bash build_wasm.sh
```

### Deploy contract

Dùng `curl.exe` (tránh alias `curl` của PowerShell). Đường dẫn file: từ **thư mục gốc repo**:

```powershell
curl.exe -X POST http://localhost:8080/api/tx/deploy `
  -F "contract_name=bench_ping" `
  -F "file=@coreservice/contracts/bench_ping/my_contract.wasm"
```

Kiểm tra (không cần `jq`):

```powershell
curl.exe -s http://localhost:8080/api/contracts
```

### Chạy k6

Không cần `ulimit`. Nếu k6 báo thiếu tài nguyên hoặc lỗi socket, giảm `RATE` / `MAX_VUS` trước.

```powershell
cd orderingservice\k6

k6 run `
  -e RATE=5000 `
  -e DURATION=60s `
  -e MAX_VUS=7000 `
  -e CONTRACT=bench_ping `
  -e TX_PREFIX=k6-rfp- `
  -e LEDGER_WAIT=60s `
  submit-tx.js
```

Một dòng (copy-paste dễ hơn):

```powershell
k6 run -e RATE=5000 -e DURATION=60s -e MAX_VUS=7000 -e CONTRACT=bench_ping -e TX_PREFIX=k6-rfp- -e LEDGER_WAIT=60s submit-tx.js
```

### Checklist Windows

- [ ] Docker Desktop đang chạy  
- [ ] Orchestrator `--addr :9090`, Core `:8080`  
- [ ] Multiaddr orderer / commit peer copy đủ port + PeerID  
- [ ] `bench_ping` deploy thành công (`/api/contracts`)  
- [ ] k6 chạy từ `orderingservice\k6` với `TX_PREFIX` riêng  

---

## 3. Postgres (Docker)

Từ thư mục gốc repo:

```bash
docker compose up -d postgres
```

Thông số mặc định (`docker-compose.yml`):

| | |
|--|--|
| Container | `fabric-postgres` |
| URL | `postgres://fabric:fabric123@localhost:5432/blockchain?sslmode=disable` |

DB mới: `init.sql` tự chạy (schema + `tx_submit_times`). DB **đã tồn tại** từ trước — chạy migration:

```bash
docker exec -i fabric-postgres psql -U fabric -d blockchain < migrations/002_tx_submit_times.sql
```

Kiểm tra:

```bash
docker exec -it fabric-postgres psql -U fabric -d blockchain -c "\dt core_service.*"
```

---

## 4. Ordering Service

Khuyến nghị dùng **Web UI orchestrator** (xem [orderingservice/README.md](../orderingservice/README.md)):

```bash
cd orderingservice/source/web && npm install && npm run build
cd .. && go build -o orchestrator ./cmd/orchestrator
./orchestrator --addr :9090
# UI orderer: http://localhost:9090 — Core API vẫn :8080 (xem mục 2.1 Windows)
```

Windows: dùng `orchestrator.exe` và `--addr :9090` (mục 2.1).

Tạo ít nhất **1 node** (port P2P vd. `6000`). Ghi lại multiaddr đầy đủ:

```
/ip4/127.0.0.1/tcp/6000/p2p/12D3KooW...
```

**Lưu ý:** Copy đủ port (`6000`, không bị cắt `60`) và đủ PeerID — sai multiaddr → Core không gửi được endorsement, ledger = 0.

---

## 5. Committing Peer

```bash
cd commitingpeer/source
go mod tidy
go build -o ../peer ./cmd/peer    # Windows: -o ../peer.exe
cd ..
./peer                            # Windows: .\peer.exe
```

Khi prompt:

1. Nhập **orderer multiaddr** (deliver stream).
2. Peer tự mirror block vào Postgres (`POSTGRES_URL` mặc định giống docker-compose).

Ghi lại **P2P multiaddr của commit peer** (Core cần để ký tx), vd.:

```
/ip4/127.0.0.1/tcp/12345/p2p/12D3KooW...
```

---

## 6. Core Service

```bash
cd coreservice/cmd/node
go run main.go
```

Prompt hoặc biến môi trường:

| Biến | Mô tả |
|------|--------|
| `ORDER_SERVICE_PEER` | Multiaddr orderer (một node bất kỳ trong cluster) |
| `COMMIT_PEER_P2P` | Multiaddr commit peer (bắt buộc để `/api/tx/submit` hoạt động) |
| `POSTGRES_URL` | Postgres (mặc định docker-compose) |
| `CORE_RECORD_SUBMIT` | Mặc định **bật** — ghi submit time cho benchmark. Tắt: `CORE_RECORD_SUBMIT=0` |

Ví dụ không cần nhập tay:

```bash
export ORDER_SERVICE_PEER="/ip4/127.0.0.1/tcp/6000/p2p/12D3KooW..."
export COMMIT_PEER_P2P="/ip4/127.0.0.1/tcp/12345/p2p/12D3KooW..."
export POSTGRES_URL="postgres://fabric:fabric123@localhost:5432/blockchain?sslmode=disable"

cd coreservice/cmd/node && go run main.go
```

Core API: **http://localhost:8080**

Kiểm tra:

```bash
curl -s http://localhost:8080/api/contracts | jq .
```

---

## 7. Contract `bench_ping`

Contract tối giản: payload JSON `{"v":"<id>"}`, chỉ `verify_tx`, **không** `PutState` — phù hợp đo throughput thuần.

### 7.1 Build WASM

```bash
cd coreservice/contracts
./build_wasm.sh
# Output: coreservice/contracts/bench_ping/my_contract.wasm
```

Cần TinyGo. Script build cả `example_asset`, `demo_inventory`, `bench_ping`.

**Windows:** Git Bash `bash build_wasm.sh`, hoặc TinyGo trực tiếp (mục 2.1).

### 7.2 Deploy lên Core

```bash
curl -X POST http://localhost:8080/api/tx/deploy \
  -F "contract_name=bench_ping" \
  -F "file=@coreservice/contracts/bench_ping/my_contract.wasm"
```

Response thành công có `status: success`. Xác nhận:

```bash
curl -s http://localhost:8080/api/contracts | jq '.contracts[] | select(.name=="bench_ping")'
```

### 7.3 Payload k6 gửi

k6 (`submit-tx.js`) với `CONTRACT=bench_ping` tạo payload hex của:

```json
{"v":"k6-rfp-<vu>-<iter>"}
```

Txid dạng: `{TX_PREFIX}{vu}-{iter}-{timestamp}`.

---

## 8. Chạy k6

```bash
cd orderingservice/k6
```

### 8.1 Benchmark RFP (steady 5000/s × 60s)

```bash
ulimit -n 65536   # bỏ qua trên Windows (mục 2.1)

k6 run \
  -e RATE=5000 \
  -e DURATION=60s \
  -e MAX_VUS=7000 \
  -e CONTRACT=bench_ping \
  -e TX_PREFIX=k6-rfp- \
  -e LEDGER_WAIT=60s \
  submit-tx.js
```

Teardown tự gọi `/api/metrics/benchmark` và in summary (submit, commit, E2E, RFP hints).

### 8.2 Các scenario khác

```bash
# Mặc định script (~6000/s × 25s)
k6 run submit-tx.js

# Burst — VU loop liên tục (không đều req/s)
k6 run -e SCENARIO=maxpush -e VUS=500 -e DURATION=10s -e CONTRACT=bench_ping submit-tx.js

# Sweep — tăng dần rate để tìm điểm bão hòa
k6 run -e SCENARIO=sweep -e CONTRACT=bench_ping submit-tx.js
```

### 8.3 Biến môi trường k6

| Biến | Mặc định | Mô tả |
|------|----------|--------|
| `BASE_URL` | `http://localhost:8080` | Core API |
| `SCENARIO` | `steady` | `steady`, `sweep`, `maxpush` |
| `RATE` | `6000` | Target req/s (`steady`) |
| `DURATION` | `25s` | Thời gian load |
| `MAX_VUS` | `max(800, RATE+800)` | Trần VU — tăng nếu k6 báo thiếu VU |
| `PRE_VUS` | `min(MAX_VUS, max(RATE,100))` | VU pre-allocated |
| `CONTRACT` | `bench_ping` | Tên contract |
| `TX_PREFIX` | `k6-` | Prefix txid — **dùng prefix riêng mỗi lần chạy** để lọc metric |
| `LEDGER_WAIT` | `15s` | Chờ drain backlog trước khi đo E2E |
| `REQ_TIMEOUT` | `30s` | Timeout HTTP submit |

Sweep thêm: `SWEEP_START`, `SWEEP_PEAK`, `SWEEP_STEP`, `SWEEP_STAGE_SEC`.

---

## 9. Đọc kết quả sau k6

### 9.1 Trong log teardown

- **benchmark (load window)** — throughput sustained trong lúc bắn tx  
- **benchmark (load + drain)** — gồm thời gian chờ ledger  
- **ledger latest / peak** — commit tx/s từ `/api/metrics/throughput`

Copy `window_start` / `window_end` từ log để query lại:

```bash
curl -s "http://localhost:8080/api/metrics/benchmark?\
since=2026-06-06T17:25:07.689Z&\
until=2026-06-06T17:26:07.689Z&\
tx_prefix=k6-rfp-" | jq .
```

### 9.2 Tiêu chí tham chiếu (RFP)

| Metric | Ngưỡng gợi ý |
|--------|----------------|
| Submit sustained | ≥ 5000 tx/s |
| Commit sustained | ≥ 5000 tx/s |
| E2E latency p95 | < 1000 ms |

Field `meets_*` trong JSON benchmark map trực tiếp các ngưỡng trên.

**Gợi ý đánh giá:**

- **Throughput:** dùng cửa sổ **load** (60s), không chia cho tổng thời gian k6.  
- **Latency p95:** chờ drain (`LEDGER_WAIT=60s` hoặc hơn), query **load + drain**, đảm bảo `e2e_pending` gần 0.  
- Nếu offer > commit sustained → backlog → p95 cao; giảm `RATE` (vd. 4000) để so steady-state.

---

## 10. Checklist nhanh

- [ ] `docker compose up -d postgres` (+ migration nếu DB cũ)  
- [ ] Orderer chạy (`--addr :9090` nếu Core cùng máy), multiaddr đúng  
- [ ] Commit peer connect orderer, mirror Postgres  
- [ ] Core: `ORDER_SERVICE_PEER` + `COMMIT_PEER_P2P`  
- [ ] Build WASM + deploy `bench_ping`  
- [ ] `ulimit -n 65536` (macOS/Linux; Windows bỏ qua)  
- [ ] k6 với `TX_PREFIX` riêng + `LEDGER_WAIT` đủ dài  
- [ ] Query benchmark với **đúng** `since`/`until` từ teardown  

**Windows:** xem checklist riêng mục 2.1.

---

## 11. Tài liệu liên quan

- [BENCHMARK_METRICS.md](./BENCHMARK_METRICS.md) — chi tiết từng field API  
- [orderingservice/k6/README.md](../orderingservice/k6/README.md) — ghi chú ngắn k6  
- [orderingservice/README.md](../orderingservice/README.md) — orderer / orchestrator  
- [commitingpeer/source/docs/COMMITTING_PEER.md](../commitingpeer/source/docs/COMMITTING_PEER.md) — commit peer  
