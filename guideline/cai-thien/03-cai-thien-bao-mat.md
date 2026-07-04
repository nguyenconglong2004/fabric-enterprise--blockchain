# Cải thiện Bảo mật (Security)

> Liên quan: [03-commitingpeer/03-validation.md](../03-commitingpeer/03-validation.md), [02-orderingservice/03-bau-lanh-dao-heartbeat.md](../02-orderingservice/03-bau-lanh-dao-heartbeat.md)
> Nguồn: `docs/leader-election-analysis.md`, đọc mã `init.sql`, `docker-compose.yml`

Phần này nêu các điểm yếu bảo mật theo thứ tự ưu tiên. Một số là **rủi ro vận hành**, một số là **rủi ro an toàn đồng thuận**.

---

## 1. 🔴 Thông tin nhạy cảm hardcode

**Hiện tại:**
- Mật khẩu PostgreSQL `fabric:fabric123` xuất hiện **trong `docker-compose.yml`, `init.sql`, và làm giá trị mặc định** trong chuỗi kết nối của cả ba dịch vụ Go.
- `sslmode=disable` trong chuỗi kết nối DB.

**Rủi ro:** lộ credential khi đẩy lên git; kết nối DB không mã hóa.

**Khắc phục:**
- Đưa secret vào **biến môi trường / secret manager** ([Docker secrets](https://docs.docker.com/engine/swarm/secrets/), [Vault](https://www.vaultproject.io/)), không commit.
- Bật **TLS cho PostgreSQL** (`sslmode=require` trở lên).
- Đổi mật khẩu mặc định, dùng tài khoản quyền tối thiểu (least privilege).

---

## 2. 🔴 Giao tiếp P2P chưa mã hóa/xác thực mạnh

**Hiện tại:** các dịch vụ giao tiếp qua libp2p stream với message JSON. libp2p có hỗ trợ kênh bảo mật (Noise/TLS) nhưng **không thấy cấu hình bắt buộc** trong mã; cũng chưa có **allowlist PeerID** (chỉ chấp peer được phép tham gia).

**Rủi ro:** với mạng permissioned, kẻ lạ kết nối vào có thể gửi giao dịch/heartbeat giả, hoặc nghe lén nếu kênh không mã hóa.

**Khắc phục:**
- Bật **[libp2p Noise](https://docs.libp2p.io/concepts/secure-comm/noise/)** hoặc TLS cho mọi kết nối (mặc định nên bật).
- **Xác thực thành viên (mTLS / PeerID allowlist):** chỉ PeerID trong danh sách membership được mở stream consensus. Đây là tinh thần [Membership Service Provider (MSP)](https://hyperledger-fabric.readthedocs.io/en/latest/membership/membership.html) của Fabric.
- Ký & xác minh **message consensus** (heartbeat, leader claim, block proposal) — hiện chỉ giao dịch mới có endorsement; message điều khiển Raft có thể bị giả mạo.

---

## 3. 🔴 Raft không lưu bền → mất an toàn đồng thuận

**Hiện tại:** `currentTerm`, `votedFor`, log đều trong RAM (xem [02-cai-thien-luu-tru.md](02-cai-thien-luu-tru.md) #1).

**Rủi ro an toàn (safety):** Raft chuẩn yêu cầu persist `term`/`votedFor` để sau restart không bầu nhầm hai Leader cùng term, không "quên" đã commit gì. Thiếu nó, một số kịch bản crash–restart có thể vi phạm **tính an toàn** (hai bản sổ cái phân kỳ), không chỉ mất dữ liệu.

**Khắc phục:** persist trước khi ACK (WAL) — xem [02-cai-thien-luu-tru.md](02-cai-thien-luu-tru.md).

---

## 4. 🟠 Validation bỏ ngỏ ở Committing Peer

**Hiện tại** (xem [03-commitingpeer/03-validation.md](../03-commitingpeer/03-validation.md)):
- Kiểm tra **liên tục chuỗi prevHash bị comment** — không bắt buộc trên hot-path.
- `ValidateTransaction()` mức từng giao dịch là **stub** → chưa chống **double-spend** (tiêu một UTXO hai lần), chưa kiểm chữ ký input UTXO.
- Hash-chain chỉ được xác minh trong luồng **sync** của orderer (SYNC-5), không phải khi commit.

**Rủi ro:** một block với chuỗi prevHash sai, hoặc giao dịch double-spend, có thể được ghi nếu lọt qua tầng sắp xếp.

**Khắc phục:**
- Bật lại **kiểm tra prevHash chain** khi commit (đảm bảo `block.PrevHash == lastCommittedHash`).
- Cài **kiểm tra double-spend**: trước khi `ApplyBlock`, xác nhận mọi `VIN` trỏ tới UTXO **đang tồn tại** và chưa bị tiêu trong cùng block.
- Xác minh **chữ ký input UTXO** (không chỉ endorsement contract).

---

## 5. 🟠 Endorsement policy còn đơn giản

**Hiện tại:** validation chấp nhận giao dịch nếu có **≥1** endorsement từ tập khóa tin cậy (`TRUSTED_ENDORSER_PUBLIC_KEYS`).

**Rủi ro:** "1 trên N" yếu — một endorser bị xâm nhập là đủ qua mặt.

**Khắc phục:** hỗ trợ **chính sách endorsement linh hoạt** kiểu Fabric (vd. "M trên N", "AND/OR theo tổ chức"). Tham khảo: [Fabric endorsement policies](https://hyperledger-fabric.readthedocs.io/en/latest/endorsement-policies.html).

---

## 6. 🟠 Cô lập & giới hạn tài nguyên smart contract

**Hiện tại:** contract chạy WASM trong wazero (đã sandbox tốt). Nhưng chưa thấy **giới hạn gas/CPU/bộ nhớ/timeout** rõ ràng cho mỗi lần chạy.

**Rủi ro:** contract độc/lỗi (vòng lặp vô hạn, cấp phát bộ nhớ lớn) có thể làm cạn tài nguyên Core Service (DoS).

**Khắc phục:**
- Đặt **timeout thực thi** và **giới hạn bộ nhớ** cho mỗi lần gọi WASM (wazero hỗ trợ `context` + giới hạn memory).
- Cân nhắc **đo gas** (đếm lệnh) để chặn contract chạy quá lâu — giống Ethereum.

---

## 7. 🟢 Khác

- **Phòng replay giao dịch:** đảm bảo `txid` chống trùng lặp (replay) — kiểm tra tx đã commit không được commit lại.
- **Rate limiting** trên `/api/tx/submit` để chống spam.
- **Audit log** cho thao tác deploy contract & thay đổi membership.
- **CORS/headers** cho Core Service khi chạy production (hiện dev proxy bỏ qua CORS).

---

## Bảng tóm tắt ưu tiên

| Mức | Vấn đề | Khắc phục cốt lõi |
|-----|--------|-------------------|
| 🔴 | Secret hardcode, DB không TLS | Secret manager + `sslmode=require` |
| 🔴 | P2P chưa mã hóa/xác thực | Noise/TLS + PeerID allowlist + ký message Raft |
| 🔴 | Raft RAM-only (an toàn) | WAL persist term/log |
| 🟠 | Double-spend & prevHash chưa kiểm | Bật validate đầy đủ trên commit |
| 🟠 | Endorsement "1/N" yếu | Chính sách M/N |
| 🟠 | WASM chưa giới hạn tài nguyên | Timeout + memory limit + gas |
| 🟢 | Replay, rate limit, audit, CORS | Bổ sung lớp phòng thủ vận hành |

> Ghi chú: nhiều điểm trên là **đánh đổi có chủ đích** trong phạm vi khóa luận (ưu tiên minh họa thuật toán hơn là cứng hóa production). Báo cáo nêu ra để thể hiện hiểu biết về khoảng cách giữa **prototype học thuật** và **hệ thống production**.
