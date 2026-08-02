# PostgreSQL — schemas & tables

Nguồn: [init.sql](../init.sql), migrations [`003_accounts.sql`](../migrations/003_accounts.sql), [`004_wallet_schema.sql`](../migrations/004_wallet_schema.sql).

Kết nối mặc định: `postgres://fabric:fabric123@localhost:5432/blockchain`

**Lưu ý:** số dư on-chain (`balance:<addr>`) và KV từ `rw_set` nằm trên **Commit Peer LevelDB** (`worldstate`), **không** trong Postgres. Postgres phục vụ explorer mirror + identity + contract metadata.

---

## Schemas

| Schema | Vai trò |
|--------|---------|
| `core_service` | WASM contract metadata, submit timestamps (benchmark) |
| `order_service` | Mirror / audit phía orderer (blocks, txs) |
| `commit_peer` | **Explorer tip** — block/tx sau khi peer commit |
| `wallet` | Account demo (login identity) — không phải ledger balance |

---

## `commit_peer` (Explorer đọc từ đây)

### `commit_peer.ledger`

| Cột | Kiểu | Ý nghĩa |
|-----|------|---------|
| `id` | SERIAL PK | |
| `block_hash` | VARCHAR UNIQUE | hex hash |
| `block_number` | BIGINT | chiều cao local peer (**không UNIQUE** — có thể trùng giữa các lần chạy) |
| `block_data` | JSONB | block JSON (hash trong JSON thường base64) |
| `num_transactions` | INT | |
| `committed_at` | TIMESTAMP | thời điểm mirror |

Explorer list tip theo **`committed_at DESC`**, không theo `block_number` (tránh tip cũ số cao từ load-test).

### `commit_peer.ledger_transactions`

| Cột | Kiểu | Ý nghĩa |
|-----|------|---------|
| `id` | SERIAL PK | |
| `block_id` | FK → ledger.id | |
| `txid` | VARCHAR | |
| `tx_index` | INT | |
| `tx_data` | JSONB | full tx + thường có `payload_decoded`, `rw_set`, `client_pubkey` |

Filter “tx của user”: `tx_data->payload_decoded->>'from'` hoặc `tx_data->>'client_pubkey'`.

---

## `wallet` (identity)

### `wallet.accounts`

| Cột | Ý nghĩa |
|-----|---------|
| `username` | alice / bob / charlie |
| `password_hash` | bcrypt |
| `address` | 40-hex P2PKH |
| `pubkey_hex` / `seed_hex` | keypair demo |
| `discount` | metadata (discount on-chain vẫn ở KV `discount:<addr>`) |
| `initial_balance` | số mint lần đầu — **không** cập nhật khi transfer |

### `wallet.sessions`

| Cột | Ý nghĩa |
|-----|---------|
| `token` | session (VARCHAR 64) |
| `account_id` | FK accounts |
| `expires_at` | |

Legacy: từng có `core_service.accounts` / `sessions` — migration copy sang `wallet.*` nếu `wallet.accounts` trống.

---

## `core_service`

### `core_service.smart_contracts`

| Cột | Ý nghĩa |
|-----|---------|
| `contract_name` | UNIQUE |
| `contract_code` | BYTEA wasm |
| `payload_schema` | JSONB schema FE (optional) |

WASM runtime chính vẫn load từ **LevelDB Core**; Postgres là bản mirror/deploy API.

### `core_service.tx_submit_times`

| Cột | Ý nghĩa |
|-----|---------|
| `txid` | PK |
| `submitted_at` | benchmark E2E |

---

## `order_service` (audit / orderer mirror)

- `order_service.blocks` — `block_hash` UNIQUE, `block_number` UNIQUE  
- `order_service.transactions` — tx trước/khi order  
- `order_service.block_transactions` — M2M block ↔ txid  

Explorer **committed** view ưu tiên `commit_peer.*`, không dùng order_service làm tip live.

---

## Ngoài Postgres

| Store | Nội dung |
|-------|----------|
| Commit Peer LevelDB `worldstate` | `kv:balance:<addr>`, `kv:discount:<addr>`, `kv:Asset_*`, … |
| Core LevelDB | WASM bytecode + meta schema deploy |
| Peer `chain.block` | chuỗi block local |

---

## Query hữu ích

```sql
-- Tip explorer
SELECT block_number, block_hash, committed_at
FROM commit_peer.ledger
ORDER BY committed_at DESC NULLS LAST, id DESC
LIMIT 5;

-- Tx mới + from
SELECT l.block_hash, lt.txid,
       lt.tx_data->'payload_decoded'->>'from' AS from_addr,
       lt.tx_data->>'contract_name' AS contract
FROM commit_peer.ledger_transactions lt
JOIN commit_peer.ledger l ON l.id = lt.block_id
ORDER BY l.committed_at DESC NULLS LAST
LIMIT 10;

-- Accounts
SELECT id, username, address, discount, initial_balance FROM wallet.accounts;
```
