-- Rename / create wallet schema (accounts no longer under core_service).
-- Safe to run on DBs that already have core_service.accounts.
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

-- Copy from legacy core_service if wallet is empty
DO $$
BEGIN
  IF EXISTS (
    SELECT 1 FROM information_schema.tables
    WHERE table_schema = 'core_service' AND table_name = 'accounts'
  ) AND (SELECT COUNT(*) FROM wallet.accounts) = 0 THEN
    INSERT INTO wallet.accounts (id, username, password_hash, address, pubkey_hex, seed_hex, discount, initial_balance, created_at)
    SELECT id, username, password_hash, address, pubkey_hex, seed_hex, discount, initial_balance, created_at
    FROM core_service.accounts
    ON CONFLICT (username) DO NOTHING;
    PERFORM setval(pg_get_serial_sequence('wallet.accounts', 'id'), COALESCE((SELECT MAX(id) FROM wallet.accounts), 1));
    IF EXISTS (
      SELECT 1 FROM information_schema.tables
      WHERE table_schema = 'core_service' AND table_name = 'sessions'
    ) THEN
      INSERT INTO wallet.sessions (token, account_id, expires_at, created_at)
      SELECT token, account_id, expires_at, created_at FROM core_service.sessions
      ON CONFLICT (token) DO NOTHING;
    END IF;
  END IF;
END $$;
