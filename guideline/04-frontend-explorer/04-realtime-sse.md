# Blockchain Explorer — Cập nhật Realtime (SSE)

> Mã nguồn: `src/components/Dashboard.jsx`, `src/api/client.js`

## 1. Server-Sent Events (SSE) là gì?

[SSE](https://developer.mozilla.org/en-US/docs/Web/API/Server-sent_events) cho phép server **đẩy** dữ liệu một chiều xuống trình duyệt qua một kết nối HTTP giữ mở. Trình duyệt dùng API `EventSource`. Khác với polling (hỏi đi hỏi lại), SSE chỉ gửi khi **có dữ liệu mới** → nhẹ và tức thời.

Explorer dùng SSE để hiện block/giao dịch mới ngay khi chúng được commit, không cần người dùng F5.

## 2. Cách Dashboard dùng SSE (`Dashboard.jsx`)

```
khi component mount:
  es = createExplorerEventSource()   // new EventSource('/api/explorer/stream')
  es.onopen   → status = "connected"
  sự kiện "ready"        → xác nhận kết nối
  sự kiện "ledger_update"→ parse JSON → cập nhật (upsert) block & giao dịch
  es.onerror  → status = "disconnected", hẹn kết nối lại sau 3000ms
```

Payload sự kiện `ledger_update`:
```json
{
  "latest_block": { "hash": "...", "number": 12, "timestamp": ..., "transactions": [...] },
  "latest_tx":    { "txid": "...", ... }
}
```

## 3. Dự phòng bằng polling

Nếu SSE đứt (mạng lỗi, server tắt), Dashboard chuyển sang **polling dự phòng**: cứ 3000ms gọi `/api/transactions` và `/api/blocks`. Khi SSE kết nối lại, dừng polling. Đây là pattern "ưu tiên đẩy, có lưới đỡ kéo" — luôn hiển thị dữ liệu dù kênh realtime trục trặc.

## 4. Hiển thị trạng thái kết nối

Dashboard hiện chấm màu:
- 🟢 Xanh = `connected` (SSE đang chạy).
- 🟡 Vàng = `connecting`.
- 🔴 Đỏ = `disconnected` (đang dùng polling, sẽ thử lại).

## 5. Vì sao chọn SSE thay vì WebSocket?

| | SSE | WebSocket |
|---|-----|-----------|
| Hướng dữ liệu | Một chiều (server→client) | Hai chiều |
| Giao thức | HTTP thường | Nâng cấp riêng |
| Tự kết nối lại | Có sẵn | Phải tự code |
| Phù hợp ở đây | ✅ (chỉ cần server đẩy update) | dư thừa |

Luồng dữ liệu của Explorer chỉ một chiều (xem cập nhật), nên SSE đơn giản và đủ dùng.

---

## Tổng kết Frontend

Blockchain Explorer là một SPA React + Vite + MUI/Tailwind, gọi REST API Core Service qua proxy, mã hóa payload (nhị phân tùy biến hoặc JSON-hex cho contract WASM), và nhận cập nhật realtime qua SSE với polling dự phòng. Nó là "bộ mặt" thân thiện để quan sát và tương tác với toàn hệ thống blockchain phía dưới.
