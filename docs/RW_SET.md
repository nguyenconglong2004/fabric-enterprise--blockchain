# RW Set + MVCC — cập nhật

Tài liệu mô tả luồng **simulate → endorse → order → validate/apply** sau khi chuyển state KV sang Commit Peer và bổ sung **MVCC** trên read-set.

Liên quan: [SETUP_RUN.md](./SETUP_RUN.md) · [ACCOUNT_UTXO_AUTH.md](./ACCOUNT_UTXO_AUTH.md) · [POSTGRES_TABLES.md](./POSTGRES_TABLES.md)

---

## 1. Trước / sau

| | Trước | Sau |
|--|--------|-----|
| `PutState` (WASM) | Ghi LevelDB **Core** | Chỉ ghi **write-set** trong RAM (tx) |
| `GetState` | Đọc LevelDB Core | Overlay write-set → HTTP Commit Peer `/wallet/state` |
| State đã commit | Core | Commit Peer LevelDB (`kv:` + `ver:`) |
| Commit | Apply mù writes | **MVCC** trên reads, rồi apply writes nếu VALID |
| Conflict concurrent | Có thể double-spend / lost update | Tx sau → `INVALID_MVCC`, skip writes |

---

## 2. Luồng end-to-end

```text
Client Submit (/api/submit)
  → Core: enrich payload (vd. transfer inject `from` từ session)
  → WASM verify_tx / execute
       PutState  → rw.Writes (không persist Core)
       GetState  → nếu hit write-set cùng tx: trả value, KHÔNG ghi Reads
                 → ngược lại: GET peer /wallet/state → RecordRead(key, value, version)
  → gắn tx.rw_set
  → Commit Peer ký endorsement
       message = txid || contract || payload || 0x00 || sha256(canonical_rw_set)
  → Orderer (passthrough rw_set) → block → Deliver
  → Commit Peer ValidateBlock (Merkle + hash + chữ ký endorser)
  → AppendBlock
  → ApplyBlock(height):
       for mỗi tx theo thứ tự:
         so mỗi read.version với version hiện tại (+ overlay tx VALID trước đó trong block)
         VALID        → apply Writes; bump ver = "<height>:<txIndex>"
         INVALID_MVCC → không apply; log peer; block vẫn commit
```

Orderer **không** kiểm MVCC / endorsement content — chỉ xếp thứ tự.

---

## 3. Wire format `rw_set`

Field trên transaction JSON: `rw_set` (`omitempty` nếu rỗng / không đụng KV).

```json
{
  "reads": [
    {
      "key": "balance:abcdef...",
      "version": "admin:1710000000000:1",
      "value": "<hex optional — snapshot lúc simulate>"
    }
  ],
  "writes": [
    {
      "key": "balance:abcdef...",
      "value": "<hex>",
      "is_delete": false
    }
  ]
}
```

### Version

| Trường hợp | `version` |
|------------|-----------|
| Key chưa từng tồn tại lúc simulate | `""` (chuỗi rỗng) |
| Sau mint / `PutKV` admin | `admin:<unixNano>:<seq>` |
| Sau tx VALID apply | `<blockHeight>:<txIndexInBlock>` (height 1-based local peer) |

**MVCC chỉ so `version`**, không so `value`. Canonical endorsement hash JSON đã sort theo key (gồm field `version`).

---

## 4. MVCC trên Commit Peer

### LevelDB schema

| Prefix | Nội dung |
|--------|----------|
| `kv:<key>` | Raw value (balance, receipt, contract state, …) |
| `ver:<key>` | Version string hiện tại |

Không dùng Postgres cho version / balance ledger. **Không cần migration SQL** cho MVCC.

### Quy tắc

1. Duyệt tx **theo thứ tự trong block**.
2. Với mỗi read: `currentVersion(key) == read.Version`?
   - `currentVersion` = overlay trong block nếu key vừa được tx VALID trước đó ghi; không thì đọc `ver:` từ DB; thiếu → `""`.
3. Sai → `INVALID_MVCC`, **bỏ qua toàn bộ writes** của tx đó.
4. Đúng → apply writes; mọi key ghi/xóa trong tx dùng cùng version mới `"<height>:<index>"` (delete → xóa cả `kv:` và `ver:`, overlay version = `""`).
5. Tx không có `rw_set` → VALID, không đụng KV.

### Overlay cùng transaction (Core)

Giống Fabric: `GetState` thấy key đã `PutState` trong **cùng** lần simulate → trả từ write-set, **không** thêm vào `reads`. Tránh false conflict với chính writes của mình.

### Quan sát lúc chạy

Log Commit Peer:

```text
[peer] tx <txid> INVALID_MVCC: read-set version mismatch on key balance:...
[peer] committed block hash=... txs=N valid=M
```

