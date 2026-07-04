# Coreservice — Phân tích tối ưu hiệu năng

Phân tích các điểm nghẽn (bottleneck) ảnh hưởng tốc độ **gửi/nhận transaction**, **ký**, và đường đi tới việc đóng block. `coreservice` là node front-end (submit/endorse) kiểu Hyperledger Fabric.

## Đường đi nóng (hot path) của 1 transaction

```
HTTP POST /api/tx/submit
  → JSON decode
  → WASM execute (ghi state vào LevelDB)
  → ký qua commit peer (round-trip libp2p)
  → gửi endorsement async tới orderer
  → ghi submit-time (batched insert Postgres)
  → JSON response
```

Việc đóng block thực sự nằm ở **commit peer** (service riêng). Node này đọc lại dữ liệu đã commit từ Postgres cho metrics/explorer.

---

## 1. Đường gửi / submit transaction (nóng nhất)

### 1.1 `os.Getenv` gọi mỗi transaction (nhiều lần/tx) — `vm/verbose.go:6`, `api/server.go:24`
```go
func Verbose() bool { return os.Getenv("CORE_LOG") == "debug" }
```
`apiVerbose()` gọi trong `HandleSubmitTx` (server.go:155, 173-174, 192), trong `signTxViaCommitPeer` (server.go:257, 266), `Execute` gọi `Verbose()` nữa (engine.go:50, 66, 132, 138, 307). `os.Getenv` lấy lock global của runtime + scan tuyến tính environment block mỗi lần.

- **Vì sao chậm:** 5000 tx/s = hàng chục nghìn `os.Getenv`/s, mỗi lần khóa process-global → contention thuần, không có lợi ích chức năng.
- **Fix:** Resolve 1 lần lúc startup → `var verbose = os.Getenv("CORE_LOG")=="debug"` (hoặc `atomic.Bool`). Áp dụng cho `SignPoolEnabled()`, `signRequestTimeout()`, `asyncEndorse()`, `endorseLeaderOnly()`, `RecordSubmitEnabled()`, `ModulePoolSize()` — tất cả đọc env trên hot path.

### 1.2 `ModulePoolSize()` parse env mỗi lần acquire — `vm/engine.go:87-100`, gọi tại `engine.go:166`
`poolFor` → `modulePoolSize()` → `os.Getenv("WASM_POOL_SIZE")` + `strconv.Atoi` mỗi lần tạo pool. Cache giá trị đã parse 1 lần.

### 1.3 `signRequestTimeout()` parse env + duration mỗi lần ký — `network/commit_peer_sign_pool.go:26-35`, gọi tại `signOnce` line 136
Mỗi tx ký = `os.Getenv("CORE_SIGN_TIMEOUT")` + `time.ParseDuration`. **Fix:** resolve thành `time.Duration` cache lúc tạo pool.

### 1.4 `SignPoolEnabled()` đọc env mỗi tx — `transport.go:196`, `commit_peer_sign_pool.go:21-24`
Cache 1 lần.

### 1.5 JSON encode/decode toàn bộ Transaction 4-5 lần/tx — `core/model.go:61-150`, `commit_peer_sign_pool.go:145-166`, `transport.go:188`
Custom `UnmarshalJSON`/`MarshalJSON` đắt:
- Cấp phát struct `Alias` riêng + copy ~12 field mỗi chiều (model.go:62-92, 113-149).
- `UnmarshalJSON` làm `hex.DecodeString(aux.Payload)` (model.go:96) — alloc + hex decode/tx.
- Tx bị JSON-encode lần 2 để gửi commit peer (`signOnce` line 145), decode lần 3 (line 154), re-unmarshal vào chính tx (line 166). Encode→decode→unmarshal/tx chỉ để ký.
- Encode lần 4 cho endorsement tới orderer (transport.go:188), lần 5 cho HTTP response (server.go:205).

