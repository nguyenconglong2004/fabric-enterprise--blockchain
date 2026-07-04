# Committing Peer — Giao thức nhận block & ký Endorsement

> Mã nguồn: `commitingpeer/source/internal/deliver/`, `internal/discovery/`

## 1. Nhận block từ Ordering Service (`deliver/client.go`)

Committing Peer dựng một **libp2p host** (cổng ngẫu nhiên) và đăng ký nhận block qua protocol `/raft-order-service/deliver/1.0.0`.

Luồng `Subscribe()`:
1. Parse multiaddr orderer → `peer.AddrInfo`.
2. `host.Connect()` → thiết lập kết nối TCP.
3. `host.NewStream(..., DeliverProtocolID)` → mở stream trên kết nối.
4. Gửi `DeliverRequest{FromIndex}` (số block bắt đầu, đánh số từ 1) — "tôi đã có tới block X, gửi tiếp từ X+1".
5. Một goroutine nền lặp `json.NewDecoder(s).Decode(&block)` và đẩy block vào `blockChan` (đệm 64).

> **Tắt mượt khéo léo:** một goroutine con chờ `ctx.Done()` rồi gọi `s.Close()`, làm `Decode()` trả `io.EOF` để vòng lặp thoát sạch. `Subscribe()` trả về một channel `done` đóng khi stream kết thúc → giúp phát hiện mất kết nối để kết nối lại.

## 2. Tự động kết nối lại khi Leader đổi (`peer.go` + `discovery/`)

Leader của cluster orderer có thể đổi. Committing Peer dùng **Discovery** giống Core Service:
- `discovery.go`: cache membership orderer (TTL ~8s), failover sang bản tốt cuối nếu refresh lỗi, `StartRefreshLoop()` làm mới nền.
- `deliverReconnectLoop()` trong `peer.go`: khi stream deliver đứt, khám phá orderer mới và kết nối lại với `fromIndex = lastCommittedBlock + 1` — **không mất block nào**. Dùng **exponential backoff** (1s → tối đa 30s) để tránh dội kết nối.

`bootstraps.go`: parse danh sách multiaddr orderer (phân tách dấu phẩy, khử trùng lặp).

## 3. Lấy membership orderer (`deliver/membership.go`)

Qua protocol `/raft-order-service/1.0.0`:
- Gửi `MembershipRequest` (type 6) → nhận `MembershipResponse` (type 7).
- Phản hồi gồm `LeaderID` và danh sách `MemberInfo{ID, Addresses, Alive, Priority}`.

## 4. Ký Endorsement giúp Core Service (`deliver/sign.go`)

Committing Peer đóng luôn vai **endorser**: nó đăng ký handler cho protocol `/fabric-enterprise/commit-peer/tx-sign/1.0.0`. Khi Core Service gửi một giao dịch (chưa ký) qua stream này:
1. Đọc `Transaction` (JSON).
2. Kiểm tra các endorsement đã có (nếu có).
3. **Ký bằng Ed25519** trên `txid + contractName + payload`.
4. Thêm một `EndorsementEntry` (khóa công khai + chữ ký) vào giao dịch.
5. Gửi lại giao dịch đã ký cho Core Service.

> Đây là lý do Committing Peer cần một cặp khóa Ed25519 (sinh mới hoặc nạp từ `COMMIT_PEER_PRIVATE_KEY` / file `endorsement.key`). Khóa công khai của nó cũng được đưa vào danh sách **endorser tin cậy** (`TRUSTED_ENDORSER_PUBLIC_KEYS`) để bước validate sau này chấp nhận chữ ký của chính nó.

Quan hệ với Core Service được mô tả ở [01-coreservice/03-crypto-endorsement.md](../01-coreservice/03-crypto-endorsement.md).

➡️ Tiếp: [03-validation.md](03-validation.md)
