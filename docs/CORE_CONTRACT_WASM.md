# Phân tích: Deploy Contract & WASM trên Core Service

Tài liệu mô tả **cách Core Service biên dịch, deploy, chạy smart contract WASM**, host ABI / SDK guest, schema Explorer, và chỗ state thực sự được ghi (RW set → Commit Peer).

Liên quan nhanh:

| Doc | Nội dung |
|-----|----------|
| [CONTRACT_DEPLOY_SCHEMA.md](./CONTRACT_DEPLOY_SCHEMA.md) | Deploy curl + thứ tự schema + invalidate |
| [RW_SET.md](./RW_SET.md) | Simulate → endorse → MVCC apply |
| [COMMON_PAYLOAD_FIELDS.md](./COMMON_PAYLOAD_FIELDS.md) | `amount` / `to` / `from` trên form FE |
| [EXAMPLE_ASSET_EXECUTE.md](./EXAMPLE_ASSET_EXECUTE.md) | Ví dụ `example_asset` |
| [SETUP_RUN.md](./SETUP_RUN.md) | TinyGo + `build_wasm.sh` |

---

## 1. Vai trò Core trong vòng đời contract

Core **không** giữ world state KV đã commit. Core chỉ:

1. **Lưu bytecode WASM** (+ schema UI) vào LevelDB (`contract_db`), mirror Postgres.
2. **Simulate** transaction: chạy WASM trong wazero → thu **RW set** (`reads` / `writes`).
3. **Xin endorsement** từ Commit Peer (ký trên `txid ‖ contract ‖ payload ‖ hash(rw_set)`).
4. Đẩy tx đã endorse sang Orderer.

World state (`balance:…`, `Asset_…`, …) nằm trên **Commit Peer**; Peer apply write-set sau MVCC. Chi tiết: [RW_SET.md](./RW_SET.md).

```text
Developer
  TinyGo build → my_contract.wasm + schema.json
       │
       ▼
POST /api/tx/deploy ──► LevelDB WASM + meta schema
                       Invalidate cache/pool
                       Postgres smart_contracts (optional)

Client Submit
  POST /api/tx/submit
       │
       ▼
  enrich payload (from session…)
       │
       ▼
  WasmEngine.Execute
    allocate → verify_tx → execute
    PutState → rw.Writes   (RAM only)
    GetState → overlay / Peer /wallet/state → rw.Reads
       │
       ▼
  Commit Peer sign → Orderer → Peer Validate + ApplyBlock
```

---

## 2. Biên dịch WASM (TinyGo)

### Toolchain

