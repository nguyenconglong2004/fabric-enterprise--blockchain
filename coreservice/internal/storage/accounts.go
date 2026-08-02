package storage

import (
	"database/sql"
	"fmt"
	"time"
)

// Account is a demo wallet user (P2PKH address + discount for spend middleware).
// Stored in schema "wallet" — identity/auth metadata, not chain world state
// (UTXO + KV live on Commit Peer).
type Account struct {
	ID             int64
	Username       string
	PasswordHash   string
	Address        string
	PubkeyHex      string
	SeedHex        string
	Discount       float64
	InitialBalance int64
	CreatedAt      time.Time
}

func (p *PostgresDB) EnsureAccountsSchema() error {
	_, err := p.db.Exec(`
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
`)
	if err != nil {
		return err
	}
	return p.migrateAccountsFromCoreService()
}

// migrateAccountsFromCoreService moves rows once from legacy core_service.* if present.
func (p *PostgresDB) migrateAccountsFromCoreService() error {
	var n int
	err := p.db.QueryRow(`
SELECT COUNT(*) FROM information_schema.tables
WHERE table_schema = 'core_service' AND table_name = 'accounts'`).Scan(&n)
	if err != nil || n == 0 {
		return nil
	}
	var walletCount int
	_ = p.db.QueryRow(`SELECT COUNT(*) FROM wallet.accounts`).Scan(&walletCount)
	if walletCount > 0 {
		return nil
	}
	_, err = p.db.Exec(`
INSERT INTO wallet.accounts (id, username, password_hash, address, pubkey_hex, seed_hex, discount, initial_balance, created_at)
SELECT id, username, password_hash, address, pubkey_hex, seed_hex, discount, initial_balance, created_at
FROM core_service.accounts
ON CONFLICT (username) DO NOTHING;

SELECT setval(pg_get_serial_sequence('wallet.accounts', 'id'), COALESCE((SELECT MAX(id) FROM wallet.accounts), 1));

INSERT INTO wallet.sessions (token, account_id, expires_at, created_at)
SELECT token, account_id, expires_at, created_at
FROM core_service.sessions
ON CONFLICT (token) DO NOTHING;
`)
	return err
}

func (p *PostgresDB) CreateAccount(a *Account) error {
	q := `
INSERT INTO wallet.accounts
  (username, password_hash, address, pubkey_hex, seed_hex, discount, initial_balance)
VALUES ($1,$2,$3,$4,$5,$6,$7)
RETURNING id, created_at`
	return p.db.QueryRow(q,
		a.Username, a.PasswordHash, a.Address, a.PubkeyHex, a.SeedHex, a.Discount, a.InitialBalance,
	).Scan(&a.ID, &a.CreatedAt)
}

func (p *PostgresDB) GetAccountByUsername(username string) (*Account, error) {
	return p.scanAccount(p.db.QueryRow(`
SELECT id, username, password_hash, address, pubkey_hex, seed_hex, discount::float8, initial_balance, created_at
FROM wallet.accounts WHERE username = $1`, username))
}

func (p *PostgresDB) GetAccountByID(id int64) (*Account, error) {
	return p.scanAccount(p.db.QueryRow(`
SELECT id, username, password_hash, address, pubkey_hex, seed_hex, discount::float8, initial_balance, created_at
FROM wallet.accounts WHERE id = $1`, id))
}

func (p *PostgresDB) GetAccountByAddress(address string) (*Account, error) {
	return p.scanAccount(p.db.QueryRow(`
SELECT id, username, password_hash, address, pubkey_hex, seed_hex, discount::float8, initial_balance, created_at
FROM wallet.accounts WHERE address = $1`, address))
}

func (p *PostgresDB) ListAccounts() ([]Account, error) {
	rows, err := p.db.Query(`
SELECT id, username, password_hash, address, pubkey_hex, seed_hex, discount::float8, initial_balance, created_at
FROM wallet.accounts ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Account
	for rows.Next() {
		var a Account
		if err := rows.Scan(&a.ID, &a.Username, &a.PasswordHash, &a.Address, &a.PubkeyHex, &a.SeedHex, &a.Discount, &a.InitialBalance, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (p *PostgresDB) scanAccount(row *sql.Row) (*Account, error) {
	var a Account
	err := row.Scan(&a.ID, &a.Username, &a.PasswordHash, &a.Address, &a.PubkeyHex, &a.SeedHex, &a.Discount, &a.InitialBalance, &a.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

func (p *PostgresDB) CreateSession(token string, accountID int64, expiresAt time.Time) error {
	_, err := p.db.Exec(`
INSERT INTO wallet.sessions (token, account_id, expires_at) VALUES ($1,$2,$3)
ON CONFLICT (token) DO UPDATE SET account_id = EXCLUDED.account_id, expires_at = EXCLUDED.expires_at`,
		token, accountID, expiresAt)
	return err
}

// DeleteSessionsForAccount drops all sessions for one account (single active login per user).
func (p *PostgresDB) DeleteSessionsForAccount(accountID int64) error {
	_, err := p.db.Exec(`DELETE FROM wallet.sessions WHERE account_id = $1`, accountID)
	return err
}

func (p *PostgresDB) DeleteSession(token string) error {
	_, err := p.db.Exec(`DELETE FROM wallet.sessions WHERE token = $1`, token)
	return err
}

func (p *PostgresDB) GetSessionAccount(token string) (*Account, error) {
	var accountID int64
	var expires time.Time
	err := p.db.QueryRow(`SELECT account_id, expires_at FROM wallet.sessions WHERE token = $1`, token).
		Scan(&accountID, &expires)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if time.Now().UTC().After(expires.UTC()) {
		_ = p.DeleteSession(token)
		return nil, fmt.Errorf("session expired")
	}
	return p.GetAccountByID(accountID)
}
