# Ordering Service — Giao block (Deliver) & Đồng bộ (Sync)

> Mã nguồn: `orderingservice/source/internal/raft/deliver.go`, `sync.go`, `sync_server.go`

Hai cơ chế này đảm bảo block đi xuống Committing Peer và các node orderer luôn cùng dữ liệu.

---

## Phần A — Deliver: giao block xuống Committing Peer

### 1. Mô hình "đăng ký & phát tỏa" (subscribe + fan-out)

`DeliverManager` (`deliver.go`) quản lý danh sách **subscriber** (mỗi Committing Peer là một subscriber):
- `subscribe()`: tạo một channel có đệm (cap 64), trả về subscriber ID.
- `NotifyNewBlock(block)`: gửi block tới mọi subscriber. Nếu subscriber chậm (channel đầy), block bị **bỏ qua không chặn** (non-blocking) để không làm nghẽn luồng commit — peer chậm sẽ tự bắt kịp bằng sync.

### 2. Xử lý stream (`HandleDeliverStream()`)

Khi một Committing Peer kết nối qua protocol `/raft-order-service/deliver/1.0.0`:
1. Peer gửi `DeliverRequest{FromIndex}` — "tôi muốn block từ số thứ tự này trở đi".
2. Orderer gửi ngay tất cả block đã commit từ `FromIndex`.
3. Orderer `subscribe()` để nhận block tương lai và stream tiếp khi có block mới commit.
4. Kết nối **giữ mở lâu dài** (long-lived) cho đến khi peer ngắt.

Mỗi block mã hóa JSON, gửi theo dòng. Đây là **streaming push** — peer không phải hỏi liên tục.

---

## Phần B — Sync: đồng bộ khi node tụt lại

Khi một orderer mới tham gia, hoặc vừa hồi sinh sau khi mất kết nối, nó có thể thiếu nhiều block. `sync.go` lo việc "đuổi kịp" (catch-up) theo mô hình **pull (kéo)** — follower chủ động kéo, không phải Leader đẩy.

### 1. Khi nào kích hoạt sync?
- Lần đầu join mà chưa có block.
- `HandleBlockCommit`: nhận commit cho entry không có trong log → biết mình đã miss.
- `handleHeartbeat`: khoảng cách heartbeat > 10s → có thể tụt xa.
- Khi bị buộc thoái vị do heartbeat "ôi".

### 2. Bốn pha của sync

**Pha 1 — Khám phá (Discovery, ~2s):**
- Phát `MsgSyncStatusRequest` kèm `commitIndex` của mình.
- Thu `MsgSyncStatusResponse` từ các peer: mỗi phản hồi gồm `commitIndex`, `commitHash`, `logLastIndex`, `leaderID`, phiên bản membership.

**Pha 2 — Chọn nguồn (`pickSyncTarget()`):**
- Nhóm các phản hồi theo cặp `(commitIndex, commitHash)`.
- Chọn nhóm có **nhiều phiếu nhất** (đồng thuận mạnh nhất). Hòa thì ưu tiên `commitIndex` cao hơn.
- Ưu tiên Leader trong nhóm thắng làm nguồn tải.

> Việc bỏ phiếu theo `(commitIndex, commitHash)` đảm bảo node sync từ **trạng thái đa số đồng ý**, không bị một node lỗi đánh lừa kéo về dữ liệu sai.

**Pha 3 — Tải block song song (`fetchBlocksParallel()`):**
- Chia khoảng `[from..to]` thành các **shard** (mỗi shard 64 block — `SyncShardSize`).
- Tải các shard **song song** từ nhiều nguồn (round-robin cân tải), thất bại thì thử nguồn kế.

**Pha 4 — Xác minh & cài đặt:**
- `verifyHashChain()`: kiểm tra mỗi block có `PrevHash` khớp `Hash` block trước; block cuối khớp `commitHash` mục tiêu. Sai → sync thất bại, thử lại.
- Append block vào `OrderingBlock`, cài log entry, cập nhật `lastCommittedHash` và phiên bản membership.
- Chuyển trạng thái Syncing → Follower, reset `lastHeartbeat`.

### 3. Phía phục vụ sync (`sync_server.go`)
- `HandleSyncStream()`: nhận yêu cầu, stream block theo từng chunk (`streamBlocks()`).
- **An toàn:** node đang tự sync sẽ **từ chối phục vụ** để không lan truyền dữ liệu cũ.

---

## 3. Điểm cần lưu ý về an toàn (theo `docs`)

Tài liệu nội bộ ghi nhận một số rủi ro của thiết kế hiện tại:
- **SYNC-1:** Follower có thể bỏ lỡ thông điệp commit và tụt lại âm thầm.
- **SYNC-5:** Hash-chain **chỉ** được xác minh trong lúc sync, **không** trên hot-path commit.
- **SYNC-4:** Không lưu bền → crash mất sạch trạng thái.

Các đề xuất khắc phục xem [cai-thien/](../cai-thien/README.md).

➡️ Tiếp: [06-networking.md](06-networking.md)
