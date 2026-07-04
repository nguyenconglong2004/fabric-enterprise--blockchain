# Báo cáo khóa luận — Hệ thống Blockchain doanh nghiệp (Fabric Enterprise Blockchain)

> Báo cáo kỹ thuật toàn diện cho dự án xây dựng **một nền tảng blockchain doanh nghiệp từ đầu (from-scratch)**, lấy cảm hứng từ kiến trúc của [Hyperledger Fabric](https://www.hyperledger.org/projects/fabric).

Tài liệu này được viết với mục tiêu: **một người chỉ có kiến thức căn bản về khoa học máy tính** (biết lập trình, hiểu khái niệm hàm băm, mạng máy tính, cơ sở dữ liệu ở mức cơ bản) vẫn có thể đọc và hiểu được toàn bộ hệ thống. Mỗi khái niệm chuyên sâu đều có **liên kết tham chiếu** để bạn tự tìm hiểu thêm.

---

## Hệ thống gồm những gì?

Dự án mô phỏng một mạng blockchain "kiểu doanh nghiệp" (permissioned blockchain — blockchain có cấp phép) với 4 thành phần lớn chạy độc lập, giao tiếp với nhau qua mạng ngang hàng (peer-to-peer):

| # | Thành phần | Ngôn ngữ | Vai trò ngắn gọn |
|---|------------|----------|------------------|
| 1 | **Core Service** (`coreservice/`) | Go | Cổng vào hệ thống: nhận giao dịch, chạy hợp đồng thông minh (smart contract) dạng WebAssembly, xin chữ ký xác nhận (endorsement) |
| 2 | **Ordering Service** (`orderingservice/`) | Go | Sắp xếp thứ tự giao dịch bằng **thuật toán đồng thuận Raft**, gom thành block, gửi xuống peer |
| 3 | **Committing Peer** (`commitingpeer/`) | Go | Nhận block đã sắp xếp, kiểm tra hợp lệ, ghi vào sổ cái (ledger) và cập nhật trạng thái thế giới (world state) |
| 4 | **Blockchain Explorer** (`BlockchainExplorer-FrontEnd/`) | React | Giao diện web để xem block, giao dịch và gửi giao dịch mới |

Một cơ sở dữ liệu **PostgreSQL** đứng chung làm nơi lưu bản sao (mirror) dữ liệu phục vụ tra cứu và đo lường hiệu năng.

> **Phạm vi báo cáo:** Theo yêu cầu, báo cáo **bỏ qua** phần *orchestrator* và *web UI* nằm trong `orderingservice/` (đây là công cụ quản trị/giám sát cluster, không thuộc luồng xử lý chính).

---

## Cấu trúc thư mục báo cáo

```
report/
├── README.md                          ← (bạn đang đọc) Mục lục & dẫn nhập
│
├── 00-tong-quan/                      Nền tảng lý thuyết & kiến trúc tổng thể
│   ├── 01-khai-niem-nen-tang.md      Blockchain, đồng thuận, smart contract... là gì
│   ├── 02-kien-truc-tong-the.md      Sơ đồ toàn hệ thống & luồng một giao dịch
│   └── 03-cong-nghe-su-dung.md       Liệt kê & giải thích mọi công nghệ/thư viện
│
├── 01-coreservice/                   Cổng vào & máy ảo hợp đồng thông minh
│   ├── 01-vai-tro-kien-truc.md
│   ├── 02-wasm-smart-contract.md
│   ├── 03-crypto-endorsement.md
│   ├── 04-networking-discovery.md
│   ├── 05-luu-tru-state.md
│   └── 06-api-va-metrics.md
│
├── 02-orderingservice/               Tầng đồng thuận Raft (phần trọng tâm)
│   ├── 01-vai-tro.md
│   ├── 02-raft-tong-quan.md
│   ├── 03-bau-lanh-dao-heartbeat.md
│   ├── 04-nhan-ban-log-va-cat-block.md
│   ├── 05-deliver-va-dong-bo.md
│   ├── 06-networking.md
│   └── 07-loadgen-va-client.md
│
├── 03-commitingpeer/                 Tầng ghi sổ cái & trạng thái
│   ├── 01-vai-tro.md
│   ├── 02-deliver-protocol.md
│   ├── 03-validation.md
│   ├── 04-luu-tru-va-worldstate.md
│   └── 05-metrics.md
│
├── 04-frontend-explorer/             Giao diện web
│   ├── 01-tong-quan-tech-stack.md
│   ├── 02-cac-component.md
│   ├── 03-binary-payload.md
│   └── 04-realtime-sse.md
│
├── 05-co-so-du-lieu/                 Lược đồ PostgreSQL
│   └── 01-postgres-schema.md
│
├── 06-benchmark-hieu-nang/           Cách đo throughput & latency
│   └── 01-benchmark-metrics.md
│
└── cai-thien/                        ⭐ Đề xuất cải thiện (tốc độ / lưu trữ / bảo mật)
    ├── README.md
    ├── 01-cai-thien-toc-do.md
    ├── 02-cai-thien-luu-tru.md
    └── 03-cai-thien-bao-mat.md
```

---

## Nên đọc theo thứ tự nào?

1. **Nếu bạn mới hoàn toàn:** đọc lần lượt `00-tong-quan/` từ file 01 → 03. Đây là nền tảng để hiểu mọi phần sau.
2. **Nếu muốn hiểu luồng dữ liệu:** đọc [02-kien-truc-tong-the.md](00-tong-quan/02-kien-truc-tong-the.md), nó kể chuyện một giao dịch đi từ lúc người dùng bấm nút đến lúc được ghi vĩnh viễn.
3. **Đi sâu từng dịch vụ:** đọc các thư mục `01-` → `04-`.
4. **Quan tâm hiệu năng / điểm yếu:** đọc `06-benchmark-hieu-nang/` rồi `cai-thien/`.

---

## Bản đồ "Hyperledger Fabric thật" vs "dự án này"

Dự án mô phỏng lại đúng triết lý tách 3 vai trò của Fabric ([tài liệu Fabric về kiến trúc](https://hyperledger-fabric.readthedocs.io/en/latest/arch-deep-dive.html)):

| Vai trò trong Fabric | Thành phần trong dự án | Ghi chú |
|----------------------|------------------------|---------|
| **Endorsing Peer** (peer xác nhận) | Core Service + Committing Peer (ký endorsement) | Chạy smart contract, ký xác nhận kết quả |
| **Ordering Service** (orderer) | Ordering Service (Raft) | Fabric thật dùng [etcd/raft](https://github.com/etcd-io/raft); dự án **tự viết Raft từ đầu** |
| **Committing Peer** (peer ghi sổ) | Committing Peer | Kiểm tra & ghi block vào ledger + world state |

Điểm khác biệt học thuật đáng chú ý: dự án **tự cài đặt thuật toán đồng thuận Raft** (không dùng thư viện), với một biến thể **bầu lãnh đạo theo độ ưu tiên (priority-based)** thay vì timeout ngẫu nhiên. Chi tiết tại [02-orderingservice/](02-orderingservice/).
