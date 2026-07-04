# Committing Peer — Phân tích tối ưu hiệu năng

Phân tích điểm nghẽn ảnh hưởng tốc độ **nhận block**, **validate**, và **commit vào world state** (đóng/ghi block). Module: `commitingpeer/source`.

## Pipeline commit (hot path)

```
deliver.Subscribe (JSON-decode block off libp2p stream)
  → blockChan (buffer 64)
  → commitLoop (1 goroutine duy nhất)
  → handleBlock
  → ValidateBlock (Ed25519 verify TUẦN TỰ)
  → BlockStorage.AppendBlock (JSON marshal + file write, 1 mutex)
  → WorldState.ApplyBlock (LevelDB batch write)
  → metrics + async Postgres mirror
```

**Sự thật cấu trúc quan trọng nhất:** toàn pipeline chạy trong **1 goroutine tuần tự** (`commitLoop`, peer.go:242-252). Mỗi chi phí per-block cộng tuyến tính, không song song. **Block N+1 không thể bắt đầu validate cho tới khi block N hoàn tất LevelDB write đồng bộ.**

---

## 1. Validate block (verify chữ ký) — bottleneck CPU lớn nhất

### 1.1 Verify Ed25519 tuần tự, đơn luồng cho mọi endorsement — `validation/engine.go:52-57, 82-92`
```go
for _, tx := range b.Transactions {
    if err := e.validateEndorsedTx(tx); err != nil { return err }
}
...
for i, ent := range list {
    if !crypto.VerifyTransaction(tx.Txid, tx.ContractName, tx.Payload, ent.Signature, ent.PublicKey) {
```
Mỗi endorsement của mỗi tx verify từng-cái-một trên commit goroutine. Ed25519 verify ~50-70µs/cái. Block 1000 tx × 1 endorsement = ~50-70ms CPU thuần **chặn cả pipeline** trong khi N+1..N+64 dồn trong `blockChan`.
- **Fix:** Parallelize verify qua `runtime.NumCPU()` worker (`errgroup` trên slice tx shard). Verify "embarrassingly parallel" — mỗi tx độc lập. **ROI cao nhất.** Cân nhắc batch verification (`filippo.io/edwards25519`).

### 1.2 Hex-decode lại key/signature mỗi verify — `crypto/keys.go:55-66, 74-77`
```go
func Verify(message []byte, signatureHex string, publicKeyHex string) bool {
    signatureBytes, err := hex.DecodeString(signatureHex)
    publicKeyBytes, err := hex.DecodeString(publicKeyHex)
```
Public key + signature hex-decode từ string **mỗi call**. Tập key tin cậy nhỏ + cố định; cùng pubkey endorser decode lại cho mọi tx mọi block. `VerifyTransaction` build message bằng `txID + contractName + string(payload)` (keys.go:75) — 3 lần nối string, alloc buffer mới/call.
- **Fix:** Cache `ed25519.PublicKey` đã decode keyed theo hex string (tập biết lúc `NewEngine`). Build message vào `[]byte` reuse (`append`, hoặc `sync.Pool` buffer) thay `string(payload)` (copy cả payload).

### 1.3 Map trusted-key rebuild mỗi transaction — `validation/engine.go:77-80`
```go
trusted := make(map[string]struct{}, len(e.trustedPubHexes))
for _, p := range e.trustedPubHexes {
    trusted[strings.ToLower(strings.TrimSpace(p))] = struct{}{}
}
```
Map (+ `ToLower`/`TrimSpace` mỗi key) rebuild **1 lần/tx** dù tập không đổi sau `NewEngine`. Block 1000 tx = 1000 map alloc.
- **Fix:** Build map normalized 1 lần trong `NewEngine`, lưu trên `Engine`. Read-only sau đó.

### 1.4 Verify hash/merkle dùng `fmt.Sprintf("%x", …)` trong hot path — `validation/engine.go:141, 146-147`
```go
merkleRootHex := fmt.Sprintf("%x", b.MerkleRoot)
hashHex := fmt.Sprintf("%x", b.Hash)
prevHashHex := fmt.Sprintf("%x", b.PrevHash)
```
`fmt.Sprintf("%x")` dùng reflection, chậm hơn `hex.EncodeToString`. Rồi `crypto.HashBlock` (keys.go:85-119) **decode hex về bytes lại** (`hex.DecodeString`) + rebuild `bytes.Buffer` với nhiều `binary.Write` reflection. Bytes → hex string → bytes lại/block.
- **Fix:** `hex.EncodeToString` (hoặc truyền `[]byte` trực tiếp). Refactor `HashBlock`/`ComputeMerkleRoot` làm trên `[]byte`, dùng `[80]byte` header + `binary.LittleEndian.PutUint32` thay `binary.Write(buf, …)`.

