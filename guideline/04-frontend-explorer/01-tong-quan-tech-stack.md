# Blockchain Explorer (Frontend) — Tổng quan & Tech Stack

> Mã nguồn: `BlockchainExplorer-FrontEnd/` · Ngôn ngữ: JavaScript (React)

## 1. Explorer làm gì?

**Blockchain Explorer** là giao diện web cho người dùng cuối, có hai chức năng:
1. **Khám phá (explorer):** xem block và giao dịch đã commit theo thời gian thực.
2. **Gửi giao dịch (transfer):** tạo và gửi giao dịch mới (gọi smart contract / chuyển UTXO).

Nó nói chuyện với **Core Service** (HTTP `:8080`) qua một proxy của Vite. Mọi dữ liệu hiển thị đều đến từ API Core Service.

## 2. Công nghệ (từ `package.json`)

| Thư viện | Phiên bản | Vai trò |
|----------|-----------|---------|
| [React](https://react.dev/) | 18.3.1 | Framework UI (hooks) |
| [React Router DOM](https://reactrouter.com/) | 6.26.2 | Điều hướng client-side |
| [Vite](https://vitejs.dev/) | 5.4.1 | Build tool & dev server (kèm proxy `/api`) |
| [Tailwind CSS](https://tailwindcss.com/) | 3.4.13 | CSS tiện ích |
| [Material-UI (MUI)](https://mui.com/) | 6.1.1 | Bộ component giao diện |
| [Emotion](https://emotion.sh/) | 11.13.x | CSS-in-JS (phụ thuộc MUI) |
| [CryptoJS](https://github.com/brix/crypto-js) | 4.1.1 | Băm SHA-256 (tạo txid phía client) |
| [@faker-js/faker](https://fakerjs.dev/) | 8.0.0 | Sinh dữ liệu giả khi demo |

**Dev:** ESLint 9, PostCSS + Autoprefixer, `@vitejs/plugin-react`.

## 3. Script build/dev (`package.json`)

```json
"dev":     "vite"           // chạy dev server (proxy tới :8080)
"build":   "vite build"     // đóng gói production
"lint":    "eslint ."       // kiểm tra mã
"preview": "vite preview"   // xem thử bản build
```

## 4. Cấu hình proxy (`vite.config.js`)

Vite chuyển tiếp mọi request `/api/*` tới `http://localhost:8080` (bật `changeOrigin`). Nhờ vậy frontend gọi đường dẫn tương đối (`/api/...`) mà không vướng [CORS](https://developer.mozilla.org/en-US/docs/Web/HTTP/CORS) khi dev.

## 5. Bố cục thư mục

```
BlockchainExplorer-FrontEnd/
├── src/
│   ├── main.jsx, App.jsx        ← gốc ứng dụng + định tuyến
│   ├── api/client.js            ← lớp gọi API Core Service
│   ├── components/              ← các component giao diện
│   │   ├── Dashboard.jsx          container chính + SSE realtime
│   │   ├── Transactions.jsx       danh sách giao dịch (phân trang)
│   │   ├── Transfer.jsx           form gửi giao dịch
│   │   ├── Blocks.jsx, BlockDetails.jsx
│   │   ├── TransactionDetailView.jsx
│   │   ├── Navbar.jsx, Navigation.jsx, Header.jsx, Receipt.jsx
│   │   ├── transactionTypes.js    hệ mã hóa payload nhị phân
│   │   └── PAYLOAD_EXAMPLES.js, transactionTypes.test.js
│   └── utils/transactionUtils.js  ← mã hóa payload contract WASM
├── docker-compose.yml           ← PostgreSQL dùng chung
├── init.sql                     ← lược đồ DB
└── package.json, vite.config.js, tailwind.config.js
```

## 6. Đọc tiếp

| Chủ đề | File |
|--------|------|
| Các component & điều hướng | [02-cac-component.md](02-cac-component.md) |
| Mã hóa payload nhị phân | [03-binary-payload.md](03-binary-payload.md) |
| Cập nhật realtime (SSE) | [04-realtime-sse.md](04-realtime-sse.md) |

> **Lưu ý:** README của thư mục này (tác giả Prayag Tandon) mô tả nó như "Ethereum-like Blockchain Explorer" — dự án đã tùy biến từ một template explorer kiểu Ethereum để gắn vào backend Fabric-like.
