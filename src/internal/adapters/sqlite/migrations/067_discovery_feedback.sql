CREATE TABLE IF NOT EXISTS discovery_feedback (
 id TEXT PRIMARY KEY,
 type TEXT NOT NULL,
 target TEXT NOT NULL,
 value TEXT NOT NULL,
 expires_at INTEGER NOT NULL DEFAULT 0,
 created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS discovery_feedback_active ON discovery_feedback(expires_at,created_at);
