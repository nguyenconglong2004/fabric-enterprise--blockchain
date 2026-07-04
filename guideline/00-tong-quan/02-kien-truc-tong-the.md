# 2. Kiến trúc tổng thể & Luồng một giao dịch

Phần này cho bạn cái nhìn "từ trên cao" về toàn hệ thống, rồi kể lại hành trình của **một giao dịch** từ lúc người dùng bấm nút đến lúc được ghi vĩnh viễn.

---

## 2.1. Sơ đồ tổng thể

```
                 ┌─────────────────────────────────────────────────────────┐
                 │                     NGƯỜI DÙNG                            │
                 │           (trình duyệt — Blockchain Explorer)            │
                 └───────────────────────────┬─────────────────────────────┘
                                              │ HTTP (REST + SSE)
                                              ▼
        ┌────────────────────────────────────────────────────────────────────┐
        │  CORE SERVICE  (Go)                                  cổng :8080      │
        │  • Nhận giao dịch (/api/tx/submit)                                   │
        │  • Chạy smart contract WASM (wazero) — "execute thử"                │
        │  • Xin Committing Peer ký endorsement                               │
        │  • Lưu mã contract + world state cục bộ (LevelDB)                   │
        └───────┬───────────────────────────────────────────┬────────────────┘
                │ libp2p: endorsement                        │ libp2p: tx-sign
                │ /raft-order-service/endorsement/1.0.0      │ (xin chữ ký)
                ▼                                            ▼
   ┌──────────────────────────────────┐         ┌─────────────────────────────┐
   │ ORDERING SERVICE  (Go) — RAFT    │         │  COMMITTING PEER (ký)        │
   │ • Leader gom tx → block          │         │  ký endorsement bằng Ed25519 │
   │ • Đồng thuận Raft (majority ACK) │         └─────────────────────────────┘
   │ • Cắt block (1000 tx / 100 ms)   │
   └───────┬──────────────────────────┘
           │ libp2p: deliver (stream block)
           │ /raft-order-service/deliver/1.0.0
           ▼
   ┌────────────────────────────────────────────────────────────────────────┐
   │  COMMITTING PEER  (Go)                              metrics :8081        │
   │  • Nhận block đã sắp xếp                                                 │
   │  • Kiểm tra hợp lệ (hash, merkle root, endorsement)                     │
   │  • Ghi block → file append-only (chain.block)                          │
   │  • Cập nhật world state UTXO (LevelDB)                                  │
   │  • Mirror block sang PostgreSQL (bất đồng bộ)                           │
   └───────┬────────────────────────────────────────────────────────────────┘
           │ ghi mirror
           ▼
   ┌────────────────────────────────────────────────────────────────────────┐
   │  POSTGRESQL  (cổng :5432)  — chỉ dùng để TRA CỨU & ĐO LƯỜNG            │
   │  schema: core_service / order_service / commit_peer                    │
   └────────────────────────────────────────────────────────────────────────┘
                              ▲
                              │ Core Service đọc lại để hiển thị Explorer
                              └──────────── (luồng đọc) ──────────────
```

> **Lưu ý quan trọng:** PostgreSQL **không** nằm trong luồng đồng thuận. Nó chỉ là **bản sao (mirror)** để Explorer tra cứu và để đo throughput/latency. Sổ cái "thật" nằm ở file `chain.block` + LevelDB của Committing Peer. Nếu PostgreSQL chết, blockchain vẫn chạy bình thường.

---

## 2.2. Ba tầng trách nhiệm (mô hình Fabric)

Hệ thống chia rõ 3 vai trò, mỗi vai trò một tiến trình riêng. Đây chính là triết lý **tách biệt thực thi — sắp xếp — kiểm tra** (Execute–Order–Validate) của Hyperledger Fabric:

| Tầng | Thành phần | Làm gì | Không làm gì |
|------|------------|--------|--------------|
| **Execute** | Core Service | Chạy contract, xin endorsement | Không quyết định thứ tự |
| **Order** | Ordering Service (Raft) | Quyết định thứ tự, gom block | Không chạy contract, không kiểm tra nội dung |
| **Validate & Commit** | Committing Peer | Kiểm tra & ghi vĩnh viễn | Không sắp xếp lại thứ tự |

Lợi ích của việc tách: mỗi tầng **mở rộng (scale) độc lập**, và Ordering Service rất "nhẹ" vì không cần hiểu nội dung giao dịch — chỉ cần xếp thứ tự.

---

## 2.3. Hành trình một giao dịch (end-to-end)