- **Vì sao chậm:** 4-5 lần serialize JSON toàn struct/tx, mỗi lần custom marshaler alloc Alias + hex string. JSON reflection + hex là chi phí CPU chủ đạo ở tx rate cao.
- **Fix:** (a) Codec nhanh hơn (`sonic`/`jsoniter`, hoặc protobuf/msgpack trên wire libp2p tới commit peer). (b) Tránh double-marshal: commit peer chỉ cần thêm signature → trả về chỉ signature/endorsement entry, bỏ re-unmarshal cả tx. (c) Giữ payload dạng raw bytes trên wire thay vì hex (hex tăng gấp đôi size + encode/decode).

### 1.6 `json.NewDecoder(r.Body).Decode` không limit / không reuse buffer — `server.go:148`
Alloc decoder mới/request, đọc body không giới hạn. Dùng `io.LimitReader` + `sync.Pool` cho buffer. Response dùng `json.NewEncoder(w).Encode(map[string]interface{}{...})` (server.go:205) — map literal 7 entry alloc/response; dùng struct typed + encoder pool rẻ hơn.

### 1.7 Ký qua commit-peer đồng bộ chặn goroutine HTTP — `server.go:173`, `commit_peer_sign_pool.go:67-90`
`HandleSubmitTx` gọi `signTxViaCommitPeer` đồng bộ, chặn tới khi commit peer round-trip xong. Pool giữ ấm **connection** (tốt) nhưng **mở stream mới mỗi tx** (commit_peer_sign_pool.go:139; comment line 38-39 xác nhận commit peer đóng sau 1 round-trip). Stream setup tốn negotiation overhead.
- **Fix:** Pipeline/batch signing. (a) gửi N tx/stream (batch sign request), hoặc (b) stream bidirectional long-lived multiplexed, request gắn id và correlate response → amortize chi phí mở stream. Hoặc đơn giản: pool **stream** chứ không chỉ connection.

### 1.8 Không cap concurrency / backpressure khi submit — `main.go:184`, `server.go:141`
`http.Server` spawn goroutine/request không giới hạn. Khi WASM pool (max 32, engine.go:98) hoặc commit-peer bão hòa, request dồn lại và `acquireModule` rơi xuống `instantiateModule` (engine.go:193) tạo **WASM instance dư không giới hạn** (xem 3.2). Thêm semaphore / worker pool sized theo backend capacity để backpressure thay vì tạo instance không giới hạn.

---

## 2. Đường nhận / endorsement transaction

### 2.1 Endorsement mở connection + stream mới mỗi tx — `transport.go:177-192`, `endorse.go:43`, `server.go:217-234`
`SendEndorsement` làm `Connect` → `NewStream` → `json.Encode` → `stream.Close()` **mỗi endorsement**. Không pool connection tới orderer (khác với sign pool). Wrapper async (`sendEndorsementAsync`, server.go:229 `go run()`) giấu latency nhưng spawn goroutine không giới hạn/tx.
- **Fix:** Thêm pool connection/stream tới orderer giống `commitPeerSignPool`. Batch nhiều endorsement/stream. Bound goroutine async bằng worker pool + buffered channel.

### 2.2 `peer.AddrInfoFromString` parse mỗi endorsement — `endorse.go:38`, `discovery.go:233, 256`
`sendOne` re-parse multiaddr orderer thành `AddrInfo` mỗi gửi. `PickAllAliveOrdererAddrs` build string (discovery.go:200-222) rồi parse lại về `AddrInfo`. Churn string→struct→string→struct/tx.
- **Fix:** Cache `peer.AddrInfo` đã resolve trong membership snapshot (parse 1 lần lúc refresh).

### 2.3 Endorsement re-derive address từ membership mỗi tx — `endorse.go:50, 62`
`PickOrdererAddr`/`PickAllAliveOrdererAddrs` iterate + `sort.Slice` member list (discovery.go:161-164, 206-209) mỗi endorsement. Sort/tx phí CPU.
- **Fix:** Precompute dial list đã order 1 lần/refresh, lưu trên cached view.

