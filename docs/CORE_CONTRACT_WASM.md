# Smart Contract WASM trên Core Service

Tài liệu kỹ thuật đầy đủ về **thiết kế, ABI, runtime wazero, SDK TinyGo, deploy, và vòng đời execute** của smart contract trong nền tảng.

| Doc liên quan | Nội dung |
|---------------|----------|
| [CONTRACT_DEPLOY_SCHEMA.md](./CONTRACT_DEPLOY_SCHEMA.md) | Deploy curl + schema (tóm tắt) |
| [RW_SET.md](./RW_SET.md) | Simulate → endorse → MVCC apply trên Commit Peer |
| [COMMON_PAYLOAD_FIELDS.md](./COMMON_PAYLOAD_FIELDS.md) | `amount` / `to` / `from` trên Explorer |
| [EXAMPLE_ASSET_EXECUTE.md](./EXAMPLE_ASSET_EXECUTE.md) | Ví dụ `example_asset` |
| [SETUP_RUN.md](./SETUP_RUN.md) | Cài TinyGo, chạy hệ thống |
| [POSTGRES_TABLES.md](./POSTGRES_TABLES.md) | Bảng `smart_contracts`, `tx_submit_times` |

---

## Mục lục

1. [Vì sao dùng WebAssembly](#1-vì-sao-dùng-webassembly)
2. [Vai trò Core trong vòng đời contract](#2-vai-trò-core-trong-vòng-đời-contract)
3. [Kiến trúc tổng quan](#3-kiến-trúc-tổng-quan)
4. [Linear memory và ABI](#4-linear-memory-và-abi)
5. [SDK guest (`fabricwasm/sdk`)](#5-sdk-guest-fabricwasmsdk)
6. [Host runtime (`WasmEngine`)](#6-host-runtime-wasmengine)
7. [Luồng `Execute` từng bước (ptr / size)](#7-luồng-execute-từng-bước-ptr--size)
8. [Biên dịch TinyGo](#8-biên-dịch-tinygo)
9. [Deploy contract](#9-deploy-contract)
10. [Schema Explorer](#10-schema-explorer)
11. [Submit transaction](#11-submit-transaction)
12. [Walkthrough: `double_credit`](#12-walkthrough-double_credit)
13. [Contract mẫu khác](#13-contract-mẫu-khác)
14. [Đa ngôn ngữ](#14-đa-ngôn-ngữ)
15. [Checklist viết contract mới](#15-checklist-viết-contract-mới)
16. [Bản đồ file](#16-bản-đồ-file)
17. [Lưu ý thiết kế](#17-lưu-ý-thiết-kế)

---

## 1. Vì sao dùng WebAssembly

### Bài toán

Core (endorsing / simulating node) cần chạy **logic do người dùng viết** để:

- Đọc state hiện tại (qua host)
- Quyết định ghi state mới
- Xuất **RW set** gắn vào transaction → Commit Peer ký endorsement → Orderer → Peer MVCC apply

Không muốn: load `.so` native tùy tiện, spin Docker mỗi tx (nặng), hay hard-code mọi nghiệp vụ trong Core.

### WASM đáp ứng gì

| Yêu cầu | WASM / wazero |
|---------|----------------|
| Sandbox | Guest chỉ gọi được import host khai báo (`env.PutState` / `GetState`) |
| Portable | Một file `.wasm` deploy; không cài JDK/Python trên node |
| Đa ngôn ngữ (nguyên tắc) | Ai compile đúng ABI đều chạy được |
| Nhẹ hơn container | In-process + module pool; phù hợp latency submit |
| Ranh giới memory rõ | Linear memory + con trỏ (`ptr`/`size`) |
| Industry | CosmWasm, Substrate contracts, … — dễ đối chiếu luận văn với Fabric chaincode |

Tên “WebAssembly” mang tính lịch sử; ở đây dùng như **embedded smart-contract VM**, không phải chạy trong trình duyệt.

### Chỗ WASM nằm trong pipeline

```text
Client → Core (WASM simulate + thu RW set) → Commit Peer sign
      → Orderer (không chạy contract)
      → Peer (MVCC + apply writes — không re-execute WASM)
```

Orderer và Commit Peer **không** cần runtime WASM. Peer tin endorsement + kiểm MVCC trên read-set.

### Phương án thay thế (đối chiếu)

| Hướng | Ưu | Nhược so với WASM hiện tại |
|--------|-----|----------------------------|
| Native plugin (`.so` / Go plugin) | Nhanh | Crash risk, khó đa ngôn ngữ, phụ thuộc OS |
| Docker chaincode (kiểu Fabric) | Cách ly mạnh | Cold start, ops nặng |
| Interpreter (Lua, JS, Python) | DX dễ vài ngôn ngữ | Determinism / gas / sandbox khó hơn |
| JVM guest | Quen enterprise | Runtime nặng, deterministic khó |
| Bytecode riêng (EVM-like) | Kiểm soát tuyệt đối | Tốn công compiler + tooling |
| Không có VM (tx cố định) | Đơn giản | Mất programmability |

Lựa chọn hiện tại: **WASM in-process** — cân bằng linh hoạt và độ phức tạp vận hành cho prototype / thesis.

---

## 2. Vai trò Core trong vòng đời contract

Core **không** giữ world state KV đã commit. Core chỉ:

1. **Lưu bytecode WASM** (+ schema UI) vào LevelDB `contract_db`, mirror Postgres.
2. **Simulate** tx: chạy WASM trong wazero → thu **RW set**.
3. **Xin endorsement** từ Commit Peer (ký trên `txid ‖ contract ‖ payload ‖ hash(rw_set)`).
4. Đẩy tx đã endorse sang Orderer.

World state (`balance:…`, `Asset_…`, …) nằm trên **Commit Peer**. Chi tiết apply: [RW_SET.md](./RW_SET.md).

---

## 3. Kiến trúc tổng quan

```text
Developer
  TinyGo build → <name>.wasm + schema.json
       │
       ▼
POST /api/tx/deploy ──► LevelDB WASM + meta schema
                       Invalidate cache/pool
                       Postgres smart_contracts (optional)
                       (không auto-deploy khi Core start)

Client Submit
  POST /api/tx/submit
       │
       ▼
  enrich payload (vd. inject from từ session)
       │
       ▼
  WasmEngine.Execute   ← mỗi transaction một lần
    allocate(size) → ptr
    Memory.Write(ptr, payload)
    verify_tx(ptr, size)
    execute(ptr, size)
       PutState  → rw.Writes   (RAM only trên Core)
       GetState  → overlay write-set cùng tx
                 → hoặc HTTP Peer /wallet/state + rw.Reads
       │
       ▼
  gắn tx.rw_set → Commit Peer sign → Orderer → Peer Validate + ApplyBlock
```

Hai phía runtime:

| Vai trò | Ai | Làm gì |
|---------|-----|--------|
| **Host** | Core `WasmEngine` (Go + wazero) | Implement `env.PutState` / `GetState`; gọi export guest |
| **Guest** | File `.wasm` (TinyGo/…) | Export `allocate` / `verify_tx` / `execute`; import `env.*` |

---

## 4. Linear memory và ABI

### ABI là gì?

**ABI (Application Binary Interface)** ở đây = hợp đồng máy giữa host và guest:

- Tên hàm
- Số / kiểu tham số (WASM chỉ truyền số nguyên `i32`/`i64`, không truyền `string`/`[]byte` Go trực tiếp)
- Cách đặt dữ liệu trong **linear memory** (một mảng byte gắn với instance module)

SDK chỉ **bọc** ABI cho dễ viết; sai ABI thì WASM không chạy đúng dù source “đúng ý”.

### Linear memory

Mỗi instance WASM có **một** linear memory. Mọi “con trỏ” (`ptr`) là **offset** trong mảng đó:

```text
Linear memory:
  [ .... | payload JSON bytes | .... | key "balance:…" | value "990" | .... ]
           ↑ ptr_payload              ↑ kPtr              ↑ vPtr / outPtr
```

- Host ghi / đọc bằng `m.Memory().Write` / `Read`
- Guest xem vùng đó như `[]byte` (TinyGo: `unsafe.Slice`)

Bytecode **không tự biết** có payload ở đâu: nó chỉ dùng **hai số** host truyền vào khi gọi hàm (`ptr`, `size`), đúng ABI đã thỏa thuận.

### Bảng ABI đầy đủ

#### Guest → Host (import module `env`)

| Hàm | Signature | Ý nghĩa |
|-----|-----------|---------|
| `PutState` | `(keyPtr, keySize, valPtr, valSize) → u32` | Đọc key/value từ memory guest → ghi **write-set**. `1` ok, `0` fail |
| `GetState` | `(keyPtr, keySize, outPtr, outCap) → u32` | Đọc state; ghi value vào buffer guest tại `outPtr`. `outCap==0` → chỉ trả độ dài (probe). Return = số byte (hoặc `0`) |

Khai báo TinyGo:

```go
//go:wasmimport env PutState
func putState(keyPtr, keySize, valPtr, valSize uint32) uint32

//go:wasmimport env GetState
func getState(keyPtr, keySize, outPtr, outCap uint32) uint32
```

#### Host → Guest (export từ module contract)

| Hàm | Signature | Ý nghĩa |
|-----|-----------|---------|
| `allocate` | `(size) → ptr` | Guest cấp `size` byte; trả địa chỉ để host `Write` payload |
| `verify_tx` | `(ptr, size) → u32` | Validate; `0` = reject; **nên không** side-effect |
| `execute` | `(ptr, size) → u32` | Nghiệp vụ; gọi Put/Get |

Return `0` từ `verify_tx`/`execute` → Core báo *“bị Smart Contract từ chối”*.

### Vì sao `ptr` + `size` thay vì truyền JSON trực tiếp?

WASM call chỉ đẩy số lên stack. Muốn truyền blob phải:

1. Đặt blob trong linear memory
2. Truyền offset + độ dài

Đó là pattern chuẩn host↔guest (giống nhiều hệ WASM khác).

---

## 5. SDK guest (`fabricwasm/sdk`)

**Module:** `fabricwasm` — `coreservice/contracts/go.mod` (tách khỏi `coreservice` để `go build` Core không compile `wasmimport`).

| File | Build tag | Vai trò |
|------|-----------|---------|
| `host.go` | luôn | API mức cao + `//export allocate` |
| `host_tinygo.go` | `tinygo` | Bind thật `env.PutState` / `GetState` |
| `host_stub.go` | `!tinygo` | Stub no-op cho gopls / `go test` trên máy thường |

### API công khai

```go
func PutState(key, value []byte) bool
func GetState(key, out []byte) (n uint32, ok bool) // out rỗng = probe size
func SizeOf(key []byte) uint32
func Allocate(size uint32) *byte
func PayloadSlice(ptr, size uint32) []byte
```

| Hàm | Việc thực sự |
|-----|----------------|
| `PutState` | Lấy `&key[0]`, `&value[0]` → gọi ABI `PutState` → host ghi write-set |
| `GetState` | Tương tự; host ghi value vào `out` |
| `SizeOf` | `GetState` với `outCap=0` |
| `Allocate` / `allocate` | `make([]byte, size)` + trả `&buf[0]` — **Core gọi `allocate`**, contract không cần viết lại |
| `PayloadSlice` | Map `(ptr,size)` thành `[]byte` để `json.Unmarshal` |

**Import SDK là đủ để có export `allocate`.** Không khai báo lại `//export allocate` trong `main` (tránh duplicate).

### Pattern contract chuẩn

```go
package main

import (
    "encoding/json"
    "fabricwasm/sdk"
)

type Payload struct {
    From   string `json:"from"`
    To     string `json:"to"`
    Amount int64  `json:"amount"`
    Memo   string `json:"memo" schema:"optional"`
}

//export verify_tx
func verify_tx(ptr, size uint32) uint32 {
    var p Payload
    if err := json.Unmarshal(sdk.PayloadSlice(ptr, size), &p); err != nil {
        return 0
    }
    // … rule checks …
    return 1
}

//export execute
func execute(ptr, size uint32) uint32 {
    // PutState / GetState …
    return 1
}

func main() {} // TinyGo entry; logic nằm ở export
```

### SDK chuyển `[]byte` → pointer như thế nào

```go
kPtr := uint32(uintptr(unsafe.Pointer(&key[0])))
return putState(kPtr, uint32(len(key)), vPtr, uint32(len(value))) == 1
```

Guest và host **cùng nhìn một linear memory**: guest đưa offset; host `Read`/`Write` tại offset đó.

> Comment cũ trong SDK còn nói “LevelDB Core”. Hành vi đúng: **write-set + GetState qua Commit Peer** — xem [RW_SET.md](./RW_SET.md).

---

## 6. Host runtime (`WasmEngine`)

**File:** `coreservice/internal/vm/engine.go`.

### Khởi tạo (`NewWasmEngine`)

1. `wazero.NewRuntime`
2. Instantiate `wasi_snapshot_preview1` (khớp TinyGo `-target wasi`)
3. Host module **`env`**: implement `PutState`, `GetState`
4. Sẵn sàng cache compiled module + **module pool** theo tên contract

### Biến môi trường

| Biến | Mặc định | Dùng |
|------|----------|------|
| `COMMIT_PEER_METRICS_URL` / `COMMIT_PEER_METRICS_HTTP` | `http://127.0.0.1:8081` | Base URL `GetState` → `/wallet/state` |
| `WASM_POOL_SIZE` | `16` (max 32) | Số sandbox tái sử dụng / contract |
| `CORE_LOG=debug` | — | Log verbose VM / host |

### Host `PutState`

1. `Memory().Read(keyPtr, keySize)` và value
2. Lấy `*RWSet` từ `context` (gắn lúc `Execute`)
3. **Copy** value (memory guest có thể tái sử dụng) → `rw.PutWrite`
4. Return `1` / `0`

**Không** ghi LevelDB ledger trên Core.

### Host `GetState`

1. **Overlay write-set** cùng tx: nếu key vừa `PutState` trong tx này → trả value, **không** ghi read-set (Fabric-style)
2. Key bị delete trong write-set → `0`
3. Ngược lại: `GET {peer}/wallet/state?key=` → `RecordRead(key, value, version)` vào read-set (phục vụ MVCC)
4. `outCap == 0` → chỉ return độ dài
5. `outCap` nhỏ hơn value → `0`

### Cache & pool

| Cơ chế | Hành vi |
|--------|---------|
| `getOrCompile` | Cache `CompiledModule` theo `contractName`; miss → load WASM LevelDB + `CompileModule` |
| `poolFor` / `acquireModule` | Giữ N instance đã `Instantiate` (`WithStartFunctions("_initialize")`) |
| `releaseModule` | Thành công → trả pool; lỗi → `Close` instance (không reuse) |
| `InvalidateContract` | Xóa cache + đóng toàn bộ pool — **bắt buộc sau deploy** |

---

## 7. Luồng `Execute` từng bước (ptr / size)

**Entry API:** `HandleSubmitTx` → `s.Engine.Execute(ctx, &tx)`.

**Không** allocate lúc Core boot. **Mỗi transaction** chạy một vòng:

### Bước chi tiết

```text
1. getOrCompile(tx.ContractName)
2. acquireModule (sandbox từ pool)
3. rw := &RWSet{}; ctx = withRWSet(ctx, rw)

4. payloadLen := len(tx.Payload)
   ptr := 0
   nếu payloadLen > 0:
      results := allocate.Call(payloadLen)   // guest SDK
      ptr = results[0]
      Memory.Write(ptr, tx.Payload)          // Core nhét JSON vào memory guest

5. callGuest(verify_tx, ptr, payloadLen)     // cùng ptr
6. callGuest(execute,  ptr, payloadLen)     // cùng ptr — KHÔNG allocate lần nữa

7. attachRWSet(tx, rw)
8. release sandbox về pool
```

`callGuest` thực chất:

```go
fn.Call(ctx, ptr, payloadLen)
// return 0 → error "bị Smart Contract từ chối"
```

### Ai tạo `ptr` và `payloadLen`?

| Biến | Nguồn |
|------|--------|
| `payloadLen` | `uint64(len(tx.Payload))` — payload JSON sau enrich |
| `ptr` | Giá trị trả về của **`allocate(payloadLen)`** trên guest; sau đó Core `Write` đúng địa chỉ đó |

`verify_tx` và `execute` **chia sẻ một vùng payload**. Contract đọc bằng:

```go
sdk.PayloadSlice(ptr, size) // unsafe map → []byte → json.Unmarshal
```

### Fallback

Nếu thiếu cả `verify_tx` và `execute`, Core thử gọi `tx.FunctionName` nếu export tồn tại.

### Hình minh họa một tx

```text
tx.Payload = {"from":"499c…","to":"e63b…","amount":10}

allocate(80) ──► ptr=0x1200
Write(0x1200, payload)
                    memory: […|{"from":"499c…","amount":10}|…]
                              ↑ 0x1200

verify_tx(0x1200, 80)  → đọc JSON, check rule, return 1
execute(0x1200, 80)    → GetState/PutState balances, return 1
```

---

## 8. Biên dịch TinyGo

### Toolchain

- Guest: **TinyGo**, target **WASI**
- Host: **wazero** + `wasi_snapshot_preview1`

### Script

```bash
cd coreservice/contracts && ./build_wasm.sh <name>
# → <name>/<name>.wasm + schema.json
# optional: ./build_wasm.sh --all
```

Với một contract:

1. `go run ./cmd/gen_schema -dir ./contracts/<name>` → `schema.json` (có `needs_from` nếu Payload có `from`).
2. `tinygo build -o <name>/<name>.wasm -target wasi -no-debug -scheduler=none ./<name>`.

| Cờ | Lý do |
|----|--------|
| `-target wasi` | Khớp WASI preview1 trên wazero |
| `-no-debug` | Nhỏ / ổn định hơn |
| `-scheduler=none` | Không cần goroutine scheduler trong sandbox |

Output: `<name>/<name>.wasm` + `schema.json`.

---

## 9. Deploy contract

Core **không** auto-deploy khi start. Dùng API deploy (hoặc `/api/deploy-example` cho file cố định `example_asset`).

### API

| Method | Path | Handler |
|--------|------|---------|
| `POST` | `/api/tx/deploy` | `HandleDeployContract` |
| `POST` | `/api/deploy-example` | `HandleDeployExampleAsset` |
| `GET` | `/api/contracts` | Chỉ tên **đã có WASM** trong LevelDB |
| `GET` | `/api/contract/schema?name=` | Schema cho form FE |

### `POST /api/tx/deploy` (multipart ≤ 10 MiB)

| Field | Bắt buộc | Nội dung |
|-------|----------|----------|
| `contract_name` | có | Tên = key LevelDB |
| `file` | có | Bytes `.wasm` |
| `schema` / `schema_file` | không | JSON `ContractSchema` ≤ 1 MiB |
| `payload_schema` | không | JSON inline (legacy) |

Luồng handler:

1. Đọc WASM  
2. `readDeploySchema` (ưu tiên bên dưới)  
3. `SaveContract` LevelDB  
4. **`InvalidateContract`**  
5. `SaveContractMetaSchema` nếu có schema  
6. Mirror Postgres `smart_contracts` (best-effort)  

### Thứ tự schema lúc deploy

1. Upload `schema` / `schema_file`  
2. Form `payload_schema`  
3. File disk `contracts/<name>/schema.json`  
4. Builtin `core/contract_schema.go`  
5. `source=none` — WASM vẫn deploy được  

### Ví dụ

```bash
cd coreservice/contracts && ./build_wasm.sh double_credit

curl -X POST http://127.0.0.1:8080/api/tx/deploy \
  -F 'contract_name=double_credit' \
  -F 'file=@double_credit/double_credit.wasm' \
  -F 'schema=@double_credit/schema.json'
```

### Lưu trữ

| Store | Key / cột | Nội dung |
|-------|-----------|----------|
| LevelDB | `contractName` | WASM |
| LevelDB | `__meta__/schema/<name>` | Schema JSON |
| Postgres `smart_contracts` | `contract_name`, `contract_code`, `payload_schema` | Mirror |

`GET /api/contracts` **không** merge tên builtin chưa deploy (`token`, `voting`, …).

---

## 10. Schema Explorer

### Wire type

```go
type ContractSchema struct {
    Name   string      `json:"name"`
    Fields []FieldSpec `json:"fields"`
}
type FieldSpec struct {
    Name, Label, Type string  // string|number|integer|boolean|address
    Required    bool
    Placeholder string `json:"placeholder,omitempty"`
}
```

### `cmd/gen_schema`

Parse AST `type Payload` trong `main.go`:

- Bỏ common FE: `from`, `to`, `amount`, `address`
- Tag: `schema:"optional"`, `schema:"label=…"`, `schema:"-"` / `skip`

**Schema chỉ phục vụ form FE** (+ cờ `needs_from`). Core **không** JSON-Schema-validate payload lúc submit — chỉ `verify_tx` / `execute` quyết định.

`needs_from`: `gen_schema` bật khi `Payload` có `json:"from"`. FE đọc cờ này (không hard-code tên contract); Core `enrichTransferPayload` cũng theo schema đã deploy/builtin.

Resolve khi FE hỏi: LevelDB meta → Postgres → builtin.

---

## 11. Submit transaction

`POST /api/tx/submit`:

1. Decode `Transaction` (payload JSON / hex tùy client)
2. `enrichTransferPayload` — clear vin/vout; với account contracts (`transfer`, `example_asset`, `double_credit`, `escrow`, `loyalty_xfer`): inject `from` từ session
3. Stamp `client_pubkey`
4. `Engine.Execute` → `tx.rw_set`
5. `signTxViaCommitPeer`
6. Gửi Orderer (discovery)

Payload sau enrich (ví dụ):

```json
{
  "from": "499cd177642d01e80a116bf1cc59ad6d7b97ce95",
  "to": "e63b92ab9b5c4e292581fecadd9a4b95864d4522",
  "amount": 10,
  "memo": ""
}
```

Sau execute thành công, Core có thể ghi `tx_submit_times` (benchmark E2E / pending) — xem metrics; Explorer chủ yếu hiện tx **đã commit**.

---

## 12. Walkthrough: `double_credit`

**File:** `coreservice/contracts/double_credit/main.go`

**Nghiệp vụ:** người gửi mất `amount`; người nhận được `amount * 2`.  
Ví dụ alice gửi 10 → alice −10, bob +20.

### Cấu trúc

| Thành phần | Việc |
|------------|------|
| `Payload` | `from`, `to`, `amount`, `memo` (optional trên schema FE) |
| `balKey` | `"balance:" + addr` |
| `getInt` / `putInt` | Đọc/ghi balance dạng ASCII số qua SDK |
| `verify_tx` | Check địa chỉ 40 hex, amount > 0, khác from/to, không tràn `*2`, memo ≤ 200 — **không** PutState |
| `execute` | Đọc 2 balance → trừ/cộng → `double_receipt:<to>` |

### Pointer trong contract này

| Pointer | Nguồn | Dùng |
|---------|--------|------|
| `ptr`/`size` của payload | Core `allocate` + `Write`, truyền vào verify/execute | `PayloadSlice` → JSON |
| `kPtr`/`vPtr` trong SDK | `&slice[0]` khi Put/Get | Host đọc key/value từ cùng linear memory |
| Buffer `GetState` | `make([]byte, n)` trong `getInt` | Host ghi value balance vào |

### `execute` (ý tưởng số)

```text
credit = amount * 2
fromBal = GetState(balance:from)
toBal   = GetState(balance:to)
require fromBal >= amount
PutState(balance:from, fromBal - amount)
PutState(balance:to,   toBal + credit)
PutState(double_receipt:to, raw_payload_json)
```

Mọi `PutState` chỉ vào RW set đến khi Peer apply.

---

## 13. Contract mẫu khác

| Thư mục | SDK | verify | execute | State |
|---------|-----|--------|---------|--------|
| `transfer` | có | có | có | `balance:`, `discount:` (debit có thể giảm), `xfer_receipt:` |
| `double_credit` | có | có | có | −amount / +2×amount; `double_receipt:` |
| `escrow` | có | có | có | `lock`/`release`/`refund` trên `escrow:<id>` |
| `loyalty_xfer` | có | có | có | fee bậc thang → `balance:treasury` + `loyalty:` + redeem |
| `example_asset` | có | có | có | `Asset_*` + move balance |
| `bench_ping` | không | có | không | không (benchmark) |
| `demo_inventory` | raw import | ghi trong verify | không | `Inv_<sku>` (legacy) |

### `escrow`

- `lock`: trừ `amount` từ `from`, lưu `{from,to,amount}` vào `escrow:<id>` (id trùng → reject).
- `release`: người gửi hoặc người nhận; cộng `amount` cho `to`, xóa escrow.
- `refund`: chỉ `from` gốc; hoàn tiền, xóa escrow.

### `loyalty_xfer`

- Trừ đủ `amount` từ sender; recipient nhận `amount - fee`; fee vào `balance:treasury`.
- Fee: &lt;50 → 0%; 50–99 → 2%; 100–499 → 5%; ≥500 → 8%.
- Điểm `loyalty:<from> += amount/10`; mỗi 100 điểm đốt → +5 balance sender.

Khuyến nghị contract mới: **SDK + tách verify/execute**.

---

## 14. Đa ngôn ngữ

Core chỉ cần `.wasm` **đúng ABI** (WASI + `env` + export trên). Không bắt buộc TinyGo.

| Ngôn ngữ | Thực tế |
|----------|---------|
| **TinyGo** | Đã có SDK — mặc định trong repo |
| **Rust** (`wasm32-wasip1`) | Rất hợp; nên làm SDK thứ hai nếu mở rộng |
| **C** | Bind ABI trực tiếp, binary nhẹ |
| **Java** | “Ra được WASM” ≠ guest WASI mỏng + import `env`; toolchain nặng (TeaVM/…) — không khuyến nghị cho guest contract |

Nguyên tắc: **bất kỳ ngôn ngữ nào xuất đúng ABI đều được**; khác nhau ở độ chín của đường compile guest WASM, không ở Core từ chối ngôn ngữ.

Java (và ngôn ngữ JVM) hợp hơn cho **client SDK submit tx**, không nhất thiết guest on-chain.

---

## 15. Checklist viết contract mới

1. Tạo `coreservice/contracts/<name>/main.go` — `Payload` (có `from` nếu account-model), `import fabricwasm/sdk`, export `verify_tx` + `execute`.
2. `./build_wasm.sh <name>` → `<name>/<name>.wasm` + `schema.json` (`needs_from` tự sinh nếu có `from`).
3. Deploy:

   ```bash
   curl -X POST http://127.0.0.1:8080/api/tx/deploy \
     -F "contract_name=<name>" \
     -F "file=@<name>/<name>.wasm" \
     -F "schema=@<name>/schema.json"
   ```

4. F5 Explorer → chọn contract → submit thử.
5. Redeploy: xác nhận log `Invalidated cache/pool`.

---

## 16. Bản đồ file

| Path | Nội dung |
|------|----------|
| `coreservice/cmd/node/main.go` | Route deploy / submit / schema / metrics |
| `coreservice/internal/api/server.go` | Deploy, submit, enrich, list/schema |
| `coreservice/internal/api/contract_schema_file.go` | Đọc `schema.json` disk |
| `coreservice/internal/api/auth.go` | Seed account — **không** auto-deploy WASM |
| `coreservice/internal/vm/engine.go` | wazero, host ABI, pool, `Execute`, invalidate |
| `coreservice/internal/state/database.go` | LevelDB contract + meta schema |
| `coreservice/internal/storage/postgres.go` | Mirror contract |
| `coreservice/internal/core/contract_schema.go` | Builtin schema |
| `coreservice/internal/core/rwset.go` | RW set + canonical hash |
| `coreservice/internal/core/model.go` | `Transaction` |
| `coreservice/contracts/sdk/*` | Guest SDK |
| `coreservice/contracts/*/main.go` | Contract mẫu |
| `coreservice/cmd/gen_schema/` | AST → schema.json |
| `coreservice/contracts/build_wasm.sh` | Build pipeline |

---

## 17. Lưu ý thiết kế

1. **Schema ≠ validation runtime** — chỉ metadata form Explorer.  
2. **Core không commit KV** — `PutState` → write-set; Peer apply sau MVCC.  
3. **`allocate` một lần / tx** — `verify_tx` và `execute` cùng `ptr`.  
4. **Redeploy phải `InvalidateContract`** — đường deploy chính đã gọi.  
5. **Pool:** lỗi → đóng instance; OK → trả pool.  
6. **Không auto-deploy** — chỉ `POST /api/tx/deploy`.  
7. **List contracts** = LevelDB only (đã deploy).  
8. Comment SDK “LevelDB” đã lỗi thời — xem [RW_SET.md](./RW_SET.md).

---

### Một câu chốt

Smart contract trên nền tảng này là **module WASM** chạy trong wazero trên Core: host cung cấp ABI `PutState`/`GetState` (thu RW set), guest export `allocate`/`verify_tx`/`execute`; mỗi submit Core cấp phát một vùng linear memory cho payload, ghi JSON vào đó, rồi gọi verify + execute với cùng `ptr`/`size` trước khi endorse và đưa vào pipeline order/commit.
