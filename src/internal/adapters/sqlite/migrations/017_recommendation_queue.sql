CREATE TABLE IF NOT EXISTS recommendation_queue (
 platform TEXT NOT NULL, account TEXT NOT NULL, content_id TEXT NOT NULL,
 payload TEXT NOT NULL, fetched_at INTEGER NOT NULL, metadata_at INTEGER NOT NULL, seen_at INTEGER NOT NULL DEFAULT 0,
 opened_at INTEGER NOT NULL DEFAULT 0, PRIMARY KEY(platform,account,content_id));
CREATE INDEX IF NOT EXISTS recommendation_queue_unopened ON recommendation_queue(platform,account,opened_at,seen_at,fetched_at,content_id);
