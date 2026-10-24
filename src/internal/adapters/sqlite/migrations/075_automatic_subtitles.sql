CREATE TABLE IF NOT EXISTS automatic_subtitle_sessions (
  id TEXT PRIMARY KEY,
  content_id TEXT NOT NULL,
  language TEXT NOT NULL,
  capability TEXT NOT NULL,
  state TEXT NOT NULL,
  processed_seconds REAL NOT NULL DEFAULT 0,
  furthest_position REAL NOT NULL DEFAULT 0,
  duration_seconds REAL NOT NULL DEFAULT 0,
  job_id TEXT NOT NULL DEFAULT '',
  error_code TEXT NOT NULL DEFAULT '',
  error_message TEXT NOT NULL DEFAULT '',
  draft_payload TEXT NOT NULL DEFAULT '',
  revision INTEGER NOT NULL DEFAULT 0,
  updated_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS automatic_subtitle_sessions_content ON automatic_subtitle_sessions(content_id, updated_at DESC);
