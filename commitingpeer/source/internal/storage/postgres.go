package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"

	_ "github.com/lib/pq"
)

// PostgresDB handles database operations for CommittingPeer
type PostgresDB struct {
	db *sql.DB
}

// NewPostgresDB creates a new database connection
func NewPostgresDB(connStr string) (*PostgresDB, error) {
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &PostgresDB{db: db}, nil
}

// SaveBlockToLedger saves a committed block to ledger
func (p *PostgresDB) SaveBlockToLedger(blockHash string, blockNumber int64, blockData interface{}, numTransactions int) (int64, error) {
	blockJSON, err := json.Marshal(blockData)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal block data: %w", err)
	}

	query := `
		INSERT INTO commit_peer.ledger (block_hash, block_number, block_data, num_transactions)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (block_hash) DO NOTHING
		RETURNING id
	`

	var blockID int64
	err = p.db.QueryRow(query, blockHash, blockNumber, blockJSON, numTransactions).Scan(&blockID)
	if err != nil {
		if err == sql.ErrNoRows {
			// Block already exists, get its ID
			err = p.db.QueryRow(`SELECT id FROM commit_peer.ledger WHERE block_hash = $1`, blockHash).Scan(&blockID)
			if err != nil {
				return 0, fmt.Errorf("failed to get existing block ID: %w", err)
			}
		} else {
			return 0, fmt.Errorf("failed to save block: %w", err)
		}
	}

	return blockID, nil
}

// SaveTransactionToLedger saves a transaction to ledger
func (p *PostgresDB) SaveTransactionToLedger(blockID int64, txid string, txIndex int, txData interface{}) error {
	txJSON, err := json.Marshal(txData)
	if err != nil {
		return fmt.Errorf("failed to marshal transaction data: %w", err)
	}

	query := `
		INSERT INTO commit_peer.ledger_transactions (block_id, txid, tx_index, tx_data)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (block_id, txid) DO NOTHING
	`

	_, err = p.db.Exec(query, blockID, txid, txIndex, txJSON)
	if err != nil {
		return fmt.Errorf("failed to save transaction: %w", err)
	}

	return nil
}

// SaveWorldState saves or updates world state
func (p *PostgresDB) SaveWorldState(key string, value []byte) error {
	query := `
		INSERT INTO commit_peer.world_state (key, value)
		VALUES ($1, $2)
		ON CONFLICT (key) DO UPDATE SET
			value = $2,
			updated_at = CURRENT_TIMESTAMP
	`

	_, err := p.db.Exec(query, key, value)
	if err != nil {
		return fmt.Errorf("failed to save world state: %w", err)
	}

	return nil
}

// GetWorldState retrieves a value from world state
func (p *PostgresDB) GetWorldState(key string) ([]byte, error) {
	query := `SELECT value FROM commit_peer.world_state WHERE key = $1`

	var value []byte
	err := p.db.QueryRow(query, key).Scan(&value)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("key not found: %s", key)
		}
		return nil, fmt.Errorf("failed to get world state: %w", err)
	}

	return value, nil
}

// GetBlockByHash retrieves a block from ledger
func (p *PostgresDB) GetBlockByHash(blockHash string) (map[string]interface{}, error) {
	query := `SELECT block_data FROM commit_peer.ledger WHERE block_hash = $1`

	var blockData []byte
	err := p.db.QueryRow(query, blockHash).Scan(&blockData)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("block not found: %s", blockHash)
		}
		return nil, fmt.Errorf("failed to get block: %w", err)
	}

	var block map[string]interface{}
	if err := json.Unmarshal(blockData, &block); err != nil {
		return nil, fmt.Errorf("failed to unmarshal block: %w", err)
	}

	return block, nil
}

// Close closes the database connection
func (p *PostgresDB) Close() error {
	return p.db.Close()
}
