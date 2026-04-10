# MembershipView trong Ordering Service

Tài liệu này mô tả **ý nghĩa của `MembershipView`**, cách các node **quản lý/sử dụng**, và **khi nào – cập nhật như thế nào** trong ordering service.

---

## 1) `MembershipView` là gì?

`MembershipView` là **trạng thái membership cục bộ** mà *mỗi node* duy trì để biết:

- **Những node nào thuộc cluster** (kể cả đang “alive” hay đã “dead”).
- **Priority (thứ tự ưu tiên)** dùng cho cơ chế bầu leader theo “join sớm ưu tiên cao”.
- **Version** để phản ánh membership đã thay đổi bao nhiêu lần.

Định nghĩa: `internal/types/membership.go`

- `MembershipView.Members`: map `peer.ID -> *MemberInfo`
- `MembershipView.Version`: tăng khi có thay đổi (add/mark dead/mark alive)

---

## 2) Dữ liệu trong `MembershipView`

### 2.1. `MemberInfo`

File: `internal/types/membership.go`

- **`PeerID`**: định danh node (libp2p `peer.ID`)
- **`JoinTime`**: thời điểm join cluster
- **`Priority`**: số càng nhỏ => priority càng cao (join càng sớm)
- **`IsAlive`**: trạng thái sống/chết theo góc nhìn membership

### 2.2. Cách gán `Priority` (join order)

Khi `AddMember(peerID, joinTime)`:

- Nếu member chưa tồn tại: `Priority = len(Members)` (theo thứ tự join)
- `IsAlive = true`
- `Version++`

Hệ quả:

- Priority là “thứ tự gia nhập” (không tái tính lại khi có node chết).
- Leader election sẽ chọn **node alive có `Priority` nhỏ nhất**.

### 2.3. `Version`

`Version` tăng khi:

- `AddMember(...)`
- `MarkDead(peerID)`
- `MarkAlive(peerID)`

File: `internal/types/membership.go`

> Lưu ý: hiện tại `Version` chủ yếu được **đính kèm trong payload membership** để đồng bộ trạng thái; code chưa triển khai cơ chế “quorum validate version” trước khi failover (mới dừng ở mức thiết kế/đề xuất trong `docs/RISKS_ANALYSIS.md`).

---

## 3) Node quản lý `MembershipView` như thế nào?

### 3.1. Khởi tạo local view

File: `internal/raft/node.go`

- Khi tạo `RaftNode`: `Membership = types.NewMembershipView()`
- Node **tự thêm chính mình** vào membership:
  - `Membership.AddMember(selfID, joinTime)`

### 3.2. Hai hướng đồng bộ membership

Trong code hiện tại, membership được đồng bộ theo hai luồng chính:

- **Luồng A – Membership broadcast từ leader**:
  - Leader broadcast full snapshot membership bằng `MsgMembershipUpdate` (payload có trường `"members"`).
  - Followers áp dụng bằng `updateMembershipFromData(...)`.

- **Luồng B – HeartbeatResponse (resync cho stale leader)**:
  - Khi follower nhận heartbeat “stale term”, follower phản hồi `MsgHeartbeatResponse` kèm `MembershipData`.
  - Stale leader step down, resync membership, và “rejoin” leader mới để được mark alive trở lại.

---

## 4) Node sử dụng `MembershipView` ở đâu?

### 4.1. Chọn leader theo priority

File: `internal/raft/leader.go`

Khi phát hiện leader chết (timeout), node gọi `selectNewLeader()`:

- `Membership.MarkDead(oldLeaderID)` (local)
- `GetHighestPriorityAliveNode()` để xác định node có quyền claim leader

### 4.2. Tính majority khi claim leader (tránh split-brain)

File: `internal/raft/leader.go`

Khi node có priority cao nhất claim leader, majority được tính theo:

- `totalCount := Membership.GetTotalCount()` (**alive + dead**)
- `majority := totalCount/2 + 1`

Mục tiêu: giảm nguy cơ một partition nhỏ tự bầu leader nếu cluster bị chia cắt.

### 4.3. Quyết định broadcast tới “alive” hay “all known”

File: `internal/raft/leader.go`, `internal/raft/membership.go`, `internal/raft/node.go`

- **Membership broadcast** (`broadcastMembershipView`): gửi tới `GetAliveMembers()`
- **Leader claim** (`MsgIAmNewLeader`): broadcast tới `GetAllMembers()` để stale-but-alive leader vẫn nhận được claim và step down (tránh 2 leader song song sau partition).

---

## 5) Khi nào `MembershipView` được update? Update như thế nào?

Phần này liệt kê các “trigger” cập nhật membership trong hệ thống.

### (1) Khởi động node (local-init)

File: `internal/raft/node.go`

- **Khi**: `NewRaftNode(...)`
- **Update**: `Membership.AddMember(self, joinTime)` => thêm self, `Version++`

### (2) Node mới join cluster (leader xử lý join request)

Files: `internal/raft/node.go`, `internal/raft/membership.go`, `internal/types/message.go`

**Bên node join (B)**:

- **Khi**: `ConnectToPeer(...)`
- Gửi join request bằng `MsgMembershipUpdate` nhưng payload là `MembershipProposal`:
  - `{ PeerID, JoinTime, Version }`

