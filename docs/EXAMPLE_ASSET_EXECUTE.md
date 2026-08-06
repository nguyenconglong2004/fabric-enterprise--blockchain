# Contract `example_asset` — execute + balance

`verify_tx` + `execute`: ghi `Asset_<id>`; chuyển `balance:<from>` → `balance:<to>` (discount như transfer).

RW set sau commit phải có `Asset_*` và hai key `balance:`. Chỉ thấy Asset → WASM cũ / chưa invalidate.

```bash
cd coreservice/contracts && ./build_wasm.sh example_asset
curl -X POST http://127.0.0.1:8080/api/tx/deploy \
  -F 'contract_name=example_asset' \
  -F 'file=@example_asset/example_asset.wasm' \
  -F 'schema=@example_asset/schema.json'
```

Balance KV: LevelDB peer. Mirror tx explorer: `commit_peer.ledger_transactions.tx_data` — [POSTGRES_TABLES.md](./POSTGRES_TABLES.md).
