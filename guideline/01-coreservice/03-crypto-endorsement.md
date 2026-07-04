# Core Service — Mật mã & Endorsement

> Mã nguồn: `coreservice/internal/crypto/keys.go`, `internal/api/server.go`, `internal/core/model.go`

## 1. Hệ chữ ký Ed25519

Dự án dùng **[Ed25519](https://ed25519.cr.yp.to/)** ([RFC 8032](https://datatracker.ietf.org/doc/html/rfc8032)) — hệ chữ ký số trên đường cong Edwards xoắn, nổi tiếng vì:
- Nhanh (ký và xác minh đều rất nhanh).
- Khóa & chữ ký gọn: khóa riêng/công khai 32 byte, chữ ký 64 byte.
- An toàn, **xác định** (cùng khóa + cùng dữ liệu → cùng chữ ký).

Các hàm trong `internal/crypto/keys.go`:

| Hàm | Ý nghĩa |
|-----|---------|
| `GenerateKeyPair()` | Tạo cặp khóa (private 32 byte, public 32 byte), mã hóa hex |
| `Sign(msg, privHex)` | Ký thông điệp, trả chữ ký 64 byte dạng hex |
| `Verify(msg, sigHex, pubHex)` | Xác minh, trả `true/false` |
| `SignTransaction(txID, contractName, payload, privHex)` | Ghép `txID + contractName + payload` thành thông điệp rồi ký |

## 2. Mô hình Endorsement (nhiều chữ ký xác nhận)

Mỗi giao dịch có thể mang **nhiều endorsement** — chữ ký từ các peer xác nhận "tôi cũng chạy ra kết quả này". Trong `internal/core/model.go`:

```go
type EndorsementEntry struct {
    PublicKey string   // khóa công khai của peer xác nhận
    Signature string   // chữ ký
}

type Transaction struct {
    Txid         string
    ContractName string
    FunctionName string
    Payload      []byte               // nhị phân, JSON mã hóa hex
    Endorsements []EndorsementEntry    // mảng chữ ký xác nhận
    Signature    string                // (legacy) phản chiếu endorsement cuối
    SenderPubKey string                // (legacy) khóa của endorser cuối
    Vin, Vout    ...                   // hỗ trợ cả UTXO
    Version, LockTime uint32
}
```

> **Tương thích ngược (backward compatibility):** Hai trường `Signature` + `SenderPubKey` là kiểu cũ (một chữ ký). Hệ thống mới ưu tiên mảng `Endorsements[]`; nếu mảng rỗng thì tự suy từ cặp cũ. Payload được **mã hóa hex** khi serialize JSON (`MarshalJSON`/`UnmarshalJSON` tùy chỉnh).

## 3. Luồng ký một giao dịch

Khi nhận `POST /api/tx/submit` (xử lý trong `internal/api/server.go`):

```
1. Giải mã JSON Transaction
2. engine.Execute()            → chạy contract WASM (validate)
        │ thất bại → trả lỗi ngay
        ▼ thành công
3. signTxViaCommitPeer()       → mở stream tx-sign tới Committing Peer
        │   Committing Peer ký Ed25519, thêm 1 EndorsementEntry
        ▼
4. sendEndorsementAsync()      → gửi giao dịch (đã endorse) tới Leader orderer
5. SubmitRecorder ghi tx_submit_times (đo latency)
6. Trả JSON: preview chữ ký + số endorsement
```

Bước 3 và 4 dùng libp2p (xem [04-networking-discovery.md](04-networking-discovery.md)). Có thể chạy bước 4 **bất đồng bộ** (`CORE_ASYNC_ENDORSE=1`) để trả lời người dùng nhanh hơn trong khi vẫn đang gửi đi sắp xếp.

## 4. Vì sao tách "execute thử" rồi mới ký?

Đây chính là mô hình **Execute–Order–Validate** của Hyperledger Fabric:
- Core Service **chạy thử** contract để biết kết quả + đảm bảo hợp lệ trước khi tốn công sắp xếp.
- Committing Peer **ký xác nhận** kết quả → tạo bằng chứng mật mã rằng giao dịch đã được một bên tin cậy duyệt.
- Khi block tới Committing Peer để ghi, nó **kiểm tra lại** các endorsement này so với danh sách khóa tin cậy (`TRUSTED_ENDORSER_PUBLIC_KEYS`).

Nhờ vậy, một giao dịch gian lận (không có endorsement hợp lệ) sẽ bị loại ở bước ghi, dù nó có lọt qua tầng sắp xếp.

Tìm hiểu thêm: [Fabric endorsement policies](https://hyperledger-fabric.readthedocs.io/en/latest/endorsement-policies.html).

➡️ Tiếp: [04-networking-discovery.md](04-networking-discovery.md)
