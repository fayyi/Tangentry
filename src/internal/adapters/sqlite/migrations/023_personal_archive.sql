CREATE TABLE IF NOT EXISTS subtitle_archive (
 content_id TEXT NOT NULL, language TEXT NOT NULL, fingerprint TEXT NOT NULL,
 payload TEXT NOT NULL, saved_at INTEGER NOT NULL,
 PRIMARY KEY(content_id,language,fingerprint));
-- Consolidate old credential scopes into the personal account
-- (discovery.PersonalAccount) without dropping their original rows.
INSERT INTO recommendation_queue(platform,account,content_id,payload,fetched_at,metadata_at,seen_at,opened_at)
 SELECT platform,'0000000000000000000000000000000000000000000000000000000000000000',content_id,payload,fetched_at,metadata_at,seen_at,opened_at
 FROM recommendation_queue WHERE account<>'0000000000000000000000000000000000000000000000000000000000000000' ORDER BY metadata_at,account
 ON CONFLICT(platform,account,content_id) DO UPDATE SET
 payload=CASE WHEN excluded.metadata_at>=recommendation_queue.metadata_at THEN excluded.payload ELSE recommendation_queue.payload END,
 metadata_at=MAX(excluded.metadata_at,recommendation_queue.metadata_at),
 fetched_at=MIN(excluded.fetched_at,recommendation_queue.fetched_at),
 seen_at=MAX(excluded.seen_at,recommendation_queue.seen_at),
 opened_at=MAX(excluded.opened_at,recommendation_queue.opened_at);
