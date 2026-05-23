# k6 — submit transaction (cùng API với Frontend)

Explorer gọi Core Service **`:8080`** qua Vite proxy (`/api` → `http://localhost:8080`).

Script này test **`POST /api/tx/submit`** — luồng đầy đủ như nút Submit trên FE:

1. Core node chạy WASM contract  
2. Commit peer ký transaction  
3. Gửi endorsement lên orderer (libp2p, không qua HTTP orderer)

## Chuẩn bị

```bash
# 1. Postgres (docker-compose)
docker compose up -d postgres

# 2. Orderer cluster (libp2p, ví dụ :6000, :6001)
cd orderingservice/source && go run ./cmd/server

# 3. Commit peer
cd commitingpeer/source && go run ./cmd/peer

# 4. Core node — port 8080 (cùng FE)
cd coreservice
# Khi prompt: OrderServicePeer = multiaddr orderer, CommitPeerP2P = multiaddr commit peer
go run ./cmd/node
```

Deploy contract (một lần) nếu chưa có `example_asset`:

```bash
curl -X POST http://localhost:8080/api/deploy-example
```

## Chạy k6

```bash
cd orderingservice/k6
brew install k6   # nếu chưa có

k6 run --vus 1 --iterations 1 submit-tx.js
k6 run submit-tx.js                                    # mặc định maxpush (đẩy tối đa)
k6 run -e VUS=500 -e DURATION=90s submit-tx.js
k6 run -e SCENARIO=open -e OPEN_RATE=5000 submit-tx.js # open-loop, cố 5000 req/s
k6 run -e SCENARIO=ramp -e RAMP_PEAK=3000 submit-tx.js
k6 run -e SCENARIO=steady -e RATE=50 submit-tx.js      # cap tải (không dùng để tìm max)
```

## Biến môi trường

### k6

| Biến | Mặc định | Mô tả |
|------|----------|--------|
| `BASE_URL` | `http://localhost:8080` | Core API (cùng FE) |
| `CONTRACT` | `example_asset` | Tên contract |
| `SCENARIO` | `maxpush` | `maxpush` (mặc định), `open`, `ramp`, `burst`, `steady` |
| `VUS` | `300` | Số VU (`maxpush`) |
| `OPEN_RATE` | `5000` | Target req/s (`open`) |
| `RAMP_PEAK` | `3000` | Đỉnh ramp (`ramp` / `burst`) |
| `MAX_VUS` | `600` | Trần VU (`open` / `ramp`) |
| `RATE` | `100` | Chỉ dùng với `SCENARIO=steady` |
| `DURATION` | `60s` | Thời gian test |
| `REQ_TIMEOUT` | `60s` | Timeout mỗi POST submit |

### Đo tx/s, block/s thật (ledger)

k6 **không** cap = throughput hệ thống. Sau test:

```bash
curl -s "http://localhost:8080/api/metrics/e2e?window=120&tx_prefix=k6-" | jq '.metrics | {tx_per_sec, blocks_per_sec, tx_e2e_ms_p95}'
```

- `tx_per_sec` / `blocks_per_sec` = số đã **commit vào ledger** (full flow).
- k6 `submit_ok`/giây = tốc độ **HTTP submit** (có thể cao hơn ledger nếu backlog).

### Core node (khuyến nghị khi load test)

| Biến | Mặc định | Mô tả |
|------|----------|--------|
| `WASM_POOL_SIZE` | `4` | Số WASM sandbox tái sử dụng / contract (1–32) |
| `CORE_LOG` | _(tắt log hot path)_ | Đặt `debug` để bật log chi tiết API/VM |
| `E2E_LOG` | `1` | Core: log `[e2e] core submit stamped` |
| `E2E_LOG_TX` | `0` | Commit peer: log từng tx vào ledger (`1` = bật) |

**Đo full flow (submit → ledger DB):** sau k6, `GET http://localhost:8080/api/metrics/e2e?window=60&tx_prefix=k6-`

Migration DB cũ (từ **root repo**):

```bash
cd /path/to/fabric-enterprise--blockchain
docker exec -i fabric-postgres psql -U fabric -d blockchain \
  < migrations/add_e2e_timestamps.sql
```

Hoặc host: `PGPASSWORD=fabric123 psql -h 127.0.0.1 -p 5432 -U fabric -d blockchain -f migrations/add_e2e_timestamps.sql`

```bash
# Ví dụ: core node trước khi chạy k6
export WASM_POOL_SIZE=16   # nên ≥ RATE/10 khi chạy steady 100+
export ORDER_SERVICE_PEER=/ip4/127.0.0.1/tcp/6000/p2p/...
export COMMIT_PEER_P2P=/ip4/127.0.0.1/tcp/.../p2p/...
cd coreservice && go run ./cmd/node
```

## Lưu ý

- HTTP 200 = core xử lý + (thường) đã gửi endorsement; **chưa** đo block commit trên orderer.
- Lỗi thường gặp: thiếu `ORDER_SERVICE_PEER`, thiếu commit peer, contract chưa deploy.
- Orderer **không** cần mở HTTP port riêng cho test này.
