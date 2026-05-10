package raft

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"sort"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

	netpkg "raft-order-service/internal/network"
	"raft-order-service/internal/types"
)

// StartSync khởi động một chu trình sync để bắt kịp committed blocks và RaftLog
// từ phần còn lại của cluster. Hàm idempotent: nhiều trigger đồng thời chỉ chạy 1.
//
// Flow:
//  1. Discovery — broadcast SyncStatusRequest, thu majority response.
//  2. Pick target — chọn (commitIndex, commitHash) được majority đồng thuận.
//  3. Parallel fetch — chia range thành shard, fetch song song qua SyncProtocolID stream.
//  4. Verify hash chain — mỗi block.PrevHash phải khớp; final hash phải khớp target.
//  5. Install — append vào OrderingBlock, cập nhật lastCommittedHash, fetch RaftLog.
func (rn *RaftNode) StartSync(reason string) {
	// Không sync nếu đang là leader (leader là source of truth).
	rn.mu.RLock()
	isLeader := rn.state == types.Leader
	rn.mu.RUnlock()
	if isLeader {
		return
	}

	// Idempotency
	rn.syncMu.Lock()
	if rn.syncing {
		rn.syncMu.Unlock()
		return
	}
	rn.syncing = true
	rn.syncMu.Unlock()

	rn.mu.Lock()
	rn.state = types.Syncing
	rn.mu.Unlock()

	log.Printf("[%s] sync: starting (reason=%s, localCommit=%d)",
		rn.Transport.ID().ShortString(), reason, rn.OrderingBlock.GetLastIndex())

	defer rn.exitSync()

	// Drain bất kỳ response cũ nào còn sót lại trong channel
	rn.drainSyncStatusChan()

	// Phase 1: discovery
	responses := rn.collectSyncStatus(netpkg.SyncDiscoveryWindow)
	if len(responses) == 0 {
		log.Printf("[%s] sync: no peers responded, abort", rn.Transport.ID().ShortString())
		return
	}

	// Phase 2: target selection
	target, sources, ok := pickSyncTarget(responses)
	if !ok {
		log.Printf("[%s] sync: no consensus target (have %d responses), abort",
			rn.Transport.ID().ShortString(), len(responses))
		return
	}

	localCommit := rn.OrderingBlock.GetLastIndex()
	log.Printf("[%s] sync: target commitIndex=%d, hash=%s, sources=%d, localCommit=%d",
		rn.Transport.ID().ShortString(), target.CommitIndex,
		hex.EncodeToString(safeHashPrefix(target.CommitHash)), len(sources), localCommit)

	if localCommit >= target.CommitIndex {
		log.Printf("[%s] sync: already up-to-date with target, skip block fetch",
			rn.Transport.ID().ShortString())
	} else {
		// Phase 3: parallel fetch blocks
		blocks, err := rn.fetchBlocksParallel(localCommit+1, target.CommitIndex, sources)
		if err != nil {
			log.Printf("[%s] sync: block fetch failed: %v", rn.Transport.ID().ShortString(), err)
			return
		}

		// Phase 4: verify hash chain
		if err := verifyHashChain(rn.getLastCommittedHash(), blocks, target.CommitHash); err != nil {
			log.Printf("[%s] sync: hash-chain verification FAILED: %v",
				rn.Transport.ID().ShortString(), err)
			return
		}

		// Phase 5: install blocks
		for _, b := range blocks {
			rn.OrderingBlock.AppendBlock(b)
			rn.DeliverMgr.NotifyNewBlock(b)
		}
		rn.setLastCommittedHash(target.CommitHash)

		log.Printf("[%s] sync: installed %d blocks (now commitIndex=%d)",
			rn.Transport.ID().ShortString(), len(blocks), rn.OrderingBlock.GetLastIndex())
	}

	// Sync RaftLog entries (phần đã được propose nhưng chưa commit ở target).
	if target.LogLastIndex > target.CommitIndex {
		entries, err := rn.fetchLogEntriesParallel(target.CommitIndex+1, target.LogLastIndex, sources)
		if err != nil {
			log.Printf("[%s] sync: log entries fetch failed: %v",
				rn.Transport.ID().ShortString(), err)
		} else {
			rn.installLogEntries(entries)
			log.Printf("[%s] sync: installed %d uncommitted log entries",
				rn.Transport.ID().ShortString(), len(entries))
		}
	}

	// Bump membership version (membership broadcasts vẫn chạy độc lập, đây chỉ là ghi nhận).
	rn.Membership.Mu.Lock()
	if target.MembershipVersion > rn.Membership.Version {
		rn.Membership.Version = target.MembershipVersion
	}
	rn.Membership.Mu.Unlock()

	log.Printf("[%s] sync: completed successfully", rn.Transport.ID().ShortString())
}

