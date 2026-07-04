# Cơ sở dữ liệu — Lược đồ PostgreSQL

> Mã nguồn: `init.sql`, `docker-compose.yml`, `migrations/`

## 1. Vai trò của PostgreSQL trong hệ thống

Cần nhấn mạnh lại: **PostgreSQL KHÔNG nằm trong luồng đồng thuận**. Sổ cái thật là file `chain.block` + LevelDB ở Committing Peer. PostgreSQL chỉ là **bản sao (mirror)** phục vụ:
- Hiển thị block/giao dịch trên Explorer.
- Lưu danh mục smart contract.
- Đo lường benchmark (submit & E2E latency).

Nếu PostgreSQL chết, blockchain vẫn chạy bình thường.

## 2. Khởi chạy (`docker-compose.yml`)

```yaml
postgres:15-alpine
  user=fabric, password=fabric123, db=blockchain
  port 5432:5432
  init script: init.sql
  healthcheck: pg_isready
```

Chuỗi kết nối mặc định mọi dịch vụ dùng:
```
postgres://fabric:fabric123@localhost:5432/blockchain?sslmode=disable
```

## 3. Ba schema, tách theo dịch vụ

Mỗi dịch vụ ghi vào schema riêng — phản ánh đúng ranh giới trách nhiệm:

| Schema | Ai ghi | Nội dung |
|--------|--------|----------|
| `core_service` | Core Service | Contract, thời điểm submit |
| `order_service` | (Orderer — tùy chọn) | Giao dịch & block đã sắp xếp |
| `commit_peer` | Committing Peer | Sổ cái đã commit (mirror) |

## 4. Các bảng (`init.sql`)

### `core_service.smart_contracts`
Lưu contract đã deploy.
| Cột | Kiểu | Ghi chú |
|-----|------|---------|
| `contract_name` | VARCHAR UNIQUE | Tên contract |
| `contract_code` | BYTEA | Bytecode WASM |
| `payload_schema` | JSONB | Schema field cho UI |
| `created_at/updated_at` | TIMESTAMP | |

### `core_service.tx_submit_times`
Đo E2E latency (ghi bởi SubmitRecorder, khi `CORE_RECORD_SUBMIT=1`).
| Cột | Kiểu |
|-----|------|
| `txid` | VARCHAR PRIMARY KEY |
| `submitted_at` | TIMESTAMPTZ |
Có index theo `submitted_at DESC` để truy vấn cửa sổ nhanh.

### `order_service.transactions` / `blocks` / `block_transactions`
Giao dịch & block ở tầng sắp xếp (kèm `tx_type` = `UTXO`/`SMART_CONTRACT`, dữ liệu JSONB, quan hệ M-N giữa block và giao dịch). Có nhiều index trên `txid`, `block_hash`, `block_number`.

### `commit_peer.ledger` / `ledger_transactions`
**Sổ cái đã commit** (bản sao chính dùng cho Explorer & benchmark).
- `commit_peer.ledger`: `block_hash` (UNIQUE), `block_number`, `block_data` (JSONB), `num_transactions`, `committed_at`.
- `commit_peer.ledger_transactions`: liên kết `block_id` → `txid`, `tx_index`, `tx_data` (JSONB). Khóa ngoại `ON DELETE CASCADE`.

> **Lưu ý:** UTXO world state **không** ở PostgreSQL — nó nằm trên đĩa (LevelDB) tại Committing Peer (xem comment cuối `init.sql`).

## 5. Sơ đồ quan hệ rút gọn

```
core_service.smart_contracts        (contract deploy)
core_service.tx_submit_times        (đo submit) ──┐
                                                  │ join theo txid
commit_peer.ledger ──1:N──▶ commit_peer.ledger_transactions ──┘ (đo commit)
        ▲
        │ committed_at  → tính throughput & E2E latency
```

## 6. Migration (`migrations/002_tx_submit_times.sql`)

Nếu DB tạo trước khi có tính năng đo submit, chạy:
```bash
docker exec -i fabric-postgres psql -U fabric -d blockchain < migrations/002_tx_submit_times.sql
```

Cách dùng các bảng này để tính throughput/latency: xem [06-benchmark-hieu-nang/](../06-benchmark-hieu-nang/01-benchmark-metrics.md).
