CREATE TEMP TABLE backup_run_target_preserved AS
SELECT run_id,target_id,name,status,error_summary,position FROM backup_run_target;

DROP TABLE backup_run_target;

CREATE TABLE backup_run_rebuilt (
  id TEXT PRIMARY KEY,
  task_id TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  size INTEGER NOT NULL,
  status TEXT NOT NULL,
  retry_available INTEGER NOT NULL DEFAULT 0 CHECK(retry_available IN (0,1)),
  retry_unavailable_reason TEXT NOT NULL DEFAULT ''
);

INSERT INTO backup_run_rebuilt(id,task_id,created_at,size,status,retry_available,retry_unavailable_reason)
SELECT id,task_id,created_at,size,status,retry_available,retry_unavailable_reason FROM backup_run;

DROP TABLE backup_run;
ALTER TABLE backup_run_rebuilt RENAME TO backup_run;
CREATE INDEX backup_run_task_created ON backup_run(task_id,created_at DESC);

CREATE TABLE backup_run_target (
  run_id TEXT NOT NULL REFERENCES backup_run(id) ON DELETE CASCADE,
  target_id TEXT NOT NULL,
  name TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL,
  error_summary TEXT NOT NULL DEFAULT '',
  position INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY(run_id,target_id)
);

INSERT INTO backup_run_target(run_id,target_id,name,status,error_summary,position)
SELECT run_id,target_id,name,status,error_summary,position FROM backup_run_target_preserved;
DROP TABLE backup_run_target_preserved;
