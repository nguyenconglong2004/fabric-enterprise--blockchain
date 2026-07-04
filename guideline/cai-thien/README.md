# Đề xuất cải thiện — Tốc độ, Lưu trữ, Bảo mật

Thư mục này tổng hợp các **điểm cần cải thiện** của hệ thống, dựa trên việc đọc mã nguồn và các tài liệu phân tích nội bộ (`docs/block-speed-optimization-analysis.md`, `docs/leader-election-analysis.md`). Mỗi đề xuất nêu: **vấn đề hiện tại → vì sao → hướng khắc phục → độ ưu tiên**.

> Đây là phần mang tính **đánh giá phản biện (critical review)** cho khóa luận — chỉ ra giới hạn thiết kế và hướng phát triển, không phủ nhận giá trị hệ thống hiện có.

## Ba trục cải thiện

| File | Trục | Vấn đề lớn nhất |
|------|------|-----------------|
| [01-cai-thien-toc-do.md](01-cai-thien-toc-do.md) | ⚡ Tốc độ | "1 block in-flight" giới hạn throughput; còn marshal/unmarshal lặp; chưa pipeline |
| [02-cai-thien-luu-tru.md](02-cai-thien-luu-tru.md) | 💾 Lưu trữ | Raft **không lưu bền** (RAM-only) → crash mất sạch; file block một-tệp; LevelDB không nén/prune |
| [03-cai-thien-bao-mat.md](03-cai-thien-bao-mat.md) | 🔒 Bảo mật | Mật khẩu DB hardcode; chưa TLS/mTLS; validation bỏ ngỏ (double-spend, prevHash chain); không lưu bền term |

## Bảng ưu tiên tổng hợp

| Mức | Hạng mục | Trục |
|-----|----------|------|
| 🔴 Cao | Lưu bền log/term/block xuống đĩa (WAL) | Lưu trữ + Bảo mật |
| 🔴 Cao | Pipeline nhiều block in-flight | Tốc độ |
| 🔴 Cao | Bật TLS/mTLS + bỏ secret hardcode | Bảo mật |
| 🟠 TB | Xác minh chuỗi prevHash + chống double-spend trên hot-path | Bảo mật |
| 🟠 TB | Kết nối libp2p bền cho mọi loại message | Tốc độ |
| 🟠 TB | Giảm marshal/unmarshal (OPT-4) | Tốc độ |
| 🟢 Thấp | Phân mảnh/nén file block, prune world state | Lưu trữ |
| 🟢 Thấp | Index & batching tốt hơn cho PostgreSQL mirror | Lưu trữ + Tốc độ |

## Ghi chú phương pháp

Các con số hiện trạng (commit ~3900 TPS, E2E p95 ~7.4s ở tải 5000 TPS) lấy từ [06-benchmark-hieu-nang/](../06-benchmark-hieu-nang/01-benchmark-metrics.md). Mọi đề xuất nên được **đo lại trước/sau** bằng cùng phương pháp benchmark để định lượng tác động.
