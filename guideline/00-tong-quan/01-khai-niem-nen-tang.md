# 1. Các khái niệm nền tảng

Phần này giải thích mọi khái niệm bạn cần biết **trước khi** đọc các phần kỹ thuật. Nếu bạn đã quen blockchain, có thể đọc lướt và quay lại khi cần.

---

## 1.1. Blockchain là gì?

**Blockchain** (chuỗi khối) là một loại **sổ cái phân tán** (distributed ledger): một danh sách các bản ghi (giao dịch) được nhóm thành các **block** (khối), mỗi block liên kết với block trước bằng **hàm băm mật mã** (cryptographic hash). Vì mỗi block "chứa dấu vân tay" của block trước, nên muốn sửa một block cũ thì phải tính lại tất cả block sau nó — điều này khiến dữ liệu gần như **không thể chỉnh sửa** (immutable).

```
Block 1  ──hash──▶  Block 2  ──hash──▶  Block 3  ──hash──▶  ...
[prev: 0000]        [prev: hash(B1)]    [prev: hash(B2)]
```

- **Hàm băm (hash):** hàm biến dữ liệu bất kỳ thành một chuỗi cố định (ví dụ 32 byte). Đặc tính quan trọng: đổi 1 bit dữ liệu → hash đổi hoàn toàn, và không thể "đảo ngược". Dự án dùng [SHA-256](https://en.wikipedia.org/wiki/SHA-2) (cụ thể là double-SHA256, giống Bitcoin).
- Tìm hiểu thêm: [Blockchain — Wikipedia](https://en.wikipedia.org/wiki/Blockchain).

### Public blockchain vs. Permissioned blockchain

| | Public (công khai) | Permissioned (có cấp phép) |
|---|---|---|
| Ví dụ | Bitcoin, Ethereum | Hyperledger Fabric, **dự án này** |
| Ai được tham gia? | Bất kỳ ai | Chỉ thành viên được cấp phép |
| Đồng thuận | Tốn năng lượng (Proof-of-Work) | Nhẹ hơn (Raft, PBFT...) |
| Định danh | Ẩn danh | Có danh tính rõ ràng (qua khóa công khai) |

Dự án thuộc nhóm **permissioned** — phù hợp doanh nghiệp: các node biết nhau, dùng chữ ký số để xác thực thay vì "đào" tốn điện.

---

## 1.2. Giao dịch (Transaction) và mô hình trạng thái

Một **giao dịch** là yêu cầu thay đổi trạng thái sổ cái. Dự án này hỗ trợ **hai mô hình** giao dịch cùng lúc:

### a) Mô hình UTXO (giống Bitcoin)
**UTXO** = *Unspent Transaction Output* (đầu ra giao dịch chưa tiêu). Thay vì lưu "số dư tài khoản", hệ thống lưu danh sách các "tờ tiền" chưa tiêu. Mỗi giao dịch:
- **Vin** (inputs): tiêu các UTXO cũ.
- **Vout** (outputs): tạo các UTXO mới (gán cho người nhận).

Tìm hiểu thêm: [UTXO model](https://en.wikipedia.org/wiki/Unspent_transaction_output).

### b) Mô hình hợp đồng thông minh (giống Ethereum/Fabric)
Giao dịch gọi một **smart contract** với:
- `contract_name`: tên hợp đồng.
- `function_name`: hàm cần gọi.
- `payload`: dữ liệu đầu vào (mã hóa nhị phân/hex).

Hợp đồng chạy và ghi kết quả vào **world state** (trạng thái thế giới — xem 1.5).

---

## 1.3. Smart Contract & WebAssembly (WASM)

**Smart contract** (hợp đồng thông minh) là một đoạn chương trình tự động chạy trên blockchain khi có giao dịch gọi tới. Trong dự án này, smart contract được biên dịch sang **[WebAssembly (WASM)](https://webassembly.org/)** — một định dạng mã máy ảo, nhỏ gọn, chạy nhanh và **cô lập an toàn (sandbox)**.

- Vì sao WASM? Vì nó **đa ngôn ngữ** (viết bằng Go, Rust, C...), **an toàn** (không truy cập được hệ thống ngoài quyền cho phép), và **xác định (deterministic)** — chạy cùng đầu vào luôn ra cùng kết quả, điều bắt buộc với blockchain.
- Dự án biên dịch contract bằng [TinyGo](https://tinygo.org/) (trình biên dịch Go cho WASM), chạy bằng [wazero](https://wazero.io/) (runtime WASM thuần Go, không cần thư viện ngoài).
- Hệ điều hành ảo cho WASM: [WASI](https://wasi.dev/) (WebAssembly System Interface).

Chi tiết tại [01-coreservice/02-wasm-smart-contract.md](../01-coreservice/02-wasm-smart-contract.md).

---

## 1.4. Đồng thuận (Consensus) và thuật toán Raft

Khi có nhiều node, phải có cách để **tất cả đồng ý về cùng một thứ tự giao dịch** — đây là bài toán **đồng thuận (consensus)**. Nếu không, mỗi node sẽ có một bản sổ cái khác nhau.

Dự án dùng **[Raft](https://raft.github.io/)** — thuật toán đồng thuận nổi tiếng vì dễ hiểu (so với [Paxos](https://en.wikipedia.org/wiki/Paxos_(computer_science))). Ý tưởng cốt lõi của Raft:

1. **Bầu một Leader (lãnh đạo):** chỉ Leader được quyền đề xuất thứ tự. Các node còn lại là **Follower**.
2. **Nhân bản log:** Leader ghi giao dịch vào "nhật ký" (log) rồi gửi cho Follower. Khi **đa số (majority)** node xác nhận đã ghi → giao dịch được **commit** (chốt).
3. **Heartbeat:** Leader định kỳ gửi tín hiệu "tôi còn sống". Nếu Follower lâu không nghe thấy → cho rằng Leader chết → bầu Leader mới.
4. **Term (nhiệm kỳ):** mỗi lần bầu cử tăng một số đếm "term", giúp phân biệt Leader cũ/mới và tránh xung đột.

> **Đa số (majority/quorum):** với N node, majority = N/2 + 1. Ví dụ 3 node cần 2, 5 node cần 3. Nhờ vậy hệ thống chịu lỗi được tối đa (N-1)/2 node chết mà vẫn hoạt động — gọi là **fault tolerance**.

Bài báo gốc Raft (rất dễ đọc): [In Search of an Understandable Consensus Algorithm](https://raft.github.io/raft.pdf). Mô phỏng trực quan: [thesecretlivesofdata.com/raft](https://thesecretlivesofdata.com/raft/).

**Điểm đặc biệt của dự án:** thay vì bầu cử bằng timeout ngẫu nhiên như Raft chuẩn, dự án dùng **bầu cử theo độ ưu tiên (priority-based)**: node nào tham gia cluster sớm hơn có độ ưu tiên cao hơn và được chọn làm Leader một cách **xác định (deterministic)**. Chi tiết tại [02-orderingservice/03-bau-lanh-dao-heartbeat.md](../02-orderingservice/03-bau-lanh-dao-heartbeat.md).

---

## 1.5. World State (Trạng thái thế giới)

Đọc cả chuỗi block để biết "số dư hiện tại" thì rất chậm. Nên blockchain thường giữ thêm một **world state** — bản chụp trạng thái mới nhất (key → value). Ví dụ: tài sản `Asset_001` đang có màu gì, kho `Inv_A12` còn bao nhiêu hàng, UTXO nào chưa tiêu.

- Block = lịch sử bất biến (history).
- World state = ảnh chụp hiện tại (snapshot), tính được từ lịch sử.

Dự án lưu world state bằng **[LevelDB](https://github.com/google/leveldb)** — một cơ sở dữ liệu key-value nhúng, rất nhanh. Tìm hiểu thêm: [world state trong Fabric](https://hyperledger-fabric.readthedocs.io/en/latest/ledger/ledger.html).

---

## 1.6. Mật mã khóa công khai & Chữ ký số

Để xác thực "ai gửi giao dịch" mà không cần mật khẩu, blockchain dùng **mật mã khóa công khai (public-key cryptography)**:
- Mỗi người có một cặp khóa: **khóa riêng (private key)** giữ bí mật, **khóa công khai (public key)** chia sẻ.
- **Ký (sign):** dùng khóa riêng tạo "chữ ký" trên dữ liệu.
- **Xác minh (verify):** ai cũng dùng khóa công khai để kiểm tra chữ ký có hợp lệ không, mà không cần biết khóa riêng.

Dự án dùng **[Ed25519](https://ed25519.cr.yp.to/)** — một hệ chữ ký số hiện đại, nhanh và an toàn (đường cong Edwards). Tìm hiểu: [Ed25519 — Wikipedia](https://en.wikipedia.org/wiki/EdDSA#Ed25519).

---

## 1.7. Endorsement (Xác nhận) — luồng kiểu Fabric

Trong Fabric (và dự án này), một giao dịch không được "ghi ngay". Nó đi qua mô hình **Execute → Order → Validate** (chạy → sắp xếp → kiểm tra):

1. **Execute (chạy thử + endorse):** Core Service chạy smart contract *thử* (chưa ghi), rồi xin một hoặc nhiều peer **ký xác nhận (endorsement)** rằng "tôi cũng chạy ra kết quả này".
2. **Order (sắp xếp):** giao dịch đã có chữ ký xác nhận được gửi lên Ordering Service để chốt thứ tự và gom vào block.
3. **Validate & Commit (kiểm tra + ghi):** Committing Peer nhận block, kiểm tra chữ ký xác nhận có hợp lệ và đủ tin cậy không, rồi mới ghi.

Mô hình này gọi là **Execute-Order-Validate**, khác với **Order-Execute** của Ethereum. Ưu điểm: chạy contract song song được, không cần mọi node chạy lại mọi contract. Tìm hiểu: [Fabric architecture](https://hyperledger-fabric.readthedocs.io/en/latest/arch-deep-dive.html).

---

## 1.8. Mạng ngang hàng (P2P) với libp2p

Các thành phần không gọi nhau qua HTTP thông thường mà qua **mạng ngang hàng (peer-to-peer)** dùng thư viện **[libp2p](https://libp2p.io/)** (chính là nền mạng của [IPFS](https://ipfs.tech/) và Ethereum 2.0).

Khái niệm cần biết:
- **PeerID:** định danh duy nhất của mỗi node, suy ra từ khóa công khai.
- **Multiaddr:** một định dạng địa chỉ "tự mô tả", ví dụ `/ip4/127.0.0.1/tcp/6000/p2p/12D3KooW...`. Đọc từ trái qua phải: dùng IPv4, địa chỉ này, qua TCP, cổng 6000, tới peer có ID này. Tìm hiểu: [multiaddr](https://github.com/multiformats/multiaddr).
- **Stream:** một "kênh" dữ liệu hai chiều mở trên kết nối giữa hai peer. Nhiều stream có thể chạy song song trên một kết nối (nhờ **multiplexing**, dự án dùng [yamux](https://github.com/hashicorp/yamux)).
- **Protocol ID:** chuỗi định danh loại stream, ví dụ `/raft-order-service/deliver/1.0.0`.

Chi tiết tại [00-tong-quan/03-cong-nghe-su-dung.md](03-cong-nghe-su-dung.md).

---

## 1.9. Throughput & Latency (Thông lượng & Độ trễ)

Hai chỉ số đo hiệu năng quan trọng:
- **Throughput (thông lượng):** số giao dịch xử lý được mỗi giây — đơn vị **TPS** (transactions per second). Mục tiêu của dự án: ~5000 TPS.
- **Latency (độ trễ):** thời gian từ lúc gửi giao dịch đến lúc nó được ghi vĩnh viễn (gọi là **end-to-end / E2E latency**).

Hai chỉ số này thường **đánh đổi (trade-off)**: gom nhiều giao dịch vào một block (batch lớn) → throughput cao nhưng latency có thể tăng khi quá tải. Chi tiết tại [06-benchmark-hieu-nang/](../06-benchmark-hieu-nang/01-benchmark-metrics.md).

---

## Tóm tắt thuật ngữ nhanh

| Thuật ngữ | Nghĩa ngắn |
|-----------|-----------|
| Block | Khối chứa nhiều giao dịch, nối nhau bằng hash |
| Hash | Dấu vân tay số của dữ liệu (SHA-256) |
| Consensus | Cơ chế để các node đồng ý cùng thứ tự |
| Raft | Thuật toán đồng thuận dựa trên bầu Leader |
| Leader / Follower | Node chỉ huy / node tuân theo trong Raft |
| Term | Số nhiệm kỳ, tăng mỗi lần bầu cử |
| Commit | Chốt vĩnh viễn một block/giao dịch |
| UTXO | Tiền chưa tiêu (mô hình kiểu Bitcoin) |
| Smart contract | Chương trình chạy trên blockchain (ở đây là WASM) |
| World state | Ảnh chụp trạng thái hiện tại (LevelDB) |
| Endorsement | Chữ ký xác nhận kết quả chạy contract |
| Ed25519 | Hệ chữ ký số dùng trong dự án |
| libp2p | Thư viện mạng ngang hàng |
| TPS | Giao dịch mỗi giây (đo throughput) |

➡️ Tiếp theo: [02-kien-truc-tong-the.md](02-kien-truc-tong-the.md)