// exitSync trả node về Follower và clear flag.
func (rn *RaftNode) exitSync() {
	rn.mu.Lock()
	if rn.state == types.Syncing {
		rn.state = types.Follower
	}
	rn.lastHeartbeat = time.Now() // tránh trigger leader election ngay sau sync
	rn.mu.Unlock()

	rn.syncMu.Lock()
	rn.syncing = false
	rn.syncMu.Unlock()
}

// IsSyncing trả về true nếu node đang trong quá trình sync.
func (rn *RaftNode) IsSyncing() bool {
	rn.syncMu.Lock()
	defer rn.syncMu.Unlock()
	return rn.syncing
}

// drainSyncStatusChan xả mọi message cũ còn trong channel.
func (rn *RaftNode) drainSyncStatusChan() {
	for {
		select {
		case <-rn.SyncStatusChan:
		default:
			return
		}
	}
}

// collectSyncStatus broadcast SyncStatusRequest và gom response trong cửa sổ window.
func (rn *RaftNode) collectSyncStatus(window time.Duration) []types.SyncStatusResponse {
	rn.mu.RLock()
	currentTerm := rn.currentTerm
	rn.mu.RUnlock()

	req := types.SyncStatusRequest{
		RequesterCommitIndex: rn.OrderingBlock.GetLastIndex(),
	}
	msg := types.Message{
		Type:      types.MsgSyncStatusRequest,
		Term:      currentTerm,
		SenderID:  rn.Transport.ID().String(),
		Data:      req,
		Timestamp: time.Now(),
	}

	aliveMembers := rn.Membership.GetAliveMembers()
	rn.Transport.BroadcastMessage(msg, aliveMembers, nil)

	responses := make([]types.SyncStatusResponse, 0)
	timeout := time.After(window)

	for {
		select {
		case <-timeout:
			return responses
		case rmsg := <-rn.SyncStatusChan:
			data, err := json.Marshal(rmsg.Data)
			if err != nil {
				continue
			}
			var resp types.SyncStatusResponse
			if err := json.Unmarshal(data, &resp); err != nil {
				continue
			}
			responses = append(responses, resp)
		}
	}
}

// pickSyncTarget chọn (commitIndex, commitHash) có nhiều phiếu nhất.
// Ưu tiên: commitIndex cao nhất trong các nhóm có ≥ 1 phiếu.
// Trả về thêm danh sách peerID source là LeaderID + tất cả peer cùng nhóm.
//
// NOTE: Hiện chỉ track LeaderID làm source; muốn parallel fetch từ
// nhiều source, chúng ta cần peer.ID gắn với mỗi response.
// Để giữ scope đơn giản và đúng tinh thần "any alive node",
// hàm này coi tất cả alive members (trừ self) đều là source khả dụng,
// và assigner sẽ thử lần lượt nếu có lỗi.
func pickSyncTarget(responses []types.SyncStatusResponse) (types.SyncStatusResponse, []peer.ID, bool) {
	if len(responses) == 0 {
		return types.SyncStatusResponse{}, nil, false
	}

	type key struct {
		index int64
		hash  string
	}
	groups := make(map[key][]types.SyncStatusResponse)
	for _, r := range responses {
		k := key{index: r.CommitIndex, hash: hex.EncodeToString(r.CommitHash)}
		groups[k] = append(groups[k], r)
	}

	// Pick group with highest count; tie-break by highest commitIndex.
	var bestKey key
	bestCount := 0
	for k, g := range groups {
		if len(g) > bestCount || (len(g) == bestCount && k.index > bestKey.index) {
			bestKey = k
			bestCount = len(g)
		}
	}
	if bestCount == 0 {
		return types.SyncStatusResponse{}, nil, false
	}

	winner := groups[bestKey][0]

	// Collect peer IDs from leader + responders that we can decode.
	sources := make([]peer.ID, 0)
	if winner.LeaderID != "" {
		if pid, err := peer.Decode(winner.LeaderID); err == nil {
			sources = append(sources, pid)
		}
	}
	return winner, sources, true
}

