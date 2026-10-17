CREATE TABLE IF NOT EXISTS discovery_batch_snapshots (
    id TEXT PRIMARY KEY,
    request_key TEXT NOT NULL,
    payload TEXT NOT NULL,
    expires_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS discovery_batch_snapshots_expiry ON discovery_batch_snapshots(expires_at);
