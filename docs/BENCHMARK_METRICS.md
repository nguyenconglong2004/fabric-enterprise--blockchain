# Benchmark metrics — hướng dẫn đọc số liệu

Tài liệu mô tả các metric hiện có để đo throughput và latency của pipeline **submit → orderer → commit peer → ledger (Postgres mirror)**.

---

## 1. Ba lớp đo lường

| Lớp | Nguồn | Ý nghĩa |
|-----|--------|---------|
| **k6 (client)** | Script `orderingservice/k6/submit-tx.js` | HTTP POST `/api/tx/submit` — Core có trả 200 + `status: success` không |
| **Submit (Core)** | Bảng `core_service.tx_submit_times` | Thời điểm Core **chấp nhận** tx (sau validate + sign) |
| **Commit (ledger)** | Bảng `commit_peer.ledger` + `ledger_transactions` | Thời điểm tx **đã commit** trên committing peer (mirror Postgres) |

**E2E latency** = `committed_at − submitted_at` (join theo `txid`).

**Lưu ý:** k6 `submit_latency_ms` (HTTP p95 ~ vài chục ms) **khác** E2E `latency_ms_p95` (thường vài giây khi backlog) — một cái đo Core accept, một cái đo cả pipeline.

---

## 2. API endpoints

Base URL mặc định: `http://localhost:8080`

### 2.1 `GET /api/metrics/benchmark` (khuyến nghị)

Endpoint tổng hợp cho benchmark / RFP. Alias: `/api/metrics/e2e`.

**Query:**

| Param | Bắt buộc | Mô tả |
|-------|----------|--------|
| `since` | Có* | Đầu cửa sổ (RFC3339 hoặc RFC3339Nano), vd. `2026-06-06T17:25:07.689Z` |
| `until` | Có* | Cuối cửa sổ |
| `lookback` | Thay since/until | Giây lookback từ *now* (mặc định 300) |
| `tx_prefix` | Khuyến nghị | Lọc txid, vd. `k6-rfp-` |

\* Nếu thiếu `since`/`until` thì dùng `lookback`.

**Ví dụ — cửa sổ load 60s (copy timestamp từ k6 teardown):**

```bash
curl -s "http://localhost:8080/api/metrics/benchmark?\
since=2026-06-06T17:25:07.689Z&\
until=2026-06-06T17:26:07.689Z&\
tx_prefix=k6-rfp-" | jq .
```

**Ví dụ — 90 giây gần nhất:**

```bash
curl -s "http://localhost:8080/api/metrics/benchmark?lookback=90&tx_prefix=k6-rfp-" | jq .
```

### 2.2 `GET /api/metrics/throughput`

Chỉ đo **commit** trên ledger (không có submit / E2E).

| Param | Mô tả |
|-------|--------|
| `mode=latest` (mặc định) | Tx commit trong `(latest_commit − window, latest]` |
| `mode=peak` | Max tx/s trong bucket 1s, scan `lookback` giây (query nặng khi DB lớn) |
| `mode=window` | Sustained commit trong `[since, until]` |
| `mode=since` | Từ `since` → now |
| `window` | Kích thước bucket giây (mặc định 1) |
| `tx_prefix` | Lọc txid |

```bash
# Commit rate tại giây mới nhất
curl -s "http://localhost:8080/api/metrics/throughput?window=1&tx_prefix=k6-rfp-" | jq .

# Peak 1s trong 60s lookback (nhẹ hơn lookback=240)
curl -s "http://localhost:8080/api/metrics/throughput?mode=peak&lookback=60&window=1&tx_prefix=k6-rfp-" | jq .
```

---

## 3. Bảng field `/api/metrics/benchmark`

### Submit (Core accept)

| Field | Công thức / nguồn |
|-------|-------------------|
| `submit_count` | Số dòng trong `tx_submit_times` với `submitted_at ∈ [since, until]` |
| `submit_tx_per_sec_sustained` | `submit_count / window_seconds` |
| `submit_tx_per_sec_peak` | Max số submit trong **một giây** trong cửa sổ |

### Commit (ledger)

| Field | Công thức / nguồn |
|-------|-------------------|
| `commit_count` | Tx commit trong cửa sổ (theo `COALESCE(ledger_committed_at, committed_at)`) |
| `commit_tx_per_sec_sustained` | `commit_count / window_seconds` |
| `commit_tx_per_sec_peak` | Max tx commit trong một giây |
| `blocks_committed` | Số block distinct có tx commit trong cửa sổ |
| `blocks_per_sec_sustained` | `blocks_committed / window_seconds` |
| `avg_tx_per_block` | `commit_count / blocks_committed` |

### E2E

| Field | Ý nghĩa |
|-------|---------|
| `e2e_completed` | Tx submit **trong cửa sổ** và **đã có** bản ghi ledger (dùng cho latency) |
| `e2e_pending` | Submit trong cửa sổ nhưng **chưa** commit tại thời điểm query |
| `e2e_tx_per_sec_peak` | Peak số tx hoàn thành E2E mỗi giây |
| `latency_ms_p50/p95/p99/avg/min/max` | Phân vị latency E2E (ms), chỉ trên `e2e_completed` |

