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
	mu   sync.Mutex
	file *os.File
}

// NewBlockStorage opens (or creates) a .block file at path.
// The file is opened with O_APPEND|O_CREATE|O_WRONLY so every
// Write call lands at the end of the file without truncating it.
func NewBlockStorage(path string) (*BlockStorage, error) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("block storage: open %q: %w", path, err)
	}
	return &BlockStorage{file: f}, nil
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
	return nil
}

// Close flushes and closes the underlying file.
func (bs *BlockStorage) Close() error {
	return bs.file.Close()
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