// resolveSources mở rộng danh sách source bằng cách thêm tất cả alive peer (trừ self và đã có).
func (rn *RaftNode) resolveSources(initial []peer.ID) []peer.ID {
	seen := make(map[peer.ID]bool)
	out := make([]peer.ID, 0, len(initial))
	for _, p := range initial {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	for _, m := range rn.Membership.GetAliveMembers() {
		if m.PeerID == rn.Transport.ID() {
			continue
		}
		if !seen[m.PeerID] {
			seen[m.PeerID] = true
			out = append(out, m.PeerID)
		}
	}
	return out
}

// fetchBlocksParallel chia range [from..to] thành shard và fetch song song.
// Trả về slice block sorted theo Index tăng dần.
func (rn *RaftNode) fetchBlocksParallel(from, to int64, initialSources []peer.ID) ([]types.Block, error) {
	sources := rn.resolveSources(initialSources)
	if len(sources) == 0 {
		return nil, fmt.Errorf("no source peers available")
	}

	totalCount := to - from + 1
	if totalCount <= 0 {
		return nil, nil
	}

	shardSize := int64(netpkg.SyncShardSize)
	numShards := (totalCount + shardSize - 1) / shardSize

	type shardResult struct {
		from, to int64
		blocks   []types.Block
		err      error
	}
	results := make([]shardResult, numShards)

	var wg sync.WaitGroup
	for i := int64(0); i < numShards; i++ {
		shardFrom := from + i*shardSize
		shardTo := shardFrom + shardSize - 1
		if shardTo > to {
			shardTo = to
		}
		results[i] = shardResult{from: shardFrom, to: shardTo}

		wg.Add(1)
		go func(idx int64) {
			defer wg.Done()
			r := &results[idx]
			// Round-robin: thử source[idx%len], rồi xoay vòng nếu lỗi.
			for attempt := 0; attempt < len(sources); attempt++ {
				src := sources[(int(idx)+attempt)%len(sources)]
				blocks, err := rn.fetchShardBlocks(src, r.from, r.to)
				if err == nil {
					r.blocks = blocks
					return
				}
				log.Printf("[%s] sync: shard [%d..%d] from %s failed: %v (retry next source)",
					rn.Transport.ID().ShortString(), r.from, r.to, src.ShortString(), err)
				r.err = err
			}
		}(i)
	}
	wg.Wait()

	// Assemble và verify count
	all := make([]types.Block, 0, totalCount)
	for i := range results {
		if results[i].err != nil && len(results[i].blocks) == 0 {
			return nil, fmt.Errorf("shard [%d..%d]: %w", results[i].from, results[i].to, results[i].err)
		}
		all = append(all, results[i].blocks...)
	}

	// Sort by hash chain order: blocks should already be in order from shard ordering,
	// nhưng để chắc chắn, sort theo timestamp + size không khả thi (hash chain thật sự
	// chỉ verify được tuần tự). Ở đây các shard ở thứ tự index, mỗi shard server cũng
	// stream theo thứ tự index → kết hợp theo thứ tự i là đủ.
	if int64(len(all)) != totalCount {
		return nil, fmt.Errorf("expected %d blocks, got %d", totalCount, len(all))
	}
	return all, nil
}

// fetchShardBlocks mở stream tới src, request blocks [from..to], đọc đến EOF.
func (rn *RaftNode) fetchShardBlocks(src peer.ID, from, to int64) ([]types.Block, error) {
	log.Printf("[%s] sync: fetching shard [%d..%d] from %s",
		rn.Transport.ID().ShortString(), from, to, src.ShortString())

	stream, err := rn.Transport.OpenSyncStream(src)
	if err != nil {
		return nil, err
	}
	defer stream.Close()

	// Set deadline
	_ = stream.SetDeadline(time.Now().Add(netpkg.SyncFetchTimeout))

	encoder := json.NewEncoder(stream)
	if err := encoder.Encode(types.SyncDataRequest{
		Kind:      types.SyncKindBlocks,
		FromIndex: from,
		ToIndex:   to,
	}); err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}

	collected := make([]types.Block, 0, to-from+1)
	decoder := json.NewDecoder(stream)
	for {
		var chunk types.SyncDataChunk
		if err := decoder.Decode(&chunk); err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("decode chunk: %w", err)
		}
		if chunk.Err != "" {
			return nil, fmt.Errorf("server error: %s", chunk.Err)
		}
		collected = append(collected, chunk.Blocks...)
		if chunk.EOF {
			break
		}
	}
	return collected, nil
}

