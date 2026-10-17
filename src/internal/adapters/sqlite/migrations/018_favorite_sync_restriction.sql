CREATE TABLE IF NOT EXISTS favorite_sync_restriction (id INTEGER PRIMARY KEY CHECK(id=1), until_at INTEGER NOT NULL CHECK(until_at>=0));
INSERT INTO favorite_sync_restriction(id,until_at)
 SELECT 1,COALESCE(MAX(CAST(json_extract(payload,'$.restrictedUntil') AS INTEGER)),0) FROM favorite_sync WHERE true
 ON CONFLICT(id) DO UPDATE SET until_at=MAX(until_at,excluded.until_at);