Giả sử người dùng muốn gọi contract `demo_inventory` để đăng ký 100 sản phẩm SKU `A12`.

### Bước 1 — Tạo & gửi (Frontend → Core Service)
- Người dùng điền form trên **Blockchain Explorer**. Frontend mã hóa dữ liệu thành **payload nhị phân (hex)** và gửi `POST /api/tx/submit` tới Core Service.
- Giao dịch gồm: `txid` (mã định danh duy nhất), `contract_name`, `function_name`, `payload`.

### Bước 2 — Execute thử (Core Service)
- Core Service nạp file WASM của `demo_inventory`, chạy hàm kiểm tra với payload. Nếu contract trả về "thành công" (status = 1) thì tiếp tục; nếu thất bại thì trả lỗi ngay cho người dùng.
- Đây là "chạy thử" — chưa ghi gì vào sổ cái thật.

### Bước 3 — Xin endorsement (Core Service → Committing Peer)
- Core Service mở stream libp2p `tx-sign` tới Committing Peer, gửi giao dịch.
- Committing Peer **ký bằng Ed25519** và trả lại giao dịch có thêm chữ ký xác nhận (endorsement).
- Để nhanh, Core Service giữ sẵn một **connection pool** ấm tới Committing Peer (không mở kết nối mới mỗi lần).

### Bước 4 — Gửi đi sắp xếp (Core Service → Ordering Service)
- Core Service hỏi **Discovery** xem ai đang là **Leader** của cluster Raft, rồi gửi giao dịch (đã có endorsement) qua stream `endorsement` tới Leader.
- Đồng thời ghi thời điểm gửi vào bảng `core_service.tx_submit_times` (phục vụ đo latency).

### Bước 5 — Đồng thuận & cắt block (Ordering Service / Raft)
- Leader gom các giao dịch vào một **TxPool** (hồ chứa). Khi đủ **1000 giao dịch** *hoặc* sau **100 ms**, Leader **cắt một block**.
- Leader tính hash + merkle root, gửi đề xuất block (`BlockProposal`) cho các Follower.
- Khi **đa số Follower xác nhận (ACK)** → Leader **commit** block và gửi `BlockCommit`.

### Bước 6 — Giao block (Ordering Service → Committing Peer)
- Mỗi block vừa commit được đẩy (fan-out) qua stream `deliver` tới mọi Committing Peer đang lắng nghe.

### Bước 7 — Kiểm tra & ghi (Committing Peer)
- Committing Peer **kiểm tra**: hash block đúng không, merkle root khớp không, endorsement có đến từ khóa tin cậy không.
- Nếu hợp lệ: **ghi block** vào file `chain.block` (chỉ ghi nối tiếp, không bao giờ sửa), rồi **cập nhật world state UTXO** trong LevelDB (một thao tác ghi nguyên tử — atomic batch).

### Bước 8 — Mirror & hiển thị
- Committing Peer ghi bản sao block sang **PostgreSQL** một cách **bất đồng bộ** (không chặn luồng chính).
- Core Service đọc PostgreSQL và đẩy cập nhật về trình duyệt qua **SSE** (`/api/explorer/stream`) để Explorer hiển thị block/giao dịch mới gần như tức thì.

---

## 2.4. Tại sao chia tiến trình thay vì một khối lớn?

- **Chịu lỗi (fault tolerance):** Ordering Service nhiều node, chết 1 vẫn chạy nhờ Raft.
- **Mở rộng riêng lẻ:** cần nhiều sức tính contract → thêm Core Service; cần ghi nhanh hơn → thêm Committing Peer.
- **Bảo mật theo lớp:** mỗi tầng chỉ thấy thứ nó cần. Ordering Service không cần biết nội dung giao dịch.
- **Phản ánh đúng thực tế doanh nghiệp:** giống mô hình Hyperledger Fabric mà ngành tài chính/chuỗi cung ứng đang dùng.

---

## 2.5. Hai loại "sổ" trong hệ thống

| | Nơi lưu | Bản chất | Ai ghi |
|---|---------|----------|--------|
| **Chuỗi block (lịch sử)** | File `chain.block` (append-only) | Bất biến, nối bằng hash | Committing Peer |
| **World state (hiện tại)** | LevelDB (`worldstate/`) | Key→Value, ghi đè được | Committing Peer |
| **Mirror tra cứu** | PostgreSQL | Bản sao JSON, có index | Committing Peer (async) + Core Service |

➡️ Tiếp theo: [03-cong-nghe-su-dung.md](03-cong-nghe-su-dung.md)
