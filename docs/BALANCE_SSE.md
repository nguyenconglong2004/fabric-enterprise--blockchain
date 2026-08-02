# Balance SSE (Wallet)

```http
GET /api/wallet/balance/stream?address=<40hex>&token=<optional>
```

Ưu tiên `address=` (trùng Wallet đang mở). Poll 1s; re-emit balance ~3s; ping keepalive.

FE: full address + một nút Copy; pin address vào EventSource.

Số dư thật: Commit Peer `GET /wallet/balance` (LevelDB). Postgres không lưu balance ledger — chỉ `wallet.accounts.initial_balance` (seed). Chi tiết bảng: [POSTGRES_TABLES.md](./POSTGRES_TABLES.md).
