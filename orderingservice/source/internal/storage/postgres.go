package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"

	_ "github.com/lib/pq"
)

// PostgresDB handles database operations for Order Service
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

// SaveTransaction saves a transaction to the database
func (p *PostgresDB) SaveTransaction(txid string, txType string, contractName string, functionName string, signature string, clientPubKey string, senderPubKey string, payload string) (int64, error) {
	query := `
		INSERT INTO order_service.transactions (txid, type, contract_name, function_name, signature, client_pubkey, sender_pubkey, payload)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (txid) DO NOTHING
		RETURNING id
	`

	var txID int64
	err := p.db.QueryRow(query, txid, txType, contractName, functionName, signature, clientPubKey, senderPubKey, payload).Scan(&txID)
	if err != nil {
		if err == sql.ErrNoRows {
			// Transaction already exists, get its ID
			err = p.db.QueryRow(`SELECT id FROM order_service.transactions WHERE txid = $1`, txid).Scan(&txID)
			if err != nil {
				return 0, fmt.Errorf("failed to get existing transaction ID: %w", err)
			}
		} else {
			return 0, fmt.Errorf("failed to save transaction: %w", err)
		}
	}

	return txID, nil
}

// SaveBlock saves a block to the database
func (p *PostgresDB) SaveBlock(blockHash string, blockNumber int64, prevHash string, blockData interface{}) (int64, error) {
	blockJSON, err := json.Marshal(blockData)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal block data: %w", err)
	}

	query := `
		INSERT INTO order_service.blocks (block_hash, block_number, prev_hash, block_data)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (block_hash) DO NOTHING
		RETURNING id
	`

	var blockID int64
	err = p.db.QueryRow(query, blockHash, blockNumber, prevHash, blockJSON).Scan(&blockID)
	if err != nil {
		if err == sql.ErrNoRows {
			// Block already exists, get its ID
			err = p.db.QueryRow(`SELECT id FROM order_service.blocks WHERE block_hash = $1`, blockHash).Scan(&blockID)
			if err != nil {
				return 0, fmt.Errorf("failed to get existing block ID: %w", err)
			}
		} else {
			return 0, fmt.Errorf("failed to save block: %w", err)
		}
	}

	return blockID, nil
}

// SaveBlockTransaction creates a relationship between a block and a transaction
func (p *PostgresDB) SaveBlockTransaction(blockID int64, txID int64, txIndex int) error {
	query := `
		INSERT INTO order_service.block_transactions (block_id, transaction_id, tx_index)
		VALUES ($1, $2, $3)
		ON CONFLICT (block_id, transaction_id) DO NOTHING
	`

	_, err := p.db.Exec(query, blockID, txID, txIndex)
	if err != nil {
		return fmt.Errorf("failed to save block-transaction relationship: %w", err)
	}

	return nil
}

// GetBlockByHash retrieves a block from database
func (p *PostgresDB) GetBlockByHash(blockHash string) (map[string]interface{}, error) {
	query := `SELECT block_data FROM order_service.blocks WHERE block_hash = $1`

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

// GetTransactionByID retrieves a transaction from database
func (p *PostgresDB) GetTransactionByID(txid string) (map[string]interface{}, error) {
	query := `SELECT txid, type, contract_name, function_name, signature, client_pubkey, sender_pubkey, payload FROM order_service.transactions WHERE txid = $1`

	var tx struct {
		Txid         string
		Type         string
		ContractName sql.NullString
		FunctionName sql.NullString
		Signature    string
		ClientPubKey string
		SenderPubKey string
		Payload      sql.NullString
	}

	err := p.db.QueryRow(query, txid).Scan(&tx.Txid, &tx.Type, &tx.ContractName, &tx.FunctionName, &tx.Signature, &tx.ClientPubKey, &tx.SenderPubKey, &tx.Payload)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("transaction not found: %s", txid)
		}
		return nil, fmt.Errorf("failed to get transaction: %w", err)
	}

	result := map[string]interface{}{
		"txid":          tx.Txid,
		"type":          tx.Type,
		"signature":     tx.Signature,
		"client_pubkey": tx.ClientPubKey,
		"sender_pubkey": tx.SenderPubKey,
	}

	if tx.ContractName.Valid {
		result["contract_name"] = tx.ContractName.String
	}
	if tx.FunctionName.Valid {
		result["function_name"] = tx.FunctionName.String
	}
	if tx.Payload.Valid {
		result["payload"] = tx.Payload.String
	}

	return result, nil
}

// Close closes the database connection
func (p *PostgresDB) Close() error {
	return p.db.Close()
}
