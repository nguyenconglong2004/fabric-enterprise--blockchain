# Ordering Service — Loadgen & Client

> Mã nguồn: `orderingservice/source/pkg/client/`, `pkg/loadgen/`, `cmd/{client,loadgen,server}/main.go`, `k6/`

## 1. Ba lệnh thực thi (`cmd/`)

| Lệnh | Vai trò |
|------|---------|
| `cmd/server/main.go` | Chạy **một node orderer** (tham gia cluster Raft) |
| `cmd/client/main.go` | Ví/CLI tương tác: tạo khóa, sync UTXO, tạo & ký giao dịch |
| `cmd/loadgen/main.go` | Công cụ **bắn tải** để đo throughput/latency |

## 2. Thư viện client (`pkg/client/client.go`)

Cung cấp API cho ứng dụng nói chuyện với cluster:
- `NewOrderClient(ctx)`: tạo client transport libp2p.
- `GetClusterNodes(peerID)`: hỏi membership, trả danh sách node.
- `ConnectToNode(addr)`: kết nối tới một peer.
- `SubmitTransaction(tx)`: gửi giao dịch (tự chuyển tiếp tới Leader nếu cần).
- `SyncUTXOs(ctx, peerAddr, address)`: hỏi Committing Peer các UTXO đã xác nhận của một địa chỉ.

CLI client (`cmd/client`) là một **ví UTXO** đầy đủ: sinh địa chỉ từ khóa Ed25519, theo dõi UTXO, tạo và ký giao dịch UTXO hoặc truy vấn trạng thái đã commit.

## 3. Bộ máy bắn tải Go (`pkg/loadgen/`)

Mục tiêu: bơm giao dịch ở tốc độ cao (mặc định **5000 TPS**) để đo hiệu năng. Đặc điểm:
- `RunSender()`: bơm giao dịch theo TPS mục tiêu.
- Giao dịch smart-contract: `ContractName`, `FunctionName`, `Payload` (hex).
- **Tái dùng stream (OPT-3):** mỗi worker giữ **một** stream endorsement, gửi tất cả tx của mình trên đó — tránh nghẽn mở stream.
- Đo: số đã gửi, số lỗi, các sự kiện block-commit.
- Tham số: `-orderer <multiaddr> -tps 5000 -duration 30s -workers 16`.

Các module phụ: `sender.go` (gửi), `runner.go` (điều phối), `watcher.go`/`deliver_watch.go` (theo dõi block commit để tính latency), `membership.go` (khám phá Leader), `tx.go` (sinh giao dịch).

## 4. Bắn tải HTTP bằng k6 (`k6/submit-tx.js`)

[k6](https://k6.io/) là công cụ load-test viết bằng JavaScript. Script `submit-tx.js` bắn `POST /api/tx/submit` tới Core Service (qua HTTP) — đo từ góc nhìn **client thật** (Core có chấp nhận giao dịch không, độ trễ HTTP bao nhiêu).

Khác biệt giữa hai công cụ:
| | loadgen (Go) | k6 |
|---|---|---|
| Bắn tới | Orderer (libp2p, bỏ qua Core) | Core Service (HTTP) |
| Đo gì | Throughput consensus thuần | Toàn pipeline submit + độ trễ HTTP |
| Khi nào dùng | Đo trần Raft | Đo trải nghiệm người dùng / E2E |

> **Lưu ý đo lường:** `submit_latency_ms` của k6 (độ trễ HTTP, vài chục ms) **khác** E2E latency (vài giây khi có backlog). Một cái đo "Core chấp nhận", một cái đo "đã ghi sổ". Chi tiết tại [06-benchmark-hieu-nang/](../06-benchmark-hieu-nang/01-benchmark-metrics.md).

## 5. Kết quả thực nghiệm tham khảo

Theo `docs/BENCHMARK_METRICS.md` (5000 req/s × 60s, prefix `k6-rfp-`):
- Submit bền vững ~4691/s, đỉnh ~5017/s.
- Commit bền vững ~3921/s, đỉnh ~6000/s.
- ~989 tx/block (batch orderer ~1000).
- E2E p95 ~7.4s khi tốc độ gửi vượt tốc độ commit (hàng đợi tích lũy backlog).

Đây là dữ liệu để phân tích nghẽn cổ chai và đề xuất cải thiện trong [cai-thien/01-cai-thien-toc-do.md](../cai-thien/01-cai-thien-toc-do.md).

---

## Tổng kết Ordering Service

Ordering Service là phần kỹ thuật sâu nhất: một **Raft tự cài đặt** với bầu Leader theo độ ưu tiên, cắt block hybrid (kích thước + thời gian), giao block bằng streaming push, và sync pull theo đồng thuận `(commitIndex, commitHash)`. Nó đã được tối ưu loạt OPT-1→8 để tiệm cận 5000 TPS, nhưng còn giới hạn ở "1 block in-flight" và "không lưu bền" — những điểm này được phân tích kỹ ở thư mục [cai-thien/](../cai-thien/README.md).
