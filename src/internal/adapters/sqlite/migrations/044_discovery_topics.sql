CREATE TABLE IF NOT EXISTS discovery_topic_rules (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    keywords TEXT NOT NULL,
    group_id TEXT NOT NULL DEFAULT '',
    revision INTEGER NOT NULL CHECK(revision > 0),
    created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS discovery_topic_rules_created ON discovery_topic_rules(created_at,id);
