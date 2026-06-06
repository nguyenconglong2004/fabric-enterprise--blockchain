-- E2E / benchmark: when Core accepted a transaction (HTTP 200 path).
CREATE TABLE IF NOT EXISTS core_service.tx_submit_times (
    txid VARCHAR(255) PRIMARY KEY,
    submitted_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_tx_submit_times_submitted_at
    ON core_service.tx_submit_times (submitted_at DESC);
