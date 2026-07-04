# Core Service — Mạng libp2p & Discovery

> Mã nguồn: `coreservice/internal/network/`, `coreservice/internal/discovery/`

## 1. Tầng vận chuyển libp2p (`network/transport.go`)

Core Service dựng một **libp2p host** lắng nghe `/ip4/0.0.0.0/tcp/0` (cổng do HĐH cấp). Mọi giao tiếp với orderer và committing peer đều qua **stream** libp2p, phân loại bằng **Protocol ID**:

| Protocol ID | Hướng | Mục đích |
|-------------|-------|----------|
| `/raft-order-service/1.0.0` | Core ↔ Orderer | Hỏi membership & leader |
| `/raft-order-service/endorsement/1.0.0` | Core → Orderer | Gửi giao dịch đã endorse |
| `/fabric-enterprise/commit-peer/tx-sign/1.0.0` | Core ↔ Committing Peer | Xin chữ ký endorsement |

> **Stream là gì?** Một kênh dữ liệu hai chiều mở trên kết nối libp2p. Nhiều stream chạy song song trên cùng một kết nối TCP nhờ **multiplexing** ([yamux](https://github.com/libp2p/go-yamux)) — giống như nhiều "làn" trên một con đường.

## 2. Connection pool ấm tới Committing Peer (`commit_peer_sign_pool.go`)

Mở kết nối mới mỗi lần ký rất tốn (bắt tay TCP + libp2p). Giải pháp: **giữ sẵn một kết nối ấm (warm connection)** tới Committing Peer.

- Bật/tắt bằng `CORE_SIGN_POOL` (mặc định bật).
- Mỗi lần ký chỉ **mở một stream mới** trên kết nối sẵn có (rẻ hơn nhiều so với mở kết nối).
- Có cơ chế thử lại: 2 lần, tự kết nối lại nếu hỏng.
- Hạn chờ: `CORE_SIGN_TIMEOUT` (mặc định 15s).

Đây là tối ưu quan trọng để đạt throughput cao — tránh "nghẽn cổ chai" ở việc bắt tay kết nối.

## 3. Discovery — tìm Leader của cluster Raft (`discovery/discovery.go`)

Ordering Service có nhiều node và Leader có thể đổi (khi Leader cũ chết). Core Service cần luôn biết **ai đang là Leader** để gửi giao dịch đúng chỗ. Đó là việc của **Discovery Client**.

Cách hoạt động:
1. Khởi tạo với danh sách **bootstrap** (vài multiaddr orderer đã biết — phân tách bằng dấu phẩy, `bootstraps.go` lo việc parse + khử trùng lặp).
2. `Refresh(ctx)`: thử từng bootstrap cho đến khi một node trả lời **membership view** (danh sách thành viên + ai là Leader). Kết quả được **cache** (TTL mặc định ~8 giây).
3. `Snapshot(ctx)`: trả cache nếu còn hạn, nếu hết hạn thì refresh.
4. `Invalidate()`: khi phát hiện Leader cũ lỗi → xóa cache nóng, tạm dùng bản tốt cuối (`lastGood`) trong cửa sổ failover ~2 phút.

Cấu trúc membership nhận về:
```go
type MembershipView struct {
    LeaderID string         // PeerID của Leader hiện tại
    Members  []MemberInfo   // mỗi member: ID, Addresses[], Alive
}
```

## 4. Gửi endorsement có dự phòng (`discovery/endorse.go`)

`SendEndorsement` định tuyến giao dịch:
- Mặc định: gửi thẳng tới **Leader**.
- Nếu `CORE_ENDORSE_FALLBACK=1`: khi Leader lỗi, thử lần lượt các Follower còn sống (phòng trường hợp đang đổi Leader).

Cơ chế cache + failover này giúp Core Service **chịu được việc Leader đổi** mà không cần khởi động lại — một yêu cầu cốt lõi của hệ thống chịu lỗi.

## 5. Lấy metrics từ Committing Peer (`metrics/commitpeer/client.go`)

Để biết "sự thật mặt đất" (ground truth) về thời điểm block được commit (phục vụ đo E2E latency), Core Service gọi HTTP tới API metrics của Committing Peer (`/metrics/throughput`, `/metrics/benchmark`, `/metrics/commit-lookup`). Địa chỉ cấu hình qua `COMMIT_PEER_METRICS_URL`.

➡️ Tiếp: [05-luu-tru-state.md](05-luu-tru-state.md)
