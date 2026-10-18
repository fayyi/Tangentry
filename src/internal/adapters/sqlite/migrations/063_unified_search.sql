-- Extend the local search index to article blocks and saved subtitle cues.
DROP TRIGGER IF EXISTS library_search_insert;
DROP TRIGGER IF EXISTS library_search_update;
DROP TRIGGER IF EXISTS library_search_delete;
DROP TRIGGER IF EXISTS library_search_subtitle_insert;
DROP TRIGGER IF EXISTS library_search_subtitle_update;
DROP TRIGGER IF EXISTS library_search_subtitle_delete;
DROP TABLE IF EXISTS library_search;

CREATE VIRTUAL TABLE library_search USING fts5(
  id UNINDEXED,
  title,
  author,
  description,
  body,
  subtitles,
  tokenize='trigram'
);

INSERT INTO library_search(id, title, author, description, body, subtitles)
SELECT
  id,
  CASE WHEN json_valid(payload) THEN COALESCE(json_extract(payload,'$.title'),'') ELSE '' END,
  CASE WHEN json_valid(payload) THEN COALESCE(json_extract(payload,'$.author'),'') ELSE '' END,
  CASE WHEN json_valid(payload) THEN COALESCE(json_extract(payload,'$.description'),'') ELSE '' END,
  CASE WHEN json_valid(payload) THEN COALESCE((SELECT group_concat(json_extract(value,'$.text'),' ') FROM json_each(payload,'$.articleBlocks')),'') ELSE '' END,
  COALESCE((SELECT group_concat(json_extract(c.value,'$.text'),' ') FROM saved_subtitles ss, json_each(ss.payload,'$.track.cues') c WHERE ss.content_id=saved_content.id AND ss.payload<>''),'')
FROM saved_content;

CREATE TRIGGER library_search_insert AFTER INSERT ON saved_content BEGIN
  INSERT INTO library_search(id,title,author,description,body,subtitles)
  VALUES(new.id,
    CASE WHEN json_valid(new.payload) THEN COALESCE(json_extract(new.payload,'$.title'),'') ELSE '' END,
    CASE WHEN json_valid(new.payload) THEN COALESCE(json_extract(new.payload,'$.author'),'') ELSE '' END,
    CASE WHEN json_valid(new.payload) THEN COALESCE(json_extract(new.payload,'$.description'),'') ELSE '' END,
    CASE WHEN json_valid(new.payload) THEN COALESCE((SELECT group_concat(json_extract(value,'$.text'),' ') FROM json_each(new.payload,'$.articleBlocks')),'') ELSE '' END,
    COALESCE((SELECT group_concat(json_extract(c.value,'$.text'),' ') FROM saved_subtitles ss, json_each(ss.payload,'$.track.cues') c WHERE ss.content_id=new.id AND ss.payload<>''),'')
  );
END;

CREATE TRIGGER library_search_update AFTER UPDATE OF payload ON saved_content BEGIN
  DELETE FROM library_search WHERE id=old.id;
  INSERT INTO library_search(id,title,author,description,body,subtitles)
  VALUES(new.id,
    CASE WHEN json_valid(new.payload) THEN COALESCE(json_extract(new.payload,'$.title'),'') ELSE '' END,
    CASE WHEN json_valid(new.payload) THEN COALESCE(json_extract(new.payload,'$.author'),'') ELSE '' END,
    CASE WHEN json_valid(new.payload) THEN COALESCE(json_extract(new.payload,'$.description'),'') ELSE '' END,
    CASE WHEN json_valid(new.payload) THEN COALESCE((SELECT group_concat(json_extract(value,'$.text'),' ') FROM json_each(new.payload,'$.articleBlocks')),'') ELSE '' END,
    COALESCE((SELECT group_concat(json_extract(c.value,'$.text'),' ') FROM saved_subtitles ss, json_each(ss.payload,'$.track.cues') c WHERE ss.content_id=new.id AND ss.payload<>''),'')
  );
END;

CREATE TRIGGER library_search_delete AFTER DELETE ON saved_content BEGIN
  DELETE FROM library_search WHERE id=old.id;
END;

CREATE TRIGGER library_search_subtitle_insert AFTER INSERT ON saved_subtitles BEGIN
  UPDATE library_search SET subtitles=COALESCE((SELECT group_concat(json_extract(c.value,'$.text'),' ') FROM saved_subtitles ss, json_each(ss.payload,'$.track.cues') c WHERE ss.content_id=NEW.content_id AND ss.payload<>''),'') WHERE id=NEW.content_id;
END;
CREATE TRIGGER library_search_subtitle_update AFTER UPDATE OF payload ON saved_subtitles BEGIN
  UPDATE library_search SET subtitles=COALESCE((SELECT group_concat(json_extract(c.value,'$.text'),' ') FROM saved_subtitles ss, json_each(ss.payload,'$.track.cues') c WHERE ss.content_id=NEW.content_id AND ss.payload<>''),'') WHERE id=NEW.content_id;
END;
CREATE TRIGGER library_search_subtitle_delete AFTER DELETE ON saved_subtitles BEGIN
  UPDATE library_search SET subtitles=COALESCE((SELECT group_concat(json_extract(c.value,'$.text'),' ') FROM saved_subtitles ss, json_each(ss.payload,'$.track.cues') c WHERE ss.content_id=OLD.content_id AND ss.payload<>''),'') WHERE id=OLD.content_id;
END;
