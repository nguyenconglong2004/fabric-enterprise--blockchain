# Setup & chạy hệ thống (Account / KV balance / RW Set)

Thứ tự quan trọng: **Postgres → Orderer → Commit Peer (:8081) → Core (:8080) → Explorer**.

---

## 0. Yêu cầu

- Docker + Docker Compose
- Go 1.22+
- Node.js (Explorer)
- TinyGo (chỉ khi rebuild WASM contract)

Repo root:

```bash
cd /Users/quynguyenvan/Documents/Thesis/fabric-enterprise--blockchain
```

---

## 1. PostgreSQL (Docker)

Compose dùng file [docker-compose.yml](../docker-compose.yml) + [init.sql](../init.sql) (đã gồm `accounts` / `sessions`).

### 1.1 Lần đầu / volume trống

`init.sql` chỉ chạy khi **volume Postgres mới** (lần đầu tạo container).

```bash
cd /Users/quynguyenvan/Documents/Thesis/fabric-enterprise--blockchain

# Tạo & chạy Postgres
docker compose up -d postgres

# Chờ healthy
docker compose ps
docker exec fabric-postgres pg_isready -U fabric
```

Kiểm tra schema accounts:

```bash
docker exec -it fabric-postgres psql -U fabric -d blockchain -c "\dt core_service.*"
```

Kỳ vọng có: `wallet.accounts`, `wallet.sessions`, cùng `core_service.*` / `commit_peer.*` cho mirror ledger.

### 1.2 DB đã tồn tại từ trước (volume cũ — không chạy lại init.sql)

Đổi tên schema: accounts nằm ở **`wallet`**, không còn `core_service`.

```bash
cd /Users/quynguyenvan/Documents/Thesis/fabric-enterprise--blockchain

# Tạo wallet.* + copy từ core_service.accounts nếu còn
docker exec -i fabric-postgres psql -U fabric -d blockchain < migrations/004_wallet_schema.sql

docker exec -it fabric-postgres psql -U fabric -d blockchain -c "\dt wallet.*"
```

(Tuỳ chọn dọn legacy sau khi Core seed/login OK:)

```bash
docker exec -it fabric-postgres psql -U fabric -d blockchain -c \
  "DROP TABLE IF EXISTS core_service.sessions; DROP TABLE IF EXISTS core_service.accounts;"
```

### 1.3 Reset sạch DB (xóa hết data Docker)

```bash
docker compose down -v
docker compose up -d postgres
docker exec fabric-postgres pg_isready -U fabric
```

### 1.4 Connection string (mặc định Core / Commit Peer)

```text
postgres://fabric:fabric123@localhost:5432/blockchain?sslmode=disable
```

Override:

```bash
export POSTGRES_URL='postgres://fabric:fabric123@localhost:5432/blockchain?sslmode=disable'
```

---

## 2. Build (một lần / sau khi pull)

```bash
export GOMODCACHE="${GOMODCACHE:-$HOME/go/pkg/mod}"

# WASM contracts (gồm transfer)
cd coreservice/contracts && bash build_wasm.sh && cd ../..

# Binaries (tuỳ chọn — có thể go run)
cd coreservice && go build -o /tmp/coreservice ./cmd/node && cd ..
cd commitingpeer/source && go build -o /tmp/commit-peer ./cmd/peer && cd ../..
cd orderingservice/source && go build -o /tmp/orderer ./cmd/server && cd ../..
```

---

## 3. Chạy Orderer

Theo cách bạn đang dùng (cluster 2 node / `cmd/server`). Ghi lại **multiaddr** in ra log, ví dụ:

```text
/ip4/127.0.0.1/tcp/6000/p2p/12D3KooW...
```

```bash
# ví dụ
cd orderingservice/source/cmd/server
go run .
```

---

## 4. Chạy Commit Peer (trước Core)

Wallet mint + `/wallet/state` nằm ở metrics **:8081**.

```bash
cd commitingpeer/source/cmd/peer

export POSTGRES_URL='postgres://fabric:fabric123@localhost:5432/blockchain?sslmode=disable'
# mặc định metrics :8081 — đừng set COMMIT_PEER_METRICS_ADDR=0

go run .
```

Prompt:

1. Orderer multiaddr (cùng addr bước 3)
2. Block file (Enter = `chain.block`)

Log cần thấy dạng:

```text
💰 Wallet (KV): http://127.0.0.1:8081/wallet/mint|balance|state
```

Copy **Commit Peer libp2p multiaddr** (P2P) để đưa vào Core.

Smoke:

```bash
curl -s 'http://127.0.0.1:8081/wallet/balance?address=0000000000000000000000000000000000000000'
```

---

## 5. Chạy Core

**Commit Peer phải đã lên :8081** trước khi Core seed (mint KV balance).

