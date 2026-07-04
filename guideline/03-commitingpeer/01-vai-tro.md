# Committing Peer — Vai trò & Tổng quan

> Mã nguồn: `commitingpeer/source/` · Ngôn ngữ: Go · Metrics: `:8081`

## 1. Committing Peer là gì?

**Committing Peer** (peer ghi sổ) là **trạm cuối** của pipeline. Nó nhận block đã được Ordering Service sắp xếp, **kiểm tra hợp lệ**, rồi **ghi vĩnh viễn** vào sổ cái và cập nhật world state. Đây là nơi giữ "sự thật" của hệ thống — file `chain.block` và LevelDB ở đây mới là sổ cái thật (PostgreSQL chỉ là bản sao).

Tương ứng vai trò **Committing Peer** trong Hyperledger Fabric.

Sáu nhiệm vụ:
1. **Nhận block** từ Ordering Service qua stream libp2p deliver.
2. **Kiểm tra** block & giao dịch (hash, merkle root, endorsement).
3. **Ghi block** vào file append-only (`chain.block`).
4. **Cập nhật world state** UTXO trong LevelDB.
5. **Mirror** sang PostgreSQL (bất đồng bộ, cho Explorer/kiểm toán).
6. **Phục vụ truy vấn UTXO** cho client; **ký endorsement** giúp Core Service.

> Committing Peer **không** chạy thuật toán đồng thuận — nó **tin tưởng** rằng block từ Ordering Service đã là thứ tự cuối (Raft đã chốt). Việc của nó là kiểm tra tính hợp lệ mật mã rồi ghi.

## 2. Cấu trúc thư mục

```
commitingpeer/source/
├── cmd/peer/main.go              ← điểm vào, CLI, sinh/nạp khóa, vòng lệnh tương tác
├── internal/
│   ├── crypto/keys.go            ← Ed25519, hash block, merkle, xác minh
│   ├── deliver/
│   │   ├── client.go               nhận block từ orderer (stream)
│   │   ├── membership.go            lấy membership orderer
│   │   └── sign.go                  ký endorsement giúp Core Service
│   ├── discovery/                ← cache membership orderer + failover
│   │   ├── discovery.go
│   │   └── bootstraps.go
│   ├── metrics/
│   │   ├── recorder.go             ghi thời điểm commit trong RAM (ground truth)
│   │   ├── query.go                truy vấn throughput/E2E
│   │   └── server.go               HTTP /metrics/*
│   ├── peer/
│   │   ├── peer.go                 ⭐ điều phối: deliver → validate → ghi
│   │   └── ledger_mirror.go        mirror PostgreSQL bất đồng bộ
│   ├── storage/
│   │   ├── block_storage.go        file block append-only
│   │   ├── world_state.go          UTXO set trong LevelDB
│   │   └── postgres.go             mirror PostgreSQL
│   ├── types/                    ← block, transaction, deliver, sync
│   └── validation/engine.go      ← kiểm tra block & endorsement
└── go.mod
```

## 3. Kiến trúc đường ống ba tầng (pipeline)

Bên trong, `peer.go` nối ba tầng tách rời nhau bằng channel — giúp mỗi tầng chạy với tốc độ riêng:

```
   ORDERER ──stream──▶ [deliver.Client] ──blockChan(64)──▶ [commitLoop]
                                                               │
                              ┌────────────────────────────────┤
                              ▼ (validate)                      │
                       [validation.Engine]                      │
                              │ hợp lệ                           │
              ┌───────────────┼───────────────┐                 │
              ▼               ▼               ▼                  │
      [BlockStorage]   [WorldState]   [Postgres mirror]          │
      (file, đồng bộ)  (LevelDB, đồng bộ) (async, không chặn)   │
                                                                 ▼
                                                       cập nhật stats (atomic)
```

## 4. Đọc tiếp

| Chủ đề | File |
|--------|------|
| Giao thức nhận block & ký endorsement | [02-deliver-protocol.md](02-deliver-protocol.md) |
| Kiểm tra hợp lệ (validation) | [03-validation.md](03-validation.md) |
| Lưu trữ: file block, world state, PostgreSQL | [04-luu-tru-va-worldstate.md](04-luu-tru-va-worldstate.md) |
| Metrics & đo lường | [05-metrics.md](05-metrics.md) |

## 5. Giao diện dòng lệnh (CLI)

Khi chạy, peer hỏi: địa chỉ orderer, đường dẫn file block, thư mục world state, khóa endorsement (sinh mới hoặc nạp từ file/env). Sau đó vào vòng lệnh tương tác:

| Lệnh | Tác dụng |
|------|----------|
| `status` | Thống kê peer, tóm tắt chuỗi, số UTXO |
| `chain` | Liệt kê mọi block đã commit |
| `block <n>` | Chi tiết một block + mọi giao dịch |
| `tx <txid>` | Tìm một giao dịch trên toàn chuỗi |
| `utxo <txid> <n>` | Tra một UTXO cụ thể |
| `worldstate` | Liệt kê mọi UTXO chưa tiêu |
| `quit` | Tắt mượt |

## 6. Thư viện phụ thuộc (`go.mod`)

| Thư viện | Vai trò |
|----------|---------|
| `github.com/libp2p/go-libp2p v0.32.2` | Mạng ngang hàng |
| `github.com/syndtr/goleveldb v1.0.0` | World state UTXO |
| `github.com/lib/pq v1.12.3` | Mirror PostgreSQL |
| `golang.org/x/crypto` | Ed25519, SHA-256 |
