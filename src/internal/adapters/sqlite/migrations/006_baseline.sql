-- Versions 1-6 shipped together; every legacy database records all of them,
-- so this file is the baseline. Statements stay idempotent for fixtures that
-- recorded only part of 1-5.
CREATE TABLE IF NOT EXISTS saved_content (id TEXT PRIMARY KEY, payload TEXT NOT NULL, saved_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS favorite_sync (id TEXT PRIMARY KEY, payload TEXT NOT NULL, created_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS downloads (id TEXT PRIMARY KEY, payload TEXT NOT NULL, created_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS notes (id TEXT PRIMARY KEY, content_id TEXT NOT NULL, body TEXT NOT NULL, position_seconds REAL, created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL);
CREATE INDEX IF NOT EXISTS notes_by_content ON notes(content_id, created_at DESC);
CREATE TABLE IF NOT EXISTS subscriptions (id TEXT PRIMARY KEY, creator_id TEXT NOT NULL UNIQUE, name TEXT NOT NULL, created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL DEFAULT 0, snapshot TEXT NOT NULL DEFAULT '');
CREATE TABLE IF NOT EXISTS subscription_refresh (id TEXT PRIMARY KEY, payload TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS read_state (content_id TEXT PRIMARY KEY, is_read INTEGER NOT NULL CHECK(is_read IN (0,1)), updated_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS reading_progress (content_id TEXT PRIMARY KEY, position_seconds REAL NOT NULL, duration_seconds REAL NOT NULL, completed INTEGER NOT NULL, revision INTEGER NOT NULL, updated_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS saved_subtitles (content_id TEXT NOT NULL, language TEXT NOT NULL, payload TEXT NOT NULL, revision INTEGER NOT NULL, saved_at INTEGER NOT NULL, PRIMARY KEY(content_id,language));