- **Ngôn ngữ guest:** Go (TinyGo), target **WASI**.
- **Runtime host:** [wazero](https://github.com/tetratelabs/wazero) + `wasi_snapshot_preview1`.
- Module Go riêng cho contracts: `fabricwasm` (`coreservice/contracts/go.mod`) — tách khỏi `coreservice` để `go build` Core không compile `//go:wasmimport`.

### Script build

`coreservice/contracts/build_wasm.sh`:

```bash
cd coreservice/contracts && ./build_wasm.sh
```

Với mỗi thư mục `example_asset`, `demo_inventory`, `bench_ping`, `transfer`:

1. `go run ./cmd/gen_schema -dir ./contracts/<name>` → ghi `schema.json`.
2. `tinygo build -o <name>/my_contract.wasm -target wasi -no-debug -scheduler=none ./<name>`.

Cờ quan trọng:

| Cờ | Lý do |
|----|--------|
| `-target wasi` | Khớp wazero + WASI preview1 |
| `-no-debug` | Nhỏ hơn, ổn định hơn |
| `-scheduler=none` | Không cần goroutine scheduler trong sandbox |

Output: `contracts/<name>/my_contract.wasm` + `schema.json`.

---

## 3. SDK guest (`fabricwasm/sdk`)

Package: `coreservice/contracts/sdk/`.

| File | Build tag | Vai trò |
|------|-----------|---------|
| `host.go` | luôn | API mức cao cho contract |
| `host_tinygo.go` | `tinygo` | `//go:wasmimport env PutState/GetState` |
| `host_stub.go` | `!tinygo` | Stub no-op cho gopls / `go test` trên máy thường |

### API công khai

```go
func PutState(key, value []byte) bool
func GetState(key, out []byte) (n uint32, ok bool) // out rỗng = probe size
func SizeOf(key []byte) uint32
func Allocate(size uint32) *byte
func PayloadSlice(ptr, size uint32) []byte
```

| Hàm | Ý nghĩa thực tế trên Core |
|-----|---------------------------|
| `PutState` | Ghi vào **write-set** của tx đang simulate (không persist LevelDB Core) |
| `GetState` | Ưu tiên overlay write-set cùng tx; không thì HTTP Commit Peer `/wallet/state` và ghi **read-set** |
| `SizeOf` | Gọi `GetState` với `outCap=0` — host trả độ dài value |
| `Allocate` | Helper cấp phát memory (thường không cần gọi tay) |
| `allocate` *(auto-export khi import sdk)* | Core gọi để ghi payload vào linear memory |
| `PayloadSlice` | Map `(ptr, size)` host đã ghi thành `[]byte` Go |

Import trong contract hiện đại:

```go
import "fabricwasm/sdk"
```

### Export bắt buộc từ guest

Core gọi theo thứ tự: `allocate` → `verify_tx` → `execute`.

| Export | Ai cung cấp | Vai trò |
|--------|-------------|---------|
| `allocate` | **SDK** (`//export` sẵn khi `import "fabricwasm/sdk"`) | Xin vùng nhớ; host ghi payload JSON |
| `verify_tx` | Contract `main` | Validate payload / rule; `0` = reject; **nên tránh side-effect** |
| `execute` | Contract `main` | Side effect qua `PutState` / đọc qua `GetState` |

Không viết lại `//export allocate` trong `main` nếu đã import SDK — TinyGo sẽ lỗi duplicate export. Contract không dùng SDK (`bench_ping`, `demo_inventory`) vẫn tự export `allocate`.

Nếu thiếu cả `verify_tx` và `execute`, Core fallback gọi `tx.FunctionName` nếu export tồn tại.

Return `0` → Core trả lỗi *“bị Smart Contract từ chối”*.

### Pattern contract chuẩn (SDK)

```go
import "fabricwasm/sdk"

// allocate đã được SDK export — không khai báo lại.

//export verify_tx
func verify_tx(ptr, size uint32) uint32 {
    var p Payload
    if err := json.Unmarshal(sdk.PayloadSlice(ptr, size), &p); err != nil {
        return 0
    }
    // … checks …
    return 1
}

//export execute
func execute(ptr, size uint32) uint32 {
    // PutState / GetState …
    return 1
}

func main() {} // TinyGo entry; không dùng
```

Tham khảo: `contracts/transfer/main.go`, `contracts/example_asset/main.go`.

### Pattern legacy (raw import)

`demo_inventory` tự khai báo `//go:wasmimport env PutState` và ghi state **trong** `verify_tx` (không có `execute`). Vẫn chạy được vì host `PutState` chỉ cần RW-set context — nhưng pattern mới khuyến nghị tách verify / execute.

`bench_ping` chỉ có `verify_tx`, không đụng state → `rw_set` rỗng (phù hợp benchmark throughput).

---

## 4. Host ABI trên Core (`WasmEngine`)

File: `coreservice/internal/vm/engine.go`.

### Khởi tạo

`NewWasmEngine`:

1. Tạo wazero Runtime.
2. Instantiate `wasi_snapshot_preview1`.
3. Host module **`env`** với hai export: `PutState`, `GetState`.
4. Cache compiled module + **module pool** per contract (`WASM_POOL_SIZE`, mặc định 16, max 32).

Env liên quan:

| Biến | Mặc định | Dùng |
|------|----------|------|
| `COMMIT_PEER_METRICS_URL` / `COMMIT_PEER_METRICS_HTTP` | `http://127.0.0.1:8081` | Base URL GetState |
| `WASM_POOL_SIZE` | `16` | Số sandbox tái sử dụng / contract |
| `CORE_LOG=debug` | — | Log verbose VM |

### Host `PutState(keyPtr, keySize, valPtr, valSize) → u32`

1. Đọc key/value từ linear memory WASM.
2. Lấy `*RWSet` từ context.
3. `rw.PutWrite(key, copy(value))`.
4. Trả `1` / `0`.

### Host `GetState(keyPtr, keySize, outPtr, outCap) → u32`

1. Nếu key có trong write-set cùng tx → trả value, **không** ghi read-set (overlay Fabric-style).
2. Nếu delete trong write-set → `0`.
3. Ngược lại: `GET {peer}/wallet/state?key=` → `RecordRead(key, value, version)`.
4. `outCap == 0` → chỉ trả độ dài (size probe).
5. Buffer nhỏ hơn value → `0`.

### `Execute(ctx, tx)` từng bước

1. `getOrCompile(contractName)` — cache hoặc load WASM từ LevelDB rồi `CompileModule`.
2. `acquireModule` từ pool (hoặc instantiate thêm nếu pool trống).
3. Gắn `RWSet` rỗng vào context.
4. Nếu payload ≠ rỗng: gọi `allocate(len)` → ghi bytes payload vào memory.
5. Gọi `verify_tx` (nếu có) rồi `execute` (nếu có).
6. `attachRWSet` → `tx.RWSet` (nil nếu không có read/write).
7. Trả module về pool; nếu lỗi thì đóng instance (không reuse).

**Deploy / đổi WASM:** sau `SaveContract` luôn gọi `InvalidateContract` — xóa compiled cache + đóng pool. Không invalidate = vẫn chạy bytecode cũ.

---

## 5. Luồng deploy

### API

| Method | Path | Handler |
|--------|------|---------|
| `POST` | `/api/tx/deploy` | `HandleDeployContract` |
| `POST` | `/api/deploy-example` | `HandleDeployExampleAsset` (file cố định `example_asset`) |
| `GET` | `/api/contracts` | `HandleListContracts` |
| `GET` | `/api/contract/schema?name=` | `HandleGetContractSchema` |

### `POST /api/tx/deploy` (multipart, max 10 MiB)

Form fields:

| Field | Bắt buộc | Nội dung |
|-------|----------|----------|
| `contract_name` | có | Tên contract (= key LevelDB) |
| `file` | có | Bytes `.wasm` |
| `schema` / `schema_file` | không | File JSON `ContractSchema` (≤ 1 MiB) |
| `payload_schema` | không | JSON schema inline (legacy) |

Các bước trong `HandleDeployContract`:

1. Parse multipart → đọc WASM.
2. `readDeploySchema` (ưu tiên bên dưới).
3. `SaveContract(name, wasm)` → LevelDB.
4. `InvalidateContract(name)`.
5. Nếu có schema → `SaveContractMetaSchema` (key `__meta__/schema/<name>`).
6. Best-effort `PostgresDB.SaveContract` → bảng `core_service.smart_contracts`.
7. JSON: `status`, `contract_name`, `size_bytes`, `schema_source`.

Ví dụ:

```bash
cd coreservice/contracts && ./build_wasm.sh

curl -X POST http://127.0.0.1:8080/api/tx/deploy \
  -F 'contract_name=transfer' \
  -F 'file=@transfer/my_contract.wasm' \
  -F 'schema=@transfer/schema.json'   # optional override
```

### Thứ tự ưu tiên schema lúc deploy (`readDeploySchema`)

1. Upload multipart `schema` / `schema_file`
2. Form `payload_schema`
3. File disk cạnh source (`contracts/<name>/schema.json` qua `readContractSchemaFromDisk`)
4. Builtin trong `core/contract_schema.go`
5. `source = "none"` — deploy WASM vẫn OK, FE có thể thiếu field động

Deploy qua `POST /api/tx/deploy` (hoặc `/api/deploy-example` cho `example_asset`). Core **không** auto-deploy khi start.

### Lưu trữ

| Store | Key / cột | Nội dung |
|-------|-----------|----------|
| LevelDB `contract_db` | `contractName` | WASM bytes |
| LevelDB | `__meta__/schema/<name>` | Schema JSON |
| Postgres `smart_contracts` | `contract_name`, `contract_code`, `payload_schema` | Mirror; update giữ schema cũ nếu payload mới nil |

`GET /api/contracts` chỉ trả tên có WASM trong LevelDB — **không** merge builtin (`token`, `voting`, …) để FE không hiện contract chưa deploy.

---

## 6. Schema (UI metadata, không validate execute)

### Wire type

```go
type ContractSchema struct {
    Name   string      `json:"name"`
    Fields []FieldSpec `json:"fields"`
}
type FieldSpec struct {
    Name, Label, Type string  // type: string|number|integer|boolean|address
    Required    bool
    Placeholder string `json:"placeholder,omitempty"`
}
```

### `cmd/gen_schema`

Parse AST `type Payload struct` trong `main.go`:

- Bỏ field Common FE: `from`, `to`, `amount`, `address` (form Explorer đã có — xem [COMMON_PAYLOAD_FIELDS.md](./COMMON_PAYLOAD_FIELDS.md)).
- Tag:
  - `` `schema:"optional"` ``
  - `` `schema:"label=..."` ``
  - `` `schema:"-"` `` / `skip`
- Map Go type → schema type (`string`, `integer`, `number`, `boolean`).

**Quan trọng:** schema chỉ phục vụ **form động FE** (`GET /api/contract/schema`). Core **không** JSON-Schema-validate payload lúc `submit` — chỉ guest `verify_tx` / `execute` quyết định chấp nhận.

### Resolve schema khi FE hỏi

`resolveContractSchema`: LevelDB meta → Postgres `payload_schema` → builtin map.

---

## 7. Luồng execute sau deploy (`POST /api/tx/submit`)

1. Decode `core.Transaction` (payload thường là JSON đã hex trên wire).
2. `enrichTransferPayload` — với contract kiểu account (`transfer`, `example_asset`): clear vin/vout; inject `from` từ session đăng nhập.
3. Stamp `client_pubkey` từ account session.
4. `Engine.Execute` → gắn `tx.rw_set`.
5. `signTxViaCommitPeer` — endorsement bind canonical RW set.
6. Gửi async/sync sang Orderer qua discovery.

Payload contract điển hình (sau enrich):

```json
{
  "from": "<40-hex address từ session>",
  "to": "<40-hex>",
  "amount": 100,
  "memo": "optional"   // transfer
}
```

`example_asset` thêm `id`, `color`, `action` và vẫn move balance tương tự transfer.

---

## 8. Contract mẫu trong repo

| Thư mục | SDK | `verify_tx` | `execute` | State |
|---------|-----|-------------|-----------|--------|
| `transfer` | `fabricwasm/sdk` | có | có | `balance:`, `discount:`, `xfer_receipt:` |
| `double_credit` | SDK | có | có | from −amount, to +amount×2; `double_receipt:` |
| `example_asset` | SDK | có | có | `Asset_*` + balance move |
| `bench_ping` | raw / không PutState | có | không | không |
| `demo_inventory` | raw `wasmimport PutState` | ghi trong verify | không | `Inv_<sku>` |

### `transfer` (tóm tắt logic)

- Verify: `from`/`to` dài 40 hex, `amount > 0`, khác địa chỉ, memo ≤ 200.
- Execute: đọc `discount:<from>` → debit = `ceil(amount/(1+d))` (nếu d>0); credit receiver đúng `amount`; ghi receipt.

### `double_credit`

- Verify: giống transfer (không discount); chặn `amount` quá lớn để `amount*2` không tràn int64.
- Execute: `balance:from -= amount`, `balance:to += amount*2` (vd. alice gửi 10 → alice −10, bob +20).

### `example_asset`

- Verify: `id`, `color`, `action ∈ {create,update,delete}`, địa chỉ + amount hợp lệ.
- Execute: ghi/xóa `Asset_<id>`; chuyển balance giống transfer.

---

## 9. Viết contract mới (checklist)

1. Tạo `coreservice/contracts/<name>/main.go` với `type Payload`, import `fabricwasm/sdk`.
2. Export `verify_tx`, `execute` (import SDK → có sẵn `allocate`).
3. Thêm tên vào vòng `for` trong `build_wasm.sh` (hoặc build tay + `gen_schema`).
4. `./build_wasm.sh` → kiểm `schema.json`.
5. Deploy:

   ```bash
   curl -X POST http://127.0.0.1:8080/api/tx/deploy \
     -F "contract_name=<name>" \
     -F "file=@<name>/my_contract.wasm"
   ```

6. F5 Explorer Transfer → chọn contract → form lấy schema mới.
7. Submit thử; nếu đổi WASM mà không thấy hành vi mới: xác nhận log `Invalidated cache/pool` và redeploy.

---

## 10. Bản đồ file

| Path | Nội dung chính |
|------|----------------|
| `coreservice/cmd/node/main.go` | Đăng ký route deploy / submit / schema |
| `coreservice/internal/api/server.go` | `HandleDeployContract`, `readDeploySchema`, `HandleSubmitTx`, list/schema |
| `coreservice/internal/api/contract_schema_file.go` | Đọc `schema.json` từ disk |
| `coreservice/internal/api/auth.go` | Seed account (alice/bob/…) — không auto-deploy contract |
| `coreservice/internal/vm/engine.go` | wazero, host ABI, pool, `Execute`, `InvalidateContract` |
| `coreservice/internal/state/database.go` | LevelDB contract + meta schema |
| `coreservice/internal/storage/postgres.go` | `SaveContract` / `GetContractPayloadSchema` |
| `coreservice/internal/core/contract_schema.go` | Builtin schema + wire types |
| `coreservice/internal/core/rwset.go` | RW set + canonical bytes cho endorsement |
| `coreservice/internal/core/model.go` | `Transaction` (`ContractName`, `Payload`, `RWSet`, …) |
| `coreservice/contracts/sdk/*` | Guest SDK |
| `coreservice/contracts/*/main.go` | Contract mẫu |
| `coreservice/cmd/gen_schema/` | AST → `schema.json` |
| `coreservice/contracts/build_wasm.sh` | Pipeline build |

---

## 11. Điểm thiết kế / lưu ý

1. **Schema ≠ validation runtime** — chỉ metadata form Explorer.
2. **Core không commit KV** — `PutState` chỉ nuôi write-set; Peer apply sau MVCC.
3. **Comment cũ trong SDK** còn nói “LevelDB Core”; hành vi đúng là RW-set + Peer ([RW_SET.md](./RW_SET.md)).
4. **Redeploy phải invalidate** — code đường deploy chính đã gọi; đừng bỏ bước này nếu viết API deploy riêng.
5. **Pool reuse** — instance lỗi bị đóng; thành công trả lại pool. Instance mới chạy `_initialize` (WASI/TinyGo).
6. **Không auto-deploy** — dùng `POST /api/tx/deploy`. List API chỉ hiện contract đã có WASM trong LevelDB.
