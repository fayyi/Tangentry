CREATE TABLE IF NOT EXISTS saved_views (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  definition_version INTEGER NOT NULL,
  definition TEXT NOT NULL CHECK(json_valid(definition)),
  icon TEXT NOT NULL,
  color TEXT NOT NULL,
  sort_order INTEGER NOT NULL,
  is_default INTEGER NOT NULL DEFAULT 0 CHECK(is_default IN (0,1)),
  builtin INTEGER NOT NULL DEFAULT 0 CHECK(builtin IN (0,1)),
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS saved_views_default ON saved_views(is_default) WHERE is_default=1;
CREATE INDEX IF NOT EXISTS saved_views_order ON saved_views(sort_order,id);
INSERT OR IGNORE INTO saved_views(id,name,definition_version,definition,icon,color,sort_order,builtin,created_at,updated_at) VALUES
 ('builtin-unread','未读文章',1,'{"version":1,"filters":{"kind":"article","read":"unread"}}','file-text','teal',0,1,0,0),
 ('builtin-week-videos','本周视频',1,'{"version":1,"filters":{"kind":"video","sinceOffset":"week"}}','play','blue',1,1,0,0),
 ('builtin-favorite-podcasts','收藏的播客',1,'{"version":1,"filters":{"kind":"audio","favorite":true}}','headphones','amber',2,1,0,0);
