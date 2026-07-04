# 3. Công nghệ & thư viện sử dụng

Liệt kê đầy đủ mọi công nghệ, kèm giải thích "vì sao dùng" và liên kết tham khảo. Chia theo từng thành phần.

---

## 3.1. Ngôn ngữ & nền tảng

| Công nghệ | Vai trò | Vì sao chọn | Tham khảo |
|-----------|---------|-------------|-----------|
| **[Go (Golang)](https://go.dev/)** | Ba dịch vụ backend (core, ordering, commit peer) | Biên dịch nhanh, concurrency mạnh nhờ goroutine, phù hợp dịch vụ mạng | [Tour of Go](https://go.dev/tour/) |
| **[React 18](https://react.dev/)** | Giao diện Explorer | Thư viện UI phổ biến nhất, có hooks | [React docs](https://react.dev/learn) |
| **[Node.js](https://nodejs.org/) + [Vite](https://vitejs.dev/)** | Build & dev server frontend | Vite khởi động cực nhanh, hot-reload | [Vite guide](https://vitejs.dev/guide/) |
| **[PostgreSQL 15](https://www.postgresql.org/)** | CSDL mirror & đo lường | Quan hệ mạnh, hỗ trợ JSONB, index linh hoạt | [PostgreSQL docs](https://www.postgresql.org/docs/) |
| **[Docker](https://www.docker.com/)** | Chạy PostgreSQL | Đóng gói nhất quán | [docker-compose.yml](../../docker-compose.yml) |

---

## 3.2. Mạng ngang hàng (P2P)

| Thư viện | Vai trò | Tham khảo |
|----------|---------|-----------|
| **[go-libp2p](https://github.com/libp2p/go-libp2p)** | Toàn bộ giao tiếp giữa các dịch vụ: tạo host, mở stream, định danh peer | [libp2p docs](https://docs.libp2p.io/) |
| **[go-multiaddr](https://github.com/multiformats/go-multiaddr)** | Định dạng địa chỉ tự mô tả `/ip4/.../tcp/.../p2p/...` | [multiaddr spec](https://github.com/multiformats/multiaddr) |
| **[go-yamux](https://github.com/libp2p/go-yamux)** | Ghép nhiều stream trên một kết nối (multiplexing) | — |

> **Vì sao P2P thay vì gRPC/HTTP?** libp2p cho định danh mật mã (PeerID suy ra từ khóa công khai), tự thương lượng giao thức, NAT traversal, và mô hình "nhiều stream song song trên một kết nối" rất hợp cho consensus. Đây cũng là nền của IPFS và Ethereum.

Phiên bản: Core Service dùng libp2p `v0.48.0`; Ordering Service & Committing Peer dùng `v0.32.2`.

---

## 3.3. Đồng thuận (Consensus)

| Công nghệ | Ghi chú |
|-----------|---------|
| **Raft tự cài đặt** | **Không** dùng thư viện ([etcd/raft](https://github.com/etcd-io/raft), [hashicorp/raft](https://github.com/hashicorp/raft)). Toàn bộ thuật toán viết tay trong `orderingservice/source/internal/raft/`. Đây là điểm học thuật trọng tâm. |
| Biến thể | Bầu lãnh đạo **theo độ ưu tiên (priority-based)** thay vì timeout ngẫu nhiên |

Bài báo gốc: [In Search of an Understandable Consensus Algorithm (Raft)](https://raft.github.io/raft.pdf).

---

## 3.4. Máy ảo hợp đồng thông minh (Smart Contract VM)

| Công nghệ | Vai trò | Tham khảo |
|-----------|---------|-----------|
| **[wazero](https://wazero.io/)** `v1.11.0` | Runtime WASM thuần Go (không cần CGo/thư viện C) để chạy contract | [wazero GitHub](https://github.com/tetratelabs/wazero) |
| **[WASI](https://wasi.dev/)** (`wasi_snapshot_preview1`) | "Hệ điều hành ảo" cho contract WASM | [WASI spec](https://github.com/WebAssembly/WASI) |
| **[TinyGo](https://tinygo.org/)** | Biên dịch contract Go → WASM nhỏ gọn | [TinyGo WASI](https://tinygo.org/docs/guides/webassembly/wasi/) |

Lệnh biên dịch (trong `coreservice/contracts/build_wasm.sh`):
```bash
tinygo build -o my_contract.wasm -target wasi -no-debug -scheduler=none ./
```

---

## 3.5. Lưu trữ (Storage)

| Công nghệ | Dùng ở đâu | Lưu gì |
|-----------|------------|--------|
| **[goleveldb](https://github.com/syndtr/goleveldb)** `v1.0.0` (LevelDB thuần Go) | Core Service, Committing Peer | Mã contract WASM, world state, UTXO set |
| **File append-only** (`chain.block`) | Committing Peer | Chuỗi block (mỗi block 1 dòng JSON) |
| **[lib/pq](https://github.com/lib/pq)** `v1.12.3` | Cả 3 dịch vụ | Driver PostgreSQL cho mirror & metrics |

> **Vì sao LevelDB?** Là CSDL key-value nhúng (không cần server riêng), ghi/đọc rất nhanh, hỗ trợ **batch nguyên tử** (ghi nhiều key cùng lúc, hoặc tất cả hoặc không) — cần thiết khi cập nhật UTXO theo block. Tìm hiểu: [LevelDB](https://github.com/google/leveldb).

---

## 3.6. Mật mã (Cryptography)

| Công nghệ | Vai trò | Tham khảo |
|-----------|---------|-----------|
| **[Ed25519](https://ed25519.cr.yp.to/)** (qua `crypto/ed25519` & `golang.org/x/crypto`) | Ký & xác minh giao dịch/endorsement | [RFC 8032](https://datatracker.ietf.org/doc/html/rfc8032) |
| **[SHA-256](https://en.wikipedia.org/wiki/SHA-2)** (double-SHA256) | Hash block & merkle root (giống Bitcoin) | — |
| **[Merkle tree](https://en.wikipedia.org/wiki/Merkle_tree)** | Tóm tắt mọi giao dịch trong block thành một root duy nhất | [Merkle tree](https://en.wikipedia.org/wiki/Merkle_tree) |

---

## 3.7. Frontend (Blockchain Explorer)

| Thư viện | Vai trò | Tham khảo |
|----------|---------|-----------|
| **[React 18.3](https://react.dev/)** | UI framework | — |
| **[React Router 6](https://reactrouter.com/)** | Điều hướng client-side | [docs](https://reactrouter.com/) |
| **[Material-UI (MUI) 6](https://mui.com/)** | Bộ component giao diện | [MUI docs](https://mui.com/material-ui/) |
| **[Tailwind CSS 3](https://tailwindcss.com/)** | CSS tiện ích | [Tailwind docs](https://tailwindcss.com/docs) |
| **[CryptoJS](https://github.com/brix/crypto-js)** | Băm SHA-256 phía client (tạo txid) | — |
| **[Emotion](https://emotion.sh/)** | CSS-in-JS (phụ thuộc của MUI) | — |
| **[@faker-js/faker](https://fakerjs.dev/)** | Sinh dữ liệu giả khi demo | — |
| **[Server-Sent Events (SSE)](https://developer.mozilla.org/en-US/docs/Web/API/Server-sent_events)** | Cập nhật block/giao dịch realtime | [MDN SSE](https://developer.mozilla.org/en-US/docs/Web/API/Server-sent_events) |

---

## 3.8. Kiểm thử tải (Load testing)

| Công cụ | Vai trò | Tham khảo |
|---------|---------|-----------|
| **[k6](https://k6.io/)** | Bắn HTTP POST `/api/tx/submit` với tải cao (script `orderingservice/k6/submit-tx.js`) | [k6 docs](https://k6.io/docs/) |
| **loadgen** (Go, tự viết) | Bắn giao dịch trực tiếp qua libp2p tới Ordering Service ở tốc độ cao (5000 TPS) | `orderingservice/source/pkg/loadgen/` |

---

## 3.9. Các giao thức (Protocol ID) libp2p dùng trong hệ thống

| Protocol ID | Giữa | Mục đích |
|-------------|------|----------|
| `/raft-order-service/1.0.0` | giữa các orderer & client | Thông điệp consensus (heartbeat, bầu cử, membership) |
| `/raft-order-service/endorsement/1.0.0` | Core → Orderer | Gửi giao dịch đã endorse để sắp xếp |
| `/raft-order-service/deliver/1.0.0` | Orderer → Committing Peer | Stream block đã commit |
| `/raft-order-service/sync/1.0.0` | giữa các orderer | Đồng bộ block/log khi node tụt lại |
| `/fabric-enterprise/commit-peer/tx-sign/1.0.0` | Core → Committing Peer | Xin chữ ký endorsement |
| `/commiting-peer/sync/1.0.0` | client → Committing Peer | Truy vấn UTXO theo địa chỉ |

---

## 3.10. Bảng tóm tắt "công nghệ ↔ vấn đề nó giải quyết"

| Vấn đề | Công nghệ |
|--------|-----------|
| Các node nói chuyện với nhau an toàn | libp2p + Ed25519 |
| Đồng ý cùng một thứ tự giao dịch | Raft (tự cài) |
| Chạy logic nghiệp vụ an toàn, đa ngôn ngữ | WASM (wazero + TinyGo) |
| Lưu trạng thái nhanh, nhúng sẵn | LevelDB |
| Dữ liệu không thể sửa | File append-only + hash chain |
| Tra cứu & báo cáo linh hoạt | PostgreSQL |
| Hiển thị realtime cho người dùng | React + SSE |
| Đo hiệu năng dưới tải | k6 + loadgen |

➡️ Tiếp theo: đi sâu từng dịch vụ — bắt đầu với [01-coreservice/](../01-coreservice/01-vai-tro-kien-truc.md)
