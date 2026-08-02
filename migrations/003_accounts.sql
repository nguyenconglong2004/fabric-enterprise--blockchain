-- Wallet identity (login / address / discount) — NOT chain world state.
-- UTXO + KV (rw_set) live on Commit Peer LevelDB.
CREATE SCHEMA IF NOT EXISTS wallet;

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

-- Optional: drop legacy tables after Core has migrated (EnsureAccountsSchema copies once).
-- DROP TABLE IF EXISTS core_service.sessions;
-- DROP TABLE IF EXISTS core_service.accounts;
