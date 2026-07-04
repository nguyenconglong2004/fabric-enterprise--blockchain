# Committing Peer — Kiểm tra hợp lệ (Validation)

> Mã nguồn: `commitingpeer/source/internal/validation/engine.go`, `internal/crypto/keys.go`

Trước khi ghi một block, Committing Peer phải chắc nó hợp lệ. Đây là "lớp gác cổng" cuối cùng — tương ứng pha **Validate** trong mô hình Execute–Order–Validate của Fabric.

## 1. Cấu hình endorser tin cậy

`Engine` được khởi tạo với một danh sách **khóa công khai Ed25519 tin cậy** (chuỗi hex, phân tách dấu phẩy — từ `TRUSTED_ENDORSER_PUBLIC_KEYS`). Nếu danh sách khác rỗng, mọi giao dịch smart-contract **bắt buộc** phải mang endorsement hợp lệ từ ít nhất một khóa tin cậy.

## 2. `ValidateBlock()` — kiểm tra ở mức block

Gồm hai phần:

### a) Toàn vẹn cấu trúc (`verifyBlockIntegrity()`)
- Block trên đường truyền phải có `merkle_root` và `hash` khác rỗng.
- **Tính lại Merkle root** từ các `txid` của giao dịch → so với `MerkleRoot` của block.
- **Tính lại block hash** (double-SHA256 của header: timestamp, nonce, prevHash, merkleRoot) → so với `Hash` của block.
- Nếu lệch → block bị từ chối.

> Việc tính lại độc lập đảm bảo block **không bị sửa đổi** trên đường truyền và nội dung khớp với "dấu vân tay" của nó. Hàm hash/merkle phải **khớp y hệt** cách Ordering Service tính (`crypto/keys.go`).

### b) Kiểm tra endorsement (`validateEndorsedTx()`)
Với mỗi giao dịch smart-contract (có `ContractName` và `Payload` khác rỗng):
- Phải có mảng `Endorsements[]` không rỗng.
- **Xác minh từng chữ ký**: `crypto.VerifyTransaction(txid, contractName, payload, sig, pubkey)`.
- Ít nhất một khóa endorser phải nằm trong **tập tin cậy**.

## 3. Hàm mật mã (`crypto/keys.go`)

| Hàm | Vai trò |
|-----|---------|
| Tạo/suy khóa Ed25519 | Sinh cặp khóa, suy public từ private |
| `Sign` / `Verify` | Ký & xác minh chữ ký |
| Hash block (double-SHA256) | Tính `Hash` từ header |
| `ComputeMerkleRoot` | Dựng cây Merkle từ danh sách `txid` |
| Verify block hash / merkle | So sánh giá trị tính lại với giá trị trên block |

## 4. Những gì hiện **chưa** kiểm tra (theo mã nguồn)

Để hiểu đúng phạm vi:
- **Liên tục chuỗi (PrevHash chain):** việc kiểm tra `PrevHash` của block khớp `Hash` block trước đang **bị comment** ở engine — block hash vẫn *bao gồm* prevHash để toàn vẹn cấu trúc, nhưng liên kết chuỗi không được kiểm bắt buộc trên hot-path. (Việc xác minh chuỗi đầy đủ chỉ diễn ra ở luồng sync của orderer.)
- **`ValidateTransaction()` mức từng giao dịch:** hiện là **stub** (trả `nil`) — kiểm tra chi tiết từng giao dịch (vd. UTXO double-spend, chữ ký input) chưa được thực thi đầy đủ.

Đây là các điểm rủi ro được nêu trong phần [cai-thien/03-cai-thien-bao-mat.md](../cai-thien/03-cai-thien-bao-mat.md).

## 5. Tính kiên cường (resilient)

Khi một block không hợp lệ, lỗi được **ghi log nhưng không làm sập pipeline** — peer tiếp tục xử lý block sau. Điều này tránh một block xấu làm tê liệt toàn bộ tiến trình ghi.

➡️ Tiếp: [04-luu-tru-va-worldstate.md](04-luu-tru-va-worldstate.md)
