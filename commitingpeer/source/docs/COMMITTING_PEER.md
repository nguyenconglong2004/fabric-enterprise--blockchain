# Committing Peer — Tài liệu Thiết kế và Triển khai

## Mục lục

1. [Tổng quan](#1-tổng-quan)
2. [Vị trí trong hệ thống](#2-vị-trí-trong-hệ-thống)
3. [Kiến trúc tổng thể](#3-kiến-trúc-tổng-thể)
4. [Cấu trúc thư mục](#4-cấu-trúc-thư-mục)
5. [Mô hình dữ liệu](#5-mô-hình-dữ-liệu)
6. [Mô tả chi tiết từng module](#6-mô-tả-chi-tiết-từng-module)
   - 6.1 [types — Kiểu dữ liệu chia sẻ](#61-types--kiểu-dữ-liệu-chia-sẻ)
   - 6.2 [deliver — Nhận block từ Ordering Service](#62-deliver--nhận-block-từ-ordering-service)
   - 6.3 [validation — Validation Engine](#63-validation--validation-engine)
   - 6.4 [storage/BlockStorage — Lưu trữ block](#64-storageblockstorge--lưu-trữ-block)
   - 6.5 [storage/WorldState — Sổ cái trạng thái](#65-storageworldstate--sổ-cái-trạng-thái)
   - 6.6 [peer — Orchestrator](#66-peer--orchestrator)
   - 6.7 [cmd/peer — Entry point & CLI](#67-cmdpeer--entry-point--cli)
7. [Luồng xử lý block (Pipeline)](#7-luồng-xử-lý-block-pipeline)
8. [Thiết kế đồng thời (Concurrency)](#8-thiết-kế-đồng-thời-concurrency)
9. [Giao thức kết nối với Ordering Service](#9-giao-thức-kết-nối-với-ordering-service)
10. [CLI Reference](#10-cli-reference)
11. [Hướng dẫn Build và Chạy](#11-hướng-dẫn-build-và-chạy)
12. [Hướng phát triển tiếp theo](#12-hướng-phát-triển-tiếp-theo)

---

## 1. Tổng quan

**Committing Peer** là thành phần cuối cùng trong pipeline xử lý giao dịch của hệ thống blockchain. Sau khi Ordering Service (sử dụng thuật toán đồng thuận Raft) đã đặt thứ tự các giao dịch và đóng gói chúng thành các block được xác nhận bởi majority, Committing Peer:

1. **Nhận** liên tục các block đã được xác nhận từ Ordering Service qua một long-lived stream.
2. **Kiểm tra hợp lệ** (validation) từng block và từng giao dịch.
3. **Ghi block** vào Block Storage (file `.block` dạng append-only).
4. **Cập nhật World State** (UTXO set trong LevelDB) để phản ánh trạng thái hiện tại của sổ cái.

Module này tương đương với vai trò *Committing Peer* trong Hyperledger Fabric — nơi dữ liệu thực sự được lưu lâu dài và trạng thái toàn cục được duy trì.

---

## 2. Vị trí trong hệ thống

```
┌─────────────────────────────────────────────────────────┐
│                    Client Applications                    │
│              (gửi Transaction qua OrderClient)           │
└────────────────────────────┬────────────────────────────┘
                             │  MsgTxRequest
                             ▼
┌─────────────────────────────────────────────────────────┐
│                    Ordering Service                       │
│                                                          │
│   Node 1 ──── Node 2 ──── Node 3   (Raft Consensus)     │
│                    │                                      │
│           Leader commits Block                           │
│       DeliverManager.NotifyNewBlock()                    │
└────────────────────────────┬────────────────────────────┘
                             │  /raft-order-service/deliver/1.0.0
                             │  (libp2p long-lived stream)
                             ▼
┌─────────────────────────────────────────────────────────┐
│                   Committing Peer                         │
│                                                          │
│  deliver.Client ──► blockChan ──► validation.Engine      │
│                                        │                 │
│                          ┌─────────────┴──────────────┐  │
│                          ▼                            ▼  │
│                   BlockStorage                  WorldState│
│                  (chain.block)                 (LevelDB)  │
└─────────────────────────────────────────────────────────┘
```

---

## 3. Kiến trúc tổng thể

Committing Peer được thiết kế theo mô hình **Pipeline đơn chiều** với ba tầng rõ ràng:

```
[Ordering Service]
        │
        │  JSON stream (libp2p)
        ▼
┌───────────────────┐
│  deliver.Client   │  ← Tầng 1: Thu nhận (Receive)
│  (goroutine nền)  │
└────────┬──────────┘
         │  blockChan  (Go buffered channel, cap=64)
         ▼
┌───────────────────┐
│ validation.Engine │  ← Tầng 2: Xác thực (Validate)
└────────┬──────────┘
         │  (nếu hợp lệ)
         ▼
┌─────────────────────────────────┐
│  BlockStorage  │   WorldState   │  ← Tầng 3: Cam kết (Commit)
│  (file .block) │   (LevelDB)    │
└─────────────────────────────────┘
```

Mỗi tầng hoạt động **độc lập** và liên kết với nhau qua channel Go, đảm bảo không có goroutine nào block lẫn nhau trong trường hợp bình thường.

---

## 4. Cấu trúc thư mục

```
commitingpeer/
├── peer.exe                          ← Executable đã build (Windows)
└── source/
    ├── go.mod                        ← Module: commiting-peer
    ├── go.sum
    ├── docs/
    │   └── COMMITTING_PEER.md        ← Tài liệu này
    ├── cmd/
    │   └── peer/
    │       └── main.go               ← Entry point, khởi tạo và CLI
    └── internal/
        ├── types/
        │   ├── block.go              ← Struct Block
        │   ├── transaction.go        ← Struct Transaction, VIN, VOUT
        │   └── deliver.go            ← Struct DeliverRequest
        ├── deliver/
        │   └── client.go             ← libp2p client, goroutine nhận block
        ├── validation/
        │   └── engine.go             ← Validation Engine (stub)
        ├── storage/
        │   ├── block_storage.go      ← Ghi/đọc file .block
        │   └── world_state.go        ← LevelDB UTXO set
        └── peer/
            └── peer.go               ← Orchestrator, commit loop, stats
```

**Quy ước đặt tên:** Tất cả packages bên dưới `internal/` không được import từ bên ngoài module — đây là cơ chế bảo vệ của Go.

---

## 5. Mô hình dữ liệu

### 5.1 Block

```go
type Block struct {
    Timestamp    int64         `json:"timestamp"`   // Unix timestamp (giây)
    Transactions []Transaction `json:"transactions"` // Danh sách giao dịch
    PrevHash     []byte        `json:"prev_hash"`   // Hash của block trước (hash chain)
    Hash         []byte        `json:"hash"`        // Double-SHA256 của block header
    Nonce        int           `json:"nonce"`       // Nonce (Bitcoin-style)
    MerkleRoot   []byte        `json:"merkle_root"` // Merkle root của danh sách tx
    Size         int           `json:"size"`        // Tổng kích thước giao dịch (bytes)
}
```

Cấu trúc `Block` là **bản gương** (mirror) của struct cùng tên trong Ordering Service, đảm bảo khả năng deserialize chính xác từ JSON stream.

### 5.2 Transaction (mô hình UTXO kiểu Bitcoin)

```go
type Transaction struct {
    Version  uint32 `json:"version"`
    Vin      []VIN  `json:"vin"`       // Danh sách input (tiêu thụ UTXO cũ)
    Vout     []VOUT `json:"vout"`      // Danh sách output (tạo UTXO mới)
    LockTime uint32 `json:"locktime"`
    Txid     string `json:"txid"`      // ID giao dịch (double-SHA256 hex)
}

type VIN struct {
    Txid      string    // Txid của output đang được tiêu thụ
    Vout      int       // Index của output trong giao dịch tham chiếu
    ScriptSig ScriptSig // Chữ ký mở khóa
}

type VOUT struct {
    Value        int64        // Giá trị (satoshi)
    N            int          // Index trong giao dịch này
    ScriptPubKey ScriptPubKey // Script khóa output vào địa chỉ
}
```

### 5.3 DeliverRequest

```go
type DeliverRequest struct {
    FromIndex int64 `json:"from_index"` // Vị trí bắt đầu stream (1-based)
}
```

Giao dịch này được gửi **một lần duy nhất** khi mở stream tới Ordering Service, yêu cầu stream block bắt đầu từ index chỉ định.

### 5.4 Schema LevelDB (World State)

World State lưu trữ **UTXO Set** — tập hợp tất cả output chưa được tiêu thụ:

```
Key:   utxo:<txid>:<vout_index>
Value: JSON-encoded VOUT
```

**Ví dụ:**
```
Key:   utxo:a1b2c3d4...ef:0
Value: {"value":5000000,"n":0,"scriptPubKey":{"addresses":["1A2B3C..."],...}}
```

Khi một block được commit:
- **Xóa:** `utxo:<vin.Txid>:<vin.Vout>` cho mỗi input trong mỗi giao dịch
- **Thêm:** `utxo:<tx.Txid>:<vout.N>` cho mỗi output trong mỗi giao dịch

Tất cả thao tác trên được gom vào **một LevelDB Batch** duy nhất — đảm bảo tính nguyên tử (atomic): hoặc toàn bộ block được áp dụng, hoặc không có gì thay đổi.

### 5.5 Block Storage Format (file .block)

```
{"timestamp":1712345678,"transactions":[...],"prev_hash":"...","hash":"...","nonce":0,"merkle_root":"...","size":512}\n
{"timestamp":1712345690,"transactions":[...],"prev_hash":"...","hash":"...","nonce":0,"merkle_root":"...","size":384}\n
...
```

Mỗi dòng là **một block** được serialize thành JSON, kết thúc bằng ký tự newline `\n`. Định dạng này đồng thời:
- Hỗ trợ **append-only** — không bao giờ sửa dữ liệu cũ
- Có thể đọc bằng `ReadAll()` với `bufio.Scanner`
- Là file text thuần, có thể đọc bằng bất kỳ text editor nào

---

## 6. Mô tả chi tiết từng module

### 6.1 `types` — Kiểu dữ liệu chia sẻ

**Vị trí:** `internal/types/`

Package này định nghĩa các struct được dùng xuyên suốt toàn bộ module. Các struct này được giữ **tách biệt** với business logic, chỉ chứa định nghĩa dữ liệu và JSON tags.

Lý do không tái sử dụng trực tiếp types từ Ordering Service: Committing Peer là một **Go module độc lập** (`commiting-peer`) với `go.mod` riêng. Việc tách biệt module cho phép deploy, version, và scale hai thành phần một cách độc lập.

---

### 6.2 `deliver` — Nhận block từ Ordering Service

**Vị trí:** `internal/deliver/client.go`

#### Chức năng

Module này chịu trách nhiệm duy nhất: **thiết lập kết nối mạng với Ordering Service và nhận block liên tục**.

#### Giao thức kết nối

Sử dụng thư viện `go-libp2p` — cùng stack mạng với Ordering Service — trên protocol ID:

```
/raft-order-service/deliver/1.0.0
```

Protocol ID này phải khớp với hằng số `DeliverProtocolID` trong Ordering Service.

#### Quy trình Subscribe

```
deliver.Client.Subscribe(ctx, ordererAddr, fromIndex, blockChan)
    │
    ├─ 1. Phân tích ordererAddr → peer.AddrInfo
    │      (dạng /ip4/<IP>/tcp/<PORT>/p2p/<PeerID>)
    │
    ├─ 2. host.Connect(ctx, addrInfo)
    │      ← Thiết lập kết nối libp2p TCP
    │
    ├─ 3. host.NewStream(ctx, peerID, DeliverProtocolID)
    │      ← Mở một stream multiplex trên kết nối đã có
    │
    ├─ 4. json.Encode(DeliverRequest{FromIndex: fromIndex})
    │      ← Gửi yêu cầu stream từ block index nào
    │
    └─ 5. Khởi động goroutine nền
           ├─ Inner goroutine: chờ ctx.Done() → đóng stream
           └─ Outer goroutine: loop json.Decode → blockChan <- block
```

#### Xử lý graceful shutdown

Vì `json.Decoder.Decode()` là **blocking call** — nó sẽ chờ mãi cho đến khi có dữ liệu mới — việc cancel context sẽ không tự động dừng goroutine. Giải pháp: một inner goroutine chờ `ctx.Done()` rồi gọi `s.Close()`, khiến `Decode()` trả về lỗi `io.EOF` và outer goroutine thoát ra.

```go
go func() {
    defer s.Close()
    go func() { <-ctx.Done(); s.Close() }()  // ← inner goroutine
    
    decoder := json.NewDecoder(s)
    for {
        var block types.Block
        if err := decoder.Decode(&block); err != nil {
            if ctx.Err() == nil { log.Printf(...) }
            return
        }
        select {
        case blockChan <- block:
        case <-ctx.Done():
            return
        }
    }
}()
```

---

### 6.3 `validation` — Validation Engine

**Vị trí:** `internal/validation/engine.go`

#### Trạng thái hiện tại: Stub

Validation Engine hiện là **placeholder** — mọi block và giao dịch đều được chấp nhận vô điều kiện.

```go
func (e *Engine) ValidateBlock(_ types.Block) error       { return nil }
func (e *Engine) ValidateTransaction(_ types.Transaction) error { return nil }
```

#### Thiết kế để mở rộng

Interface được thiết kế để dễ dàng thêm logic thực trong các phase sau:

| Phương thức | Logic dự kiến |
|-------------|---------------|
| `ValidateBlock` | Kiểm tra hash chain (`block.Hash == DoubleHA256(header)`), Merkle root, chữ ký của leader |
| `ValidateTransaction` | Kiểm tra chữ ký ECDSA trên ScriptSig, kiểm tra UTXO tồn tại trong world state, phát hiện double-spend |

---

### 6.4 `storage/BlockStorage` — Lưu trữ block

**Vị trí:** `internal/storage/block_storage.go`

#### Ghi block: `AppendBlock`

```go
f, _ := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
```

Cờ `os.O_APPEND` đảm bảo mọi lệnh `Write()` đều ghi vào **cuối file**, kể cả khi nhiều process cùng ghi (OS-level atomic append trên file systems POSIX). Cờ `os.O_CREATE` tạo file nếu chưa tồn tại. Không bao giờ dùng `os.O_TRUNC` để tránh mất dữ liệu cũ.

Mutex `sync.Mutex` bảo vệ lệnh ghi trong trường hợp nhiều goroutine cùng gọi `AppendBlock` (hiện tại chỉ có commit loop gọi, nhưng mutex đảm bảo an toàn khi mở rộng).

#### Đọc block: `ReadAll`

```go
func ReadAll(path string) ([]types.Block, error)
```

Hàm này mở file ở chế độ **read-only** (không ảnh hưởng đến file đang được ghi) và dùng `bufio.Scanner` để đọc từng dòng JSON. Buffer được mở rộng lên 16 MB để xử lý block lớn chứa nhiều giao dịch.

Trả về `nil, nil` nếu file chưa tồn tại (peer mới chưa nhận block nào) — tránh lỗi "file not found" không cần thiết.

---

### 6.5 `storage/WorldState` — Sổ cái trạng thái

**Vị trí:** `internal/storage/world_state.go`

**Thư viện:** `github.com/syndtr/goleveldb`

LevelDB là key-value store nhúng (embedded), phù hợp cho world state vì:
- **Hiệu năng cao** với workload ghi tuần tự (write-ahead log)
- **Atomic batch writes** — cả một block được áp dụng hoặc không
- **Iterator** hỗ trợ prefix scan để liệt kê toàn bộ UTXO

#### `ApplyBlock` — Áp dụng block vào world state

```go
func (ws *WorldState) ApplyBlock(block types.Block) error {
    batch := new(leveldb.Batch)
    for _, tx := range block.Transactions {
        for _, vin := range tx.Vin {
            batch.Delete([]byte(utxoKey(vin.Txid, vin.Vout)))  // tiêu thụ
        }
        for _, vout := range tx.Vout {
            val, _ := json.Marshal(vout)
            batch.Put([]byte(utxoKey(tx.Txid, vout.N)), val)    // tạo mới
        }
    }
    return ws.db.Write(batch, nil)  // atomic
}
```

Điểm quan trọng: tất cả `Delete` và `Put` được đưa vào `leveldb.Batch` trước khi gọi `Write` một lần duy nhất. Đây là cách duy nhất đảm bảo **tính nguyên tử** — nếu process crash giữa chừng, LevelDB sẽ rollback toàn bộ batch chứ không để world state ở trạng thái nửa vời.

#### `AllUTXOs` — Liệt kê toàn bộ UTXO

```go
iter := ws.db.NewIterator(util.BytesPrefix([]byte("utxo:")), nil)
```

`util.BytesPrefix` tạo ra một range query chỉ quét các key bắt đầu bằng `"utxo:"`, bỏ qua mọi namespace khác có thể thêm vào sau này.

---

### 6.6 `peer` — Orchestrator

**Vị trí:** `internal/peer/peer.go`

`CommittingPeer` là **"dây kết nối"** nối tất cả module lại. Nó không chứa business logic mà chỉ điều phối dòng chảy dữ liệu.

#### Cấu trúc nội bộ

```go
type CommittingPeer struct {
    deliverClient *deliver.Client       // Tầng 1: nhận
    validator     *validation.Engine   // Tầng 2: xác thực
    blockStore    *storage.BlockStorage // Tầng 3a: ghi file
    worldState    *storage.WorldState  // Tầng 3b: cập nhật DB

    blockChan  chan types.Block  // Channel nối tầng 1 và 2-3 (buffer=64)

    // Runtime stats (thread-safe)
    blockCount    int64        // atomic.AddInt64
    mu            sync.RWMutex
    lastBlockHash []byte
    lastBlockTime time.Time
    lastBlockTxs  int
    ordererAddr   string
}
```

#### `commitLoop` — Vòng lặp commit chính

```go
func (p *CommittingPeer) commitLoop(ctx context.Context) {
    for {
        select {
        case block := <-p.blockChan:
            p.handleBlock(block)
        case <-ctx.Done():
            return
        }
    }
}
```

Vòng lặp này chạy trong một goroutine riêng, xử lý từng block tuần tự. Thứ tự xử lý được đảm bảo vì Go channel có tính **FIFO**.

#### `handleBlock` — Xử lý một block

```
handleBlock(block)
    ├─ validator.ValidateBlock(block)  → nếu lỗi: log và bỏ qua
    ├─ blockStore.AppendBlock(block)   → nếu lỗi: log và bỏ qua (không rollback)
    ├─ worldState.ApplyBlock(block)    → nếu lỗi: log và bỏ qua
    └─ cập nhật stats (atomic)
```

> **Lưu ý thiết kế:** Hiện tại nếu `blockStore` thành công nhưng `worldState` thất bại, sẽ xảy ra **inconsistency** — block đã vào file nhưng world state chưa được cập nhật. Trong phase 2, cần thêm cơ chế recovery: khi khởi động, so sánh số block trong file với trạng thái world state, và re-apply các block còn thiếu.

#### `GetStats` — Thread-safe stats snapshot

```go
func (p *CommittingPeer) GetStats() Stats {
    p.mu.RLock()
    defer p.mu.RUnlock()
    return Stats{
        BlockCount: atomic.LoadInt64(&p.blockCount),
        ...
    }
}
```

`blockCount` dùng `sync/atomic` thay vì mutex vì đây là số nguyên đơn và atomic operations nhanh hơn đáng kể. Các trường còn lại dùng `sync.RWMutex` cho phép nhiều goroutine đọc đồng thời.

---

### 6.7 `cmd/peer` — Entry point & CLI

**Vị trí:** `cmd/peer/main.go`

#### Startup sequence

```
1. Nhập thông tin qua stdin (orderer address, blockfile, dbpath)
2. Khởi tạo BlockStorage
3. Khởi tạo WorldState (LevelDB)
4. Khởi tạo deliver.Client (libp2p host trên random port)
5. Khởi tạo validation.Engine
6. Khởi tạo CommittingPeer và gọi Start()
7. Khởi tạo readline interactive CLI
8. Redirect log.SetOutput → readline.Stdout (tách log khỏi input prompt)
9. Vào CLI loop
```

#### Tách log khỏi input

Kỹ thuật quan trọng: `log.SetOutput(rl.Stdout())` khiến tất cả output từ `log.Printf()` chạy qua readline, cho phép readline tạm thời ẩn prompt khi cần in log và hiện lại sau. Nếu không làm điều này, log messages sẽ xen lẫn vào giữa input đang gõ dở.

---

## 7. Luồng xử lý block (Pipeline)

Dưới đây là sequence diagram từ khi Ordering Service commit một block đến khi Committing Peer ghi xong:

```
Ordering Service          deliver.Client        CommittingPeer
      │                        │                      │
      │  (block committed)     │                      │
      │─ NotifyNewBlock() ─►   │                      │
      │                        │                      │
      │  JSON stream           │                      │
      │─ encoder.Encode() ──►  │                      │
      │                        │ decoder.Decode()      │
      │                        │ goroutine nền nhận    │
      │                        │─ blockChan <- block ─►│
      │                        │                      │
      │                        │          commitLoop select
      │                        │                      │
      │                        │          ValidateBlock()
      │                        │          (hiện tại: pass)
      │                        │                      │
      │                        │          AppendBlock()
      │                        │          → ghi vào chain.block
      │                        │                      │
      │                        │          ApplyBlock()
      │                        │          → LevelDB batch write
      │                        │          (xóa UTXO cũ, thêm UTXO mới)
      │                        │                      │
      │                        │          atomic stats update
      │                        │          log.Printf("committed block...")
```

---

## 8. Thiết kế đồng thời (Concurrency)

| Goroutine | Chạy ở đâu | Nhiệm vụ |
|-----------|------------|----------|
| deliver goroutine (outer) | `deliver.Subscribe()` | Đọc stream, đẩy vào `blockChan` |
| deliver goroutine (inner) | `deliver.Subscribe()` | Chờ `ctx.Done()`, đóng stream |
| commit loop | `peer.Start()` | Đọc `blockChan`, validate, persist |
| CLI loop | `main()` goroutine chính | Đọc input, in output |

```
main goroutine ─── CLI loop
       │
       └── go peer.Start()
                 ├── go commitLoop()          ← goroutine 1
                 └── deliver.Subscribe()
                           ├── go outerRecv() ← goroutine 2
                           └── go innerStop() ← goroutine 3
```

**Điểm đồng bộ:**
- `blockChan` (buffer=64): không block deliver goroutine trừ khi commit loop bị chậm
- `sync.Mutex` trong `BlockStorage.AppendBlock`: bảo vệ file handle
- `sync.RWMutex` trong `CommittingPeer`: bảo vệ stats fields
- `sync/atomic` cho `blockCount`: tránh overhead của mutex với counter đơn giản

---

## 9. Giao thức kết nối với Ordering Service

### Protocol ID

```
/raft-order-service/deliver/1.0.0
```

Được định nghĩa trong cả hai bên:
- Ordering Service: `orderingservice/source/internal/network/protocol.go`
- Committing Peer: `commitingpeer/source/internal/deliver/client.go`

### Định dạng địa chỉ

```
/ip4/<IP>/tcp/<PORT>/p2p/<libp2p-PeerID>
```

**Ví dụ:**
```
/ip4/127.0.0.1/tcp/6000/p2p/12D3KooWAbcDef...
```

PeerID được lấy từ output khi khởi động một node Ordering Service:
```
Node ID: 12D3KooW...
Address: /ip4/0.0.0.0/tcp/6000/p2p/12D3KooW...
```

### Handshake

```
Peer ──── DeliverRequest{FromIndex: 1} ────► Orderer
Peer ◄──── Block{...} ◄──── Block{...} ◄──── ... (stream vô hạn)
```

`FromIndex = 1` yêu cầu toàn bộ chain từ block đầu tiên. Orderer sẽ gửi trước tất cả block hiện có, sau đó tiếp tục stream các block mới khi chúng được commit.

---

## 10. CLI Reference

### Khởi động

```
> peer.exe
=== Committing Peer ===

Enter orderer address: /ip4/127.0.0.1/tcp/6000/p2p/<PeerID>
Enter block file path (default: chain.block):
Enter world state directory (default: worldstate):

Committing peer started!
```

### Danh sách lệnh

#### `status` — Trạng thái tổng quan

Hiển thị thông tin peer, blockchain và world state theo thời gian thực.

```
=== Committing Peer Status ===
Orderer    : /ip4/127.0.0.1/tcp/6000/p2p/12D3KooW...
Block file : chain.block
World state: worldstate

=== Blockchain ===
Committed blocks : 5
Last block hash  : a1b2c3d4e5f60718...
Last block time  : 2026-04-12T10:30:00+07:00
Last block txs   : 3

=== World State ===
Unspent outputs (UTXOs): 12
```

---

#### `chain` — Danh sách block

Đọc toàn bộ file `chain.block` và liệt kê các block dạng bảng tóm tắt.

```
=== Blockchain (5 blocks) ===
  Block #1     hash=a1b2c3d4e5f60718...  prev=0000000000000000...  txs=2    size=384  time=2026-04-12T10:20:00+07:00
  Block #2     hash=b2c3d4e5f6071829...  prev=a1b2c3d4e5f60718...  txs=3    size=576  time=2026-04-12T10:25:00+07:00
  ...
```

---

#### `block <n>` — Chi tiết một block

Hiển thị đầy đủ thông tin của block thứ `n` (1-based), bao gồm tất cả giao dịch với inputs và outputs.

```
> block 2

=== Block #2 ===
Hash       : b2c3d4e5f6071829...
PrevHash   : a1b2c3d4e5f60718...
MerkleRoot : d4e5f607182930ab...
Timestamp  : 2026-04-12T10:25:00+07:00
Nonce      : 0
Size       : 576 bytes
Txs        : 3

--- Transactions ---
  [1] txid=abc123def456...
       in  0: 0000000000000000...[0]
       out 0: value=5000000     addr=1A2B3C...
  [2] txid=def456ghi789...
       ...
```

---

#### `tx <txid>` — Tra cứu giao dịch

Tìm kiếm một giao dịch theo txid xuyên suốt toàn bộ chain. Cho biết block chứa giao dịch đó.

```
> tx abc123def456789012345678901234567890123456789012345678901234567890

=== Transaction ===
Txid    : abc123def456789012345678901234567890123456789012345678901234567890
Block   : #2  (hash b2c3d4e5f607...)
Version : 1    LockTime: 0
Inputs  : 1
  in  [0]  prev=0000000000000000...[0]
Outputs : 2
  out [0]  value=4000000     addr=1ReceiverAddr...
  out [1]  value=1000000     addr=1ChangeAddr...
```

---

#### `utxo <txid> <n>` — Tra cứu UTXO

Kiểm tra một output cụ thể còn trong world state (chưa bị tiêu thụ) hay không.

```
> utxo abc123def456... 0

=== UTXO abc123def456...[0] ===
Value  : 4000000
Index  : 0
Addr   : 1ReceiverAddr...
Script : OP_DUP OP_HASH160 ... OP_EQUALVERIFY OP_CHECKSIG
```

Nếu UTXO đã bị spent hoặc không tồn tại:
```
UTXO abc123def4...[0] not found (spent or never existed): leveldb: not found
```

---

#### `worldstate` — Toàn bộ UTXO set

Liệt kê tất cả UTXO hiện đang tồn tại trong LevelDB.

```
=== World State (12 UTXOs) ===
     1. abc123def4...[0]  value=4000000     addr=1ReceiverAddr...
     2. abc123def4...[1]  value=1000000     addr=1ChangeAddr...
     3. def456ghi7...[0]  value=7500000     addr=1AnotherAddr...
     ...
```

---

#### `help` và `quit`

```
> help     ← in lại danh sách lệnh
> quit     ← tắt peer và giải phóng tài nguyên
```

---

## 11. Hướng dẫn Build và Chạy

### Yêu cầu

- Go 1.21 trở lên
- Kết nối internet (lần đầu để `go mod tidy` download dependencies)

### Build

```bash
cd commitingpeer/source
go mod tidy          # download dependencies (chỉ cần lần đầu)
go build -o ../peer.exe ./cmd/peer/
```

### Chạy

Đảm bảo ít nhất một node Ordering Service đã được khởi động và lấy địa chỉ của nó:

```
# Trong cửa sổ Ordering Service:
Node ID: 12D3KooWXxxx
Address: /ip4/0.0.0.0/tcp/6000/p2p/12D3KooWXxxx

# Địa chỉ để connect từ máy khác:
/ip4/<IP_của_node>/tcp/6000/p2p/12D3KooWXxxx
```

Sau đó chạy peer:

```bash
cd commitingpeer
./peer.exe
```

### Files được tạo ra khi chạy

| File/Thư mục | Nội dung | Có thể xóa không? |
|---|---|---|
| `chain.block` | Tất cả block đã commit (JSON, newline-delimited) | Không — mất toàn bộ blockchain |
| `worldstate/` | LevelDB database chứa UTXO set | Có thể tái tạo bằng cách replay `chain.block` |

---

## 12. Hướng phát triển tiếp theo

### Phase 2: Validation thực sự

- **Block validation:** Xác minh `block.Hash` bằng cách tính lại Double-SHA256 của block header; kiểm tra `block.PrevHash` khớp với hash của block trước trong chain.
- **Merkle root validation:** Tính lại Merkle root từ danh sách txid và so sánh với `block.MerkleRoot`.
- **Transaction validation:** Kiểm tra chữ ký ECDSA của ScriptSig; kiểm tra UTXO tham chiếu trong VIN tồn tại trong world state; phát hiện double-spend trong cùng một block.

### Phase 3: Khôi phục sau sự cố (Crash Recovery)

Khi peer khởi động lại sau crash:
1. Đọc số block từ `chain.block`.
2. So sánh với trạng thái world state (có thể lưu block index cuối đã apply vào một key đặc biệt trong LevelDB, ví dụ `meta:last_block_index`).
3. Nếu lệch, re-apply các block còn thiếu từ file vào world state.

### Phase 4: Tái kết nối tự động

Khi stream bị ngắt (orderer restart, mất mạng), deliver client cần:
1. Phát hiện lỗi kết nối.
2. Chờ một khoảng thời gian (exponential backoff).
3. Tái kết nối và gửi lại `DeliverRequest` với `FromIndex = lastCommittedBlock + 1`.

### Phase 5: Hỗ trợ nhiều Orderer

Hiện tại peer chỉ kết nối tới một orderer duy nhất. Cần mở rộng để:
- Kết nối tới tất cả node trong cluster.
- Nếu node đang kết nối thất bại, tự động chuyển sang node khác.
