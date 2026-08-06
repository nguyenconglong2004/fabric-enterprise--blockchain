# Deploy contract + auto schema + invalidate WASM cache

Tài liệu đầy đủ (ABI, linear memory, Execute, SDK, walkthrough): [CORE_CONTRACT_WASM.md](./CORE_CONTRACT_WASM.md).

## Schema tự sinh (không viết tay)

Mỗi contract có `type Payload struct` trong `main.go`. Khi build:

```bash
cd coreservice/contracts && ./build_wasm.sh example_asset
```

Script gọi `go run ./cmd/gen_schema` → ghi `contracts/<name>/schema.json`, rồi TinyGo → `<name>/<name>.wasm`.

Tag tùy chọn trên field:

- `` `schema:"optional"` `` — không bắt buộc trên form FE
- `` `schema:"label=Product SKU"` `` — nhãn hiển thị
- `` `schema:"-"` `` — không đưa vào schema

## Deploy

Chỉ cần upload WASM — Core tự đọc `schema.json` cạnh source (hoặc upload schema nếu muốn override):

```bash
cd coreservice/contracts && ./build_wasm.sh example_asset

curl -X POST http://127.0.0.1:8080/api/tx/deploy \
  -F 'contract_name=example_asset' \
  -F 'file=@example_asset/example_asset.wasm'
```

Thứ tự ưu tiên schema lúc deploy: **upload** → form `payload_schema` → **file disk** (`gen_schema`) → builtin `contract_schema.go`.

## Invalidate

Sau `SaveContract` → `InvalidateContract`. Không invalidate = chạy WASM cũ.

## Frontend

Sau deploy: **F5** trang Transfer — dropdown lấy `GET /api/contracts`, form lấy `GET /api/contract/schema?name=...`.

Postgres mirror: `core_service.smart_contracts.payload_schema` — xem [POSTGRES_TABLES.md](./POSTGRES_TABLES.md).
