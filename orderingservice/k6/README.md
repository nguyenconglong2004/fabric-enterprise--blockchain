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

# Mặc định: bench_ping, ~6000 req/s × 25s
k6 run submit-tx.js

# Ép hơn nữa (sau khi restart orderer 100ms interval)
k6 run -e RATE=8000 -e DURATION=30s -e MAX_VUS=9000 submit-tx.js

# Sweep 4k → 10k (+1500 mỗi 15s)
k6 run -e SCENARIO=sweep submit-tx.js

# Burst tối đa (không đều — chỉ stress)
k6 run -e SCENARIO=maxpush -e VUS=200 -e DURATION=5s submit-tx.js
```

## Biến môi trường

| Biến | Mặc định | Mô tả |
|------|----------|--------|
| `SCENARIO` | `steady` | `steady` hoặc `maxpush` |
| `CONTRACT` | `bench_ping` | Contract (nhẹ hơn `example_asset`) |
| `RATE` | `6000` | Target req/s (`steady`) |
| `DURATION` | `25s` | Thời gian test |
| `MAX_VUS` | `max(800, RATE+800)` | Trần VU để k6 đạt RATE |
| `SCENARIO` | `steady` | `steady`, `sweep`, `maxpush` |
| `SWEEP_*` | 2500→6000 / 15s | Chỉ `sweep` |
| `PRE_VUS` | `min(MAX_VUS, max(RATE,50))` | VU khởi tạo sẵn |
| `VUS` | `100` | Chỉ `maxpush` |
| `LEDGER_WAIT` | `12s` | Chờ mirror PG trước metrics |
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

Postgres → orderer → commit peer → core `:8080`.

Deploy contract (multipart, dùng chung `POST /api/tx/deploy`):

```bash
# example_asset (shortcut)
curl -X POST http://localhost:8080/api/deploy-example

# bench_ping — sau khi build WASM
curl -X POST http://localhost:8080/api/tx/deploy \
  -F "contract_name=bench_ping" \
  -F "file=@/path/to/coreservice/contracts/bench_ping/my_contract.wasm"
```

k6 benchmark: `k6 run -e CONTRACT=bench_ping submit-tx.js`
