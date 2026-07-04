# Committing Peer — Metrics & Đo lường

> Mã nguồn: `commitingpeer/source/internal/metrics/`

Committing Peer là nơi giao dịch được **commit thật**, nên nó là nguồn "sự thật mặt đất" (ground truth) tốt nhất để đo throughput và E2E latency.

## 1. Recorder trong RAM (`recorder.go`)

Để đo không bị trễ do PostgreSQL, Committing Peer ghi thời điểm commit **ngay trong bộ nhớ**:
- Map `txid → thời điểm commit`.
- Danh sách block (timestamp, hash, danh sách txid).
- Cửa sổ lưu giữ (retention) mặc định 2 giờ (`COMMIT_PEER_METRICS_RETENTION`, đơn vị giây).
- Thread-safe bằng `sync.RWMutex`.
- Bật/tắt bằng `COMMIT_PEER_RECORD_METRICS` (mặc định bật).

> Vì recorder nằm trong RAM, nó phản ánh thời điểm commit **tức thì**, không lệ thuộc độ trễ ghi PostgreSQL — chính xác hơn khi đo benchmark.

## 2. Truy vấn (`query.go`)

| Truy vấn | Ý nghĩa |
|----------|---------|
| `Window(since, until)` | Đếm tx/block commit trong khoảng |
| `Peak(lookback)` | Bucket 1s dày nhất trong cửa sổ lookback (đỉnh) |
| `Latest` | Cửa sổ gần nhất |
| `Since` | Từ một mốc đến hiện tại |
| `CommitBenchmark` | tx/s bền vững & đỉnh, block/s, trung bình tx/block |
| `ComputeE2E(submits)` | Ghép thời điểm submit (từ Core) với commit → phân vị latency p50/p95/p99 |

## 3. HTTP endpoints (`server.go`)

Server tại `:8081` (đổi qua `COMMIT_PEER_METRICS_ADDR`, đặt `0` để tắt):

| Endpoint | Method | Mục đích |
|----------|--------|----------|
| `/metrics/throughput` | GET | tx/s, block/s theo mode `window`/`peak`/`latest`/`since`; lọc `tx_prefix` |
| `/metrics/benchmark` | GET | bền vững vs đỉnh, trung bình tx/block |
| `/metrics/commit-lookup` | POST | tra thời điểm commit của danh sách txid (body `{"txids":[...]}`) |
| `/health` | GET | kiểm tra sống |

Core Service gọi các endpoint này (qua `metrics/commitpeer/client.go`) để tổng hợp benchmark E2E — xem [06-benchmark-hieu-nang/](../06-benchmark-hieu-nang/01-benchmark-metrics.md).

## 4. Vì sao đo ở Committing Peer?

- **Submit** đo ở Core (`tx_submit_times`) = lúc giao dịch *vào* hệ thống.
- **Commit** đo ở Committing Peer = lúc giao dịch *ra* (đã ghi vĩnh viễn).
- **E2E latency = commit_time − submit_time** (ghép theo `txid`).

Hai mốc này ở hai dịch vụ khác nhau, nên cần cả hai nguồn để vẽ bức tranh đầy đủ về độ trễ.

---

## Tổng kết Committing Peer

Committing Peer là "thủ kho": nhận block từ orderer, kiểm tra mật mã, ghi bất biến vào file + cập nhật UTXO nguyên tử trong LevelDB, mirror sang PostgreSQL để tra cứu, và phục vụ truy vấn UTXO + ký endorsement. Thiết kế pipeline tách tầng + mirror bất đồng bộ giúp tốc độ ghi không phụ thuộc DB ngoài. Các điểm validation còn bỏ ngỏ (chuỗi prevHash, double-spend) là hướng cải thiện bảo mật.