### 2.4 (Thông tin) Không verify signature trên đường nhận
Ký được ủy quyền cho commit peer. "Sequential signature verification" không phải bottleneck ở đây. Crypto trong `crypto/keys.go` không nằm hot path service này.

---

## 3. WASM Execution / State Writes

### 3.1 LevelDB write đồng bộ mỗi state put, không batch, không tắt sync — `vm/engine.go:58`, `state/database.go:118-119`
Host function `PutState` (engine.go:58) → `LedgerDB.Put([]byte(key), value, nil)` (database.go:119) **đồng bộ trong contract execution**, `WriteOptions` mặc định (`nil`). Mỗi contract ghi state → LevelDB write trên critical path tx.
- **Vì sao chậm:** Per-tx `Put` options mặc định, lấy DB write lock, có thể sync WAL. Không `leveldb.Batch`, không `WriteOptions{Sync:false}`, không coalesce.
- **Fix:** (a) set `WriteOptions{Sync:false}` để bỏ fsync/write (chấp nhận được vì ledger thật là commit peer/Postgres). (b) Gom write của 1 tx vào `leveldb.Batch`, write 1 lần cuối `Execute`. (c) Write buffer in-memory flush định kỳ. Xác nhận yêu cầu durability — nếu chỉ dùng cho `/api/state` read thì async best-effort được.

### 3.2 Tạo WASM instance không giới hạn khi tải cao — `vm/engine.go:189-198`
```go
select {
case mod := <-pc.slots:
    return mod, ...
default:
    mod, err := e.instantiateModule(...)   // overflow: instance mới mỗi lần pool rỗng
```
Pool cạn (default 16, max 32) → `acquireModule` instantiate module mới **mỗi request** không cap, `releaseModule` (engine.go:206-210) đóng nó (pool đầy).
- **Vì sao chậm:** `InstantiateModule` đắt (alloc linear memory, chạy `_initialize` qua `WithStartFunctions`, engine.go:113). Burst load = trả full instantiation/tx overflow rồi vứt ngay.
- **Fix:** Block trên pool channel (context timeout) thay vì `default:` overflow → backpressure; hoặc grow pool hard cap, giữ overflow instance.

### 3.3 `string(keyBytes)` alloc trong host function mỗi write — `engine.go:56`
`key := string(keyBytes)` copy key bytes → string mỗi state write, rồi `[]byte(key)` lại trong `PutState` (database.go:119) — copy round-trip. Truyền `[]byte` xuyên suốt.

### 3.4 Read payload WASM alloc mỗi call — `engine.go:46-47`
`m.Memory().Read(...)` rồi `string(...)`/DB write copy. Nhỏ, nhưng cộng 3.3 = 2 copy key+value/write.

### 3.5 `allocate` + memory write mỗi tx — `engine.go:250-265`
Mỗi tx có payload gọi WASM `allocate` export (engine.go:255) rồi `Memory().Write`. Mỗi `Call` cross host/WASM boundary với alloc `[]uint64` arg slice. Reuse instance (pool) giảm thiểu; win lớn hơn là fix 3.2.

---

## 4. Signing

### 4.1 Ký = round-trip libp2p mỗi tx (xem 1.7) — `commit_peer_sign_pool.go:131-174`
Ed25519 sign chạy ở commit peer; node này ship tx qua libp2p + chờ. Chi phí = **network + JSON serialize/tx**, không phải crypto. Single-flight-per-stream (stream mới mỗi tx, line 139) là bottleneck ký chính.

### 4.2 (Flag) Crypto helper local re-decode key mỗi call — `crypto/keys.go:32-58`
`Sign`/`Verify` làm `hex.DecodeString` key mỗi call (keys.go:33, 47-53), `SignTransaction` build message bằng nối string `txID + contractName + string(payload)` (keys.go:64) — alloc string copy cả payload/call. Không trên hot path (ký remote), nhưng nếu thêm verify local: decode key 1 lần, hash qua `[]byte`/`io.Writer` thay nối string.

