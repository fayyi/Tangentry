CREATE TABLE IF NOT EXISTS agent_operations (
  id TEXT PRIMARY KEY,
  action TEXT NOT NULL,
  content_id TEXT NOT NULL,
  state TEXT NOT NULL,
  source TEXT NOT NULL,
  cost_class TEXT NOT NULL,
  estimated_duration TEXT NOT NULL,
  external_processing INTEGER NOT NULL DEFAULT 0 CHECK(external_processing IN (0,1)),
  started_at INTEGER NOT NULL,
  finished_at INTEGER NOT NULL DEFAULT 0,
  error TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS agent_operations_state ON agent_operations(state, started_at DESC);
