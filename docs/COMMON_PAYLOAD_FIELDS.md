# Common payload fields (Amount + Address)

## FE — Submit

**Common** luôn có:

| Field | JSON key | Ý nghĩa |
|-------|----------|---------|
| Amount | `amount` | integer > 0 |
| Address (to) | `to` | 40-hex người nhận |

Preset `→ alice|bob|charlie` điền đủ 40 hex. Schema contract không lặp `amount`/`to`.

`from`: FE gắn `account.address` khi login; Core có thể ghi đè nếu có session.

Schema file: `coreservice/contracts/<name>/schema.json`.  
Account/address trong DB: [POSTGRES_TABLES.md](./POSTGRES_TABLES.md) → `wallet.accounts`.