### 1.5 Merkle root tính lại từ đầu mỗi block — `crypto/keys.go:125-162`, gọi từ `engine.go:142`
`ComputeMerkleRoot` alloc `hashes := make([][]byte, n)` + slice 32-byte mới/internal node/level (keys.go:154). Block lớn = O(n) alloc/level + double-SHA256/node.
- **Fix:** Reuse scratch buffer/arena cho level slices; tránh `make([]byte,32)`/node. Cân nhắc verify merkle chỉ như integrity gate rẻ (orderer đã làm).

---

## 2. State / DB write — LevelDB write đồng bộ/block

### 2.1 LevelDB batch write đồng bộ trong serial path + options mặc định — `storage/world_state.go:64-67, 35`
```go
if err := ws.db.Write(batch, nil); err != nil {
```
`nil` WriteOptions = goleveldb default `Sync:false` (không fsync, tốt). Nhưng write inline trên commit goroutine (peer.go:267), chặn validate block kế trên memtable write + compaction stall. `leveldb.OpenFile(path, nil)` (world_state.go:35) **default options**: không tune `WriteBuffer`, `BlockCacheCapacity`, `CompactionTableSize`. Tải bền vững → compaction stall ("L0 slowdown/stop").
- **Fix:**
  - Tune `leveldb.Options`: `WriteBuffer` lớn (64-128MB), `BlockCacheCapacity`, `OpenFilesCacheCapacity`, raise `CompactionL0Trigger`/`WriteL0SlowdownTrigger`, `Filter: filter.NewBloomFilter(10)`.
  - Decouple world-state write khỏi validate: pipeline block N+1 validate trong khi block N write (goroutine riêng + handoff có thứ tự). Hoặc batch mutation nhiều block vào 1 LevelDB batch khi channel dồn.

### 2.2 Không batch xuyên block; 1 batch == 1 block — `storage/world_state.go:44-67`
Mỗi `ApplyBlock` tạo `leveldb.Batch` mới, write ngay. Block tới nhanh hơn commit (channel đầy) = không coalesce. Mỗi write = memtable insert + WAL append riêng.
- **Fix:** Khi `blockChan` backlog, drain nhiều block, merge UTXO delta vào 1 batch (group-commit). Giảm mạnh write-amplification + WAL sync.

### 2.3 `json.Marshal(vout)`/output trong write loop — `storage/world_state.go:54-61`
```go
for _, vout := range tx.Vout {
    val, err := json.Marshal(vout)
    batch.Put([]byte(utxoKey(tx.Txid, vout.N)), val)
}
```
Mỗi UTXO JSON-marshal riêng (reflection-heavy), key build bằng `fmt.Sprintf("utxo:%s:%d", …)` (world_state.go:122-124)/output/tx. Block tạo nghìn output = nghìn `json.Marshal` + `fmt.Sprintf` alloc.
- **Fix:** Thay `fmt.Sprintf` bằng byte-slice (`append` `"utxo:"` + txid + `':'` + `strconv.AppendInt`). Thay JSON value VOUT bằng encoder binary hand-rolled, hoặc tối thiểu reuse `json.Encoder`/buffer. UTXO value là struct nhỏ cố định — JSON phí.

### 2.4 `AllUTXOs`/`UTXOCount` full-scan + `fmt.Sscanf`/entry — `storage/world_state.go:85-104, 107-115`
```go
fmt.Sscanf(string(iter.Key()), "utxo:%64s", &txid)
```
`AllUTXOs` iterate cả keyspace, `json.Unmarshal` mọi value, `fmt.Sscanf`/key (parser rất chậm). `UTXOCount` full O(n) iterate chỉ để đếm. Gọi trên **sync hot path** (`HandleSyncStream`, peer.go:366) cho *mọi* wallet sync request; `UTXOCount` gọi trên `status`.
- **Fix:**
  - `HandleSyncStream` full UTXO scan/request + linear address match (peer.go:373-384). UTXO set lớn + nhiều wallet sync = thảm họa. Thêm secondary index: key `addr:<address>:<txid>:<n>` → sync thành prefix range scan thay full-table scan.
  - Maintain atomic UTXO counter update trong `ApplyBlock` thay scan cho `UTXOCount`.
  - Bỏ `fmt.Sscanf`; `v.N` authoritative — parse key bằng `bytes.Split` hoặc lưu txid trong value.

