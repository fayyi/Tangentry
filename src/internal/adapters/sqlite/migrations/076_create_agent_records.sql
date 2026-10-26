CREATE TABLE IF NOT EXISTS agent_runs (
  id TEXT PRIMARY KEY,
  client TEXT NOT NULL DEFAULT '',
  trigger_kind TEXT NOT NULL DEFAULT '',
  state TEXT NOT NULL,
  started_at INTEGER NOT NULL,
  finished_at INTEGER NOT NULL DEFAULT 0,
  error TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS agent_review_events (
  id TEXT PRIMARY KEY,
  idempotency_key TEXT NOT NULL UNIQUE,
  run_id TEXT NOT NULL DEFAULT '',
  content_id TEXT NOT NULL,
  title TEXT NOT NULL,
  author TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL,
  rating TEXT NOT NULL DEFAULT '',
  reason TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS agent_review_events_content ON agent_review_events(content_id, created_at DESC);

CREATE TABLE IF NOT EXISTS agent_reports (
  id TEXT PRIMARY KEY,
  report_date TEXT NOT NULL,
  kind TEXT NOT NULL,
  markdown TEXT NOT NULL,
  created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS agent_reports_date ON agent_reports(report_date, kind, created_at);

CREATE TABLE IF NOT EXISTS agent_note_drafts (
  id TEXT PRIMARY KEY,
  content_id TEXT NOT NULL,
  payload TEXT NOT NULL,
  state TEXT NOT NULL DEFAULT 'draft',
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS agent_audit_events (
  id TEXT PRIMARY KEY,
  run_id TEXT NOT NULL DEFAULT '',
  action TEXT NOT NULL,
  network_fetch INTEGER NOT NULL DEFAULT 0 CHECK(network_fetch IN (0,1)),
  external_processing INTEGER NOT NULL DEFAULT 0 CHECK(external_processing IN (0,1)),
  outcome TEXT NOT NULL,
  created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS agent_audit_events_created ON agent_audit_events(created_at DESC);
