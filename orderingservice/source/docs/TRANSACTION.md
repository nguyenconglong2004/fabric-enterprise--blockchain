# Transaction trong Ordering Service

Tài liệu này mô tả **cấu trúc dữ liệu** và **các bước tạo transaction** trong ordering service, bao gồm cơ chế ký Ed25519 và định dạng serialization theo chuẩn Bitcoin.

---

## 1. Cấu trúc dữ liệu

### 1.1 Transaction

File: `internal/types/transaction.go`

```go
type Transaction struct {
    Version  uint32 `json:"version"`
    Vin      []VIN  `json:"vin"`
    Vout     []VOUT `json:"vout"`
    LockTime uint32 `json:"locktime"`
    Txid     string `json:"txid"`
}
```

| Field | Kiểu | Ý nghĩa |
|---|---|---|
| `Version` | uint32 | Phiên bản giao thức (hiện tại = 1) |
| `Vin` | []VIN | Danh sách đầu vào — tham chiếu các output trước đó |
| `Vout` | []VOUT | Danh sách đầu ra — chuyển coin đến địa chỉ đích |
| `LockTime` | uint32 | Thời gian khóa (hiện tại = 0) |
| `Txid` | string | ID duy nhất của transaction — 64 ký tự hex |

---

### 1.2 VIN — Transaction Input

```go
type VIN struct {
    Txid      string    `json:"txid"`
    Vout      int       `json:"vout"`
    ScriptSig ScriptSig `json:"scriptSig"`
}

type ScriptSig struct {
    ASM string `json:"asm"`
    Hex string `json:"hex"`
}
```

| Field | Ý nghĩa |
|---|---|
| `Txid` | Txid của transaction trước đó chứa output đang được spend |
| `Vout` | Chỉ số output trong transaction trước đó |
| `ScriptSig.Hex` | Chữ ký mở khóa — `sig (64 bytes) \|\| pubkey (32 bytes)` = 96 bytes → 192 ký tự hex |
| `ScriptSig.ASM` | Dạng đọc được: `"<sig_hex> <pubkey_hex>"` |

---

### 1.3 VOUT — Transaction Output

```go
type VOUT struct {
    Value        int64        `json:"value"`
    N            int          `json:"n"`
    ScriptPubKey ScriptPubKey `json:"scriptPubKey"`
}

type ScriptPubKey struct {
    ASM       string   `json:"asm"`
    Hex       string   `json:"hex"`
    Addresses []string `json:"addresses"`
}
```

| Field | Ý nghĩa |
|---|---|
| `Value` | Số lượng coin chuyển đi |
| `N` | Chỉ số output trong transaction này (0, 1, ...) |
| `ScriptPubKey.Hex` | Script khóa output — P2PKH: `"76a914" + addr(40 hex) + "88ac"` |
| `ScriptPubKey.ASM` | `"OP_DUP OP_HASH160 <addr> OP_EQUALVERIFY OP_CHECKSIG"` |
| `ScriptPubKey.Addresses` | Danh sách địa chỉ nhận (thường 1 phần tử) |

---

### 1.4 Sơ đồ liên kết UTXO giữa các transaction

```
Transaction A                          Transaction B
──────────────────────────────         ──────────────────────────────
Vout[0]: value=500  → addr_X           Vin[0]: Txid=A, Vout=0
Vout[1]: value=200  → addr_Y      ←─── Vin[1]: Txid=A, Vout=1
                                       Vout[0]: value=300 → addr_Z
                                       Vout[1]: value=400 → addr_X  (tiền thối)
```

Mỗi `VIN` trong Transaction B tham chiếu chính xác một `VOUT` đã tồn tại trong Transaction A. Mô hình này gọi là **UTXO (Unspent Transaction Output)**.

---

## 2. Keypair và Địa chỉ

File: `internal/types/sig.go`

### 2.1 Sinh Ed25519 Keypair

```go
seed, priv, pub, err := NewEd25519Keypair()
```

| Giá trị trả về | Kích thước | Mục đích |
|---|---|---|
| `seed` | 32 bytes | Backup — dùng để khôi phục lại `priv` |
| `priv` | 64 bytes | Private key — dùng để ký transaction |
| `pub` | 32 bytes | Public key — dùng để tạo địa chỉ |

Để khôi phục từ seed đã lưu:

```go
priv, err := PrivFromSeedHex(seedHex)
```

---

### 2.2 Tạo địa chỉ từ Public Key

```go
address := AddressFromPub(pub)  // 40 ký tự hex
```

Quy trình nội bộ (`HashPubKey`):

```
pub (32 bytes)
    │
    ├─ SHA256(pub)       → 32 bytes
    └─ RIPEMD160(result) → 20 bytes → hex encode → address (40 chars)
```

Đây là chuẩn derivation P2PKH của Bitcoin.

---

