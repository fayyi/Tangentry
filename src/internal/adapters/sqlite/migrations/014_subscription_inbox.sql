-- Existing source snapshots are archived into the inbox by a Go step.
CREATE TABLE IF NOT EXISTS subscription_inbox (sequence INTEGER PRIMARY KEY AUTOINCREMENT, content_id TEXT NOT NULL UNIQUE, payload TEXT NOT NULL, title TEXT NOT NULL, author TEXT NOT NULL, kind TEXT NOT NULL, first_seen_at INTEGER NOT NULL, last_seen_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS subscription_inbox_sources (content_id TEXT NOT NULL, source_id TEXT NOT NULL, source_name TEXT NOT NULL, PRIMARY KEY(content_id,source_id));
CREATE INDEX IF NOT EXISTS inbox_sources_by_source ON subscription_inbox_sources(source_id,content_id);