---

## 3. Block storage (file) write

### 3.1 JSON marshal cả block + file write không buffer dưới mutex/block — `storage/block_storage.go:47-63`
```go
data, err := json.Marshal(block)
data = append(data, '\n')
bs.mu.Lock()
defer bs.mu.Unlock()
if _, err := bs.file.Write(data); err != nil {
```
Mỗi block JSON-marshal (reflection, alloc lớn gồm hex-encode mọi tx payload qua custom `MarshalJSON`), write raw `Write` (không `bufio.Writer`). Block — gồm mọi tx/vin/vout — serialize JSON **lại** ở đây, riêng với Postgres path marshal **lại nữa** (peer.go:304, 319). Mỗi block JSON-encode ít nhất 3 lần. Không `fsync` (durability yếu nhưng throughput cao).
- **Fix:**
  - Marshal block **1 lần**, reuse bytes cho chain-file, metrics, raw passthrough Postgres.
  - Wrap file `bufio.Writer`, flush định kỳ/idle thay syscall/block.
  - JSON text newline phình; cân nhắc binary length-prefixed framing.

### 3.2 `CommittedTipHash` lấy dưới mutex mỗi block khi validate — `peer/peer.go:257` → `block_storage.go:73-82`
```go
if err := p.validator.ValidateBlock(block, p.blockStore.CommittedTipHash()); err != nil {
```
`CommittedTipHash()` lock `bs.mu` + **alloc copy hash** mỗi block. Contend với lock `AppendBlock`. `verifyPrevHash` đang **disabled** (engine.go:40-42) → alloc/lock này phí thuần lúc này.
- **Fix:** prev-hash check disabled → ngừng gọi `CommittedTipHash()`/block, hoặc lock-free qua `atomic.Value`.

### 3.3 Startup `ReadAll` cả chain file vào memory — CLI re-read mỗi lần — `block_storage.go:33, 95-121`; CLI: `cmd/peer/main.go:320, 342, 383`
`NewBlockStorage` gọi `ReadAll` load + `json.Unmarshal` **mọi block** lúc startup (O(chain) memory + CPU). Tệ hơn: lệnh interactive `chain`, `block <n>`, `tx <txid>` mỗi cái gọi `storage.ReadAll(blockFile)` lại — re-parse cả chain từ disk/lệnh.
- **Fix:** Index (offset/block) → `block <n>`/`tx` seek thay full re-parse. Startup: count block/derive tip bằng scan tail thay unmarshal mọi block (chỉ cần `committedCount` + last hash).

---

## 4. Networking / nhận block (deliver)

### 4.1 `json.Decoder` decode stream block — nặng + single-stream — `deliver/client.go:119-136`
```go
decoder := json.NewDecoder(s)
for {
    var block types.Block
    if err := decoder.Decode(&block); err != nil { ... }
    select {
    case blockChan <- block:
```
Block tới dạng JSON qua 1 libp2p stream, decode bằng `encoding/json` (reflection, custom `Transaction.UnmarshalJSON` tại transaction.go:35-80 alloc `Alias` + hex-decode mọi payload/tx). Decode trên receive goroutine rồi **cả `types.Block` copy by value vào channel** (`blockChan <- block`) — struct copy + GC pressure từ alloc/block/tx thật.
- **Fix:**
  - Gửi `*types.Block` (pointer) qua `blockChan` tránh struct copy.
  - Thay `encoding/json` bằng codec nhanh (`json-iterator`, `easyjson`, hoặc binary protocol khớp orderer) — đây là receive hot path.
  - Decode vào block backed bởi `sync.Pool` giảm alloc churn.

### 4.2 `blockChan` buffer 64 — consumer tuần tự — `peer/peer.go:75`
```go
blockChan: make(chan types.Block, 64),
```
Buffer 64 block nhưng consumer (`commitLoop`) đơn luồng, mỗi commit gồm LevelDB write đồng bộ + verify tuần tự. Orderer burst nhanh hơn commit throughput → producer block trên `blockChan <- block` (client.go:132) → **TCP backpressure lên orderer stream** → cap throughput end-to-end ở stage chậm nhất (verify hoặc LevelDB write).
- **Fix:** Buffer size ít quan trọng hơn serial consumer. Split commit loop thành stage (decode → verify(parallel) → commit giữ thứ tự) → verify fan-out trong khi commit ordered. Khi đó buffer nhỏ đủ.

