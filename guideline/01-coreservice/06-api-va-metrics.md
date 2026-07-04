# Core Service — REST API & Metrics

> Mã nguồn: `coreservice/internal/api/server.go`, `internal/api/benchmark.go`

Core Service mở một HTTP server tại `:8080`. Đây là giao diện duy nhất mà thế giới bên ngoài (trình duyệt, k6, công cụ tra cứu) chạm tới.

## 1. Bảng các endpoint

### Quản lý contract & giao dịch
| Route | Method | Mục đích |
|-------|--------|----------|
| `/api/tx/deploy` | POST | Tải lên contract WASM (multipart: `contract_name`, file, `payload_schema`) |
| `/api/deploy-example` | POST | Deploy nhanh contract `example_asset` mẫu |
| `/api/tx/submit` | POST | Gửi giao dịch: chạy contract → ký → đẩy đi sắp xếp |
| `/api/contracts` | GET | Liệt kê contract đã deploy |
| `/api/contract/schema` | GET | Lấy schema UI của một contract (`?name=`) |

### Tra cứu sổ cái
| Route | Method | Mục đích |
|-------|--------|----------|
| `/api/state` | GET | Đọc world state theo key (`?key=`) |
| `/api/block` | GET | Lấy block theo hash (`?hash=`) |
| `/api/blocks` | GET | Liệt kê block đã commit (`?limit=`) |
| `/api/transactions` | GET | Liệt kê giao dịch đã commit (`?limit=`) |

### Realtime & đo lường
| Route | Method | Mục đích |
|-------|--------|----------|
| `/api/explorer/stream` | GET (SSE) | Đẩy block/giao dịch mới về trình duyệt theo thời gian thực |
| `/api/metrics/throughput` | GET | Tốc độ commit (tx/s, block/s) theo cửa sổ |
| `/api/metrics/benchmark` | GET | Tổng hợp submit + commit + E2E latency |
| `/api/metrics/e2e` | GET | Bí danh của `/benchmark` |

## 2. `/api/tx/submit` — endpoint quan trọng nhất

Body là một JSON `Transaction`. Luồng xử lý (chi tiết tại [03-crypto-endorsement.md](03-crypto-endorsement.md)):

```
giải mã JSON → engine.Execute() (chạy WASM)
   → signTxViaCommitPeer() (xin endorsement)
   → sendEndorsementAsync() (đẩy tới Leader orderer)
   → SubmitRecorder ghi thời điểm
   → trả JSON {status, signature preview, endorsement count}
```

Có thể tinh chỉnh bằng cờ trong `internal/api/perf.go`:
- `CORE_ASYNC_ENDORSE=1`: trả lời người dùng ngay, gửi đi sắp xếp ở nền.
- `CORE_ENDORSE_FALLBACK=0`: chỉ gửi Leader (1) hay thử cả Follower.

## 3. `/api/explorer/stream` — Server-Sent Events (SSE)

[SSE](https://developer.mozilla.org/en-US/docs/Web/API/Server-sent_events) là cơ chế server **đẩy** dữ liệu một chiều xuống trình duyệt qua một kết nối HTTP giữ mở. Khi có block/giao dịch mới, Core gửi sự kiện `ledger_update` chứa block mới nhất; Explorer nhận và cập nhật giao diện ngay mà không cần hỏi lại liên tục (polling).

Vì sao SSE chứ không phải [WebSocket](https://developer.mozilla.org/en-US/docs/Web/API/WebSockets_API)? Vì luồng dữ liệu ở đây **một chiều** (server → client), SSE đơn giản hơn, tự kết nối lại, chạy trên HTTP thường.

## 4. Metrics & benchmark (`internal/api/benchmark.go`)

`/api/metrics/benchmark` tổng hợp ba lớp đo trong một cửa sổ `[since, until]`:
- **Submit** (Core chấp nhận): đếm từ `core_service.tx_submit_times`.
- **Commit** (đã ghi sổ): đếm từ `commit_peer.ledger_transactions`.
- **E2E latency**: ghép submit ↔ commit theo `txid`, tính phân vị p50/p95/p99.

Phản hồi còn có các gợi ý đạt chuẩn RFP (`meets_submit_sustained_5000`, `meets_latency_p95_under_1s`...). Công thức và cách dùng chi tiết tại [06-benchmark-hieu-nang/01-benchmark-metrics.md](../06-benchmark-hieu-nang/01-benchmark-metrics.md).

## 5. Tổng kết Core Service

Core Service là "bộ não điều phối": nó **không** quyết định thứ tự (việc của Ordering Service) và **không** ghi sổ cái cuối (việc của Committing Peer), nhưng nó **kết nối tất cả lại**: chạy contract, thu thập chữ ký, định tuyến tới đúng Leader, và là cửa sổ duy nhất ra thế giới bên ngoài.

➡️ Tiếp: [02-orderingservice/](../02-orderingservice/01-vai-tro.md)
