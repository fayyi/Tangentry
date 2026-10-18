CREATE TABLE IF NOT EXISTS backup_target (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  type TEXT NOT NULL,
  path TEXT NOT NULL,
  enabled INTEGER NOT NULL DEFAULT 1 CHECK(enabled IN (0,1))
);
CREATE TABLE IF NOT EXISTS backup_task (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  include_downloads INTEGER NOT NULL DEFAULT 0 CHECK(include_downloads IN (0,1)),
  retention INTEGER NOT NULL DEFAULT 0,
  enabled INTEGER NOT NULL DEFAULT 1 CHECK(enabled IN (0,1))
);
CREATE TABLE IF NOT EXISTS backup_task_target (
  task_id TEXT NOT NULL REFERENCES backup_task(id) ON DELETE CASCADE,
  target_id TEXT NOT NULL REFERENCES backup_target(id) ON DELETE CASCADE,
  position INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY(task_id,target_id)
);
CREATE TABLE IF NOT EXISTS backup_run (
  id TEXT PRIMARY KEY,
  task_id TEXT NOT NULL REFERENCES backup_task(id) ON DELETE CASCADE,
  created_at INTEGER NOT NULL,
  size INTEGER NOT NULL,
  status TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS backup_run_task_created ON backup_run(task_id,created_at DESC);