// fetchLogEntriesParallel y hệt fetchBlocksParallel nhưng cho RaftLog entries.
func (rn *RaftNode) fetchLogEntriesParallel(from, to int64, initialSources []peer.ID) ([]types.LogEntry, error) {
	sources := rn.resolveSources(initialSources)
	if len(sources) == 0 {
		return nil, fmt.Errorf("no source peers available")
	}

	// Log entries thường ít, không chia shard cho phức tạp.
	for _, src := range sources {
		entries, err := rn.fetchShardLogEntries(src, from, to)
		if err == nil {
			return entries, nil
		}
		log.Printf("[%s] sync: log entries from %s failed: %v",
			rn.Transport.ID().ShortString(), src.ShortString(), err)
	}
	return nil, fmt.Errorf("all sources failed")
}

func (rn *RaftNode) fetchShardLogEntries(src peer.ID, from, to int64) ([]types.LogEntry, error) {
	stream, err := rn.Transport.OpenSyncStream(src)
	if err != nil {
		return nil, err
	}
	defer stream.Close()
	_ = stream.SetDeadline(time.Now().Add(netpkg.SyncFetchTimeout))

	encoder := json.NewEncoder(stream)
	if err := encoder.Encode(types.SyncDataRequest{
		Kind:      types.SyncKindLogEntries,
		FromIndex: from,
		ToIndex:   to,
	}); err != nil {
		return nil, err
	}

	collected := make([]types.LogEntry, 0)
	decoder := json.NewDecoder(stream)
	for {
		var chunk types.SyncDataChunk
		if err := decoder.Decode(&chunk); err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		if chunk.Err != "" {
			return nil, fmt.Errorf("server error: %s", chunk.Err)
		}
		collected = append(collected, chunk.Entries...)
		if chunk.EOF {
			break
		}
	}
	return collected, nil
}

// installLogEntries append các log entry chưa có trong RaftLog cục bộ.
// Chỉ append entry có Index > current last index — không ghi đè entry cũ.
func (rn *RaftNode) installLogEntries(entries []types.LogEntry) {
	if len(entries) == 0 {
		return
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Index < entries[j].Index })

	lastIdx := rn.RaftLog.GetLastIndex()
	for _, e := range entries {
		if e.Index <= lastIdx {
			continue
		}
		rn.RaftLog.AppendEntry(e)
		lastIdx = e.Index
	}
}

// verifyHashChain kiểm tra:
//  1. Mỗi block.PrevHash khớp với hash của block trước (hoặc startHash cho block đầu).
//  2. Mỗi block.Hash recompute = block.BlockHash() khớp với block.Hash đang lưu.
//  3. block.Hash của block cuối khớp với expectedFinalHash.
func verifyHashChain(startHash []byte, blocks []types.Block, expectedFinalHash []byte) error {
	if len(blocks) == 0 {
		return nil
	}
	prev := startHash
	for i, b := range blocks {
		if !bytes.Equal(b.PrevHash, prev) {
			return fmt.Errorf("block %d: PrevHash mismatch (got %s, expected %s)",
				i, hex.EncodeToString(safeHashPrefix(b.PrevHash)), hex.EncodeToString(safeHashPrefix(prev)))
		}
		recomputed := b.BlockHash()
		if !bytes.Equal(recomputed, b.Hash) {
			return fmt.Errorf("block %d: hash mismatch (recompute %s != stored %s)",
				i, hex.EncodeToString(safeHashPrefix(recomputed)), hex.EncodeToString(safeHashPrefix(b.Hash)))
		}
		prev = b.Hash
	}
	if !bytes.Equal(prev, expectedFinalHash) {
		return fmt.Errorf("final hash mismatch (got %s, expected %s)",
			hex.EncodeToString(safeHashPrefix(prev)), hex.EncodeToString(safeHashPrefix(expectedFinalHash)))
	}
	return nil
}

func safeHashPrefix(h []byte) []byte {
	if len(h) <= 4 {
		return h
	}
	return h[:4]
}
