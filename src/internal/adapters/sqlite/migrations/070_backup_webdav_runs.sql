CREATE TABLE IF NOT EXISTS backup_run_target (
  run_id TEXT NOT NULL REFERENCES backup_run(id) ON DELETE CASCADE,
  target_id TEXT NOT NULL,
  name TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL,
  error_summary TEXT NOT NULL DEFAULT '',
  PRIMARY KEY(run_id,target_id)
);
