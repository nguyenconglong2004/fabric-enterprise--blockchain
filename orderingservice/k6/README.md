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
k6 run submit-tx.js
k6 run -e RATE=100 -e VUS=80 -e DURATION=2m submit-tx.js
k6 run -e SCENARIO=burst submit-tx.js
```

## Biến môi trường

| Biến | Mặc định | Mô tả |
|------|----------|--------|
| `BASE_URL` | `http://localhost:8080` | Core API (cùng FE) |
| `CONTRACT` | `example_asset` | Tên contract |
| `SCENARIO` | `steady` | `steady` hoặc `burst` (150 req/s peak) |
| `RATE` | `50` | Request/giây (`steady`) |
| `DURATION` | `60s` | Thời gian test |
| `VUS` | `50` | Virtual users |

## Lưu ý

- HTTP 200 = core xử lý + (thường) đã gửi endorsement; **chưa** đo block commit trên orderer.
- Lỗi thường gặp: thiếu `ORDER_SERVICE_PEER`, thiếu commit peer, contract chưa deploy.
- Orderer **không** cần mở HTTP port riêng cho test này.
