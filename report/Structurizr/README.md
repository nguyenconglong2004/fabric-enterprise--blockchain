# Structurizr — mô hình C4 cho báo cáo

Mô hình hoá kiến trúc hệ thống bằng [Structurizr DSL](https://docs.structurizr.com/dsl),
thay cho các hình vẽ tay trong báo cáo.

## File

| File | Nội dung |
|------|----------|
| `workspace.dsl` | Điểm vào — nạp `model.dsl` + `views.dsl` |
| `model.dsl` | Người dùng, container, component, quan hệ, triển khai |
| `views.dsl` | Các view + style (màu/hình) |

## Quy ước hình ảnh (để phân loại nhanh)

**MÀU = tầng trách nhiệm** (Execute–Order–Validate):

| Màu | Tầng | Thành phần |
|-----|------|-----------|
| 🟦 Xanh dương | Execute | Core Service |
| 🟪 Tím | Order | Ordering Service (Raft) |
| 🟩 Xanh lá | Validate | Committing Peer |

**HÌNH = loại gói:**

| Hình | Loại | Ví dụ |
|------|------|-------|
| ⬡ Lục giác | Compute (xử lý nặng) | Contract VM, Leader/Block Cutter, Validation |
| ▭ Ống (pipe) | Network (libp2p / client) | Endorsement Client, Deliver Fan-out |
| ▢ Bo góc | Logic (điều phối/trạng thái) | Discovery, Raft Consensus, Membership |
| 🛢 Trụ nâu | Storage (lưu trữ) | LevelDB, Block Storage, PostgreSQL |
| ⬭ Ellipse xám | Support (crypto/metrics) | Crypto, Metrics |

## View ↔ Hình trong báo cáo

| Key view | Loại | Thay cho |
|----------|------|----------|
| `context` | System Context | (bổ sung) |
| `arch` | Container | `fig:overall-arch` — `Images/fg_1_architecture.png` |
| `core_components` | Component | (bổ sung) — gói con Core Service |
| `ordering_components` | Component | (bổ sung) — gói con Ordering Service |
| `cp_components` | Component | `fig:cp-pipeline` |
| `tx_journey` | Dynamic | mục *Hành trình của một giao dịch* |
| `block_pipeline` | Dynamic | `fig:block-pipeline` |
| `deployment` | Deployment | (bổ sung) — cổng & node |

> Các hình `fig:wasm-abi` (ABI sequence), `fig:raft-states` (state machine),
> `fig:election` (flowchart) **không** thuộc mô hình C4 — dùng PlantUML/TikZ,
> không dùng Structurizr.

## Xem & xuất ảnh

> Lưu ý: image `structurizr/lite` cũ nay chỉ in thông báo ngừng phát triển rồi
> thoát. Dùng image hợp nhất **`structurizr/structurizr`**.

### Server cục bộ (khuyến nghị — xem tương tác + Export PNG/SVG)

```bash
docker run -it --rm -p 8088:8080 \
  -v "$(pwd):/usr/local/structurizr" \
  structurizr/structurizr local
# mở http://localhost:8088  → chọn view ở dropdown → nút "Export" (PNG/SVG)
```

### Kiểm tra cú pháp

```bash
docker run --rm -v "$(pwd):/usr/local/structurizr" \
  structurizr/structurizr validate -workspace workspace.dsl
```

### Xuất PlantUML / Mermaid (dạng text)

```bash
docker run --rm -v "$(pwd):/usr/local/structurizr" \
  structurizr/structurizr export -workspace workspace.dsl -format plantuml
```

> Xuất **PNG/SVG chuẩn Structurizr** (đúng màu/hình) chỉ làm qua nút *Export*
> trên giao diện web ở trên; bản CLI này không hỗ trợ xuất PNG/SVG trực tiếp.

## Nhúng vào LaTeX

Xuất PNG/SVG từ Structurizr Lite vào `report/Images/`, rồi trong figure tương ứng:

```latex
\includegraphics[width=\textwidth]{Images/<ten-view>.png}
```
