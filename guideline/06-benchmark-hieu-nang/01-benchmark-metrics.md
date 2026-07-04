# Benchmark & Hiệu năng — Cách đo Throughput & Latency

> Nguồn: `docs/BENCHMARK_METRICS.md`, `docs/BENCH_PING_AND_K6.md`, các API metrics

Phần này giải thích cách dự án đo hiệu năng — kiến thức cần để đọc kết quả thực nghiệm và phần [cai-thien/](../cai-thien/README.md).

## 1. Ba lớp đo lường

Một giao dịch đi qua nhiều chặng; đo ở chặng khác nhau cho con số khác nhau:

| Lớp | Nguồn | Đo cái gì |
|-----|-------|-----------|
| **k6 (client)** | Script `k6/submit-tx.js` | HTTP POST `/api/tx/submit` — Core có trả 200 + `success` không, độ trễ HTTP |
| **Submit (Core)** | Bảng `core_service.tx_submit_times` | Lúc Core **chấp nhận** giao dịch (sau validate + ký) |
| **Commit (ledger)** | `commit_peer.ledger(_transactions)` | Lúc giao dịch **đã ghi** vào sổ cái |

**E2E latency = `committed_at − submitted_at`** (ghép theo `txid`).

> ⚠️ `submit_latency_ms` của k6 (vài chục ms) **khác** `latency_ms_p95` E2E (có thể vài giây khi có backlog). Một cái đo "Core nhận", một cái đo "cả pipeline".

## 2. Hai chỉ số cốt lõi

- **Throughput (TPS):** số giao dịch/giây. Phân biệt:
  - *Sustained* (bền vững): trung bình trong cả cửa sổ tải.
  - *Peak* (đỉnh): nhiều nhất trong một giây bất kỳ.
- **Latency (độ trễ):** thời gian E2E, đo bằng phân vị (p50/p95/p99) — p95 = 95% giao dịch nhanh hơn giá trị này.

## 3. API đo lường

### `GET /api/metrics/benchmark` (khuyến nghị, trên Core Service)
Tổng hợp submit + commit + E2E latency trong cửa sổ.
- Tham số: `since`, `until` (RFC3339), hoặc `lookback` (giây), `tx_prefix` (lọc theo tiền tố txid).
- Trả về: `submit_count`, `submit_tx_per_sec_sustained/peak`, `commit_count`, `commit_tx_per_sec_*`, `blocks_committed`, `avg_tx_per_block`, `e2e_completed/pending`, `latency_ms_p50/p95/p99/avg/min/max`, và các cờ gợi ý đạt chuẩn (`meets_*`).

### `GET /api/metrics/throughput`
Chỉ đo commit (tx/s, block/s) theo mode `latest`/`peak`/`window`/`since`.

### Trên Committing Peer (`:8081`)
`/metrics/throughput`, `/metrics/benchmark`, `/metrics/commit-lookup` — nguồn ground truth từ recorder trong RAM (xem [03-commitingpeer/05-metrics.md](../03-commitingpeer/05-metrics.md)).

## 4. Vì sao đo theo "cửa sổ"?

k6 in ra hai mốc thời gian khi kết thúc (teardown):
- **Cửa sổ load:** `loadStart → loadStart + DURATION` — đo throughput **trong lúc bắn** (chuẩn RFP sustained).
- **Cửa sổ load + drain:** kéo dài thêm thời gian chờ pipeline xử backlog — `e2e_pending` thấp hơn.

Quan hệ: `submit_count ≈ e2e_completed + e2e_pending` trong cùng cửa sổ. **Pending không có nghĩa mất tx** — thường là tx submit cuối cửa sổ hoặc commit xảy ra sau `until`.

## 5. Hiện tượng backlog (quan trọng để hiểu hệ thống)

Khi **tốc độ gửi > tốc độ commit bền vững**, giao dịch dồn vào hàng đợi (TxPool) → **backlog tích lũy** → E2E latency tăng dù Core chấp nhận nhanh. Đây là biểu hiện kinh điển của hệ thống đạt **điểm bão hòa (saturation)**. Tìm hiểu: [Little's Law](https://en.wikipedia.org/wiki/Little%27s_law) — độ trễ tỉ lệ với số việc tồn đọng chia cho tốc độ xử lý.

## 6. Kết quả thực nghiệm tham khảo (5000 req/s × 60s)

Theo `docs/BENCHMARK_METRICS.md`:
| Chỉ số | Giá trị |
|--------|---------|
| Submit sustained | ~4691/s (đỉnh ~5017/s) |
| Commit sustained | ~3921/s (đỉnh ~6000/s) |
| Trung bình tx/block | ~989 (batch ~1000) |
| E2E p95 | ~7.4s (khi offer > commit → backlog) |
| `e2e_pending` | ~16k (tx còn in-flight ở biên cửa sổ, không mất) |

**Diễn giải:** Core nhận gần đủ 5000/s, nhưng commit "trần" ~3900/s bền vững → chênh lệch ~800/s tạo backlog → p95 tăng lên giây. Đây chính là động lực cho các đề xuất ở [cai-thien/01-cai-thien-toc-do.md](../cai-thien/01-cai-thien-toc-do.md) (đặc biệt: pipeline nhiều block in-flight).

## 7. Lỗi đo thường gặp

| Triệu chứng | Nguyên nhân | Khắc phục |
|-------------|-------------|-----------|
| Benchmark toàn 0 | Sai `since`/`until` | Copy timestamp từ log teardown k6 |
| `submit_count=0` | `CORE_RECORD_SUBMIT=0` hoặc chưa migration | Bật recording, chạy migration |
| Peak timeout | `lookback` quá lớn trên DB lớn | Giảm lookback (60–90s) |
| Commit thấp dù submit cao | Orderer/commit peer chưa sync | Kiểm tra log, multiaddr |
