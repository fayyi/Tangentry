-- Viewing and search history moved out of browser storage. Times are
-- milliseconds, as the browser recorded them.
CREATE TABLE IF NOT EXISTS viewing_history (
 content_id TEXT PRIMARY KEY, item TEXT NOT NULL,
 opened_at INTEGER NOT NULL, open_count INTEGER NOT NULL DEFAULT 1);
CREATE INDEX IF NOT EXISTS viewing_history_recent ON viewing_history(opened_at DESC);
CREATE TABLE IF NOT EXISTS search_history (
 id TEXT PRIMARY KEY, query TEXT NOT NULL, source TEXT NOT NULL, created_at INTEGER NOT NULL);
CREATE INDEX IF NOT EXISTS search_history_recent ON search_history(created_at DESC);
