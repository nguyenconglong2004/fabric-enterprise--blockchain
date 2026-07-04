# Blockchain Explorer — Các Component & Điều hướng

> Mã nguồn: `BlockchainExplorer-FrontEnd/src/components/`, `src/App.jsx`, `src/api/client.js`

## 1. Điều hướng (`App.jsx`)

Dùng [React Router](https://reactrouter.com/) với ba tuyến đường, tất cả render qua `Dashboard` với prop `section` khác nhau:

```
BrowserRouter
 ├─ Navbar         (thanh tiêu đề tĩnh)
 ├─ Navigation     (3 link: Transactions | Transfer | Blocks)
 └─ Routes
     ├─ /transactions → Dashboard(section="transactions")
     ├─ /transfer     → Dashboard(section="transfer")
     └─ /blocks       → Dashboard(section="blocks")
```

`<Link>` của React Router đổi trang **không reload** (SPA — single page application).

## 2. Bảng các component

| Component | Vai trò |
|-----------|---------|
| **Dashboard.jsx** | Container chính: quản lý state block/giao dịch, mở SSE realtime, chọn section con để render |
| **Transactions.jsx** | Danh sách giao dịch, phân trang 8/trang, mở rộng xem chi tiết, giải mã payload |
| **Transfer.jsx** | Form gửi giao dịch: tự lấy danh sách contract, sinh field theo schema, tạo VOUT, gọi `/api/tx/submit` |
| **TransactionDetailView.jsx** | Xem chi tiết một giao dịch: txid, contract, hàm, khóa công khai, chữ ký, VIN/VOUT, payload (hex + giải mã) |
| **Blocks.jsx** | Chọn block, tìm giao dịch liên quan, hiện thông tin cơ bản |
| **BlockDetails.jsx** | Bảng chi tiết một block |
| **Receipt.jsx** | Biên lai giao dịch (from/to/amount/gas/block hash) |
| **Navbar.jsx** | Thanh tiêu đề (MUI AppBar) |
| **Navigation.jsx** | Thanh điều hướng ngang |
| **Header.jsx** | Header tái dùng (ít dùng) |

## 3. Lớp gọi API (`src/api/client.js`)

Tất cả URL là tương đối (`/api/...`), được Vite proxy chuyển tới Core Service.

| Hàm | Endpoint | Mục đích |
|-----|----------|----------|
| `apiGet(path)` | (bất kỳ) | GET, tự xử lý JSON/text |
| `apiPostJson(path, body)` | (bất kỳ) | POST JSON |
| `getContracts()` | `/api/contracts` | Lấy danh sách contract |
| `getCommittedBlocks(limit)` | `/api/blocks?limit=` | Lấy block đã commit |
| `getCommittedTransactions(limit)` | `/api/transactions?limit=` | Lấy giao dịch đã commit |
| `getBlockByHash(hash)` | `/api/block?hash=` | Lấy block theo hash |
| `submitTx(tx)` | `/api/tx/submit` | Gửi giao dịch |
| `createExplorerEventSource()` | `/api/explorer/stream` | Mở SSE realtime |

## 4. Luồng gửi giao dịch (Transfer)

```
Người dùng vào /transfer
  → Transfer.jsx gọi getContracts() lấy danh sách + schema
  → render form động theo schema (mỗi field 1 input)
  → người dùng điền + (tùy chọn) tạo VOUT
  → bấm Submit:
       serializePayload()          (nếu schema nhị phân)
       hoặc serializeContractPayload()  (nếu contract WASM)
  → POST /api/tx/submit
  → Core ký + đẩy đi sắp xếp
  → SSE ledger_update → Dashboard cập nhật danh sách
```

Cách mã hóa payload chi tiết tại [03-binary-payload.md](03-binary-payload.md).

➡️ Tiếp: [03-binary-payload.md](03-binary-payload.md)
