# RW Set: simulate trên Core, lưu trên Commit Peer

## Tóm tắt thay đổi

Trước đây `PutState` trong WASM ghi thẳng LevelDB của **Core**. Giờ:

1. Core **chỉ thu thập** read/write set khi `verify_tx` / `execute`
2. `rw_set` gắn vào transaction JSON, đi qua ký endorsement → orderer → deliver
3. Commit Peer **apply write-set** vào LevelDB (`kv:<key>`) trong `ApplyBlock`

```text
submit
  → (transfer: inject from từ session)
  → WASM verify_tx / execute
       PutState → write-set (không persist Core)
       GetState → write-set overlay → GET CommitPeer /wallet/state
  → gắn tx.rw_set
  → Commit Peer ký (message gồm canonical rw_set)
  → Orderer → deliver
  → ApplyBlock: chỉ kv writes từ rw_set
```

## Wire format `rw_set`

```json
{
  "reads":  [{ "key": "...", "value": "<hex optional>" }],
  "writes": [{ "key": "...", "value": "<hex>", "is_delete": false }]
}
```

Field trên transaction: `rw_set` (omitempty nếu rỗng).

## Endorsement (breaking)

Message ký mới:

```text
txid || contract_name || payload || 0x00 || sha256(canonical_rw_set_json)
```

`canonical` = JSON đã sort key reads/writes. RW set rỗng → chỉ thêm `0x00` (không có bytes sau).

**Cần restart Commit Peer + Core** với binary mới. Chữ ký cũ (không có `0x00`/rw) sẽ verify fail.

## API

| Endpoint | Vai trò |
|----------|---------|
| `GET :8081/wallet/state?key=` | Commit Peer: KV đã commit (`found`, `value` hex) |
| `GET :8080/api/state?key=` | Core proxy sang Commit Peer |

## File chính

- Core: `internal/core/rwset.go`, `model.go`, `internal/vm/engine.go`, `api/server.go`
- Orderer: `internal/types/rwset.go`, `transaction.go` (passthrough)
- Commit Peer: `types/rwset.go`, `storage/world_state.go`, `metrics/wallet.go`, `crypto/signer.go` (+ ed25519/mldsa), `deliver/sign.go`, `validation/engine.go`

## Demo nhanh

1. Commit Peer metrics `:8081` lên trước.
2. Restart Core (seed/mint như cũ).
3. Submit contract có `PutState` (vd. `transfer` hoặc `example_asset`).
4. Sau block commit:

```bash
curl -s 'http://127.0.0.1:8081/wallet/state?key=xfer_receipt:<addr>' | jq
# hoặc
curl -s 'http://127.0.0.1:8080/api/state?key=xfer_receipt:<addr>' | jq
```

5. `bench_ping` (không PutState): `rw_set` null/rỗng, vẫn submit được.

## Chưa làm (phase sau)

- MVCC version trên read-set lúc commit
- Đồng bộ / invalidate cache nếu sau này Core cache lại KV
