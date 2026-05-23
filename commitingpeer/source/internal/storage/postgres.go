package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "github.com/lib/pq"
)

// PostgresDB persists committed blocks and transactions for explorer / audit.
// UTXO world state lives in LevelDB (WorldState), not here.
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

// SaveBlockToLedger saves a committed block to ledger (source of truth for block close time).
func (p *PostgresDB) SaveBlockToLedger(
	blockHash string,
	blockNumber int64,
	blockData interface{},
	numTransactions int,
	ledgerCommittedAt time.Time,
) (int64, error) {
	blockJSON, err := json.Marshal(blockData)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal block data: %w", err)
	}

	query := `
		INSERT INTO commit_peer.ledger (block_hash, block_number, block_data, num_transactions, ledger_committed_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (block_hash) DO NOTHING
		RETURNING id
	`

	var blockID int64
	err = p.db.QueryRow(
		query, blockHash, blockNumber, blockJSON, numTransactions, ledgerCommittedAt,
	).Scan(&blockID)
	if err != nil {
		if err == sql.ErrNoRows {
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

// SaveTransactionToLedger saves a transaction row; ledger_committed_at is the SoT end of full flow.
func (p *PostgresDB) SaveTransactionToLedger(
	blockID int64,
	txid string,
	txIndex int,
	txData interface{},
	submittedAtMs int64,
	ledgerCommittedAt time.Time,
) error {
	txJSON, err := json.Marshal(txData)
	if err != nil {
		return fmt.Errorf("failed to marshal transaction data: %w", err)
	}

	var submittedAt interface{}
	if submittedAtMs > 0 {
		submittedAt = time.UnixMilli(submittedAtMs).UTC()
	}

	query := `
		INSERT INTO commit_peer.ledger_transactions (
			block_id, txid, tx_index, tx_data, submitted_at, ledger_committed_at
		)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (block_id, txid) DO NOTHING
	`

	_, err = p.db.Exec(
		query, blockID, txid, txIndex, txJSON, submittedAt, ledgerCommittedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to save transaction: %w", err)
	}

	return nil
}

// Close closes the database connection
func (p *PostgresDB) Close() error {
	return p.db.Close()
}
