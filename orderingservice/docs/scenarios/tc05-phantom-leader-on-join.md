# TC05 — Node mới tự nhận là Leader khi join cluster (Phantom Leader)

## 1. Bối cảnh

| Node | Priority | Vai trò | Trạng thái |
|---|---|---|---|
| **f0** | 0 (cao nhất) | **Leader** | Đang chạy bình thường |
| **f1** | 1 | Follower | Đang chạy bình thường |
| **f2** | 2 | _Mới thêm_ | Vừa `Start()`, chưa join |

**Scenario:** f2 được thêm vào cluster qua orchestrator `AddNode()` (hoặc CLI). Bug xảy ra khi f2 connect tới f1 (Follower), không phải f0 (Leader).

---

## 2. Luồng lỗi (trước khi fix)

```
Thời gian | f0 (Leader)          | f1 (Follower)                  | f2 (mới thêm)
----------+----------------------+--------------------------------+--------------------------------
t = 0s    | Chạy bình thường     | Chạy bình thường               | [NewRaftNode]
          |                      |                                | Membership: {f2}
          |                      |                                | state = Follower
t = 0ms   |                      |                                | [Start()]
          |                      |                                | len(GetAliveMembers()) == 1
          |                      |                                | → becomeLeader() ← BUG
          |                      |                                | → log: "I AM NOW THE LEADER"
          |                      |                                | → gửi heartbeat, bật auto-propose
t = 300ms |                      |                                | [orchestrator: sleep 300ms]
t = 300ms |                      |                                | Transport.Connect(f1.addr) OK
          |                      |                                | requestMembershipJoin(f1.ID)
          |                      |                                | → gửi MsgMembershipUpdate (Proposal)
          |                      |  Nhận MsgMembershipUpdate      |
          |                      |  f1.IsLeader() = false         |
          |                      |  → "Not leader, ignoring"      |
          |                      |  ← DROP IM LẶNG               |
----------+----------------------+--------------------------------+--------------------------------
t = ∞     | f0 không biết f2     | Bình thường                    | f2 mãi là "phantom leader"
          | tồn tại              |                                | cluster 1-node riêng của f2
          |                      |                                | f0 không gửi HB tới f2
          |                      |                                | f2 không nhận HB → không step down
```

**Hệ quả:**
- f2 hoạt động như leader của cluster 1-node, không được tích hợp vào cluster f0+f1.
- f0 và f1 không biết f2 tồn tại → không gửi block data, không gửi heartbeat tới f2.
- f2 propose block độc lập (nếu có transaction) → split brain tiềm ẩn.

---

## 3. Nguyên nhân gốc

Hai lỗi chồng nhau:

**Lỗi 1 — `Start()` tự elect Leader:**

```go
// node.go:170-172 (trước fix)
if len(rn.Membership.GetAliveMembers()) == 1 {
    rn.becomeLeader()  // ← mọi node mới đều rơi vào đây
}
```

Node mới membership view chỉ có self (1 member) → điều kiện luôn đúng → luôn self-elect.

**Lỗi 2 — Join request bị drop khi connect vào Follower:**

```go
// membership.go:38-40
if !rn.IsLeader() {
    rn.Logger.Printf("[%s] Not leader, ignoring join request", ...)
    return  // ← drop im lặng, không forward về leader
}
```

`findAnyNodeAddress()` trong orchestrator duyệt map không có thứ tự → có thể trả về địa chỉ Follower → join bị mất.

---

## 4. Fix

### 4.1 Loại bỏ auto-leader trong `Start()`

```go
// node.go — Start() sau fix: chỉ khởi động goroutines
func (rn *RaftNode) Start() {
    rn.Logger.Printf("[%s] Starting node", ...)
    rn.Transport.SetDeliverStreamHandler(...)
    rn.Transport.SetEndorsementStreamHandler(...)
    rn.Transport.SetSyncStreamHandler(...)
    go rn.processMessages()
    go rn.monitorHeartbeat()
    // ← KHÔNG còn becomeLeader() ở đây
}
```

### 4.2 Tách thành hai path rõ ràng

```go
// Path 1: Node đầu tiên
node.BootstrapAsLeader()  // → becomeLeader()

// Path 2: Join cluster
node.JoinCluster(bootstrapAddr)  // → query leader → join leader trực tiếp
```

### 4.3 `JoinCluster` query leader trước khi join