**Bên leader (A)**: `handleMembershipUpdate(...)`

- **Update**:
  - `Membership.AddMember(B)`
  - `Membership.MarkAlive(B)` (phục hồi nếu trước đó bị mark dead)
- **Sync**:
  - `broadcastMembershipView()` tới các node khác
  - gửi `MsgMembershipAck` về B kèm full snapshot (`serializeMembershipView()`)

### (3) Leader broadcast membership (đồng bộ snapshot)

File: `internal/raft/membership.go`

- **Khi**: leader gọi `broadcastMembershipView()`
- **Payload**: `serializeMembershipView()` tạo map:
  - `"members"`: danh sách member + địa chỉ
  - `"version"`: `Membership.Version`

### (4) Follower áp dụng broadcast membership (apply snapshot)

File: `internal/raft/membership.go`

Follower nhận `MsgMembershipUpdate` có trường `"members"` sẽ coi là broadcast snapshot từ leader và:

- **Chặn stale broadcast theo term**: chỉ apply nếu `msg.Term >= currentTerm`
- **Apply**: `updateMembershipFromData(msg.Data)`
  - Ghi đè `Membership.Members[...]`
  - Cập nhật `Membership.Version`
  - Lưu địa chỉ vào `peerstore` và kết nối nền để đảm bảo có route tới peers

### (5) Leader mark-dead follower khi gửi thất bại

Files: `internal/raft/heartbeat.go`, `internal/raft/leader.go`

- **Khi**: leader gửi heartbeat tới follower mà `SendMessage` lỗi
- **Update**:
  - `Membership.MarkDead(follower)`
  - `broadcastMembershipView()` để đồng bộ thay đổi

### (6) Follower mark-dead leader cũ khi heartbeat timeout (local)

Files: `internal/raft/heartbeat.go`, `internal/raft/leader.go`

- **Khi**: `checkHeartbeat()` thấy quá `HeartbeatTimeout`
- **Update**:
  - `selectNewLeader()` sẽ `Membership.MarkDead(oldLeaderID)` (local) rồi chọn highest priority alive

> Đây là cập nhật *cục bộ*, nên trong network partition, các node có thể mark-dead khác nhau cho đến khi có cơ chế resync.

### (7) Stale leader resync membership qua `MsgHeartbeatResponse`

Files: `internal/raft/heartbeat.go`, `internal/types/message.go`

**Follower nhận heartbeat stale term**:

- **Khi**: `handleHeartbeat()` thấy `msg.Term < currentTerm`
- **Hành vi**: gửi `MsgHeartbeatResponse` về sender, kèm:
  - `CurrentTerm`, `CurrentLeaderID`, và `MembershipData = serializeMembershipView()`

**Stale leader nhận HeartbeatResponse**:

- Nếu `resp.CurrentTerm > curTerm`:
  - step down về `Follower`
  - cập nhật `currentTerm/currentLeaderID`
  - `updateMembershipFromData(resp.MembershipData)` để resync membership
  - `Membership.MarkAlive(self)` (phục hồi local)
  - gửi join request tới leader mới (`requestMembershipJoin(leaderID)`) để leader **MarkAlive** mình trong view toàn cluster và resume heartbeat tới mình

### (8) Client request membership (read-only)

Files: `internal/raft/membership.go`, `pkg/client/client.go`

- Client gửi `MsgMembershipRequest`
- Node trả `MsgMembershipResponse` với `serializeMembershipView()`
- Đây là **đọc** membership, không làm thay đổi view.

---

## 6) `serializeMembershipView()` và `updateMembershipFromData()` (chi tiết payload)

File: `internal/raft/membership.go`

### 6.1. Serialize

`serializeMembershipView()` đóng gói:

- `members[]`: mỗi member gồm:
  - `peer_id`, `join_time`, `priority`, `is_alive`, `addresses[]`
- `version`

### 6.2. Apply

`updateMembershipFromData(data)`:

- Parse danh sách member + version
- Cập nhật `Membership.Members` theo snapshot (ghi đè)
- Cập nhật `Membership.Version`
- Đưa address vào `peerstore` và kết nối nền tới peers (best-effort)

---

## 7) Tóm tắt nhanh (TL;DR)

- **`MembershipView`** là state cục bộ về **thành viên cluster + priority + alive/dead + version**.
- **Priority** được gán theo **thứ tự join**; leader là **alive node có priority nhỏ nhất**.
- Membership được đồng bộ chủ yếu bằng:
  - **Leader broadcast snapshot** (`MsgMembershipUpdate` có `"members"`)
  - **HeartbeatResponse** để stale leader step down + resync + rejoin
- Membership được update khi:
  - node join (`AddMember/MarkAlive`), follower/leader bị coi là chết (`MarkDead`), hoặc stale leader quay lại (resync + rejoin).

---

## 8) Tài liệu liên quan

- `docs/LEADER_ELECTION.md`: mô tả tổng quan leader election + vai trò membership trong failover
- `docs/LEADER_FAILOVER_FLOW.md`: flow thay thế leader
- `docs/RISKS_ANALYSIS.md`: rủi ro membership desync/stale và đề xuất giảm thiểu

