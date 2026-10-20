ALTER TABLE backup_run ADD COLUMN retry_available INTEGER NOT NULL DEFAULT 0 CHECK(retry_available IN (0,1));