### 2.3 Tạo ScriptPubKey khóa output vào một địa chỉ

```go
spk := MakeP2PKHScriptPubKey(addr)
// spk.Hex = "76a914" + addr + "88ac"
```

Ý nghĩa từng opcode trong script hex:

| Bytes | Opcode | Ý nghĩa |
|---|---|---|
| `76` | OP_DUP | Nhân đôi phần tử trên stack |
| `a9` | OP_HASH160 | SHA256 rồi RIPEMD160 phần tử trên stack |
| `14` | (push 20 bytes) | Đẩy 20 bytes tiếp theo lên stack |
| `{addr}` | — | 20-byte pubKeyHash của người nhận |
| `88` | OP_EQUALVERIFY | Kiểm tra bằng nhau, fail nếu không khớp |
| `ac` | OP_CHECKSIG | Kiểm tra chữ ký Ed25519 |

---

## 3. Các bước tạo Transaction

### Bước 1 — Chuẩn bị Keypair và danh sách UTXO

```go
seed, priv, pub, _ := types.NewEd25519Keypair()
myAddr := types.AddressFromPub(pub)

// Client tự theo dõi UTXOs của mình
utxos := []types.ClientUTXO{
    {Txid: "abc123...", VoutIdx: 0, Out: vout},
}
```

`ClientUTXO` là struct phía client đại diện cho một output chưa được spend:

```go
type ClientUTXO struct {
    Txid    string
    VoutIdx int
    Out     VOUT
}
```

---

### Bước 2 — `CreateTransaction`: Chọn input và xây cấu trúc

```go
tx, err := types.CreateTransaction(priv, fromAddr, toAddr, amount, availableUTXOs)
```

Luồng xử lý bên trong:

```
[1] Greedy input selection
    Duyệt qua availableUTXOs, cộng dồn Out.Value
    Dừng khi total >= amount
    Nếu total < amount → lỗi "insufficient funds"

[2] Xây VINs (ScriptSig để trống, sẽ điền ở bước ký)
    Vin[i] = { Txid: utxo.Txid, Vout: utxo.VoutIdx, ScriptSig: {} }
    prevOuts[i] = utxo.Out   ← lưu lại để dùng khi tính sighash

[3] Xây VOUTs
    Vout[0] = { Value: amount,       ScriptPubKey: P2PKH(toAddr)   }
    Vout[1] = { Value: total-amount, ScriptPubKey: P2PKH(fromAddr) }
              (chỉ thêm Vout[1] nếu total > amount — tiền thối)

[4] Gọi tx.SignEd25519(priv, prevOuts)
    → Điền ScriptSig cho từng input + tính Txid
```

---

### Bước 3 — `SignEd25519`: Ký từng input

```go
err := tx.SignEd25519(priv, prevOuts)
```

Với **mỗi input `i`**, thực hiện tuần tự:

```
[1] txCopy = tx.ShallowCopyEmptySigs()
    Tạo bản sao transaction với TẤT CẢ ScriptSig = rỗng

[2] txCopy.Vin[i].ScriptSig.Hex = prevOuts[i].ScriptPubKey.Hex
    Inject ScriptPubKey của output đang được spend vào input đang ký
    → Đảm bảo chữ ký bao phủ điều kiện tiêu thụ output

[3] Tính sighash (double SHA256)
    raw     = txCopy.Serialize()
    h1      = SHA256(raw)
    sighash = SHA256(h1)

[4] Ký bằng Ed25519
    sig = ed25519.Sign(priv, sighash)     // 64 bytes

[5] Ghép ScriptSig
    pub                      = priv.Public()          // 32 bytes
    script                   = sig || pub              // 96 bytes
    Vin[i].ScriptSig.Hex     = hex(script)            // 192 ký tự hex
    Vin[i].ScriptSig.ASM     = "<sig_hex> <pub_hex>"

Sau khi ký xong tất cả inputs:
    tx.Txid = tx.ComputeTxID()
```

**Lý do inject ScriptPubKey trước khi hash:** Chữ ký cần bao phủ điều kiện của output nguồn. Nếu không, kẻ tấn công có thể thay đổi output đích (`toAddr`) mà không làm vô hiệu chữ ký hiện tại.

---

### Bước 4 — `ComputeTxID`: Tính định danh transaction

```go
txid := tx.ComputeTxID()
```

```
raw   = tx.Serialize()          // Serialize đầy đủ (bao gồm ScriptSig đã ký)
h1    = SHA256(raw)
h2    = SHA256(h1)              // Double SHA256
Txid  = hex(reverse(h2))       // Đảo byte — chuẩn hiển thị little-endian của Bitcoin
```

Txid được tính **sau khi ký xong** vì ScriptSig là một phần của dữ liệu serialized.

---

### Bước 5 — `Serialize`: Định dạng nhị phân (Bitcoin wire format)

