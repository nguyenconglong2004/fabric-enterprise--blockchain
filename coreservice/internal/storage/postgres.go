package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	_ "github.com/lib/pq"
)

func normalizeBlockHash(blockHash string) string {
	h := strings.TrimSpace(strings.ToLower(blockHash))
	h = strings.TrimPrefix(h, "0x")
	return h
}

// PostgresDB handles database operations
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

// SaveContract saves a smart contract to database. payloadSchemaJSON may be nil; if nil on update, existing schema is kept.
func (p *PostgresDB) SaveContract(contractName string, contractCode []byte, payloadSchemaJSON []byte) error {
	query := `
		INSERT INTO core_service.smart_contracts (contract_name, contract_code, payload_schema)
		VALUES ($1, $2, $3)
		ON CONFLICT (contract_name) DO UPDATE SET
			contract_code = EXCLUDED.contract_code,
			payload_schema = COALESCE(EXCLUDED.payload_schema, core_service.smart_contracts.payload_schema),
			updated_at = CURRENT_TIMESTAMP
	`

	_, err := p.db.Exec(query, contractName, contractCode, nullIfEmpty(payloadSchemaJSON))
	if err != nil {
		return fmt.Errorf("failed to save contract: %w", err)
	}

	return nil
}

func nullIfEmpty(b []byte) interface{} {
	if len(b) == 0 {
		return nil
	}
	return b
}

// GetContractPayloadSchema returns raw JSON for UI (ContractSchema shape) or nil if unset / missing column.
func (p *PostgresDB) GetContractPayloadSchema(contractName string) ([]byte, error) {
	query := `SELECT payload_schema FROM core_service.smart_contracts WHERE contract_name = $1`

	var raw []byte
	err := p.db.QueryRow(query, contractName).Scan(&raw)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	if len(raw) == 0 {
		return nil, nil
	}
	return raw, nil
}

// GetContract retrieves a smart contract from database
func (p *PostgresDB) GetContract(contractName string) ([]byte, error) {
	query := `SELECT contract_code FROM core_service.smart_contracts WHERE contract_name = $1`

	var code []byte
	err := p.db.QueryRow(query, contractName).Scan(&code)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("contract not found: %s", contractName)
		}
		return nil, fmt.Errorf("failed to get contract: %w", err)
	}

	return code, nil
}

// SaveBlock saves a block to database
func (p *PostgresDB) SaveBlock(blockHash string, blockNumber int64, prevHash string, blockData interface{}) error {
	blockJSON, err := json.Marshal(blockData)
	if err != nil {
		return fmt.Errorf("failed to marshal block data: %w", err)
	}

	query := `
		INSERT INTO order_service.blocks (block_hash, block_number, prev_hash, block_data, num_transactions)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (block_hash) DO NOTHING
	`

	var numTx int
	if blockMap, ok := blockData.(map[string]interface{}); ok {
		if txs, exists := blockMap["transactions"]; exists {
			numTx = len(txs.([]interface{}))
		}
	}

	_, err = p.db.Exec(query, blockHash, blockNumber, prevHash, blockJSON, numTx)
	if err != nil {
		return fmt.Errorf("failed to save block: %w", err)
	}

	return nil
}

// GetBlockByHash retrieves a block by hash
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

// GetCommittedBlockByHash retrieves a committed block by hash from commit_peer ledger.
func (p *PostgresDB) GetCommittedBlockByHash(blockHash string) (map[string]interface{}, error) {
	h := normalizeBlockHash(blockHash)
	if h == "" {
		return nil, fmt.Errorf("committed block not found: empty hash")
	}
	query := `SELECT block_data FROM commit_peer.ledger WHERE block_hash = $1`

	var blockData []byte
	err := p.db.QueryRow(query, h).Scan(&blockData)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("committed block not found: %s", blockHash)
		}
		return nil, fmt.Errorf("failed to get committed block: %w", err)
	}

	var block map[string]interface{}
	if err := json.Unmarshal(blockData, &block); err != nil {
		return nil, fmt.Errorf("failed to unmarshal committed block: %w", err)
	}

	return block, nil
}

