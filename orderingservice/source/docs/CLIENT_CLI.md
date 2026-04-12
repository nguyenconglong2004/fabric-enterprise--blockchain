# Ordering Service Client — Tài liệu Thiết kế và CLI

## Mục lục

1. [Tổng quan](#1-tổng-quan)
2. [Kiến trúc Client](#2-kiến-trúc-client)
3. [Quản lý Keypair và Địa chỉ ví](#3-quản-lý-keypair-và-địa-chỉ-ví)
4. [Mô hình UTXO phía Client](#4-mô-hình-utxo-phía-client)
5. [Tạo và ký giao dịch](#5-tạo-và-ký-giao-dịch)
6. [Giao tiếp với Ordering Service](#6-giao-tiếp-với-ordering-service)
7. [Auto-send và Load Testing](#7-auto-send-và-load-testing)
8. [CLI Reference](#8-cli-reference)
9. [Luồng hoàn chỉnh từ đầu đến cuối](#9-luồng-hoàn-chỉnh-từ-đầu-đến-cuối)

---

## 1. Tổng quan

Client (`cmd/client/main.go`) là công cụ tương tác trực tiếp với Ordering Service cluster. Client có hai nhiệm vụ chính:

1. **Quản lý ví (wallet):** Sinh và lưu trữ keypair Ed25519, theo dõi UTXO phía client, ký giao dịch trước khi gửi đi.
2. **Gửi giao dịch:** Submit transaction đơn lẻ (`tx`) hoặc tự động ở tốc độ cấu hình được (`start`) để stress-test cluster.

Client sử dụng cùng stack mạng `go-libp2p` với các node trong cluster, kết nối trực tiếp tới bất kỳ node nào (không cần biết node đó có phải là leader hay không — node sẽ tự forward giao dịch về leader).

---

## 2. Kiến trúc Client

```
cmd/client/main.go
        │
        ├── walletState            ← quản lý keypair + UTXO local
        │     ├── priv  ed25519.PrivateKey
        │     ├── pub   ed25519.PublicKey
        │     ├── address  string  (40-hex, P2PKH)
        │     └── utxos  map[string]ClientUTXO
        │
        └── pkg/client.OrderClient ← giao tiếp mạng
              ├── Transport         (libp2p host)
              ├── SubmitTransaction()       (chờ ACK)
              ├── SubmitTransactionFast()   (không chờ)
              └── GetClusterNodes()
```

**`OrderClient`** (`pkg/client/client.go`) được khởi tạo với một libp2p host ngẫu nhiên (random port). Khi kết nối tới một node trong cluster, client:
1. Yêu cầu danh sách tất cả node qua `GetClusterNodes()` (gửi `MsgMembershipRequest`).
2. Lưu node đầu tiên trong danh sách làm `targetNode` — địa chỉ gửi mọi giao dịch sau đó.

---

## 3. Quản lý Keypair và Địa chỉ ví

File liên quan: `internal/types/sig.go`

### 3.1 Thuật toán khóa: Ed25519

Client dùng **Ed25519** — thuật toán chữ ký số đường cong elliptic (Edwards-curve). So với ECDSA (Bitcoin gốc), Ed25519 nhanh hơn, chữ ký nhỏ hơn (64 bytes), và không cần số ngẫu nhiên khi ký (deterministic).

```
Seed (32 bytes ngẫu nhiên)
    └── ed25519.NewKeyFromSeed(seed)
            ├── Private Key (64 bytes = seed || pubkey)
            └── Public Key  (32 bytes)
                    └── SHA256 → RIPEMD160 → Address (20 bytes = 40 hex chars)
```

### 3.2 Derivation địa chỉ: P2PKH kiểu Bitcoin

```go
// internal/types/sig.go
func AddressFromPub(pub ed25519.PublicKey) string {
    sha := sha256.Sum256(pub)      // SHA256(pubkey)
    rip := ripemd160.New()
    rip.Write(sha[:])              // RIPEMD160(sha)
    return hex.EncodeToString(rip.Sum(nil))
}
```

Địa chỉ là **40 ký tự hex** (20 bytes) — tương đương `pubKeyHash` trong Bitcoin P2PKH. ScriptPubKey khóa output về địa chỉ này:

```
OP_DUP OP_HASH160 <address> OP_EQUALVERIFY OP_CHECKSIG
Hex: 76a914<address_20bytes>88ac
```

### 3.3 Khôi phục ví từ seed

Seed là **dữ liệu duy nhất cần lưu**. Từ seed, mọi thứ khác được tái tạo deterministic:
- Private key → `ed25519.NewKeyFromSeed(seed)`
- Public key → trích xuất từ private key
- Address → `SHA256(RIPEMD160(pubkey))`

---

## 4. Mô hình UTXO phía Client

### 4.1 `walletState.utxos`

```go
type walletState struct {
    priv    ed25519.PrivateKey
    pub     ed25519.PublicKey
    address string
    utxos   map[string]ClientUTXO  // key: "txid:voutIndex"
}

type ClientUTXO struct {
    Txid    string
    VoutIdx int
    Out     VOUT
}
```

Client tự duy trì một **local UTXO set** trong bộ nhớ — không query từ World State. Mỗi lần gửi giao dịch thành công, `applyTx()` cập nhật map:

```go
func (w *walletState) applyTx(tx Transaction) {
    // Xóa các input đã tiêu thụ
    for _, vin := range tx.Vin {
        delete(w.utxos, fmt.Sprintf("%s:%d", vin.Txid, vin.Vout))
    }
    // Thêm output nào thuộc địa chỉ của ví (tiền thối)
    myScript := MakeP2PKHScriptPubKey(w.address)
    for i, vout := range tx.Vout {
        if vout.ScriptPubKey.Hex == myScript.Hex {
            w.addUTXO(tx.Txid, i, vout)
        }
    }
}
```

### 4.2 Genesis UTXO (Coinbase-like)

Trong mô hình UTXO thuần túy, không ai có coin ban đầu — mọi output đều phải tham chiếu một output trước đó. Để khởi động, client tạo **giao dịch coinbase giả**:

```go
Vin[0] = VIN{
    Txid:      "000...counter",   // ← không tham chiếu UTXO thực
    ScriptSig: { ASM: "coinbase-N" },
}
Vout[0] = VOUT{ Value: amount, ScriptPubKey: P2PKH(wallet.address) }
```

Giao dịch này hợp lệ về mặt cấu trúc, nhưng trong triển khai thực tế cần cơ chế kiểm soát để chặn coinbase giả. Hiện tại Validation Engine ở Committing Peer là stub nên chấp nhận tất cả.

---

## 5. Tạo và ký giao dịch

File liên quan: `internal/types/transaction.go`

### 5.1 Greedy input selection

`CreateTransaction()` chọn UTXO theo thứ tự trong map cho đến khi tổng giá trị ≥ `amount`:

```
availableUTXOs = [UTXO_A=60000, UTXO_B=80000, UTXO_C=30000]
amount = 100000

Chọn: UTXO_A(60000) → tổng=60000 < 100000
      UTXO_B(80000) → tổng=140000 ≥ 100000  ← dừng

Vout[0]: 100000 → toAddr      (thanh toán)
Vout[1]:  40000 → fromAddr    (tiền thối)
```

### 5.2 Cơ chế ký Ed25519 (sighash)

`SignEd25519()` ký từng input riêng biệt theo quy trình:

```
Với mỗi Vin[i]:
  1. Tạo bản sao tx với toàn bộ ScriptSig bị xóa trắng
  2. Inject ScriptPubKey của output đang được tiêu thụ vào ScriptSig[i]
     (tức là: "tôi đang chi tiêu output này")
  3. Serialize bản sao → SHA256 → SHA256 = sighash (32 bytes)
  4. sig = Ed25519.Sign(priv, sighash)  → 64 bytes
  5. ScriptSig.Hex = hex(sig || pubkey) → 192 ký tự hex
     ScriptSig.ASM = "<sig_hex> <pubkey_hex>"
  6. Sau khi ký xong tất cả input → tính lại Txid
```

Lý do inject ScriptPubKey vào bước 2: đảm bảo chữ ký **cam kết** vào output đang tiêu thụ — không thể tái sử dụng chữ ký này để chi tiêu một output khác.

### 5.3 Cấu trúc ScriptSig sau khi ký

```
ScriptSig.Hex = <64-byte-sig-hex><32-byte-pubkey-hex>
             = 128 ký tự hex + 64 ký tự hex
             = 192 ký tự hex (96 bytes)

ScriptSig.ASM = "<ed25519_signature> <public_key>"
```

---

## 6. Giao tiếp với Ordering Service

### 6.1 Khám phá cluster: `GetClusterNodes()`

Khi khởi động, client chỉ biết địa chỉ của **một node bất kỳ** trong cluster. Nó dùng node đó để lấy danh sách đầy đủ:

```
Client ── MsgMembershipRequest ──► Node X
Client ◄── MsgMembershipResponse ── Node X
           (danh sách tất cả alive members + địa chỉ)
```

Sau đó client lấy `allNodes[0]` làm `targetNode` — node nhận mọi giao dịch.

### 6.2 Hai chế độ submit

| Hàm | Cơ chế | Dùng khi |
|-----|--------|---------|
| `SubmitTransaction()` | Gửi rồi `time.Sleep(500ms)` chờ `MsgTxResponse` | Lệnh `tx` — cần xác nhận |
| `SubmitTransactionFast()` | Gửi ngay, không chờ phản hồi | Lệnh `start` — throughput cao |

### 6.3 Luồng message

```
Client                          Target Node (any)         Leader
  │                                  │                      │
  │── MsgTxRequest{tx} ─────────────►│                      │
  │                                  │                      │
  │                          if IsLeader():                  │
  │                            TxPool.append(tx)            │
  │                            MsgTxResponse → Client       │
  │                                  │                      │
  │                          if !IsLeader():                 │
  │                            forward ──── MsgTxRequest ──►│
  │                                                         │
  │◄── MsgTxResponse ───────────────────────────────────────│
  │    (từ leader, qua stream riêng)
```

Khi không phải leader, node nhận giao dịch **forward** cho leader và leader gửi ACK trực tiếp về client (qua stream ngược lại từ leader về client).

---

## 7. Auto-send và Load Testing

### 7.1 Goroutine auto-send

Lệnh `start [tps]` khởi động một goroutine với ba ticker:

```
goroutine auto-send
  ├── time.Ticker(1/tps giây)    ← gửi tx
  ├── time.Ticker(5 giây)        ← in stats
  └── select:
        case <-ticker.C:       → makeAutoTx() + SubmitTransactionFast()
        case <-statsTicker.C:  → in "sent=X acked=Y"
        case newTPS := <-speedChan: → ticker.Reset(1/newTPS)
        case <-stopChan:       → return
```

### 7.2 `makeAutoTx` — Tự cấp vốn khi hết UTXO

```
makeAutoTx(n):
  if wallet.utxos == {} → fund(100000)
  tx = CreateTransaction(priv, wallet.addr, wallet.addr, 1, utxos)
  if err (insufficient funds) → fund(100000) → retry
  wallet.applyTx(tx)
  return tx
```

Auto-send gửi 1 satoshi cho **chính ví mình** — mục đích chỉ để test throughput của cluster, không mang ý nghĩa kinh tế.

### 7.3 Đo hiệu năng

Hai counter atomic theo dõi song song:

| Counter | Tăng khi | Ý nghĩa |
|---------|---------|---------|
| `sendCount` | `SubmitTransactionFast` trả nil | Tx đã rời khỏi client |
| `AutoRecvCount` | Nhận được `MsgTxResponse` từ ordering node | Ordering node đã chấp nhận tx vào TxPool |

Hiệu số `sendCount - AutoRecvCount` = số tx "trên đường" hoặc bị mất mạng. Khi cluster bị overload, `AutoRecvCount` sẽ tụt hậu xa so với `sendCount`.

---

## 8. CLI Reference

### Startup

```
> client.exe

=== Raft Ordering Service Client ===

Enter address of a node in the cluster: /ip4/127.0.0.1/tcp/6000/p2p/12D3KooW...
Discovering cluster nodes...
Found 3 node(s) in cluster
```

---

### `keygen` — Tạo keypair Ed25519 mới

**Cú pháp:** `keygen`

**Mô tả:** Sinh 32 bytes ngẫu nhiên làm seed, tạo cặp khóa Ed25519, tính địa chỉ ví P2PKH.

```
> keygen
New keypair generated.
  Seed (hex): a1b2c3d4e5f6789012345678901234567890123456789012345678901234abcd
  Address:    9f8e7d6c5b4a3e2f1d0c9b8a7e6f5d4c
```

> Lưu `Seed (hex)` vào nơi an toàn. Nếu mất seed, không thể khôi phục ví.

---

### `wallet <seed_hex>` — Load ví từ seed đã có

**Cú pháp:** `wallet <seed_hex>`

**Mô tả:** Khôi phục keypair từ seed 64-hex. Địa chỉ ví được tính lại — luôn cho cùng kết quả với cùng seed.

```
> wallet a1b2c3d4e5f6789012345678901234567890123456789012345678901234abcd
Wallet loaded.
  Address: 9f8e7d6c5b4a3e2f1d0c9b8a7e6f5d4c
```

---

### `addr` — Hiển thị địa chỉ ví hiện tại

**Cú pháp:** `addr`

```
> addr
Address: 9f8e7d6c5b4a3e2f1d0c9b8a7e6f5d4c
```

---

### `fund <amount>` — Tạo UTXO genesis

**Cú pháp:** `fund <amount>`

**Mô tả:** Tạo một giao dịch coinbase-like với input không tham chiếu UTXO thực nào, output `amount` satoshi về địa chỉ ví. UTXO này được thêm ngay vào local wallet — **không gửi lên network** ở bước này, sẽ được dùng làm input cho giao dịch kế tiếp.

```
> fund 100000
Genesis UTXO created (not submitted to network).
  Txid:    ab34cd56ef789012ab34cd56ef789012ab34cd56ef789012ab34cd56ef789012
  Amount:  100000
  Address: 9f8e7d6c5b4a3e2f1d0c9b8a7e6f5d4c
```

> Phải chạy `fund` (hoặc nhận tiền thối từ giao dịch trước) trước khi dùng `tx`.

---

### `utxos` — Liệt kê UTXO trong ví

**Cú pháp:** `utxos`

**Mô tả:** In nội dung local UTXO set. Tổng `Total` là số dư có thể chi tiêu.

```
> utxos
UTXOs (2):
  ab34cd56ef789012...[0]  value=99999
  bc45de67fa890123...[1]  value=50000
Total: 149999
```

> UTXO set này chỉ phản ánh góc nhìn của client — **không đồng bộ** với World State trên Committing Peer.

---

### `tx <to_addr> <amount>` — Gửi một giao dịch có ký

**Cú pháp:** `tx <to_addr> <amount>`

**Mô tả:** Tạo giao dịch UTXO đầy đủ (greedy input selection, P2PKH output, tiền thối), ký bằng Ed25519, gửi lên cluster và chờ ACK.

```
> tx 1a2b3c4d5e6f7890ab1a2b3c4d5e6f7890ab1a2b3c4d5e6f7890ab1a2b3c4d 5000
Transaction submitted: bc45de67fa890123bc45de67fa890123bc45de67fa890123bc45de67fa890123
  Inputs:  1  Outputs: 2
```

Sau khi gửi, `wallet.applyTx()` cập nhật local UTXO:
- Xóa input đã dùng
- Thêm tiền thối (Vout có địa chỉ = ví mình)

Nếu số dư không đủ:
```
Error creating transaction: insufficient funds: have 50000, need 100000
```

---

### `start [tps]` — Bắt đầu auto-send

**Cú pháp:** `start [tps]`  (mặc định `tps = 1.0`)

**Mô tả:** Khởi động goroutine nền gửi giao dịch tự động. Mỗi giao dịch gửi 1 satoshi cho chính ví, tự động tái cấp vốn nếu hết UTXO. Cứ 5 giây in thống kê.

```
> start 10
Auto-send started at 10.00 TPS (signed Ed25519 transactions).
[Auto] Sent tx#1 ab34cd56... (total: 1)
[Auto] Sent tx#2 bc45de67... (total: 2)
...
[Auto] Stats: sent=50  acked=48
```

---

### `stop` — Dừng auto-send

**Cú pháp:** `stop`

```
> stop
Auto-send stopped. Sent: 250  Acked: 243
```

---

### `speed <tps>` — Thay đổi tốc độ trong khi đang chạy

**Cú pháp:** `speed <tps>`

**Mô tả:** Gửi tín hiệu vào `speedChan`, goroutine auto-send nhận được và gọi `ticker.Reset()` — không cần dừng rồi start lại.

```
> speed 50
[Auto] Speed changed to 50.00 TPS
```

---

### `status` — Xem thống kê auto-send

**Cú pháp:** `status`

```
> status
Auto-send: RUNNING | Wallet: 9f8e7d6c... | TX counter: 300 | Sent: 287 | Acked: 280
```

| Trường | Ý nghĩa |
|--------|---------|
| `Auto-send` | `RUNNING` hoặc `STOPPED` |
| `Wallet` | 8 ký tự đầu của địa chỉ ví |
| `TX counter` | Số thứ tự tx được tạo (kể cả lỗi) |
| `Sent` | Số tx gửi thành công ra khỏi client |
| `Acked` | Số tx nhận được `MsgTxResponse` từ ordering node |

---

### `help` — In danh sách lệnh

```
> help

=== Commands ===
  keygen              - Generate a new Ed25519 keypair
  wallet <seed_hex>   - Load keypair from existing seed hex
  addr                - Show current wallet address
  fund <amount>       - Create a genesis (coinbase-like) UTXO for current address
  utxos               - List available UTXOs in local wallet
  tx <to_addr> <amt>  - Create and submit a signed Ed25519 transaction
  start [tps]         - Start auto-send signed transactions (default 1 TPS)
  stop                - Stop auto-send
  speed <tps>         - Change TPS in real-time
  status              - Show auto-send statistics
  help                - Show this message
  quit                - Exit
```

---

### `quit` — Thoát

```
> quit
Shutting down...
```

Nếu auto-send đang chạy, `stopChan` được đóng trước khi thoát.

---

## 9. Luồng hoàn chỉnh từ đầu đến cuối

### 9.1 Phiên làm việc thông thường

```
1. Khởi động:  client.exe
2. Kết nối:    nhập địa chỉ một node bất kỳ
               → client discover toàn bộ cluster
               → chọn allNodes[0] làm targetNode

3. Tạo ví:     keygen          ← sinh seed + keypair
               (hoặc wallet <seed>  ← khôi phục)

4. Cấp vốn:    fund 100000     ← thêm genesis UTXO vào local wallet

5. Gửi tx:     tx <addr> 5000
                 └─ CreateTransaction():
                      greedy select UTXO từ wallet
                      build Vin + Vout (payment + change)
                      SignEd25519() → ScriptSig = sig||pubkey
                 └─ SubmitTransaction(signedTx, targetNode)
                      gửi MsgTxRequest qua libp2p
                      chờ 500ms nhận MsgTxResponse
                 └─ wallet.applyTx()
                      cập nhật local UTXO set

6. Kiểm tra:   utxos           ← xem số dư sau giao dịch
```

### 9.2 Luồng dữ liệu đầy đủ (tx → block committed)

```
Client
  │
  │ (1) CreateTransaction + SignEd25519
  │     Vin[0] = { Txid: "ab34...", ScriptSig: sig||pubkey }
  │     Vout[0] = { value: 5000, addr: to }
  │     Vout[1] = { value: 94999, addr: self }
  │
  │ (2) SubmitTransaction → MsgTxRequest
  ▼
Ordering Node (any)
  │
  │ (3a) Nếu là Follower: forward → Leader
  │ (3b) Nếu là Leader:
  │       TxPool.append(tx)
  │       gửi MsgTxResponse → Client
  │
  │ (4) Leader: ProposeBlock(txs)
  │       NewBlock(txs, prevHash)
  │       BroadcastMessage(MsgBlockProposal)
  ▼
Followers
  │ (5) HandleBlockProposal: xác nhận log continuity
  │       gửi MsgBlockProposalAck → Leader
  ▼
Leader (nhận đủ majority ACKs)
  │ (6) commitBlock():
  │       OrderingBlock.AppendBlock(block)
  │       BroadcastMessage(MsgBlockCommit)
  ▼
Followers
  │ (7) HandleBlockCommit():
  │       OrderingBlock.AppendBlock(block)
  │       DeliverMgr.NotifyNewBlock(block) → subscriber channels
  ▼
Committing Peer
  │ (8) deliver goroutine: json.Decode(block) → blockChan
  │ (9) commitLoop: ValidateBlock → AppendBlock → ApplyBlock(WorldState)
  ▼
chain.block + LevelDB (WorldState)
     Giao dịch được ghi vĩnh viễn
```

### 9.3 Vòng lặp UTXO

```
                fund 100000
                    │
                    ▼
         wallet.utxos = { genesis: 100000 }
                    │
              tx addr 5000
                    │
         CreateTransaction:
           input:  genesis(100000)
           out[0]: 5000  → toAddr
           out[1]: 94999 → self (tiền thối)  ← không có phí giao dịch
                    │
         wallet.applyTx():
           xóa: genesis
           thêm: change(94999)
                    │
                    ▼
         wallet.utxos = { change: 94999 }
                    │
              tx addr 1000
                    │
                   ...
```

> Mỗi giao dịch tiêu thụ hết UTXO đã chọn và trả lại phần dư dưới dạng UTXO mới (`change`). Không có phí giao dịch trong triển khai hiện tại — giá trị được bảo toàn 100%.