---

## 5. Networking (libp2p / HTTP)

### 5.1 Membership fetch dùng channel buffer 1 + timeout 8s, shared — `transport.go:69, 94, 168-173`
`membershipCh = make(chan *MembershipView, 1)`, `handleMainProtocolStream` `select{case ch<-mv: default:}` (transport.go:93-96). Nếu 2 refresh race (loop background main.go:116 + on-demand `Snapshot`), response có thể tới sai waiter hoặc bị drop. Timeout 8s (transport.go:171) quá dài, chặn goroutine refresh.
- **Fix:** Channel response per-request keyed theo request id thay vì singleton; giảm timeout.

### 5.2 Mỗi refresh membership `Host.Connect` mới — `transport.go:141, 151`
`Connect` + `NewStream` mỗi refresh (5s background, main.go:116 + on-demand). Nhỏ ở cadence 5s, nhưng cộng dial endorsement (2.1) → host tích lũy dial churn.

### 5.3 SSE explorer 2 query DB đầy đủ mỗi 2s/client — `server.go:770, 786, 797`
`HandleExplorerStream` chạy `ListCommittedBlocks(1)` + `ListCommittedTransactions(1)` (cái sau JOIN 2 bảng, postgres.go:280-286) **mỗi 2s cho mỗi browser**. Nhiều tab explorer mở khi bench = load JOIN ổn định lên cùng Postgres mà metrics dùng.
- **Fix:** 1 poller goroutine chung fan-out tới mọi SSE client (broadcast), thay ticker/client. Hoặc Postgres LISTEN/NOTIFY.

### 5.4 libp2p negotiate security/mux mỗi stream
Mỗi `NewStream` (sign pool line 139, endorsement transport.go:182) negotiate protocol. Connection pooled (sign) ổn, nhưng stream-per-tx (1.7, 2.1) trả negotiation lặp. Multiplex qua ít stream long-lived.

### 5.5 commit-peer metrics HTTP client: timeout 120s, không tune keep-alive — `metrics/commitpeer/client.go:24-27`
Dùng `http.Client`/`http.Transport` mặc định. Ổn cho metrics (tần suất thấp), nhưng `LookupCommits` (client.go:142) POST batch lớn (5000 txid, line 139), `io.ReadAll` cả body (line 161) build `map[string]time.Time` mọi txid — alloc lớn window bench to. Stream/decode dần nếu window cực lớn.

---

## 6. State / DB (Postgres)

### 6.1 Submit recorder thiết kế tốt (điểm cộng) — `storage/submit_recorder.go`
Batch đúng: buffered channel (65536, line 38), ticker flush 50ms (line 70), multi-value INSERT 512 row (line 96, 113-122), enqueue non-block drop-on-full (line 53-57). Tốt. Nhỏ: build SQL bằng `fmt.Fprintf`/flush (line 118) — precompute template theo batch size hoặc dùng `COPY`/`pq.CopyIn` cho insert throughput cao hơn.

### 6.2 `Record` gọi đồng bộ trong request path — `server.go:201, 153`
Ổn — enqueue non-block. Không phải bottleneck.

### 6.3 Query analytic nặng `LIKE prefix || '%'` + CTE mỗi metrics request — `storage/throughput.go:37-78`, `storage/benchmark.go:139-184, 305-360`
- **Vì sao chậm:** (a) `LIKE prefix || '%'` sargable chỉ với index `text_pattern_ops`; không có → full scan. (b) `GetThroughputLatest` chạy **2** round-trip riêng (tx line 55 + block line 76) khi 1 query trả cả hai được. (c) `GetThroughputPeak` 2 query (line 146, 183) re-derive cùng CTE `filtered`/`bounds`/`recent` 2 lần. (d) `fillCommitWindow` 3 query tuần tự (line 147, 159, 179), `fillE2EWindow` 3 query nữa (line 301, 315, 362) — tới ~8 round-trip tuần tự/`/api/metrics/benchmark`.
- **Fix:** Gộp aggregate liên quan vào 1 query. Index: `ledger_transactions(txid text_pattern_ops)`, `ledger(committed_at)`, `ledger_transactions(block_id)`, covering index cho JOIN. Chạy sub-query độc lập concurrent (goroutine, 1 `*sql.DB` pool).

