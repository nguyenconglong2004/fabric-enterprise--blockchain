# Ordering Service — Tổng quan Raft trong dự án

> Mã nguồn: `orderingservice/source/internal/raft/`

## 1. Nhắc lại Raft chuẩn

[Raft](https://raft.github.io/) chia bài toán đồng thuận thành ba mảnh:
1. **Bầu lãnh đạo (Leader Election):** chọn một node làm Leader.
2. **Nhân bản log (Log Replication):** Leader ghi log rồi sao chép cho Follower.
3. **An toàn (Safety):** đảm bảo không có hai Leader hợp lệ cùng term, log không bị "rẽ nhánh".

Mỗi node ở một trong ba trạng thái: **Follower** (theo sau), **Candidate** (ứng cử), **Leader** (lãnh đạo). Thời gian chia thành các **term** (nhiệm kỳ), mỗi term có tối đa một Leader.

Đọc bài báo gốc (rất dễ hiểu): [In Search of an Understandable Consensus Algorithm](https://raft.github.io/raft.pdf). Mô phỏng: [thesecretlivesofdata.com/raft](https://thesecretlivesofdata.com/raft/).

## 2. Raft của dự án khác Raft chuẩn ở đâu?

Dự án giữ tinh thần Raft nhưng thay đổi cơ chế bầu cử cho **xác định và đơn giản hơn**:

| Khía cạnh | Raft chuẩn | Dự án này |
|-----------|------------|-----------|
| Bầu Leader | Timeout **ngẫu nhiên**, ai hết giờ trước thì ứng cử & xin phiếu | **Theo độ ưu tiên (priority)**: node tham gia sớm hơn = ưu tiên cao hơn = được chọn xác định |
| Trạng thái | Follower / Candidate / Leader | Follower / **ClaimingLeader** / Leader / Syncing |
| Phiếu bầu | Mỗi node bầu cho một candidate | Follower **ACK** lời tuyên bố "Tôi là Leader mới" của node ưu tiên cao nhất |
| Nội dung log | Lệnh tùy ý | Mỗi log entry chứa **một block** |
| Lưu bền (persistence) | Ghi đĩa (term, log) | **Toàn bộ trong RAM** (không ghi đĩa) — đánh đổi: nhanh nhưng mất khi crash |
| Thư viện | etcd/raft, hashicorp/raft | **Tự viết hoàn toàn** |

### Vì sao bầu theo độ ưu tiên?
- **Tránh split vote** (chia phiếu): trong Raft chuẩn, nhiều node có thể cùng ứng cử và chia phiếu, phải bầu lại. Priority-based loại bỏ điều này vì luôn có một node "đáng làm Leader nhất" được xác định trước.
- **Hội tụ nhanh, dễ suy luận:** ai cũng biết trước thứ tự ưu tiên → ít bất định.

Đánh đổi: thiết kế này thiên về **tính đơn giản & tốc độ hội tụ** hơn là tổng quát hóa. Các điểm cần lưu ý về an toàn được liệt kê trong `docs/leader-election-analysis.md` (và phần [cai-thien/](../cai-thien/README.md)).

## 3. Các trạng thái node (`raft/node.go`)

```
        ┌──────────┐  hết heartbeat timeout & mình ưu tiên cao nhất còn sống
        │ Follower │ ───────────────────────────────────────────┐
        └────┬─────┘                                             ▼
             │ nhận heartbeat hợp lệ                    ┌─────────────────┐
             │◀─────────────────────────────────────── │ ClaimingLeader  │
             │                                          │ (gửi "I AM NEW  │
             │ phát hiện tụt lại / gap > 10s            │  LEADER", chờ   │
             ▼                                          │  đa số ACK)     │
        ┌──────────┐                                    └────────┬────────┘
        │ Syncing  │ ◀── kéo block từ peer khác                  │ đủ majority ACK
        └────┬─────┘                                             ▼
             │ xong → Follower                            ┌──────────┐
             └──────────────────────────────────────────▶│  Leader  │
                                                          └──────────┘
```

- **Follower:** trạng thái mặc định, nghe heartbeat từ Leader, nhận & ACK block proposal.
- **ClaimingLeader:** đang tự tuyên bố làm Leader, chờ thu đủ ACK đa số.
- **Leader:** gom giao dịch, cắt block, điều phối commit, gửi heartbeat.
- **Syncing:** đang kéo block bị thiếu từ peer khác (xem [05-deliver-va-dong-bo.md](05-deliver-va-dong-bo.md)).

## 4. Đa số (Quorum) tính trên tổng số thành viên

Một quyết định quan trọng (`raft/leader.go`, `membership.go`):
```go
totalCount := Membership.GetTotalCount()   // cả node sống lẫn chết
majority := totalCount/2 + 1
```
Quorum tính trên **tổng số thành viên** (sống + chết), **không** chỉ trên số node đang sống.

> **Vì sao?** Để **chống split-brain** (não phân ly): nếu mạng chia đôi 50–50, không bên nào đạt quá bán trên tổng → không thể có hai Leader cùng commit ở hai phía. Đây là nguyên tắc an toàn nền tảng của Raft. Tìm hiểu: [split-brain](https://en.wikipedia.org/wiki/Split-brain_(computing)).

## 5. Term (nhiệm kỳ) và log

- `currentTerm`: số nhiệm kỳ, tăng mỗi khi một node tuyên bố làm Leader. Giữ trong RAM.
- `RaftLog` (`types/block.go`): danh sách `LogEntry` append-only, mỗi entry gồm `Index`, `PrevLogIndex`, `Term`, và `Block`.
- `OrderingBlock`: danh sách block đã **commit** (khác với log đã ghi nhưng chưa commit).

```go
type LogEntry struct {
    Index        int64   // vị trí trong log
    PrevLogIndex int64   // index của entry liền trước (để kiểm tra liên tục)
    Term         int64   // term khi tạo
    Type         string  // "BLOCK_PROPOSING"
    Block        Block   // block thực sự
}
```

`PrevLogIndex` cho phép Follower kiểm tra log có **liên tục** không (không bị nhảy cóc) trước khi chấp nhận entry mới — đây là cốt lõi đảm bảo an toàn của Raft.

➡️ Tiếp: [03-bau-lanh-dao-heartbeat.md](03-bau-lanh-dao-heartbeat.md)
