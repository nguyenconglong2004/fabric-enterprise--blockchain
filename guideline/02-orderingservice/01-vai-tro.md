# Ordering Service — Vai trò & Tổng quan

> Mã nguồn: `orderingservice/source/` · Ngôn ngữ: Go
> **Phạm vi báo cáo:** chỉ phần consensus/ordering/networking. **Bỏ qua** `internal/orchestrator/`, `cmd/orchestrator/`, `web/` (công cụ quản trị/giám sát).

## 1. Ordering Service là gì?

**Ordering Service** (dịch vụ sắp xếp) là **trái tim đồng thuận** của hệ thống. Nhiệm vụ: nhận các giao dịch (đã được Core Service endorse), **quyết định thứ tự** của chúng, **gom thành block**, rồi **giao block** xuống các Committing Peer.

Đây là phần học thuật trọng tâm của khóa luận vì nó **tự cài đặt thuật toán đồng thuận Raft từ đầu** (không dùng thư viện), với một biến thể **bầu lãnh đạo theo độ ưu tiên**.

Tương ứng vai trò **Orderer** trong Hyperledger Fabric (Fabric thật dùng [etcd/raft](https://github.com/etcd-io/raft); ở đây viết tay).

## 2. Vì sao cần sắp xếp thứ tự?

Trong hệ phân tán, nhiều giao dịch đến gần như đồng thời từ nhiều nguồn. Nếu mỗi node tự quyết thứ tự, các bản sao sổ cái sẽ khác nhau → mất tính nhất quán. Ordering Service đảm bảo **mọi node thấy cùng một thứ tự giao dịch duy nhất** — gọi là **total order** (thứ tự toàn phần). Đây là điều kiện tiên quyết để world state hội tụ giống nhau ở mọi nơi.

## 3. Cấu trúc thư mục (phần trong phạm vi)

```
orderingservice/source/
├── cmd/
│   ├── server/main.go        ← chạy một node orderer
│   ├── client/main.go        ← ví/CLI tương tác (UTXO, sync)
│   └── loadgen/main.go       ← công cụ bắn tải (load test)
├── internal/
│   ├── raft/                 ← ⭐ TOÀN BỘ THUẬT TOÁN RAFT (tự viết)
│   │   ├── node.go             trạng thái node, khởi động, join cluster
│   │   ├── config.go           tham số tinh chỉnh (heartbeat, batch...)
│   │   ├── consensus.go        điều phối chung, định tuyến message
│   │   ├── leader.go           bầu lãnh đạo theo độ ưu tiên
│   │   ├── heartbeat.go        gửi/nhận heartbeat, phát hiện Leader chết
│   │   ├── membership.go       quản lý danh sách thành viên cluster
│   │   ├── transaction.go      gom tx → block, propose, ACK, commit
│   │   ├── deliver.go          giao block xuống Committing Peer
│   │   ├── sync.go             đồng bộ block khi node tụt lại (pull)
│   │   ├── sync_server.go      phục vụ block cho node khác sync
│   │   ├── endorsement.go      nhận tx từ Core Service
│   │   └── events.go           sự kiện nội bộ
│   ├── network/
│   │   ├── transport.go        libp2p host, gửi/broadcast message
│   │   └── protocol.go         Protocol ID + hằng số thời gian
│   ├── api/server.go         ← HTTP: /api/leader, /api/membership...
│   └── types/                ← kiểu dữ liệu: block, transaction, message...
└── pkg/
    ├── client/client.go        thư viện client
    └── loadgen/                bộ máy bắn tải
```

## 4. Các tài liệu nội bộ đáng giá

Trong `orderingservice/docs/` có sẵn các phân tích thiết kế (đã được phản ánh vào báo cáo này):
- `leader-election-analysis.md` — phân tích bầu cử & các tình huống lỗi.
- `heartbeat.md` — cơ chế heartbeat.
- `block-speed-optimization-analysis.md` — các tối ưu tốc độ cắt block (OPT-1 → OPT-8).
- `scenarios/` — các kịch bản kiểm thử lỗi (timeout, Leader crash...).

## 5. Đọc tiếp theo chủ đề

| Chủ đề | File |
|--------|------|
| Tổng quan Raft trong dự án | [02-raft-tong-quan.md](02-raft-tong-quan.md) |
| Bầu lãnh đạo & heartbeat | [03-bau-lanh-dao-heartbeat.md](03-bau-lanh-dao-heartbeat.md) |
| Nhân bản log & cắt block | [04-nhan-ban-log-va-cat-block.md](04-nhan-ban-log-va-cat-block.md) |
| Giao block & đồng bộ | [05-deliver-va-dong-bo.md](05-deliver-va-dong-bo.md) |
| Mạng libp2p | [06-networking.md](06-networking.md) |
| Loadgen & client | [07-loadgen-va-client.md](07-loadgen-va-client.md) |

## 6. Thư viện phụ thuộc (`source/go.mod`)

| Thư viện | Vai trò |
|----------|---------|
| `github.com/libp2p/go-libp2p v0.32.2` | Mạng ngang hàng |
| `github.com/multiformats/go-multiaddr v0.12.0` | Địa chỉ multiaddr |
| `github.com/chzyer/readline v1.5.1` | CLI cho client/server |
| `golang.org/x/crypto v0.23.0` | Ed25519, hash |

**Không** dùng thư viện Raft nào — toàn bộ thuật toán tự viết.
