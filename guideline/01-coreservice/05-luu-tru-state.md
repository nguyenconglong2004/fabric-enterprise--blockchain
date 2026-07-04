# Core Service — Lưu trữ: LevelDB & PostgreSQL

> Mã nguồn: `coreservice/internal/state/database.go`, `coreservice/internal/storage/`

Core Service dùng hai loại lưu trữ với mục đích khác nhau.

## 1. LevelDB — lưu trữ nhúng cục bộ (`internal/state/database.go`)

**[LevelDB](https://github.com/google/leveldb)** (qua [goleveldb](https://github.com/syndtr/goleveldb)) là CSDL key-value nhúng — chạy ngay trong tiến trình, không cần server riêng. Core Service mở **hai instance**:

| Kho | Thư mục | Lưu gì | Định dạng key |
|-----|---------|--------|---------------|
| **ContractDB** | `./data/contract_db/` | Mã WASM của contract | tên contract |
| **ContractDB** | (cùng) | Schema UI của contract | `__meta__/schema/{tên}` |
| **LedgerDB** | `./data/ledger_db/` | World state (kết quả contract ghi) | key tùy contract |

Các thao tác chính:
- `SaveContract(name, wasmCode)` / `GetContract(name)` — lưu & lấy bytecode.
- `SaveContractMetaSchema(name, json)` — lưu schema.
- `PutState(key, value)` — hàm host mà contract WASM gọi để ghi world state (xem [02-wasm-smart-contract.md](02-wasm-smart-contract.md)).
- `ListContracts()` — liệt kê contract đã deploy.

Khi mở DB, có cơ chế **thử lại 3 lần** và **xóa file LOCK** nếu tiến trình trước thoát đột ngột để lại khóa.

> **Vì sao tách ContractDB và LedgerDB?** Để mã chương trình (ít đổi) và dữ liệu trạng thái (đổi liên tục) không lẫn lộn, dễ sao lưu/dọn dẹp riêng.

## 2. PostgreSQL — bản sao tra cứu (`internal/storage/postgres.go`)

PostgreSQL **không** nằm trong luồng đồng thuận; nó là **bản sao (mirror)** chỉ-đọc của sổ cái Committing Peer, phục vụ:
- Hiển thị block/giao dịch trên Explorer (`/api/blocks`, `/api/transactions`).
- Lưu danh mục contract đã deploy.
- Đo lường benchmark.

Kết nối qua biến `POSTGRES_URL`. Các bảng Core Service đụng tới (lược đồ đầy đủ tại [05-co-so-du-lieu/](../05-co-so-du-lieu/01-postgres-schema.md)):

| Bảng | Vai trò với Core |
|------|------------------|
| `core_service.smart_contracts` | Lưu mã + schema contract |
| `core_service.tx_submit_times` | Ghi thời điểm submit (đo E2E) |
| `commit_peer.ledger` | Đọc block đã commit để hiển thị |
| `commit_peer.ledger_transactions` | Đọc giao dịch đã commit |

Nếu PostgreSQL không sẵn sàng, Core vẫn chạy; các API tra cứu trả lỗi 503 nhưng luồng submit giao dịch không bị ảnh hưởng.

## 3. SubmitRecorder — ghi thời điểm submit hiệu năng cao (`storage/submit_recorder.go`)

Để đo **E2E latency** (thời gian từ submit đến commit), cần biết chính xác lúc nào Core chấp nhận từng giao dịch. Nhưng gọi DB cho **mỗi** giao dịch ở tốc độ 5000 TPS sẽ làm sập DB.

Giải pháp: **batch INSERT qua channel**:
- Mỗi giao dịch chấp nhận → đẩy `(txid, submitted_at)` vào một channel đệm lớn (buffer rất lớn, ~65k).
- Một goroutine nền gom nhiều bản ghi rồi `INSERT` hàng loạt, hoặc xả định kỳ (vài chục ms).
- Bật/tắt bằng `CORE_RECORD_SUBMIT` (mặc định bật).

Đây là pattern **batching** kinh điển để giảm số lần gọi I/O — overhead thấp hơn nhiều so với "một goroutine mỗi giao dịch".

## 4. Truy vấn throughput & benchmark (`storage/throughput.go`, `storage/benchmark.go`)

Các truy vấn SQL tính throughput/latency theo cửa sổ thời gian. Chi tiết công thức tại [06-benchmark-hieu-nang/](../06-benchmark-hieu-nang/01-benchmark-metrics.md). Tóm tắt:
- `GetThroughputLatest/Peak/Window/Since`: đếm giao dịch & block commit trong cửa sổ.
- `GetBenchmarkMetrics`: ghép `tx_submit_times` với thời điểm commit → tính phân vị latency (p50/p95/p99).

➡️ Tiếp: [06-api-va-metrics.md](06-api-va-metrics.md)