### 4.3 libp2p host default; không tune transport/muxer — `deliver/client.go:31-37`
`libp2p.New(...)` chỉ listen addr — default security (noise/TLS), default yamux/mplex window. Single-stream block feed throughput cao, default mux receive window có thể throttle block lớn.
- **Fix:** Block lớn → tune muxer (yamux `MaxStreamWindowSize`). Ưu tiên thấp hơn JSON/verify.

---

## 5. Transaction processing & signing (endorsement tx-sign)

### 5.1 Endorse path verify, sign, rồi **self-verify** — 2 Ed25519 op + nhiều JSON round-trip/request — `deliver/sign.go:44-70`
```go
if err := verifyExistingEndorsements(&tx); ... // verify (loop)
sig, err := crypto.SignTransaction(...)         // sign
if !crypto.VerifyTransaction(...) {             // self-verify chữ ký vừa tạo
```
Sau ký, code verify chữ ký của chính mình (sign.go:66) — Ed25519 verify phí mỗi endorse. `verifyExistingEndorsements` cũng re-verify mọi endorsement trước mỗi lần. + `json.NewDecoder(s).Decode` + `json.NewEncoder(s).Encode`/request đồng bộ qua network.
- **Fix:** Bỏ self-verify (Ed25519 sign deterministic, đúng by construction; self-verify chỉ bắt private key xấu → check 1 lần lúc startup). Cache private key decode (`SignTransaction`→`Sign` hex-decode private key mỗi call, keys.go:46-51).

### 5.2 Private key hex-decode mỗi sign — `crypto/keys.go:45-52`
```go
func Sign(message []byte, privateKeyHex string) (string, error) {
    privateKeyBytes, err := hex.DecodeString(privateKeyHex)
```
Endorsement private key cố định/process nhưng hex-decode mỗi signing request.
- **Fix:** Decode 1 lần lúc `RegisterTxSignHandler`, truyền `ed25519.PrivateKey` trực tiếp.

### 5.3 tx-sign handler per-stream OK; message build alloc — `crypto/keys.go:69-72`
`txID + contractName + string(payload)` — cùng vấn đề string-concat alloc như 1.2.

---

## 6. Postgres mirror (explorer) — async nhưng alloc nặng

### 6.1 JSON encode 3-4 lần/tx trong mirror — `peer/peer.go:296-334`
```go
txData, err := ledgerTransactionRecord(tx)   // json.Marshal(tx) rồi json.Unmarshal vào map
raw, err := json.Marshal(txData)             // marshal map lại
```
`ledgerTransactionRecord` (peer.go:318-334) marshal tx → JSON, unmarshal vào `map[string]interface{}`, optional unmarshal payload lại, rồi `saveBlockToDatabase` marshal map **lại** (peer.go:304). 2-3 pass JSON/tx + pass thứ 3 marshal cả block (peer.go:319/postgres.go:49).
- **Fix:** Build row JSON 1 pass. Cần `payload_decoded` → dùng `json.RawMessage` splice thay full unmarshal→remarshal. Async nên không chặn commit, nhưng 2 worker (default, ledger_mirror.go:26) có thể thành trần throughput explorer + queue-full fallback **goroutine không giới hạn** (ledger_mirror.go:92 `go p.saveBlockToDatabase(...)`) → overload bền vững spawn goroutine/block, cạn pool 8-conn/memory.

### 6.2 `stmt.Exec` per-row trong DB transaction — N round-trip/block — `storage/postgres.go:82-96`
```go
stmt, err := dbTx.Prepare(`INSERT INTO ... ledger_transactions ...`)
for _, row := range txRows {
    stmt.Exec(blockID, row.Txid, row.TxIndex, row.TxData)
}
```
Dù comment "batch insert", mỗi tx = `Exec` riêng = round-trip riêng tới Postgres trong transaction. Block 1000 tx = 1000 round-trip. Statement cũng `Prepare` mới/block (không cache).
- **Fix:** Multi-row insert thật: `pq.CopyIn` (COPY protocol, nhanh nhất), hoặc 1 `INSERT ... VALUES (...),(...)...` batched param (Postgres tới 65535 param), hoặc `pgx` `Batch`. Cache prepared statement trên `PostgresDB`. Pool nhỏ (`SetMaxOpenConns(8)`, postgres.go:33) — ổn cho 2 worker, xem lại nếu tăng.