```
JoinCluster(bootstrapAddr):
  1. Connect tới bootstrap peer (bất kỳ — Follower hay Leader)
  2. Gửi MsgMembershipRequest
  3. Nhận MsgMembershipResponse: chứa leader_id + members[]
  4. Nếu leader_id rỗng → retry (cluster đang bầu chọn)
  5. Nếu leader khác bootstrap → nạp addresses vào peerstore
  6. Gửi MsgMembershipUpdate (Proposal) trực tiếp tới leader
```

`MsgMembershipRequest` / `MsgMembershipResponse` được xử lý bởi bất kỳ node nào (không yêu cầu leader), field `leader_id` đã có sẵn trong response.

### 4.4 Cập nhật orchestrator và CLI server

| Caller | Trước fix | Sau fix |
|---|---|---|
| `orchestrator.CreateNetwork` | `spawnNode` (Start auto-elects) | `spawnNode` → `BootstrapAsLeader()` |
| `orchestrator.AddNode` | `spawnNode` + `ConnectToPeer(any)` | `spawnNode` + `JoinCluster(any)` |
| `cmd/server` — first node | `Start()` (auto-elects) | `Start()` + `BootstrapAsLeader()` |
| `cmd/server` — joining node | `Start()` + `ConnectToPeer(addr)` | `Start()` + `JoinCluster(addr)` |

---

## 5. Luồng sự kiện sau fix

```
Thời gian | f0 (Leader)          | f1 (Follower)                  | f2 (mới thêm)
----------+----------------------+--------------------------------+--------------------------------
t = 0ms   |                      |                                | [Start()] — state = Follower
          |                      |                                | KHÔNG tự elect
t = 300ms |                      |                                | [JoinCluster(f1.addr)]
          |                      |                                | Transport.Connect(f1)
          |                      |                                | → Gửi MsgMembershipRequest
          |                      |  Nhận MsgMembershipRequest     |
          |                      |  handleMembershipRequest()     |
          |                      |  → trả MsgMembershipResponse   |
          |                      |    leader_id = f0.ID           |
          |                      |    members = [f0, f1]          |
          |                      |  ← response với leader = f0    |
          |                      |                                | Nhận response
          |                      |                                | leader_id = f0
          |                      |                                | Nạp f0 addresses vào peerstore
          |                      |                                | requestMembershipJoin(f0)
t ≈ 300ms | Nhận MsgMembership   |                                |
          | Update (Proposal f2) |                                |
          | handleMembership     |                                |
          | Update():            |                                |
          |  AddMember(f2)       |                                |
          |  MarkAlive(f2)       |                                |
          |  broadcastMembership |                                |
          |  → gửi MsgMembership |  Nhận broadcast membership    |
          |    Ack về f2         |  update từ f0                 | Nhận MsgMembershipAck
          |                      |  updateMembershipFromData()    | handleMembershipAck():
          |                      |  Membership: [f0, f1, f2]     |  updateMembership()
          |                      |                                |  currentLeaderID = f0
          |                      |                                |  state = Follower
          |                      |                                |  StartSync("first-join")
----------+----------------------+--------------------------------+--------------------------------
t ≥ 2s    | Gửi HB tới f1, f2   | Nhận HB từ f0                 | Nhận HB từ f0
          | Membership: 3 nodes  | Membership: 3 nodes           | Membership: 3 nodes
```

---

## 6. Trạng thái cuối

| Node | State | currentLeaderID | Membership |
|---|---|---|---|
| f0 | Leader | f0 | [f0, f1, f2] alive |
| f1 | Follower | f0 | [f0, f1, f2] alive |
| **f2** | **Follower** | **f0** | **[f0, f1, f2] alive** |

f2 **không bao giờ in** `*** I AM NOW THE LEADER ***`. Cluster nhất quán ngay sau khi join.

---

## 7. Files thay đổi

| File | Thay đổi |
|---|---|
| [source/internal/raft/node.go](../../source/internal/raft/node.go) | Xóa `becomeLeader()` khỏi `Start()`; thêm `BootstrapAsLeader()`, `JoinCluster()`, `loadLeaderAddrs()`, field `MembershipResponseChan` |
| [source/internal/raft/consensus.go](../../source/internal/raft/consensus.go) | Thêm `case MsgMembershipResponse` → forward vào `MembershipResponseChan` |
| [source/internal/orchestrator/manager.go](../../source/internal/orchestrator/manager.go) | `CreateNetwork` → `BootstrapAsLeader()`; `AddNode` → `JoinCluster()` |
| [source/cmd/server/main.go](../../source/cmd/server/main.go) | Startup và `connect` command dùng `BootstrapAsLeader`/`JoinCluster` |
