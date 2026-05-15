package storage

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"commiting-peer/internal/types"
)

// BlockStorage appends serialized blocks to a flat file.
// Each block is stored as a single line of JSON followed by '\n',
// so the file is both append-only and human-readable.
type BlockStorage struct {
	mu             sync.Mutex
	file           *os.File
	lastHash       []byte // hash of last committed block (nil if chain file empty); used for prev_hash checks
	committedCount int64  // blocks in chain file (matches deliver 1-based index of next expected block - 1)
}

// NewBlockStorage opens (or creates) a .block file at path.
// The file is opened with O_APPEND|O_CREATE|O_WRONLY so every
// Write call lands at the end of the file without truncating it.
func NewBlockStorage(path string) (*BlockStorage, error) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("block storage: open %q: %w", path, err)
	}

	bs := &BlockStorage{file: f}
	blocks, err := ReadAll(path)
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("block storage: load existing chain: %w", err)
	}
	if len(blocks) > 0 {
		last := blocks[len(blocks)-1].Hash
		bs.lastHash = append([]byte(nil), last...)
		bs.committedCount = int64(len(blocks))
	}
	return bs, nil
}

// AppendBlock serializes block to JSON and writes it as one line to the file.
func (bs *BlockStorage) AppendBlock(block types.Block) error {
	data, err := json.Marshal(block)
	if err != nil {
		return fmt.Errorf("block storage: marshal: %w", err)
	}
	data = append(data, '\n')

	bs.mu.Lock()
	defer bs.mu.Unlock()

	if _, err := bs.file.Write(data); err != nil {
		return fmt.Errorf("block storage: write: %w", err)
	}
	bs.lastHash = append([]byte(nil), block.Hash...)
	bs.committedCount++
	return nil
}

// Close flushes and closes the underlying file.
func (bs *BlockStorage) Close() error {
	return bs.file.Close()
}

// CommittedTipHash returns the hash of the last block successfully appended to
// the chain file, or nil if the chain is still empty (next block must use an
// all-zero prev_hash / genesis link).
func (bs *BlockStorage) CommittedTipHash() []byte {
	bs.mu.Lock()
	defer bs.mu.Unlock()
	if len(bs.lastHash) == 0 {
		return nil
	}
	out := make([]byte, len(bs.lastHash))
	copy(out, bs.lastHash)
	return out
}

// CommittedBlockCount returns how many blocks are stored in the chain file.
// Subscribe to the orderer with FromIndex = CommittedBlockCount()+1 to avoid
// replaying blocks already on disk.
func (bs *BlockStorage) CommittedBlockCount() int64 {
	bs.mu.Lock()
	defer bs.mu.Unlock()
	return bs.committedCount
}

// ReadAll opens path (read-only) and deserializes every newline-delimited JSON
// block stored there.  Returns nil, nil if the file does not yet exist.
func ReadAll(path string) ([]types.Block, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("block storage: open for read %q: %w", path, err)
	}
	defer f.Close()

	var blocks []types.Block
	scanner := bufio.NewScanner(f)
	// Allow lines up to 16 MB (large blocks with many transactions).
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var b types.Block
		if err := json.Unmarshal(line, &b); err != nil {
			return nil, fmt.Errorf("block storage: parse block: %w", err)
		}
		blocks = append(blocks, b)
	}
	return blocks, scanner.Err()
}
