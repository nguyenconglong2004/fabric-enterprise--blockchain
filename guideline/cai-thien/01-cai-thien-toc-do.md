# Cải thiện Tốc độ (Performance)

> Liên quan: [02-orderingservice/04-nhan-ban-log-va-cat-block.md](../02-orderingservice/04-nhan-ban-log-va-cat-block.md), [06-benchmark-hieu-nang/](../06-benchmark-hieu-nang/01-benchmark-metrics.md)
> Nguồn phân tích: `docs/block-speed-optimization-analysis.md`

## Bối cảnh

Ở tải 5000 TPS: Core nhận ~4691/s nhưng **commit bền vững chỉ ~3921/s** → chênh ~800/s tạo backlog → **E2E p95 lên ~7.4s**. Mục tiêu cải thiện: nâng commit sustained tiệm cận offer rate để giữ latency dưới giây.

Hệ thống **đã làm** các tối ưu OPT-1, OPT-2, OPT-3, OPT-5, OPT-8 (commit ngay khi đủ ACK, propose theo sự kiện, stream endorsement bền, kênh hot-path riêng, bỏ log từng tx). Dưới đây là phần **còn lại**.

---

## 1. 🔴 Pipeline nhiều block in-flight (quan trọng nhất)

**Hiện tại:** Leader làm tuần tự `propose → chờ majority ACK → commit → mới propose block kế`. Chỉ **một block "đang bay" (in-flight)** tại một thời điểm. Throughput bị chặn ở `1 / RTT-commit` — mỗi block tốn trọn một vòng khứ hồi mạng tới đa số Follower.

**Vì sao chậm:** thời gian mạng (RTT) bị "lãng phí" — trong lúc chờ ACK block N, Leader không làm gì cho block N+1.

**Khắc phục:** cho phép **nhiều block in-flight** (pipelining), giống cách Raft chuẩn nhân bản log liên tục. Leader gửi block N+1, N+2... trước khi N được commit, miễn giữ đúng thứ tự ACK.

**Điều kiện kèm theo (theo doc, các mục SYNC-1/2/5):**
- Đảm bảo **giao đúng thứ tự** trên stream (libp2p stream đã FIFO, nhưng logic xử lý phải tôn trọng `PrevLogIndex`).
- Follower không được **âm thầm bỏ lỡ commit** (SYNC-1) — cần cơ chế phát hiện lỗ hổng và sync ngay.

**Tác động kỳ vọng:** lớn — có thể nâng commit sustained gấp nhiều lần khi RTT là nút cổ chai.

---

## 2. 🟠 Kết nối libp2p bền cho mọi loại message

**Hiện tại:** OPT-3 mới làm stream bền cho **endorsement**. Một số đường khác vẫn "một message = mở một stream mới" (`transport.go: SendMessage` mở rồi đóng stream mỗi lần).

**Vì sao chậm:** mở/đóng stream tốn (thương lượng protocol, cấp phát). Ở tần suất cao (heartbeat, ACK, commit broadcast) chi phí tích lũy.

**Khắc phục:** duy trì **kết nối + stream bền** giữa các orderer cho các message lặp lại (ACK, commit, heartbeat), tái dùng encoder/decoder JSON.

---

## 3. 🟠 Giảm marshal/unmarshal lặp (OPT-4 chưa làm)

**Hiện tại:** message dùng `Data interface{}`; mỗi handler **marshal rồi unmarshal lại** để lấy đúng kiểu → tốn CPU & sinh rác (garbage) cho GC.

**Khắc phục:**
- Dùng kiểu cụ thể thay vì `interface{}`, hoặc giữ payload đã giải mã.
- Cân nhắc chuyển từ JSON sang định dạng nhị phân nhanh hơn ([Protocol Buffers](https://protobuf.dev/), [MessagePack](https://msgpack.org/), hoặc [gob](https://pkg.go.dev/encoding/gob)) cho hot-path — JSON tốn cho parse/encode ở 5000 TPS.

---

## 4. 🟠 Song song hóa & cân chỉnh batch

- **Merkle song song:** hiện chỉ bật cho block > 1000 tx. Có thể hạ ngưỡng hoặc song song hóa thêm bước hash header.
- **Cân chỉnh `AutoProposeBlockSize`/`Interval`:** batch lớn hơn → ít vòng đồng thuận hơn nhưng latency cơ sở cao hơn khi tải thấp. Nên đo nhiều cấu hình để tìm điểm tối ưu cho từng mức tải.
- **WASM pool:** tăng `WASM_POOL_SIZE` nếu Core là nút nghẽn khi nhiều contract chạy song song (đo trước).

---

## 5. 🟢 Tối ưu tầng lưu trữ ảnh hưởng tốc độ

- **PostgreSQL mirror:** đã batch async; có thể dùng `COPY` thay vì `INSERT` nhiều dòng để mirror nhanh hơn nữa (nếu mirror là điểm tụt khi DB lớn).
- **SubmitRecorder:** đã batch qua channel — kiểm tra kích thước buffer/tần suất flush khớp với tải đỉnh.
- **LevelDB write:** gom nhiều block vào một batch lớn hơn nếu an toàn (đánh đổi với độ trễ commit từng block).

---

## Lộ trình đề xuất

1. **Đo baseline** kỹ (commit sustained, p95) bằng cùng cấu hình.
2. Làm **pipeline nhiều block in-flight** (#1) — tác động lớn nhất, kèm cơ chế phát hiện gap.
3. **Kết nối bền toàn diện** (#2) + **giảm marshal** (#3).
4. Cân chỉnh batch & pool, đo lại từng bước.

> Mọi thay đổi hot-path phải đo lại throughput **và** kiểm tra không phá an toàn (thứ tự, không mất tx). Xem ràng buộc an toàn ở [03-cai-thien-bao-mat.md](03-cai-thien-bao-mat.md) và [02-cai-thien-luu-tru.md](02-cai-thien-luu-tru.md).
