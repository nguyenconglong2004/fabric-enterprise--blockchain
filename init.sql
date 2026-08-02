-- Create schemas
CREATE SCHEMA IF NOT EXISTS core_service;
CREATE SCHEMA IF NOT EXISTS order_service;
CREATE SCHEMA IF NOT EXISTS commit_peer;
CREATE SCHEMA IF NOT EXISTS wallet;

-- Core Service: Smart Contracts
CREATE TABLE IF NOT EXISTS core_service.smart_contracts (
    id SERIAL PRIMARY KEY,
    contract_name VARCHAR(255) NOT NULL UNIQUE,
    contract_code BYTEA NOT NULL,
    payload_schema JSONB,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_contract_name ON core_service.smart_contracts(contract_name);

-- Order Service: Transactions
CREATE TABLE IF NOT EXISTS order_service.transactions (
    id SERIAL PRIMARY KEY,
    txid VARCHAR(255) NOT NULL UNIQUE,
    tx_data JSONB NOT NULL,
    tx_type VARCHAR(50), -- 'UTXO' or 'SMART_CONTRACT'
    contract_name VARCHAR(255),
    function_name VARCHAR(255),
    signature VARCHAR(255),
    client_pubkey VARCHAR(255),
    sender_pubkey VARCHAR(255),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_txid ON order_service.transactions(txid);
CREATE INDEX idx_tx_type ON order_service.transactions(tx_type);
CREATE INDEX idx_contract_name ON order_service.transactions(contract_name);

-- Order Service: Blocks
CREATE TABLE IF NOT EXISTS order_service.blocks (
    id SERIAL PRIMARY KEY,
    block_hash VARCHAR(255) NOT NULL UNIQUE,
    block_number BIGINT NOT NULL UNIQUE,
    prev_hash VARCHAR(255),
    block_data JSONB NOT NULL,
    num_transactions INT,
    timestamp TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_block_hash ON order_service.blocks(block_hash);
CREATE INDEX idx_block_number ON order_service.blocks(block_number);

-- Order Service: Block Transactions (M-to-M relationship)
CREATE TABLE IF NOT EXISTS order_service.block_transactions (
    id SERIAL PRIMARY KEY,
    block_id INT NOT NULL,
    txid VARCHAR(255) NOT NULL,
    tx_index INT,
    FOREIGN KEY (block_id) REFERENCES order_service.blocks(id) ON DELETE CASCADE,
    UNIQUE(block_id, txid)
);

CREATE INDEX idx_block_txid ON order_service.block_transactions(block_id);
CREATE INDEX idx_block_tx_txid ON order_service.block_transactions(txid);

-- Commit Peer: Ledger (committed blocks)
CREATE TABLE IF NOT EXISTS commit_peer.ledger (
    id SERIAL PRIMARY KEY,
    block_hash VARCHAR(255) NOT NULL UNIQUE,
    block_number BIGINT NOT NULL,
    block_data JSONB NOT NULL,
    num_transactions INT,
    committed_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_ledger_hash ON commit_peer.ledger(block_hash);
CREATE INDEX idx_ledger_number ON commit_peer.ledger(block_number);

-- Commit Peer: Ledger Transactions
CREATE TABLE IF NOT EXISTS commit_peer.ledger_transactions (
    id SERIAL PRIMARY KEY,
    block_id INT NOT NULL,
    txid VARCHAR(255) NOT NULL,
    tx_index INT,
    tx_data JSONB NOT NULL,
    FOREIGN KEY (block_id) REFERENCES commit_peer.ledger(id) ON DELETE CASCADE,
    UNIQUE(block_id, txid)
);

CREATE INDEX idx_ledger_tx_block ON commit_peer.ledger_transactions(block_id);
CREATE INDEX idx_ledger_tx_txid ON commit_peer.ledger_transactions(txid);

-- UTXO world state for the commit peer is stored on disk (LevelDB), not in PostgreSQL.

-- Core: submit timestamps for E2E / benchmark metrics (CORE_RECORD_SUBMIT=1)
CREATE TABLE IF NOT EXISTS core_service.tx_submit_times (
    txid VARCHAR(255) PRIMARY KEY,
    submitted_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_tx_submit_times_submitted_at
    ON core_service.tx_submit_times (submitted_at DESC);

-- Nếu DB đã tạo trước khi có cột payload_schema:
-- ALTER TABLE core_service.smart_contracts ADD COLUMN IF NOT EXISTS payload_schema JSONB;


CREATE TABLE IF NOT EXISTS wallet.accounts (
    id              SERIAL PRIMARY KEY,
    username        VARCHAR(64)  NOT NULL UNIQUE,
    password_hash   TEXT         NOT NULL,
    address         VARCHAR(40)  NOT NULL UNIQUE,
    pubkey_hex      VARCHAR(64)  NOT NULL,
    seed_hex        VARCHAR(64)  NOT NULL,
    discount        NUMERIC(8,4) NOT NULL DEFAULT 0,
    initial_balance BIGINT       NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_wallet_accounts_address ON wallet.accounts (address);

CREATE TABLE IF NOT EXISTS wallet.sessions (
    token       VARCHAR(64) PRIMARY KEY,
    account_id  INT NOT NULL REFERENCES wallet.accounts(id) ON DELETE CASCADE,
    expires_at  TIMESTAMPTZ NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_wallet_sessions_account ON wallet.sessions (account_id);
CREATE INDEX IF NOT EXISTS idx_wallet_sessions_expires ON wallet.sessions (expires_at);