`/wallet/state` sau apply:

```bash
curl -s 'http://127.0.0.1:8081/wallet/state?key=balance:<ADDR>' | jq
# { "key", "found", "value": "<hex>", "version": "12:0" }
```

---

## 5. Endorsement (breaking)

```text
txid || contract_name || payload || 0x00 || sha256(canonical_rw_set_json)
```

- `canonical` = JSON sort reads/writes theo `key`, rồi SHA-256.
- RW rỗng / null → sau `0x00` không thêm bytes hash (digest input rỗng).
- Peer **ký** những gì Core gửi (không re-simulate lúc endorse); lúc **commit** mới MVCC.

**Restart Commit Peer + Core** cùng bản code. Binary lệch (có/không `version` trong rw) → verify endorsement fail.

---

## 6. API liên quan

| Endpoint | Vai trò |
|----------|---------|
| `GET :8081/wallet/state?key=` | Peer: `{ found, value (hex), version }` |
| `GET :8080/api/state?key=` | Core proxy sang peer |
| `POST :8081/wallet/mint` | Admin ghi balance → bump version `admin:…` |
| `GET :8081/wallet/balance?address=` | Đọc `kv:balance:<addr>` |

Mint off-chain làm tăng version → tx đang pending có thể `INVALID_MVCC` (đúng ý MVCC; client retry).

---

## 7. File chính

| Layer | Path |
|-------|------|
| Core types + RecordRead | `coreservice/internal/core/rwset.go`, `model.go` |
| WASM host Put/GetState | `coreservice/internal/vm/engine.go` |
| Core submit / proxy state | `coreservice/internal/api/server.go` |
| Orderer passthrough | `orderingservice/source/internal/types/rwset.go`, `transaction.go` |
| Peer types | `commitingpeer/source/internal/types/rwset.go` |
| MVCC ApplyBlock | `commitingpeer/source/internal/storage/world_state.go` |
| `/wallet/state` | `commitingpeer/source/internal/metrics/wallet.go` |
| Endorse sign | `commitingpeer/source/internal/deliver/sign.go`, `crypto/signer.go` |
| Block integrity + endorser sig | `commitingpeer/source/internal/validation/engine.go` |
| Commit loop + log INVALID | `commitingpeer/source/internal/peer/peer.go` |
| Unit test MVCC | `commitingpeer/source/internal/storage/world_state_test.go` |

---

## 8. Demo / kiểm thử

### Happy path

1. Postgres → Orderer → Peer (`:8081`) → Core → Explorer ([SETUP_RUN.md](./SETUP_RUN.md)).
2. Login `alice` / `password123`, Submit contract `transfer`.
3. Sau block:

```bash
curl -s "http://127.0.0.1:8081/wallet/state?key=balance:<ALICE>" | jq
curl -s "http://127.0.0.1:8081/wallet/state?key=xfer_receipt:<TO>" | jq
```

4. Contract không đụng state (`bench_ping`): `rw_set` null/rỗng, vẫn commit.

### MVCC conflict

1. Đảm bảo Alice có balance (seed/mint).
2. Submit **hai** transfer gần như cùng lúc (cùng đọc version balance cũ).
3. Kỳ vọng:
   - Một tx apply (balance đổi đúng một lần trừ).
   - Peer log `INVALID_MVCC` cho tx còn lại.
4. Unit test không cần cluster:

```bash
cd commitingpeer/source && go test ./internal/storage/ -count=1 -run MVCC
```

---

## 9. Phạm vi đã làm / chưa làm

| Hạng mục | Trạng thái |
|----------|------------|
| Thu thập rw_set trên Core | Done |
| Endorsement bind canonical rw_set | Done |
| Apply writes trên Peer | Done |
| Version trên read + `/wallet/state` | Done |
| MVCC lúc ApplyBlock (+ overlay trong block) | Done |
| Overlay cùng-tx không ghi read-set | Done |
| Postgres / explorer cột `validation_code` | Chưa (optional — INVALID chỉ thấy ở log peer) |
| Cache KV phía Core + invalidate | Chưa (Core không cache committed KV) |
| Peer re-execute WASM | Không làm (tin endorse + MVCC) |
| `verifyPrevHash` / `ValidateTransaction` stub | Chưa gắn (ngoài scope RW/MVCC) |

---

## 10. Ghi chú thesis / thiết kế

- Mô hình gần Fabric **endorsement + MVCC**, rút gọn: không VSCC đa endorser phức tạp; một trusted commit-peer key.
- An toàn concurrent dựa vào **version read-set**, không dựa vào lock lúc simulate.
- Client gặp `INVALID_MVCC` → đọc lại state → submit lại (retry), giống ứng dụng Fabric.
