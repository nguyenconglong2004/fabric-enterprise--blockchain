# Core Service — Vai trò & Kiến trúc

> Mã nguồn: `coreservice/` · Ngôn ngữ: Go · Cổng HTTP: `:8080`

## 1. Core Service là gì?

**Core Service** là **cổng vào (gateway)** và đồng thời là **tầng tính toán (compute layer)** của hệ thống. Nó tương ứng với vai trò **Endorsing Peer / Gateway** trong Hyperledger Fabric. Mọi giao dịch của người dùng đều đi qua đây đầu tiên.

Ba nhiệm vụ chính:
1. **Thực thi smart contract** — chạy hợp đồng WASM trong sandbox cô lập (dùng [wazero](https://wazero.io/)) để "chạy thử" giao dịch.
2. **Điều phối endorsement** — sau khi contract chạy thành công, xin Committing Peer ký xác nhận, rồi gửi giao dịch tới Leader của Ordering Service.
3. **Phục vụ tra cứu & realtime** — cung cấp REST API cho Explorer, đẩy cập nhật qua SSE, tổng hợp số liệu benchmark.

> **Core Service gần như "không trạng thái" (stateless)** đối với sổ cái: trạng thái chốt cuối nằm ở Committing Peer. Core Service chỉ giữ bản cache cục bộ (mã contract, world state demo) và đọc lại từ PostgreSQL khi cần hiển thị.

---

## 2. Cấu trúc thư mục

```
coreservice/
├── cmd/node/main.go                 ← điểm khởi động: dựng libp2p, DB, engine, HTTP server
├── contracts/                       ← mã nguồn smart contract (biên dịch sang WASM)
│   ├── build_wasm.sh                  script biên dịch TinyGo
│   ├── example_asset/main.go          contract mẫu: tạo/sửa tài sản
│   ├── demo_inventory/main.go         contract mẫu: quản lý kho
│   └── bench_ping/main.go             contract tối giản để đo throughput
├── internal/
│   ├── api/                         ← tầng HTTP
│   │   ├── server.go                  định tuyến & xử lý /api/*
│   │   ├── benchmark.go               tổng hợp submit/commit/E2E latency
│   │   └── perf.go                    các cờ tinh chỉnh hiệu năng (env)
│   ├── core/
│   │   ├── model.go                   kiểu Transaction, Block, Endorsement
│   │   └── contract_schema.go         schema mô tả field cho UI
│   ├── crypto/keys.go               ← Ed25519: tạo khóa, ký, xác minh
│   ├── discovery/                   ← tìm Leader & membership cluster
│   │   ├── discovery.go               cache membership + failover
│   │   ├── endorse.go                 gửi endorsement tới Leader/Follower
│   │   └── bootstraps.go              parse danh sách multiaddr
│   ├── metrics/commitpeer/client.go ← HTTP client lấy metrics từ Committing Peer
│   ├── network/
│   │   ├── transport.go               libp2p host: stream endorsement/tx-sign
│   │   └── commit_peer_sign_pool.go   pool kết nối ấm tới Committing Peer
│   ├── state/database.go            ← LevelDB: ContractDB + LedgerDB
│   ├── storage/                     ← truy vấn PostgreSQL
│   │   ├── postgres.go                kết nối & lưu/đọc mirror
│   │   ├── submit_recorder.go         ghi thời điểm submit (batch INSERT)
│   │   ├── throughput.go              SQL tính throughput
│   │   └── benchmark.go               SQL tổng hợp E2E latency
│   └── vm/
│       ├── engine.go                  WasmEngine: cache + pool module + Execute
│       └── verbose.go                 bật log debug
└── go.mod
```

---

## 3. Luồng khởi động (`cmd/node/main.go`)

Khi chạy, Core Service thực hiện tuần tự:

1. **Dựng libp2p host** — lắng nghe `/ip4/0.0.0.0/tcp/0` (HĐH tự chọn cổng), in ra PeerID.
2. **Kết nối PostgreSQL** — thử lại 10 lần (cách nhau 2s); nếu thất bại vẫn chạy tiếp ở chế độ hạn chế (một số API trả 503).
3. **Mở LevelDB** — hai kho `./data/contract_db/` (mã contract) và `./data/ledger_db/` (world state). Thử lại 3 lần, xóa file LOCK nếu kẹt.
4. **Tạo WasmEngine** — chuẩn bị pool module WASM (mặc định 16 sandbox/contract, biến `WASM_POOL_SIZE`).
5. **Nhập địa chỉ Orderer & Committing Peer** — qua biến môi trường `ORDER_SERVICE_PEER`, `COMMIT_PEER_P2P` (hoặc nhập từ bàn phím). Tạo `discovery.Client` và **làm ấm** kết nối tới Committing Peer.
6. **Đăng ký route HTTP** và chạy server tại `:8080`.
7. **Chờ tín hiệu dừng** (Ctrl+C) → tắt mượt (graceful shutdown).

---

## 4. Các thành phần con (đọc tiếp)

| Chủ đề | File báo cáo |
|--------|--------------|
| Máy ảo WASM & smart contract | [02-wasm-smart-contract.md](02-wasm-smart-contract.md) |
| Mật mã & endorsement | [03-crypto-endorsement.md](03-crypto-endorsement.md) |
| Mạng libp2p & discovery | [04-networking-discovery.md](04-networking-discovery.md) |
| Lưu trữ LevelDB & PostgreSQL | [05-luu-tru-state.md](05-luu-tru-state.md) |
| REST API & metrics | [06-api-va-metrics.md](06-api-va-metrics.md) |

---

## 5. Các biến môi trường quan trọng

| Biến | Mặc định | Tác dụng |
|------|----------|----------|
| `POSTGRES_URL` | `postgres://fabric:fabric123@localhost:5432/blockchain?sslmode=disable` | Chuỗi kết nối CSDL |
| `ORDER_SERVICE_PEER` | (trống) | multiaddr orderer để khám phá membership |
| `COMMIT_PEER_P2P` | (trống) | multiaddr Committing Peer để xin chữ ký |
| `WASM_POOL_SIZE` | 16 | Số sandbox WASM dựng sẵn cho mỗi contract (1–32) |
| `CORE_SIGN_POOL` | 1 (bật) | Dùng pool kết nối ấm tới Committing Peer |
| `CORE_SIGN_TIMEOUT` | 15s | Hạn chờ ký endorsement |
| `CORE_RECORD_SUBMIT` | 1 (bật) | Ghi `tx_submit_times` để đo E2E latency |
| `CORE_ASYNC_ENDORSE` | 1 (bật) | Gửi endorsement bất đồng bộ |
| `CORE_ENDORSE_FALLBACK` | 0 | Thử lại các Follower khi Leader lỗi |
| `CORE_LOG` | (trống) | Đặt `debug` để in log chi tiết |
