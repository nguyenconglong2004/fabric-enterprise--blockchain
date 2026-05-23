package raft

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

	"raft-order-service/internal/types"
)

// StartSync starts a sync cycle to catch up committed blocks and RaftLog from the cluster.
// Idempotent: multiple concurrent triggers run only one.
func (rn *RaftNode) StartSync(reason string) {
	rn.mu.RLock()
	isLeader := rn.state == types.Leader
	rn.mu.RUnlock()
	if isLeader {
		return
	}

	rn.syncMu.Lock()
	if rn.syncing {
		rn.syncMu.Unlock()
		return
	}
	rn.syncing = true
	rn.syncMu.Unlock()

	rn.mu.Lock()
	oldState := rn.state
	rn.state = types.Syncing
	rn.mu.Unlock()

	if oldState != types.Syncing {
		rn.Emitter.StateChanged(rn.Transport.ID(), oldState, types.Syncing)
	}

	rn.Logger.Printf("[%s] sync: starting (reason=%s, localCommit=%d)",
		rn.Transport.ID().ShortString(), reason, rn.OrderingBlock.GetLastIndex())

	defer rn.exitSync()

	rn.drainSyncStatusChan()

	// Phase 1: discovery
	responses := rn.collectSyncStatus(rn.Config.GetSyncDiscoveryWindow())
	if len(responses) == 0 {
		rn.Logger.Printf("[%s] sync: no peers responded, abort", rn.Transport.ID().ShortString())
		return
	}

	// Phase 2: target selection
	target, sources, ok := pickSyncTarget(responses)
	if !ok {
		rn.Logger.Printf("[%s] sync: no consensus target (have %d responses), abort",
			rn.Transport.ID().ShortString(), len(responses))
		return
	}

	localCommit := rn.OrderingBlock.GetLastIndex()
	rn.Logger.Printf("[%s] sync: target commitIndex=%d, hash=%s, sources=%d, localCommit=%d",
		rn.Transport.ID().ShortString(), target.CommitIndex,
		hex.EncodeToString(safeHashPrefix(target.CommitHash)), len(sources), localCommit)

	if localCommit >= target.CommitIndex {
		rn.Logger.Printf("[%s] sync: already up-to-date with target, skip block fetch",
			rn.Transport.ID().ShortString())
	} else {
		// Phase 3: parallel fetch blocks
		blocks, err := rn.fetchBlocksParallel(localCommit+1, target.CommitIndex, sources)
		if err != nil {
			rn.Logger.Printf("[%s] sync: block fetch failed: %v", rn.Transport.ID().ShortString(), err)
			return
		}

		// Phase 4: verify hash chain
		if err := verifyHashChain(rn.getLastCommittedHash(), blocks, target.CommitHash); err != nil {
			rn.Logger.Printf("[%s] sync: hash-chain verification FAILED: %v",
				rn.Transport.ID().ShortString(), err)
			return
		}

		// Phase 5: install blocks
		for _, b := range blocks {
			rn.OrderingBlock.AppendBlock(b)
			rn.DeliverMgr.NotifyNewBlock(b)
		}
		rn.setLastCommittedHash(target.CommitHash)

		rn.Logger.Printf("[%s] sync: installed %d blocks (now commitIndex=%d)",
			rn.Transport.ID().ShortString(), len(blocks), rn.OrderingBlock.GetLastIndex())
	}

	// Sync RaftLog entries
	if target.LogLastIndex > target.CommitIndex {
		entries, err := rn.fetchLogEntriesParallel(target.CommitIndex+1, target.LogLastIndex, sources)
		if err != nil {
			rn.Logger.Printf("[%s] sync: log entries fetch failed: %v",
				rn.Transport.ID().ShortString(), err)
		} else {
			rn.installLogEntries(entries)
			rn.Logger.Printf("[%s] sync: installed %d uncommitted log entries",
				rn.Transport.ID().ShortString(), len(entries))
		}
	}

	rn.Membership.Mu.Lock()
	if target.MembershipVersion > rn.Membership.Version {
		rn.Membership.Version = target.MembershipVersion
	}
	rn.Membership.Mu.Unlock()

	rn.Logger.Printf("[%s] sync: completed successfully", rn.Transport.ID().ShortString())
}