// GetBlockByNumber retrieves a block by number
func (p *PostgresDB) GetBlockByNumber(blockNumber int64) (map[string]interface{}, error) {
	query := `SELECT block_data FROM order_service.blocks WHERE block_number = $1`

	var blockData []byte
	err := p.db.QueryRow(query, blockNumber).Scan(&blockData)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("block not found: %d", blockNumber)
		}
		return nil, fmt.Errorf("failed to get block: %w", err)
	}

	var block map[string]interface{}
	if err := json.Unmarshal(blockData, &block); err != nil {
		return nil, fmt.Errorf("failed to unmarshal block: %w", err)
	}

	return block, nil
}

// SaveTransaction saves a transaction to database
func (p *PostgresDB) SaveTransaction(txid string, txType string, contractName, functionName string, signature, clientPubKey, senderPubKey string, txData interface{}) error {
	txJSON, err := json.Marshal(txData)
	if err != nil {
		return fmt.Errorf("failed to marshal transaction: %w", err)
	}

	query := `
		INSERT INTO order_service.transactions (txid, tx_type, contract_name, function_name, signature, client_pubkey, sender_pubkey, tx_data)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (txid) DO NOTHING
	`

	_, err = p.db.Exec(query, txid, txType, contractName, functionName, signature, clientPubKey, senderPubKey, txJSON)
	if err != nil {
		return fmt.Errorf("failed to save transaction: %w", err)
	}

	return nil
}

// GetTransaction retrieves a transaction
func (p *PostgresDB) GetTransaction(txid string) (map[string]interface{}, error) {
	query := `SELECT tx_data FROM order_service.transactions WHERE txid = $1`

	var txData []byte
	err := p.db.QueryRow(query, txid).Scan(&txData)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("transaction not found: %s", txid)
		}
		return nil, fmt.Errorf("failed to get transaction: %w", err)
	}

	var tx map[string]interface{}
	if err := json.Unmarshal(txData, &tx); err != nil {
		return nil, fmt.Errorf("failed to unmarshal transaction: %w", err)
	}

	return tx, nil
}

// ListCommittedBlocks returns latest committed blocks from commit_peer ledger.
func (p *PostgresDB) ListCommittedBlocks(limit int) ([]map[string]interface{}, error) {
	if limit <= 0 {
		limit = 20
	}

	query := `
		SELECT block_data
		FROM commit_peer.ledger
		ORDER BY block_number DESC
		LIMIT $1
	`

	rows, err := p.db.Query(query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to list committed blocks: %w", err)
	}
	defer rows.Close()

	blocks := make([]map[string]interface{}, 0)
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, fmt.Errorf("failed to scan committed block: %w", err)
		}

		var block map[string]interface{}
		if err := json.Unmarshal(raw, &block); err != nil {
			return nil, fmt.Errorf("failed to unmarshal committed block: %w", err)
		}
		blocks = append(blocks, block)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed iterating committed blocks: %w", err)
	}

	return blocks, nil
}

// ListCommittedTransactions returns latest committed transactions from commit_peer ledger.
func (p *PostgresDB) ListCommittedTransactions(limit int) ([]map[string]interface{}, error) {
	if limit <= 0 {
		limit = 50
	}

	query := `
		SELECT lt.tx_data, l.block_hash, l.block_number
		FROM commit_peer.ledger_transactions lt
		JOIN commit_peer.ledger l ON lt.block_id = l.id
		ORDER BY l.block_number DESC, lt.tx_index ASC
		LIMIT $1
	`

	rows, err := p.db.Query(query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to list committed transactions: %w", err)
	}
	defer rows.Close()

	txs := make([]map[string]interface{}, 0)
	for rows.Next() {
		var (
			raw         []byte
			blockHash   string
			blockNumber int64
		)
		if err := rows.Scan(&raw, &blockHash, &blockNumber); err != nil {
			return nil, fmt.Errorf("failed to scan committed transaction: %w", err)
		}

		var tx map[string]interface{}
		if err := json.Unmarshal(raw, &tx); err != nil {
			return nil, fmt.Errorf("failed to unmarshal committed transaction: %w", err)
		}
		tx["block_hash"] = blockHash
		tx["block_number"] = blockNumber
		txs = append(txs, tx)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed iterating committed transactions: %w", err)
	}

	return txs, nil
}

// Close closes the database connection
func (p *PostgresDB) Close() error {
	return p.db.Close()
}