### Gợi ý pass RFP (trong response)

| Field | Điều kiện |
|-------|-----------|
| `meets_submit_sustained_5000` | `submit_tx_per_sec_sustained >= 5000` |
| `meets_commit_sustained_5000` | `commit_tx_per_sec_sustained >= 5000` |
| `meets_latency_p95_under_1s` | `e2e_completed > 0` và `latency_ms_p95 < 1000` |

Đây là **hint tự động**, không thay thế đánh giá nghiệp vụ (cửa sổ đo, backlog, v.v.).

---

## 4. Hai cửa sổ thời gian (k6 teardown)

k6 `submit-tx.js` in hai block benchmark:

| Cửa sổ | `since` → `until` | Dùng khi |
|--------|-------------------|----------|
| **load window** | `loadStart` → `loadStart + DURATION` | Đo throughput **trong lúc bắn tx** (RFP sustained) |
| **load + drain** | `loadStart` → thời điểm sau `LEDGER_WAIT` | Đo sau khi chờ pipeline xử backlog; `e2e_pending` thấp hơn |

**Pending không có nghĩa tx mất:** thường là tx submit **cuối cửa sổ load** hoặc commit xảy ra **sau** `until`. Công thức:

```
submit_count ≈ e2e_completed + e2e_pending   (trong cùng cửa sổ load)
```

Khi offer rate > commit sustained, backlog tích lũy → E2E latency cao dù Core accept nhanh.

---

## 5. Nguồn dữ liệu Postgres

| Schema / bảng | Ghi bởi | Dùng cho |
|---------------|---------|----------|
| `core_service.tx_submit_times` | Core (`SubmitRecorder`) | Submit metrics, E2E join |
| `commit_peer.ledger` | Commit peer mirror | Thời gian block commit |
| `commit_peer.ledger_transactions` | Commit peer mirror | Tx trong block |

Migration (DB cũ chưa có bảng submit):

```bash
docker exec -i fabric-postgres psql -U fabric -d blockchain < migrations/002_tx_submit_times.sql
```

---

## 6. Biến môi trường Core

| Biến | Mặc định | Mô tả |
|------|----------|--------|
| `CORE_RECORD_SUBMIT` | **bật** | Ghi `tx_submit_times`. Set `0` hoặc `false` để tắt (submit metrics = 0) |
| `POSTGRES_URL` | `postgres://fabric:fabric123@localhost:5432/blockchain?sslmode=disable` | Kết nối DB mirror |

Submit recording dùng **batch INSERT** qua channel (buffer 65536), không gọi DB mỗi request — overhead thấp hơn so với goroutine-per-tx.

---

## 7. k6 metrics (client-side)

Script k6 cũng xuất metric riêng (phần `TOTAL RESULTS`):

| Metric | Ý nghĩa |
|--------|---------|
| `submit_ok` | Số lần HTTP 200 + Core `status: success` |
| `submit_fail` | Submit không thành công |
| `submit_latency_ms` | Thời gian HTTP round-trip tới Core |

**Không dùng** `submit_ok / tổng thời gian test` làm sustained rate — thời gian test gồm cả teardown/wait. Dùng **benchmark API** với cửa sổ load chính xác.

---

## 8. Lỗi thường gặp

| Triệu chứng | Nguyên nhân | Cách xử lý |
|-------------|-------------|------------|
| Benchmark toàn 0 | Sai `since`/`until` (ngày/giờ không trùng lúc chạy k6) | Copy timestamp từ log teardown |
| `submit_count = 0` | `CORE_RECORD_SUBMIT=0` hoặc chưa migration | Bật recording, chạy migration |
| Peak throughput timeout | `mode=peak&lookback=240` trên DB lớn | Giảm `lookback` (60–90) hoặc tăng timeout client |
| `commit_count` thấp, k6 `submit_ok` cao | Orderer/commit peer chưa sync, sai multiaddr | Kiểm tra Core logs, commit peer deliver |
| `e2e_pending` cao sau load | Backlog; drain chưa đủ | Tăng `LEDGER_WAIT`, query cửa sổ load+drain |

---

## 9. Ví dụ kết quả thực tế (5000 req/s × 60s)

Cửa sổ load `k6-rfp-`:

- Submit sustained ~4691/s, peak ~5017/s  
- Commit sustained ~3921/s, peak ~6000/s  
- `avg_tx_per_block` ~989 — batch orderer ~1000 tx/block  
- E2E p95 ~7.4s khi offer > commit sustained (queue)  
- `e2e_pending` ~16k — tx còn in-flight tại biên cửa sổ, không phải mất tx  

Chi tiết setup và lệnh k6: [BENCH_PING_AND_K6.md](./BENCH_PING_AND_K6.md).
