# Explorer — tip ledger & lọc tx theo user

## Tip block / tx

Peer commit OK nhưng FE không đổi → tip Postgres còn `block_number` cũ rất cao.

**Fix:** list / SSE theo `committed_at DESC` (+ `id`), không theo `block_number`.  
Bảng: [`commit_peer.ledger`](./POSTGRES_TABLES.md) / `ledger_transactions`.

## Lọc tx theo user

```http
GET /api/transactions?limit=100&username=alice
```

- Có session → username lấy từ session.
- Không username/session → `[]`.
- Khớp nếu address là **`payload_decoded.from` hoặc `payload_decoded.to`**, hoặc khớp `client_pubkey` trong `tx_data` (người submit).

FE chỉ load khi đã login; SSE cũng giữ tx thuộc account (gửi **hoặc** nhận).
