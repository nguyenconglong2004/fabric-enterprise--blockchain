# Ordering Service — Bầu lãnh đạo & Heartbeat

> Mã nguồn: `orderingservice/source/internal/raft/leader.go`, `heartbeat.go`, `membership.go`
> Tài liệu nội bộ: `docs/leader-election-analysis.md`, `docs/heartbeat.md`

## 1. Độ ưu tiên = thứ tự tham gia (`membership.go`)

Mỗi thành viên cluster có một **priority** (số nguyên), gán theo **thứ tự tham gia**: tham gia càng sớm, số priority càng nhỏ, càng được ưu tiên làm Leader.

```go
type MemberInfo struct {
    PeerID   peer.ID
    JoinTime time.Time
    Priority int        // nhỏ hơn = ưu tiên cao hơn
    IsAlive  bool
}
type MembershipView struct {
    Members map[peer.ID]*MemberInfo
    Version int64        // tăng mỗi lần thay đổi
}
```

Hàm `GetHighestPriorityAliveNode()` trả về node sống có priority nhỏ nhất — chính là ứng viên Leader hợp lệ.

## 2. Các hằng số thời gian (`network/protocol.go`)

| Hằng số | Giá trị | Ý nghĩa |
|---------|---------|---------|
| `HeartbeatInterval` | 2s | Leader gửi heartbeat nếu 2s qua chưa gửi block nào |
| `HeartbeatTimeout` | 5s | Follower quá 5s không nghe Leader → khởi động bầu cử |
| `DetectionTimeout` | 3s | Ngưỡng phát hiện (ticker giám sát chạy mỗi 1s) |
| Cửa sổ chờ ACK | 10s (2×HBT) | Thời gian thu ACK đa số khi tuyên bố Leader |
| Hạn Leader kỳ vọng | 15s (3×HBT) | Follower chờ node ưu tiên cao nhất ra tuyên bố |

> Lưu ý: block proposal/commit cũng đóng vai trò heartbeat — khi cluster đang bận cắt block, không cần gửi heartbeat riêng (tiết kiệm mạng).

## 3. Vòng giám sát heartbeat (`heartbeat.go`)

`monitorHeartbeat()` chạy một ticker mỗi 1 giây, gọi `checkHeartbeat()` tùy trạng thái:

- **Leader:** nếu `time.Since(lastBlockSentTime) >= 2s` → gửi `MsgHeartbeat` (kèm term hiện tại) song song tới mọi thành viên sống. Nếu gửi tới một peer thất bại → đánh dấu peer đó chết, phát lại membership cập nhật.
- **Follower:** nếu `time.Since(lastHeartbeat) > 5s` → gọi `selectNewLeader()` (bắt đầu bầu cử). Nếu khoảng cách > 10s → kích hoạt **đồng bộ** (`StartSync("rejoin-after-disconnect")`) vì có thể đã tụt lại nhiều.
- **ClaimingLeader:** bỏ qua kiểm tra (tránh kích hoạt bầu cử chồng chéo).

## 4. Quy trình bầu Leader (`leader.go`)

Khi Follower phát hiện Leader mất:

### Bước 1 — `selectNewLeader()`
- Đánh dấu Leader cũ là chết.
- Chọn node **ưu tiên cao nhất còn sống**.
- Nếu **chính mình** là node đó → chuyển sang `ClaimingLeader`, gọi tiếp bước 2. Nếu không → chờ node kia ra tuyên bố (trong hạn 15s).

### Bước 2 — `sendIAmNewLeaderAndWaitForAcks()`
- **Tăng term** (`currentTerm++`).
- Phát `MsgIAmNewLeader` (kèm term mới) tới mọi thành viên.
- Chờ tối đa 10s thu các `MsgLeaderClaimAck`.

### Bước 3 — Follower xét lời tuyên bố
Follower trả **YES/NO** dựa trên ba điều kiện:
1. Người tuyên bố có đúng là node ưu tiên cao nhất còn sống không?
2. `term >= currentTerm` của Follower không?
3. Leader hiện tại có thực sự đã "ôi" (stale) không?

### Bước 4 — `finishClaim()`
- Nếu thu được **≥ majority** ACK YES → trở thành **Leader**, bắt đầu vòng cắt block.
- Nếu không → quay lại **Follower**.

```
Follower phát hiện Leader chết
        │
        ▼
selectNewLeader()  ── mình là ưu tiên cao nhất? ──┐ Không → chờ node kia (≤15s)
        │ Có                                       │
        ▼                                          ▼
ClaimingLeader: term++, broadcast "I AM NEW LEADER"
        │
        ▼
thu ACK ── đủ majority YES? ──┐ Không → về Follower
        │ Có                  
        ▼
   trở thành Leader
```

## 5. Khi nhận heartbeat (`handleHeartbeat()`)

Follower khi nhận `MsgHeartbeat`:
- Nếu `msg.Term < currentTerm` (heartbeat từ Leader cũ): trả `HeartbeatResponse` để Leader cũ tự thoái vị (step down).
- Nếu khoảng cách từ heartbeat trước > 10s: kích hoạt đồng bộ (đã tụt quá xa).
- Ngược lại: cập nhật `lastHeartbeat`, `currentLeaderID`, `currentTerm`; xóa `expectedLeaderID` khi đã xác nhận Leader hợp lệ.

## 6. Tại sao thiết kế này hiệu quả?

- **Hội tụ nhanh:** không có vòng chia phiếu lặp lại như timeout ngẫu nhiên.
- **Xác định:** ai cũng tính được Leader kế tiếp → dễ debug, dễ suy luận.
- **Chống split-brain:** vẫn yêu cầu majority ACK trên tổng thành viên (xem [02-raft-tong-quan.md](02-raft-tong-quan.md) §4).

**Điểm cần lưu ý (theo `docs/leader-election-analysis.md`):** các tình huống biên như "phantom leader khi join", "Leader nhường ghế cho node ưu tiên cao hơn vừa hồi sinh" được mô tả trong `docs/scenarios/`. Vì không lưu bền term/log xuống đĩa, một số kịch bản crash có rủi ro mất an toàn — xem [cai-thien/03-cai-thien-bao-mat.md](../cai-thien/03-cai-thien-bao-mat.md) và [cai-thien/02-cai-thien-luu-tru.md](../cai-thien/02-cai-thien-luu-tru.md).

➡️ Tiếp: [04-nhan-ban-log-va-cat-block.md](04-nhan-ban-log-va-cat-block.md)
