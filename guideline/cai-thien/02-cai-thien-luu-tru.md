# Cải thiện Lưu trữ (Storage)

> Liên quan: [02-orderingservice/02-raft-tong-quan.md](../02-orderingservice/02-raft-tong-quan.md), [03-commitingpeer/04-luu-tru-va-worldstate.md](../03-commitingpeer/04-luu-tru-va-worldstate.md)

## 1. 🔴 Raft không lưu bền (RAM-only) — rủi ro lớn nhất

**Hiện tại:** Ordering Service giữ **toàn bộ** `currentTerm`, `RaftLog`, `OrderingBlock` **trong RAM**. Tài liệu nội bộ gọi đây là **SYNC-4**: crash = mất sạch.

**Vì sao nghiêm trọng:**
- Raft chuẩn **bắt buộc** ghi bền (persist) `currentTerm`, `votedFor`, và log entry **xuống đĩa trước khi** phản hồi. Không có điều này, sau khi restart một node có thể bầu sai, hoặc cluster mất block đã "commit".
- Đây vừa là vấn đề **lưu trữ** vừa là vấn đề **an toàn đồng thuận** (xem [03-cai-thien-bao-mat.md](03-cai-thien-bao-mat.md)).

**Khắc phục:**
- Thêm **Write-Ahead Log (WAL)**: ghi nối tiếp log entry + term xuống đĩa trước khi ACK. Tham khảo: [WAL](https://en.wikipedia.org/wiki/Write-ahead_logging), cách [etcd lưu Raft](https://etcd.io/docs/latest/learning/persistent-storage-files/).
- Dùng một embedded store cho metadata Raft (vd. [bbolt](https://github.com/etcd-io/bbolt) hoặc LevelDB) cho `term`/`votedFor`/`commitIndex`.
- **Snapshot + compaction:** định kỳ chụp world state và cắt bớt log cũ để log không phình vô hạn (Raft snapshotting).

**Phụ thuộc:** đây là tiền đề cho pipeline an toàn (#1 của [01-cai-thien-toc-do.md](01-cai-thien-toc-do.md)).

---

## 2. 🟢 File block một-tệp (`chain.block`)

**Hiện tại:** toàn bộ chuỗi block ghi vào **một file** JSON-dòng append-only ở Committing Peer. Đọc lại quét tuần tự (buffer 16 MB/dòng).

**Hạn chế khi lớn:**
- Một file khổng lồ khó sao lưu/di chuyển; đọc một block cũ phải quét.
- Không có index theo block number/hash trên file (phải dựa PostgreSQL mirror).

**Khắc phục:**
- **Phân mảnh (segment/rolling files):** mỗi N block hoặc mỗi kích thước → một file (giống WAL segment của Kafka/etcd).
- **File index** (block number → offset) để seek nhanh.
- **Nén** các segment cũ (gzip/zstd) vì JSON nén rất tốt.
- Cân nhắc định dạng nhị phân thay JSON để giảm dung lượng (JSON tốn byte cho key lặp lại).

---

## 3. 🟢 World State (LevelDB) — nén & dọn

**Hiện tại:** UTXO set lưu LevelDB với key `utxo:<txid>:<vout>`. Ghi/xóa theo batch nguyên tử (tốt).

**Khắc phục:**
- **Compaction chủ động:** LevelDB tự nén nhưng có thể cấu hình để giảm khuếch đại ghi (write amplification) cho khối lượng UTXO lớn.
- **Tách cold/hot:** UTXO ít truy cập có thể chuyển sang lưu trữ rẻ hơn.
- **Tuning options:** kích thước write buffer, block cache — đo theo khối lượng thật.

---

## 4. 🟠 PostgreSQL mirror — tăng trưởng & truy vấn

**Hiện tại:** mirror async ghi `commit_peer.ledger(_transactions)`; benchmark query quét theo thời gian.

**Khắc phục:**
- **Phân vùng theo thời gian (partitioning):** chia `ledger`/`ledger_transactions` theo ngày/tuần để query cửa sổ nhanh và xóa dữ liệu cũ dễ (`DROP PARTITION`). Tham khảo: [PostgreSQL partitioning](https://www.postgresql.org/docs/current/ddl-partitioning.html).
- **Chính sách lưu giữ (retention):** mirror không cần giữ vô hạn — xóa/đẩy lưu trữ lạnh dữ liệu cũ.
- **`COPY` thay `INSERT`** cho mirror khối lượng lớn.
- **Index phù hợp:** đã có index trên `committed_at`, `txid`, `block_number` — kiểm tra query benchmark thực sự dùng index (EXPLAIN).

---

## 5. 🟢 Sao lưu & phục hồi (Backup / Recovery)

**Hiện tại:** không thấy cơ chế backup chính thức cho `chain.block` + LevelDB.

**Khắc phục:**
- Quy trình **snapshot nhất quán** world state + chuỗi block.
- Tài liệu phục hồi: từ file block có thể **dựng lại** world state (replay) — nên có công cụ replay chính thức.
- Sao lưu định kỳ ra ngoài máy chủ.

---

## Bảng tóm tắt

| Mức | Việc | Lợi ích |
|-----|------|---------|
| 🔴 | WAL + persist Raft (term/log/commitIndex) | Không mất dữ liệu khi crash; bật pipeline an toàn |
| 🔴 | Snapshot + log compaction | Log không phình vô hạn |
| 🟠 | Partition + retention PostgreSQL | Query nhanh, dọn dữ liệu dễ |
| 🟢 | Segment + nén + index file block | Quản lý chuỗi lớn, tiết kiệm đĩa |
| 🟢 | Tuning LevelDB | Giảm write amplification |
| 🟢 | Backup/replay tooling | Khôi phục sau sự cố |
