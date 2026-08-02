# k6 — benchmark submit transaction

Hướng dẫn đầy đủ: [HuongDanCaiDat.txt](../../HuongDanCaiDat.txt), [HuongDanSuDung.txt](../../HuongDanSuDung.txt).

## Chạy nhanh

```bash
cd orderingservice/k6
ulimit -n 65536   # macOS/Linux

k6 run -e RATE=2000 -e DURATION=60s -e LEDGER_WAIT=90s \
  -e MAX_VUS=3000 -e TX_PREFIX=my-run- -e CONTRACT=bench_ping submit-tx.js
```

## Scenario

| `SCENARIO` | Hành vi |
|------------|---------|
| `steady` (mặc định) | Cố định `RATE` req/s |
| `sweep` | Tăng dần rate để tìm điểm bão hòa |
| `maxpush` | N VU loop liên tục — stress burst |

## Biến môi trường

| Biến | Mặc định | Mô tả |
|------|----------|--------|
| `BASE_URL` | `http://localhost:8080` | Core API |
| `RATE` | `6000` | Target req/s (`steady`) |
| `DURATION` | `25s` | Thời gian load |
| `MAX_VUS` | `max(800, RATE+800)` | Trần VU |
| `TX_PREFIX` | `k6-` | Prefix txid — **riêng mỗi run** |
| `LEDGER_WAIT` | `15s` | Chờ drain trước metric E2E |
| `CONTRACT` | `bench_ping` | Tên contract |

Nếu k6 báo thiếu VU → tăng `MAX_VUS` hoặc giảm `RATE`.

## Đọc kết quả

Dùng block **`benchmark (load window)`** trong log teardown (không dùng load+drain cho throughput).

```bash
curl -s "http://localhost:8080/api/metrics/throughput?window=1&tx_prefix=my-run-"
```