// exitSync transitions node back to Follower and clears syncing flag.
func (rn *RaftNode) exitSync() {
	rn.mu.Lock()
	oldState := rn.state
	if rn.state == types.Syncing {
		rn.state = types.Follower
	}
	rn.lastHeartbeat = time.Now()
	rn.mu.Unlock()

	if oldState == types.Syncing {
		rn.Emitter.StateChanged(rn.Transport.ID(), types.Syncing, types.Follower)
	}

	rn.syncMu.Lock()
	rn.syncing = false
	rn.syncMu.Unlock()
}

// IsSyncing returns true if the node is currently syncing.
func (rn *RaftNode) IsSyncing() bool {
	rn.syncMu.Lock()
	defer rn.syncMu.Unlock()
	return rn.syncing
}

// drainSyncStatusChan drains stale messages from the channel.
func (rn *RaftNode) drainSyncStatusChan() {
	for {
		select {
		case <-rn.SyncStatusChan:
		default:
			return
		}
	}
}

// collectSyncStatus broadcasts SyncStatusRequest and collects responses within the window.
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

// pickSyncTarget selects the (commitIndex, commitHash) with the most votes.
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

	sources := make([]peer.ID, 0)
	if winner.LeaderID != "" {
		if pid, err := peer.Decode(winner.LeaderID); err == nil {
			sources = append(sources, pid)
		}
	}
	return winner, sources, true
}

// resolveSources expands the source list with all alive peers (except self).
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

// fetchBlocksParallel divides range [from..to] into shards and fetches in parallel.
func (rn *RaftNode) fetchBlocksParallel(from, to int64, initialSources []peer.ID) ([]types.Block, error) {
	sources := rn.resolveSources(initialSources)
	if len(sources) == 0 {
		return nil, fmt.Errorf("no source peers available")
	}

	totalCount := to - from + 1
	if totalCount <= 0 {
		return nil, nil
	}

	shardSize := int64(rn.Config.GetSyncShardSize())
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
			for attempt := 0; attempt < len(sources); attempt++ {
				src := sources[(int(idx)+attempt)%len(sources)]
				blocks, err := rn.fetchShardBlocks(src, r.from, r.to)
				if err == nil {
					r.blocks = blocks
					return
				}
				rn.Logger.Printf("[%s] sync: shard [%d..%d] from %s failed: %v (retry next source)",
					rn.Transport.ID().ShortString(), r.from, r.to, src.ShortString(), err)
				r.err = err
			}
		}(i)
	}
	wg.Wait()

	all := make([]types.Block, 0, totalCount)
	for i := range results {
		if results[i].err != nil && len(results[i].blocks) == 0 {
			return nil, fmt.Errorf("shard [%d..%d]: %w", results[i].from, results[i].to, results[i].err)
		}
		all = append(all, results[i].blocks...)
	}

	if int64(len(all)) != totalCount {
		return nil, fmt.Errorf("expected %d blocks, got %d", totalCount, len(all))
	}
	return all, nil
}

// fetchShardBlocks opens a stream to src and requests blocks [from..to].
func (rn *RaftNode) fetchShardBlocks(src peer.ID, from, to int64) ([]types.Block, error) {
	rn.Logger.Printf("[%s] sync: fetching shard [%d..%d] from %s",
		rn.Transport.ID().ShortString(), from, to, src.ShortString())

	stream, err := rn.Transport.OpenSyncStream(src)
	if err != nil {
		return nil, err
	}
	defer stream.Close()

	_ = stream.SetDeadline(time.Now().Add(rn.Config.GetSyncFetchTimeout()))

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

// fetchLogEntriesParallel fetches RaftLog entries.
func (rn *RaftNode) fetchLogEntriesParallel(from, to int64, initialSources []peer.ID) ([]types.LogEntry, error) {
	sources := rn.resolveSources(initialSources)
	if len(sources) == 0 {
		return nil, fmt.Errorf("no source peers available")
	}

	for _, src := range sources {
		entries, err := rn.fetchShardLogEntries(src, from, to)
		if err == nil {
			return entries, nil
		}
		rn.Logger.Printf("[%s] sync: log entries from %s failed: %v",
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
	_ = stream.SetDeadline(time.Now().Add(rn.Config.GetSyncFetchTimeout()))

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

// installLogEntries appends new log entries that don't already exist locally.
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

// verifyHashChain verifies PrevHash chain + final hash.
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
