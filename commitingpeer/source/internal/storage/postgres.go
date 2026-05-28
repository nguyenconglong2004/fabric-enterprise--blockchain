package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "github.com/lib/pq"
)

// PostgresDB persists committed blocks and transactions for explorer / audit.
type PostgresDB struct {
	db *sql.DB
}

// LedgerTxRow is one transaction row for batch insert.
type LedgerTxRow struct {
	Txid    string
	TxIndex int
	TxData  []byte
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
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(4)
	return &PostgresDB{db: db}, nil
}

// SaveBlockWithTransactions inserts block + all txs in one DB transaction (one round-trip commit).
func (p *PostgresDB) SaveBlockWithTransactions(
	blockHash string,
	blockNumber int64,
	blockData interface{},
	txRows []LedgerTxRow,
	committedAt time.Time,
) error {
	if committedAt.IsZero() {
		committedAt = time.Now().UTC()
	}
	blockJSON, err := json.Marshal(blockData)
	if err != nil {
		return fmt.Errorf("marshal block: %w", err)
	}

	dbTx, err := p.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer dbTx.Rollback()

	var blockID int64
	err = dbTx.QueryRow(`
		INSERT INTO commit_peer.ledger (block_hash, block_number, block_data, num_transactions, committed_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (block_hash) DO NOTHING
		RETURNING id
	`, blockHash, blockNumber, blockJSON, len(txRows), committedAt).Scan(&blockID)
	if err != nil {
		if err == sql.ErrNoRows {
			err = dbTx.QueryRow(`SELECT id FROM commit_peer.ledger WHERE block_hash = $1`, blockHash).Scan(&blockID)
			if err != nil {
				return fmt.Errorf("get existing block id: %w", err)
			}
		} else {
			return fmt.Errorf("insert block: %w", err)
		}
	}

	if len(txRows) == 0 {
		return dbTx.Commit()
	}

	stmt, err := dbTx.Prepare(`
		INSERT INTO commit_peer.ledger_transactions (block_id, txid, tx_index, tx_data)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (block_id, txid) DO NOTHING
	`)
	if err != nil {
		return fmt.Errorf("prepare tx insert: %w", err)
	}
	defer stmt.Close()

	for _, row := range txRows {
		if _, err := stmt.Exec(blockID, row.Txid, row.TxIndex, row.TxData); err != nil {
			return fmt.Errorf("insert tx %s: %w", row.Txid, err)
		}
	}

	return dbTx.Commit()
}

// Close closes the database connection
func (p *PostgresDB) Close() error {
	return p.db.Close()
}
