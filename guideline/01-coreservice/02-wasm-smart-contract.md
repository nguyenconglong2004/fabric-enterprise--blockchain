# Core Service — Máy ảo WASM & Smart Contract

> Mã nguồn chính: `coreservice/internal/vm/engine.go`, `coreservice/contracts/`

## 1. Vì sao dùng WebAssembly cho smart contract?

Smart contract cần ba tính chất:
- **Xác định (deterministic):** cùng đầu vào → luôn cùng kết quả trên mọi node (nếu khác nhau, các node sẽ bất đồng về world state).
- **Cô lập (sandboxed):** contract không được phá hệ thống host.
- **Đa ngôn ngữ:** lập trình viên viết bằng ngôn ngữ quen thuộc.

**[WebAssembly (WASM)](https://webassembly.org/)** thỏa cả ba: là mã máy ảo dạng stack, chạy trong bộ nhớ tuyến tính cô lập, biên dịch được từ nhiều ngôn ngữ. Đây cũng là hướng đi của nhiều blockchain hiện đại (Polkadot, CosmWasm, Fabric với chaincode-as-a-service).

## 2. Runtime WASM: wazero

Dự án chạy WASM bằng **[wazero](https://wazero.io/)** — runtime **thuần Go** (không cần CGo hay thư viện C), nên dễ build và triển khai. Contract dùng giao diện hệ thống **[WASI](https://wasi.dev/)** (`wasi_snapshot_preview1`).

## 3. Biên dịch contract: TinyGo

Contract viết bằng Go, biên dịch sang WASM bằng **[TinyGo](https://tinygo.org/)** (script `contracts/build_wasm.sh`):

```bash
tinygo build -o my_contract.wasm -target wasi -no-debug -scheduler=none ./
```
- `-target wasi`: nhắm runtime server-side (không phải trình duyệt).
- `-scheduler=none`: bỏ bộ lập lịch goroutine cho nhẹ và xác định hơn.

Kết quả là các file `.wasm` đặt cạnh mã nguồn, ví dụ `contracts/demo_inventory/my_contract.wasm`.

## 4. Giao diện hàm (ABI) giữa host và contract

Contract và Core Service "nói chuyện" qua một số hàm quy ước:

**Hàm contract xuất ra (host gọi):**
- `allocate(size) -> ptr`: contract cấp phát bộ nhớ trong vùng tuyến tính của nó để host ghi payload vào.
- `verify_tx(ptr, size) -> status` **hoặc** `execute(ptr, size) -> status`: hàm chính, trả `1` = thành công, `0` = thất bại. Engine ưu tiên `verify_tx`, nếu không có thì gọi `execute`.

**Hàm host cung cấp cho contract (contract gọi):**
- `PutState(keyPtr, keySize, valPtr, valSize) -> status`: ghi một cặp key→value vào world state (LevelDB). Host đọc key/value từ bộ nhớ tuyến tính của contract.

```
HOST (Core Service)                    CONTRACT (WASM)
   │  allocate(len(payload))  ──────────▶  cấp phát, trả ptr
   │  ghi payload vào ptr
   │  verify_tx(ptr, len)     ──────────▶  giải mã JSON, xử lý
   │                          ◀──────────  PutState(key,val)  (nếu cần ghi)
   │  nhận status (0/1)       ◀──────────  return 1
```

## 5. WasmEngine — tối ưu hiệu năng (`internal/vm/engine.go`)

Chạy WASM "từ đầu" mỗi giao dịch thì chậm (phải biên dịch + khởi tạo module). Engine dùng hai kỹ thuật:

1. **Cache module đã biên dịch** — `getOrCompile()` biên dịch WASM một lần rồi giữ lại bản đã compile.
2. **Pool module dựng sẵn (module pool)** — với mỗi contract, dựng sẵn N sandbox (mặc định 16, tối đa 32, biến `WASM_POOL_SIZE`) trong một channel có đệm. Mỗi giao dịch lấy một sandbox rảnh ra dùng rồi trả lại — tránh chi phí khởi tạo lặp lại và cho phép xử lý song song nhiều giao dịch.

> Đây là pattern **object pool** kinh điển để giảm độ trễ khởi tạo. Tham khảo: [object pool pattern](https://en.wikipedia.org/wiki/Object_pool_pattern).

## 6. Quản lý schema contract (`internal/core/contract_schema.go`)

Mỗi contract có một **schema** mô tả các trường dữ liệu đầu vào để Explorer tự sinh form nhập liệu:

```go
type ContractSchema struct {
    Name   string
    Fields []FieldSpec   // mỗi field: Name, Label, Type, Required, Placeholder
}
```

- **Schema có sẵn (builtin):** `example_asset`, `token`, `voting`, `demo_inventory`, `bench_ping`.
- **Schema tùy chỉnh:** khi deploy contract mới, có thể nộp kèm schema JSON; lưu trong LevelDB dưới key `__meta__/schema/{tên}` và mirror sang PostgreSQL.

## 7. Ba contract mẫu

| Contract | Đầu vào (JSON) | Hành vi |
|----------|----------------|---------|
| **example_asset** | `{id, color, action}` | Nếu `action=="create"` → `PutState("Asset_{id}", payload)` |
| **demo_inventory** | `{op, sku, qty}` | Nếu `op=="register"` và `qty>=0` → `PutState("Inv_{sku}", payload)` |
| **bench_ping** | `{v}` | Chỉ kiểm tra định dạng payload, **không ghi state** — dùng đo throughput thuần |

`bench_ping` cố ý không ghi gì để đo "trần" tốc độ pipeline mà không bị nghẽn ở tầng lưu trữ.

## 8. Triển khai contract (deploy)

- `POST /api/tx/deploy` (multipart form): trường `contract_name`, file `.wasm`, tùy chọn `payload_schema` (JSON).
- `POST /api/deploy-example`: deploy nhanh contract `example_asset` dựng sẵn.

Mã WASM được lưu vào ContractDB (LevelDB) và mirror sang bảng `core_service.smart_contracts` (PostgreSQL).

➡️ Tiếp: [03-crypto-endorsement.md](03-crypto-endorsement.md)
