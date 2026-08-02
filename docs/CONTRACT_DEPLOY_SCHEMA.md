# Deploy contract + schema + invalidate WASM cache

## Deploy

```bash
cd coreservice/contracts && ./build_wasm.sh

curl -X POST http://127.0.0.1:8080/api/tx/deploy \
  -F 'contract_name=example_asset' \
  -F 'file=@example_asset/my_contract.wasm' \
  -F 'schema=@example_asset/schema.json'
```

Schema: file `schema` → (legacy form) → builtin → hoặc `schema.json` cạnh wasm lúc seed.

## Invalidate

Sau `SaveContract` → `InvalidateContract` (xóa compiled + pool). Không invalidate = chạy WASM cũ.

Postgres mirror: `core_service.smart_contracts` (`contract_code`, `payload_schema`) — xem [POSTGRES_TABLES.md](./POSTGRES_TABLES.md). Runtime chính vẫn LevelDB Core.
