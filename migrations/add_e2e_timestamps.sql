-- Chạy từ thư mục gốc repo (fabric-enterprise--blockchain):
--
-- Cách 1 — Postgres trong docker-compose (khuyến nghị):
--   docker exec -i fabric-postgres psql -U fabric -d blockchain \
--     < migrations/add_e2e_timestamps.sql
--
-- Cách 2 — psql trên máy host (port map 5432):
--   PGPASSWORD=fabric123 psql -h 127.0.0.1 -p 5432 -U fabric -d blockchain \
--     -f migrations/add_e2e_timestamps.sql
--
-- (Không dùng psql "$POSTGRES_URL" — URI postgres:// thường không tương thích psql CLI.)

ALTER TABLE commit_peer.ledger
    ADD COLUMN IF NOT EXISTS ledger_committed_at TIMESTAMPTZ;

UPDATE commit_peer.ledger
SET ledger_committed_at = committed_at
WHERE ledger_committed_at IS NULL;

ALTER TABLE commit_peer.ledger_transactions
    ADD COLUMN IF NOT EXISTS submitted_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS ledger_committed_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_ledger_tx_ledger_committed
    ON commit_peer.ledger_transactions (ledger_committed_at);

CREATE INDEX IF NOT EXISTS idx_ledger_ledger_committed
    ON commit_peer.ledger (ledger_committed_at);
