# Blockchain Explorer — Mã hóa Payload nhị phân

> Mã nguồn: `src/components/transactionTypes.js`, `src/utils/transactionUtils.js`
> Tài liệu gốc: `TRANSACTION_SYSTEM_GUIDE.md`, `BINARY_PAYLOAD_UPDATE.md`

## 1. Vì sao mã hóa nhị phân thay vì JSON?

`payload` của giao dịch cần nhỏ gọn và đúng kiểu để smart contract đọc. Frontend có **hai cơ chế** mã hóa, đều xuất ra **chuỗi hex**:

1. **Hệ nhị phân tùy biến** (`transactionTypes.js`) — đóng gói từng trường thành byte (tiết kiệm ~70% so với JSON).
2. **Hệ payload contract** (`transactionUtils.js`) — JSON → UTF-8 → hex, để contract Go dùng `json.Unmarshal` đọc.

> **Vì sao có hai?** Hệ nhị phân tối ưu kích thước cho các loại giao dịch cố định; hệ JSON-hex linh hoạt hơn cho contract WASM tùy ý (như `demo_inventory`). Contract WASM trong dự án hiện đọc payload bằng `json.Unmarshal`, nên Transfer dùng `serializeContractPayload()` (JSON-hex) khi gửi tới contract.

## 2. Hệ nhị phân tùy biến (`transactionTypes.js`)

### Các kiểu trường (FIELD_TYPES)
| Kiểu | Kích thước | Ghi chú |
|------|-----------|---------|
| STRING | biến đổi | tiền tố độ dài UINT16 + UTF-8 |
| UINT8/16/32/64 | 1/2/4/8 byte | số nguyên không dấu, big-endian |
| FLOAT/DOUBLE | 4/8 byte | [IEEE 754](https://en.wikipedia.org/wiki/IEEE_754) |
| BYTES | biến đổi | tiền tố độ dài + dữ liệu thô |
| BOOLEAN | 1 byte | 0x00 / 0x01 |
| ADDRESS | 20 byte | địa chỉ kiểu Ethereum (bỏ `0x`) |

### Bộ mã hóa/giải mã
- `BinaryPayloadEncoder`: ghi byte vào buffer (big-endian), `toHexString()` xuất hex.
- `BinaryPayloadDecoder`: parse hex → `Uint8Array`, đọc tuần tự theo offset.

### Các loại giao dịch định nghĩa sẵn (TRANSACTION_TYPES)
`TRANSFER`, `CONTRACT_CALL`, `TOKEN_SWAP`, `STAKE`, `USER_PROFILE` — mỗi loại có schema gồm danh sách field (name, label, type UI, payloadType nhị phân, required, validation).

### Hàm công khai
| Hàm | Vai trò |
|-----|---------|
| `serializePayload(txType, fields)` | form → hex nhị phân |
| `deserializePayload(txType, hex)` | hex → object field |
| `getPayloadField(txType, hex, name)` | trích **một** field mà không giải mã toàn bộ |
| `createTransaction(base, txType, fields)` | dựng giao dịch hoàn chỉnh |

### Ví dụ (USER_PROFILE)
```
{nickname:'john_doe', firstName:'John', lastName:'Doe', age:30, isVerified:true}
→ 08 6a6f686e5f646f65   ("john_doe", len=8)
  04 4a6f686e           ("John", len=4)
  03 446f65             ("Doe", len=3)
  1e                    (age=30)
  01                    (isVerified=true)
→ 24 byte (so với 98 byte JSON ≈ tiết kiệm 75%)
```

## 3. Hệ payload contract WASM (`utils/transactionUtils.js`)

Dành cho contract WASM (đọc JSON):
| Hàm | Vai trò |
|-----|---------|
| `serializeContractPayload(fields)` | `JSON.stringify` → UTF-8 → hex |
| `deserializeContractPayload(hex, names)` | hex → UTF-8 → `JSON.parse` (có fallback nhị phân cũ) |
| `coercePayloadFieldsBySchema(schema, fields)` | ép kiểu input HTML (string) về int/float đúng |
| `createVIN/createVOUT` | dựng input/output UTXO |
| `normalizeVout`, `formatVoutTransferLine` | chuẩn hóa & hiển thị VOUT |

`coercePayloadFieldsBySchema` quan trọng: input HTML luôn ra chuỗi, nhưng contract cần số nguyên/thực — hàm này ép kiểu theo schema trước khi mã hóa.

## 4. Tóm tắt tài liệu gốc

- **`TRANSACTION_SYSTEM_GUIDE.md`** (tiếng Việt, ~500 dòng): hướng dẫn đầy đủ hệ nhị phân — kiểu trường, cách thêm loại giao dịch mới, API, validation, chi tiết định dạng byte.
- **`BINARY_PAYLOAD_UPDATE.md`**: ghi chú chuyển từ JSON sang nhị phân, bảng so sánh kích thước (tiết kiệm 66–75%), lý do (tương thích contract, hiệu quả, trích field nhanh, an toàn kiểu).
- **`transactionTypes.test.js`**: bộ test tự viết (assertEquals/assertNull) cho serialize/deserialize.

➡️ Tiếp: [04-realtime-sse.md](04-realtime-sse.md)
