-- Versions 10-12 changed no schema.
CREATE TABLE IF NOT EXISTS subscription_groups (id TEXT PRIMARY KEY, name TEXT NOT NULL, name_key TEXT NOT NULL UNIQUE, created_at INTEGER NOT NULL, revision INTEGER NOT NULL CHECK(revision > 0));
CREATE TABLE IF NOT EXISTS subscription_group_members (source_id TEXT PRIMARY KEY, group_id TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS subscription_members_by_group ON subscription_group_members(group_id);