---

## 7. Metrics recorder — global lock trên commit path

### 7.1 `RecordBlock` lấy write lock + trim **mỗi** block committed — `metrics/recorder.go:56-74, 76-92`
```go
r.mu.Lock()
defer r.mu.Unlock()
for _, id := range ids { r.txs[id] = txCommit{at: at} }
r.blocks = append(r.blocks, blockCommit{...})
r.trimLocked(at)
```
Gọi từ `handleBlock` (peer.go:289) **trên serial commit path**. Lấy write lock process-wide, insert mọi txid vào map (growth/rehash), append slice, rồi `trimLocked` **iterate cả `r.txs` map** (recorder.go:87-91) mỗi block evict expired. Retention window lớn + tx volume cao → `r.txs` khổng lồ, full-map scan/block = O(map size) → chậm commit trực tiếp.
- **Fix:**
  - Không full-scan `r.txs` mỗi block. Trim lazy (chỉ khi `len(blocks)` quá threshold, hoặc time-bucket tx → eviction O(expired) không O(all)).
  - `RecordBlock` copy slice txid (recorder.go:61 `append([]string(nil), txids...)`) — caller (peer.go:283) đã build txids mới → copy thừa.
  - Record off commit path (push buffered channel cho metrics goroutine) → commit latency tách hẳn metrics bookkeeping.

### 7.2 Query endpoint copy/scan mọi sample dưới lock — `metrics/query.go:41-68, 127`
`collectTxSamples` copy cả `txs` map vào slice dưới `RLock` mỗi query; `ThroughputLatest`/`ThroughputPeak` scan mọi sample tìm max timestamp. Mỗi metrics HTTP hit (k6/bench poll thường xuyên) lấy lock + O(n) work, contend với `RecordBlock` trên commit path.
- **Fix:** Track `latest` commit time atomically; bucket sample theo giây → window query không rescan. Ưu tiên thấp hơn 7.1.

---

## 8. Discovery — minor

### 8.1 `parseMembershipData` dùng `map[string]interface{}` reflection — `deliver/membership.go:80-119`, `client.go:46-66`
Membership response decode vào `map[string]interface{}` rồi type-assert field-by-field. Ngoài block hot path (chỉ refresh/failover, 5s, peer.go:158), impact thấp.
- **Fix:** Struct typed cho membership response tránh interface boxing. Ưu tiên thấp.

---

## Bảng ưu tiên (impact cao trước)

1. **Parallelize verify chữ ký** (1.1) + cache key decode/message buffer (1.2, 1.3) — bỏ chi phí CPU serial chủ đạo.
2. **Pipeline commit loop** → validate, file write, LevelDB write overlap thay serial/block (peer.go:242-293).
3. **Tune LevelDB & decouple/batch world-state write** (2.1, 2.2); kill `json.Marshal` + `fmt.Sprintf`/output (2.3).
4. **Ngừng triple JSON-marshal mỗi block** qua chain-file/metrics/Postgres (3.1, 6.1); marshal 1 lần, reuse.
5. **Fix metrics `trimLocked` full-map scan/block** + move record off commit path (7.1).
6. **Postgres multi-row insert/COPY** + cache prepared statement (6.2).
7. **Thay `fmt.Sprintf("%x")` round-trip trong hash/merkle verify** bằng byte-level (1.4, 1.5).
8. **Thêm address→UTXO index** → `HandleSyncStream` ngừng full-scan world state (2.4); maintain atomic UTXO count.
9. **Gửi `*types.Block` qua `blockChan`** + codec block nhanh hơn (4.1).
10. **Bỏ self-verify thừa trong tx-sign** + decode private key 1 lần (5.1, 5.2).

### File liên quan
- `internal/peer/peer.go` (serial commit loop, triple marshal)
- `internal/validation/engine.go` (verify tuần tự, map/tx)
- `internal/crypto/keys.go` (hex decode/call, fmt/binary.Write hashing)
- `internal/storage/world_state.go` (marshal/output, fmt.Sprintf key, full scan)
- `internal/storage/block_storage.go` (write không buffer, ReadAll startup + CLI cmd)
- `internal/storage/postgres.go` (Exec per-row, Prepare mới)
- `internal/deliver/client.go` (json.Decoder stream, channel by-value), `sign.go` (self-verify, key decode/request)
- `internal/metrics/recorder.go` (write lock + full-map trim/block)
