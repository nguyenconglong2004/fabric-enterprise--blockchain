# k6 — submit transaction

## Cách k6 đang push

| `SCENARIO` | Executor | Hành vi |
|------------|----------|---------|
| **`steady`** (mặc định) | `constant-arrival-rate` | Cố **RATE req/s** mỗi giây — **đều** (open-loop) |
| `maxpush` | `constant-vus` | N VU loop liên tục — **nhanh nhất có thể**, giây nào nhiều giây kia ít |

Tổng tx (ước): **`RATE × thời gian`** với `steady` (vd. `2000 × 10s ≈ 20_000` submit).

## Chạy

```bash
cd orderingservice/k6

# Đều ~2000 req/s trong 10s (~20k tx)
k6 run submit-tx.js

# Tăng tải + thời gian
k6 run -e RATE=1500 -e DURATION=30s -e MAX_VUS=800 submit-tx.js

# Burst tối đa (không đều — chỉ stress)
k6 run -e SCENARIO=maxpush -e VUS=200 -e DURATION=5s submit-tx.js
```

## Biến môi trường

| Biến | Mặc định | Mô tả |
|------|----------|--------|
| `SCENARIO` | `steady` | `steady` hoặc `maxpush` |
| `RATE` | `2000` | Target req/s (`steady`) |
| `DURATION` | `10s` | Thời gian test |
| `MAX_VUS` | `max(400, RATE+100)` | Trần VU để k6 đạt RATE |
| `PRE_VUS` | `min(MAX_VUS, max(RATE,50))` | VU khởi tạo sẵn |
| `VUS` | `100` | Chỉ `maxpush` |
| `LEDGER_WAIT` | `8s` | Chờ mirror PG trước metrics |
| `TX_PREFIX` | `k6-` | Lọc ledger |

Nếu k6 báo không đủ VU để giữ RATE → tăng `MAX_VUS` hoặc giảm `RATE`.

## Đo tx/s ledger

```bash
# Giây sát commit mới nhất
curl -s "http://localhost:8080/api/metrics/throughput?window=1&tx_prefix=k6-"

# Đỉnh 1 giây trong 60s trước latest (so sánh burst)
curl -s "http://localhost:8080/api/metrics/throughput?mode=peak&lookback=60&window=1&tx_prefix=k6-"
```

| `mode` | Ý nghĩa |
|--------|---------|
| `latest` | Tx trong `(latest − window, latest]` |
| `peak` | **Max** tx/s trong `lookback` giây (bucket = `window`, thường 1) |
| `since` | Trung bình từ `since` → now |

**Lưu ý:** `latest` với `window=2,3` thấp hơn `window=1` vì trung bình thêm giây ít tx. Dùng `peak` để tìm giây nóng nhất trong burst.

## Chuẩn bị stack

Postgres → orderer → commit peer → core `:8080` → `curl -X POST http://localhost:8080/api/deploy-example` nếu cần.
