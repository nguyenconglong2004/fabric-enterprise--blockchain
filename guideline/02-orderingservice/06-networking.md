# Ordering Service — Networking (libp2p)

> Mã nguồn: `orderingservice/source/internal/network/transport.go`, `protocol.go`, `internal/api/server.go`

## 1. Tầng vận chuyển (`transport.go`)

Bọc một **libp2p host**. Các thao tác chính:
- `SendMessage(peerID, msg)`: mở stream, mã hóa JSON message, đóng stream.
- `BroadcastMessage(msg, members, failureHandler)`: gửi tới từng thành viên **bất đồng bộ**, có callback xử lý lỗi (dùng để đánh dấu peer chết).
- `NewStream()`: tạo encoder/decoder JSON trên stream.

Các handler stream đăng ký:
- Consensus chung: `SetStreamHandler(ProtocolID)`.
- Deliver: `SetDeliverStreamHandler()`.
- Endorsement (nhận tx): `SetEndorsementStreamHandler()`.
- Sync: `SetSyncStreamHandler()`.

## 2. Protocol ID & message (`protocol.go`, `types/message.go`)

| Protocol ID | Mục đích |
|-------------|----------|
| `/raft-order-service/1.0.0` | Message consensus (heartbeat, bầu cử, membership) |
| `/raft-order-service/deliver/1.0.0` | Giao block xuống Committing Peer |
| `/raft-order-service/endorsement/1.0.0` | Nhận tx từ Core Service |
| `/raft-order-service/sync/1.0.0` | Đồng bộ block/log giữa các orderer |

Các loại message (`types/message.go`):
```
MsgHeartbeat, MsgHeartbeatResponse           ← nhịp tim
MsgIAmNewLeader, MsgLeaderClaimAck           ← bầu Leader
MsgMembershipUpdate/Ack/Request/Response     ← quản lý thành viên
MsgTxRequest, MsgTxResponse                  ← giao dịch
MsgBlockProposal, MsgBlockProposalAck,
MsgBlockCommit                               ← nhân bản & commit block
MsgSyncStatusRequest, MsgSyncStatusResponse  ← đồng bộ
```

## 3. Tối ưu hot-path trên mạng

Hai tối ưu quan trọng cho throughput cao:

**OPT-3 — Stream endorsement bền:** Thay vì mở một stream mới cho **mỗi** giao dịch (gây ~10.000 lần mở stream/giây ở 5000 TPS), `HandleEndorsementStream()` đọc nhiều tx trong một vòng lặp `for { decoder.Decode(&tx) }` trên **một stream giữ mở**. Loadgen workers cũng giữ một stream/worker và tái dùng. Đây là khác biệt lớn nhất giúp vượt nghẽn cổ chai mở stream.

**OPT-5 — Bỏ qua kênh chung cho hot-path:** `MsgBlockProposalAck` đi thẳng vào `BlockAckChan` riêng; `MsgTxRequest` xử lý ngay trên goroutine của stream — tránh dồn mọi message qua một hàng đợi chung gây tranh chấp.

> **Lưu ý hiện trạng:** chưa có kết nối bền cho **mọi** loại message (OPT-3 mới làm cho endorsement). Một số đường vẫn "một message = một stream". Hướng cải thiện ở [cai-thien/01-cai-thien-toc-do.md](../cai-thien/01-cai-thien-toc-do.md).

## 4. HTTP API (`internal/api/server.go`)

Ngoài P2P, orderer mở một HTTP server (cổng riêng) cho tra cứu/giám sát nhẹ:

| Route | Mục đích |
|-------|----------|
| `GET /api/leader` | Trả PeerID + địa chỉ Leader hiện tại |
| `GET /api/membership` | Danh sách thành viên sống + Leader |
| `POST /api/submit-tx` | Nhận giao dịch smart-contract (kèm endorsement) |

➡️ Tiếp: [07-loadgen-va-client.md](07-loadgen-va-client.md)
