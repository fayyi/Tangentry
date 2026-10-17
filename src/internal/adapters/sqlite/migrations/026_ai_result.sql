-- AI output keyed by the hash of the exact source sent to the model, so the
-- same text is never paid for twice; model records the latest writer.
CREATE TABLE IF NOT EXISTS ai_result (
 kind TEXT NOT NULL, source_hash TEXT NOT NULL, target TEXT NOT NULL,
 model TEXT NOT NULL, value TEXT NOT NULL, created_at INTEGER NOT NULL,
 PRIMARY KEY(kind, source_hash, target));
-- Legacy cache rows are looked up by the content and track they were made for.
CREATE INDEX IF NOT EXISTS ai_result_by_target ON ai_result(kind, target);
