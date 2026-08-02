# Account model + KV balance (không UTXO)

Balance on-chain = KV trên **Commit Peer LevelDB**, không phải cột Postgres.

```text
Submit → Core WASM (verify_tx / execute) → rw_set
      → Commit Peer ApplyBlock → kv:balance:<addr>, …
Explorer đọc tip từ commit_peer.ledger* (Postgres mirror).
```

Keys: `balance:<40hex>`, `discount:<40hex>` (debit = ceil(amount/(1+d))).

Demo seed: alice / bob / charlie — `password123` (mint lần đầu vào KV). Identity: bảng `wallet.accounts` — chi tiết [POSTGRES_TABLES.md](./POSTGRES_TABLES.md).

RW set: [RW_SET.md](./RW_SET.md). Chạy hệ thống: [SETUP_RUN.md](./SETUP_RUN.md).