### 6.4 Pool `database/sql` không cấu hình — `storage/postgres.go:24-35`
`NewPostgresDB` không set `SetMaxOpenConns`/`SetMaxIdleConns`/`SetConnMaxLifetime`. Default `MaxIdleConns=2`, `MaxOpenConns` không giới hạn.
- **Vì sao chậm:** Chỉ 2 idle conn → query metrics/explorer/submit-flush concurrent liên tục mở+đóng connection (mỗi conn Postgres mới đắt: TLS/auth/fork backend). Unlimited open conn có thể đè Postgres khi burst.
- **Fix:** Set `SetMaxOpenConns` (theo headroom `max_connections`), `SetMaxIdleConns` = concurrency kỳ vọng, `ConnMaxLifetime`.

### 6.5 `json.Unmarshal` per-row vào `map[string]interface{}` cho list endpoint — `postgres.go:260-264, 305-311`
`ListCommittedBlocks`/`ListCommittedTransactions` unmarshal mỗi row JSON vào generic map (postgres.go:261, 306). Generic-map unmarshal là mode JSON chậm nhất (reflection + boxing + map alloc/field). Cộng SSE poller (5.3) gọi mỗi 2s + list `limit=50` = alloc nặng.
- **Fix:** Nếu consumer chỉ cần raw JSON → pass `json.RawMessage`, tránh decode+re-encode.

### 6.6 `GetCommittedBlockByHash`/`GetBlock` decode→encode round-trip — `postgres.go:147-169`, `server.go:458-466`
`HandleGetBlock` decode block JSON → map (postgres.go:163) rồi re-encode vào response map (server.go:462). Dùng `json.RawMessage` passthrough.

---

## Bảng ưu tiên (impact cao trước)

| # | Vấn đề | File:line | Impact |
|---|--------|-----------|--------|
| 1 | `os.Getenv`/parse env trên hot path | verbose.go:6; server.go:24; sign_pool.go:26; engine.go:50+ | Cao — lock global/tx, fix dễ |
| 2 | Stream libp2p mới **mỗi tx** để ký | sign_pool.go:139; server.go:173 | Cao — round-trip tuần tự/tx |
| 3 | JSON marshal/unmarshal Transaction 4-5×/tx (custom marshaler + hex) | model.go:61-150; sign_pool.go:145-166; transport.go:188 | Cao — CPU + alloc/tx |
| 4 | Tạo WASM instance không giới hạn khi burst | engine.go:189-198 | Cao khi burst |
| 5 | LevelDB Put đồng bộ/write, không batch, không no-sync | engine.go:58; database.go:119 | Cao nếu contract ghi state |
| 6 | Connect+Stream+goroutine/tx cho endorsement (không pool) | transport.go:177-192; server.go:229 | Trung-Cao |
| 7 | Pool Postgres không tune (idle=2) | postgres.go:24-35 | Trung |
| 8 | Metrics: nhiều round-trip DB tuần tự + LIKE không index | throughput.go:55-76; benchmark.go:147-362 | Trung (ngoài hot path) |
| 9 | SSE poll/client + JOIN mỗi 2s | server.go:770-809 | Trung |
| 10 | Generic-map JSON unmarshal trên list/read endpoint | postgres.go:260, 305 | Thấp-Trung |

**Điểm cộng:** WASM compile cache (engine.go:126-156), module pool, warm sign-pool *connection*, batched async submit recorder (submit_recorder.go) đã thiết kế tốt. Win còn lại: bỏ env read/tx, stream setup/tx, JSON serialize dư, và overflow path không giới hạn.
