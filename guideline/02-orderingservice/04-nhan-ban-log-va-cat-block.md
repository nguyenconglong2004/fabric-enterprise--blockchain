# Ordering Service — Nhân bản log & Cắt block

> Mã nguồn: `orderingservice/source/internal/raft/transaction.go`
> Tài liệu nội bộ: `docs/block-speed-optimization-analysis.md`

Đây là "đường nóng" (hot path) của hệ thống — nơi quyết định throughput. Phần này mô tả cách giao dịch biến thành block đã commit.

## 1. Thu thập giao dịch — TxPool

Leader nhận giao dịch (từ Core Service qua stream endorsement, hoặc submit trực tiếp) và gom vào một **TxPool** (hồ chứa trong RAM). Giao dịch chờ ở đây cho đến khi được gói vào block.

## 2. Cắt block — vòng tự động (`StartAutoProposeBlock()`)

Leader chạy một vòng lặp **vừa theo sự kiện vừa theo timeout (hybrid)**:

```
lặp mãi:
   nếu len(TxPool) >= AutoProposeBlockSize (1000):
        → cắt block NGAY (không chờ)
   ngược lại:
        → chờ tối đa AutoProposeInterval (100ms) rồi xả phần còn lại
   sau khi propose:
        → chờ block trước commit xong (hoặc timeout 10s) mới propose tiếp
```

| Tham số (`config.go`) | Mặc định | Ý nghĩa |
|------------------------|----------|---------|
| `AutoProposeBlockSize` | 1000 tx | Đủ ngần này thì cắt block ngay |
| `AutoProposeInterval` | 100 ms | Nếu chưa đủ, chờ tối đa ngần này rồi cắt |

> **Đánh đổi batch size:** block lớn → ít block hơn, throughput cao hơn (chi phí đồng thuận chia cho nhiều giao dịch). Nhưng nếu tải thấp, phải chờ tới 100ms mới cắt → latency tăng. Cơ chế hybrid cân bằng: tải cao thì cắt ngay theo kích thước, tải thấp thì xả theo thời gian.

Các tham số đều có getter/setter **thread-safe** trong `config.go`, chỉnh được lúc chạy.

## 3. Đề xuất block (`proposeBlockWithTxs()`)

Leader làm:
1. Lấy N giao dịch đầu từ TxPool, tạo `Block`:
   - Tính **MerkleRoot** từ các `txid` (cây Merkle — tóm tắt mọi giao dịch thành một hash).
   - Tính **Hash** = double-SHA256 của header (timestamp, prevHash, merkleRoot, nonce...). Định dạng header kiểu Bitcoin.
   - `PrevHash` = `lastCommittedHash` (nối chuỗi block).
2. Tạo `LogEntry` với `Index`, `PrevLogIndex`, `Term`, gắn block; append vào `RaftLog`.
3. Phát `MsgBlockProposal` (kèm entry) tới mọi Follower.

> **Merkle root** cho phép sau này chứng minh một giao dịch thuộc block mà không cần tải cả block. Tìm hiểu: [Merkle tree](https://en.wikipedia.org/wiki/Merkle_tree). Với block > 1000 tx, việc tính merkle được **song song hóa** để nhanh hơn.

## 4. Chờ ACK đa số (`waitForBlockAcks()`)

Follower nhận proposal, kiểm tra rồi gửi lại `MsgBlockProposalAck`. Leader:
- Đếm ACK; khi đạt **majority** (với cluster 1 node thì chính mình = 1 ACK là đủ — fast path).
- **Tối ưu (OPT-1):** kiểm tra majority **ngay khi nhận mỗi ACK**, không chờ ticker định kỳ → loại bỏ độ trễ ~100ms.
- Timeout dự phòng: 5s.

## 5. Commit block (`commitBlock()`)

Khi đủ ACK:
1. Thêm block vào danh sách `OrderingBlock` (đã commit).
2. Cập nhật `lastCommittedHash = block.Hash` (cho PrevHash của block kế).
3. Gọi `DeliverMgr.NotifyNewBlock()` → đẩy block xuống Committing Peer (xem [05-deliver-va-dong-bo.md](05-deliver-va-dong-bo.md)).
4. Xóa các giao dịch đã đóng block khỏi TxPool.
5. Phát `MsgBlockCommit` cho Follower để chúng cũng commit.

## 6. Phía Follower (`HandleBlockProposal`, `HandleBlockCommit`)

- **`HandleBlockProposal`:** xác minh người gửi là Leader, `term >= currentTerm`, và **kiểm tra liên tục log** (`entry.PrevLogIndex == lastIndex` của mình). Nếu khớp → append entry, gửi ACK. Nếu lệch (có lỗ hổng) → từ chối (sẽ phải sync).
- **`HandleBlockCommit`:** tìm entry theo index trong log, thêm block vào `OrderingBlock`, cập nhật `lastCommittedHash`. Nếu không tìm thấy entry (đã miss proposal) → kích hoạt sync.

## 7. Các tối ưu tốc độ đã làm (theo `docs/block-speed-optimization-analysis.md`)

| Mã | Nội dung | Trạng thái |
|----|----------|------------|
| **OPT-1** | Commit ngay khi đủ majority ACK (không chờ ticker) | ✅ Đã làm |
| **OPT-2** | Propose theo sự kiện (đủ batch → cắt ngay) | ✅ Đã làm |
| **OPT-3** | Stream endorsement bền (mỗi worker giữ 1 stream, gửi nhiều tx) thay vì mở stream mỗi tx | ✅ Đã làm |
| **OPT-5** | Tách kênh riêng cho hot-path (ACK → `BlockAckChan`, không qua kênh chung) | ✅ Đã làm |
| **OPT-8** | Bỏ log theo từng tx trên hot-path, chỉ log theo block | ✅ Đã làm |

Kết quả quan sát: sau OPT-8, mỗi block có thể đạt 1000+ tx ở tải bền vững (trước đó nghẽn ~71 tx/block do tranh chấp khóa khi log từng tx).

**Hạn chế còn lại (cũng theo doc):**
- Vẫn **1 block in-flight** (propose → ACK → commit tuần tự, chưa pipeline) — giới hạn throughput ở mức `1 / RTT-commit`.
- Còn marshal/unmarshal lặp (OPT-4 chưa làm).
- **Không lưu bền** — toàn bộ log/block trong RAM, crash là mất (SYNC-4).

Các hướng cải thiện tiếp xem [cai-thien/01-cai-thien-toc-do.md](../cai-thien/01-cai-thien-toc-do.md).

➡️ Tiếp: [05-deliver-va-dong-bo.md](05-deliver-va-dong-bo.md)
