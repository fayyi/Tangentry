CREATE TABLE IF NOT EXISTS article_documents (content_id TEXT PRIMARY KEY, payload TEXT NOT NULL, revision INTEGER NOT NULL CHECK(revision>0));
