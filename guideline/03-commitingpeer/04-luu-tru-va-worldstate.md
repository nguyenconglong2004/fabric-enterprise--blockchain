# Committing Peer — Lưu trữ: Block, World State & PostgreSQL

> Mã nguồn: `commitingpeer/source/internal/storage/`, `internal/peer/ledger_mirror.go`

Committing Peer giữ **ba lớp lưu trữ**, mỗi lớp một mục đích.

## 1. Block Storage — sổ cái lịch sử (`storage/block_storage.go`)

Chuỗi block lưu trong **một file chỉ ghi nối tiếp (append-only)**, mặc định `chain.block`:
- Mỗi block là **một dòng JSON** (newline-delimited).
- Mở file với cờ `O_APPEND | O_CREATE | O_WRONLY` — **không bao giờ** mở chế độ truncate (xóa). Đây là đảm bảo "bất biến ở mức file": dữ liệu cũ không thể bị ghi đè.
- `sync.Mutex` bảo vệ ghi file + metadata.
- Theo dõi: số block đã commit (đánh số từ 1) và hash block cuối (để kiểm tra `prev_hash`).
- Đọc lại: `bufio.Scanner` với buffer tới 16 MB (chứa được block lớn nhiều giao dịch).

> **Vì sao append-only?** Đây là bản chất của blockchain — chỉ thêm, không sửa. File chỉ-thêm cũng thân thiện với đĩa (ghi tuần tự nhanh) và dễ kiểm toán.

## 2. World State — UTXO set (`storage/world_state.go`)

Trạng thái hiện tại (UTXO chưa tiêu) lưu trong **[LevelDB](https://github.com/syndtr/goleveldb)**:
- **Lược đồ key:** `utxo:<txid>:<vout_index>` → giá trị JSON của `VOUT`.
- **`ApplyBlock()` — cập nhật nguyên tử:** với mỗi block, tạo một `leveldb.Batch`:
  - Mỗi `VIN` (input): `batch.Delete(utxo:<txid>:<vout>)` — đánh dấu UTXO đã tiêu.
  - Mỗi `VOUT` (output): `batch.Put(utxo:<txid>:<n>, JSON)` — tạo UTXO mới.
  - `db.Write(batch)` — **một thao tác ghi duy nhất, tất-cả-hoặc-không** (atomic).
- Truy vấn: `GetUTXO`, `AllUTXOs` (quét tiền tố `utxo:`), `UTXOCount`.

> **Vì sao atomic batch quan trọng?** Nếu ghi nửa chừng (xóa input nhưng chưa tạo output) rồi crash, world state sẽ sai. Batch nguyên tử của LevelDB đảm bảo toàn bộ thay đổi của một block hoặc áp dụng hết, hoặc không gì cả.

## 3. PostgreSQL Mirror — tra cứu & kiểm toán (`storage/postgres.go`, `peer/ledger_mirror.go`)

PostgreSQL là **bản sao bất đồng bộ**, **không** nằm trong luồng commit:
- Hai bảng: `commit_peer.ledger` (block) và `commit_peer.ledger_transactions` (giao dịch trong block).
- `SaveBlockWithTransactions()`: chèn block + mọi giao dịch trong **một transaction DB** (một lần commit DB).
- `ON CONFLICT DO NOTHING` → idempotent (chèn lại không lỗi).
- Pool: tối đa 8 kết nối mở, 4 nhàn rỗi.

**Mirror bất đồng bộ (`ledger_mirror.go`):**
- Block được coi là **đã commit ngay khi ghi xong file + world state**; việc ghi PostgreSQL diễn ra sau, ở các worker nền.
- Hàng đợi `ledgerMirrorJob` (mặc định 512, biến `COMMIT_PEER_PG_QUEUE`), số worker qua `COMMIT_PEER_PG_WORKERS` (mặc định 2).
- Nếu hàng đợi đầy → sinh goroutine ghi ngay (non-blocking) để không chặn pipeline.

> **Triết lý:** tốc độ commit blockchain **không** bị phụ thuộc vào tốc độ ghi PostgreSQL. Nếu DB chậm/chết, blockchain vẫn chạy; mirror chỉ là tiện ích tra cứu.

## 4. So sánh ba lớp lưu trữ

| Lớp | Công nghệ | Đặc tính | Ai cần nó |
|-----|-----------|----------|-----------|
| Block storage | File append-only | Bất biến, ghi tuần tự, là sổ cái thật | Tính bất biến & kiểm toán |
| World state | LevelDB | Ghi đè được, atomic batch, truy vấn nhanh | Tra trạng thái hiện tại (UTXO) |
| Mirror | PostgreSQL | Có index, JSONB, async | Explorer & báo cáo |

## 5. Phục vụ truy vấn UTXO cho client (`peer.go`)

Committing Peer mở handler protocol `/commiting-peer/sync/1.0.0`:
- Client gửi `SyncRequest{Address}`.
- Peer quét world state, lọc UTXO theo địa chỉ, trả `SyncResponse{UTXOs[]}`.

Đây là cách ví client (xem [02-orderingservice/07-loadgen-va-client.md](../02-orderingservice/07-loadgen-va-client.md)) biết số dư đã xác nhận của mình.

➡️ Tiếp: [05-metrics.md](05-metrics.md)