```bash
cd coreservice/cmd/node

export POSTGRES_URL='postgres://fabric:fabric123@localhost:5432/blockchain?sslmode=disable'
export COMMIT_PEER_METRICS_URL='http://127.0.0.1:8081'

# tuỳ chọn — khỏi gõ prompt
# export ORDER_SERVICE_PEER='/ip4/127.0.0.1/tcp/6000/p2p/12D3KooW...'
# export COMMIT_PEER_P2P='/ip4/127.0.0.1/tcp/..../p2p/12D3KooW...'

go run .
```

Prompt:

1. `OrderServicePeer >` — multiaddr orderer  
2. `CommitPeerP2P >` — multiaddr commit peer  

Log seed:

```text
👤 Seeded alice address=... balance=1000
🌐 Core Node API Server đang chạy tại http://localhost:8080
```

Deploy contract thủ công (ví dụ):

```bash
cd coreservice/contracts && ./build_wasm.sh
curl -X POST http://127.0.0.1:8080/api/tx/deploy \
  -F 'contract_name=transfer' \
  -F 'file=@transfer/my_contract.wasm'
```

Nếu mint fail (`commit peer :8081`): tắt Core, mở lại peer metrics, restart Core.

Kiểm tra login:

```bash
curl -s -X POST http://127.0.0.1:8080/api/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"alice","password":"password123"}' | jq
```

---

## 6. Explorer (FE)

```bash
cd BlockchainExplorer-FrontEnd
npm install
npm run dev
```

Mở URL Vite (thường `http://localhost:5173`).

| Việc | Cách |
|------|------|
| Đăng nhập | **Sign in** — `alice` / `password123` |
| Xem ví | **Wallet** — address + balance SSE (KV) |
| Chuyển tiền | **Submit** → contract `transfer` (dán address bob) |

Lấy address bob (login bob hoặc query DB):

```bash
docker exec -it fabric-postgres psql -U fabric -d blockchain \
  -c "SELECT username, address, discount FROM wallet.accounts;"
```

---

## 7. Sau khi transfer — kiểm RW set / KV

Sau khi tx vào block:

```bash
# key receipt từ contract transfer: xfer_receipt:<to_address>
curl -s "http://127.0.0.1:8081/wallet/state?key=xfer_receipt:<TO_ADDRESS_40HEX>" | jq

# balance KV
curl -s "http://127.0.0.1:8081/wallet/balance?address=<ADDRESS>" | jq

# hoặc qua Core proxy
curl -s "http://127.0.0.1:8080/api/state?key=xfer_receipt:<TO_ADDRESS_40HEX>" | jq
```

Balance:

```bash
TOKEN='...'   # từ /api/auth/login
curl -s http://127.0.0.1:8080/api/wallet/balance -H "Authorization: Bearer $TOKEN" | jq
```

---

## 8. Tài khoản demo

| User | Pass | Discount |
|------|------|----------|
| alice | password123 | 10% |
| bob | password123 | 0% |
| charlie | password123 | 5% |

Alice có thể gửi tới `floor(in × 1.1)` (công thức A).

---

## 9. Lỗi thường gặp

| Triệu chứng | Cách xử lý |
|-------------|------------|
| Core seed mint fail | Peer chưa listen `:8081` — restart peer rồi Core |
| FE `ECONNREFUSED :8080` | Core đang chờ prompt `CommitPeerP2P` — nhập addr |
| Login 503 / no accounts | Postgres down hoặc thiếu bảng — chạy mục 1.2 |
| Endorsement verify fail | Binary cũ/mới lệch RW set — restart **peer + core** cùng bản code mới |
| `transfer` missing | `cd coreservice/contracts && bash build_wasm.sh` rồi restart Core |
| init.sql không chạy | Volume cũ — dùng 1.2 hoặc `docker compose down -v` |

---

## 10. One-liner tóm tắt (sau khi orderer/peer addr đã biết)

```bash
# Terminal A — DB
docker compose up -d postgres

# Terminal B — Orderer (giữ chạy)
# Terminal C — Commit Peer
cd commitingpeer/source/cmd/peer && \
  POSTGRES_URL='postgres://fabric:fabric123@localhost:5432/blockchain?sslmode=disable' go run .

# Terminal D — Core
cd coreservice/cmd/node && \
  POSTGRES_URL='postgres://fabric:fabric123@localhost:5432/blockchain?sslmode=disable' \
  COMMIT_PEER_METRICS_URL='http://127.0.0.1:8081' \
  go run .

# Terminal E — FE
cd BlockchainExplorer-FrontEnd && npm run dev
```

Chi tiết kỹ thuật: [ACCOUNT_UTXO_AUTH.md](./ACCOUNT_UTXO_AUTH.md), [RW_SET.md](./RW_SET.md).