```
┌─────────────────────────────────────────────────────┐
│ Version          │ 4 bytes  (little-endian uint32)  │
├──────────────────┼─────────────────────────────────-│
│ VIN count        │ varint                            │
│  (lặp lại)       │                                   │
│  ├ prev_txid     │ 32 bytes (little-endian)          │
│  ├ prev_vout     │ 4 bytes  (little-endian uint32)   │
│  ├ scriptSig len │ varint                            │
│  ├ scriptSig     │ n bytes                           │
│  └ sequence      │ 4 bytes  = 0xffffffff             │
├──────────────────┼───────────────────────────────────│
│ VOUT count       │ varint                            │
│  (lặp lại)       │                                   │
│  ├ value         │ 8 bytes  (little-endian uint64)   │
│  ├ scriptPubKey  │ varint len + n bytes              │
│  └ len           │                                   │
├──────────────────┼───────────────────────────────────│
│ LockTime         │ 4 bytes  (little-endian uint32)   │
└─────────────────────────────────────────────────────┘
```

**varint** (variable-length integer):

| Giá trị | Encoding |
|---|---|
| < 0xFD | 1 byte |
| ≤ 0xFFFF | `0xFD` + 2 bytes LE |
| ≤ 0xFFFFFFFF | `0xFE` + 4 bytes LE |
| lớn hơn | `0xFF` + 8 bytes LE |

---

## 4. Luồng hoàn chỉnh từ Client đến Ordering Service

```
Client (cmd/client/main.go)
│
├─ keygen
│     └─ NewEd25519Keypair() → hiện seed + address
│
├─ fund <amount>
│     └─ Tạo genesis UTXO (coinbase-like, không cần prevOut)
│        Lưu vào local UTXO tracker
│
└─ tx <toAddr> <amount>
      │
      ├─ CreateTransaction(priv, myAddr, toAddr, amount, myUTXOs)
      │       ├─ Greedy chọn inputs
      │       ├─ Xây VINs + VOUTs
      │       └─ SignEd25519(priv, prevOuts)
      │               ├─ Với mỗi input: inject ScriptPubKey → double SHA256 → Ed25519 sign
      │               └─ ScriptSig.Hex = hex(sig || pub) = 192 ký tự
      │                  Txid = double-SHA256(serialize(signedTx))
      │
      ├─ client.SubmitTransaction(signedTx, targetNode)
      │       └─ Gửi qua libp2p P2P stream (JSON encoding)
      │
      └─ applyTx(signedTx)
              └─ Cập nhật local UTXO tracker:
                 xóa các input đã spend,
                 thêm output change thuộc về myAddr

Ordering Service Node (internal/raft/transaction.go)
│
├─ HandleTxRequest(msg)
│       ├─ tx.Validate()        — kiểm tra txid/vin/vout không rỗng
│       ├─ Không phải leader   → forwardTxToLeader()
│       └─ Là leader           → TxPool = append(TxPool, tx)
│
└─ StartAutoProposeBlock(batchSize)
        │
        ├─ Khi TxPool có đủ tx:
        │       ProposeBlock(batchSize)
        │         ├─ Lấy N tx đầu từ TxPool
        │         ├─ types.NewBlock(txs, prevHash)
        │         └─ Broadcast BlockProposal → Raft consensus
        │
        └─ Khi đạt majority ACK:
                commitBlock(entry)
                  ├─ AppendBlock → OrderingBlock
                  ├─ Xóa tx đã commit khỏi TxPool
                  └─ Broadcast BlockCommit đến followers
```

---

## 5. Tóm tắt các hàm chính

| Hàm | File | Mục đích |
|---|---|---|
| `NewEd25519Keypair()` | `internal/types/sig.go` | Sinh seed / private key / public key |
| `AddressFromPub(pub)` | `internal/types/sig.go` | Tạo địa chỉ P2PKH từ public key |
| `MakeP2PKHScriptPubKey(addr)` | `internal/types/sig.go` | Tạo script khóa output theo chuẩn P2PKH |
| `PrivFromSeedHex(hex)` | `internal/types/sig.go` | Khôi phục private key từ seed hex |
| `CreateTransaction(...)` | `internal/types/transaction.go` | Chọn inputs + xây cấu trúc + ký |
| `SignEd25519(priv, prevOuts)` | `internal/types/transaction.go` | Ký từng input bằng Ed25519 |
| `ComputeTxID()` | `internal/types/transaction.go` | Tính Txid — double SHA256 + byte reverse |
| `Serialize()` | `internal/types/transaction.go` | Mã hóa nhị phân Bitcoin wire format |
| `ShallowCopyEmptySigs()` | `internal/types/transaction.go` | Tạo bản sao xóa ScriptSig (dùng khi tính sighash) |
| `Validate()` | `internal/types/transaction.go` | Kiểm tra cấu trúc tối thiểu (txid, vin, vout) |
