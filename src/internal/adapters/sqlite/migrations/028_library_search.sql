-- Library text search. Trigram tokens match Chinese text without word
-- segmentation; triggers keep the index in step with saved_content.
CREATE VIRTUAL TABLE IF NOT EXISTS library_search USING fts5(id UNINDEXED, title, author, description, tokenize='trigram');
DELETE FROM library_search;
INSERT INTO library_search(id,title,author,description)
 SELECT id, CASE WHEN json_valid(payload) THEN COALESCE(json_extract(payload,'$.title'),'') ELSE '' END, CASE WHEN json_valid(payload) THEN COALESCE(json_extract(payload,'$.author'),'') ELSE '' END, CASE WHEN json_valid(payload) THEN COALESCE(json_extract(payload,'$.description'),'') ELSE '' END FROM saved_content;
CREATE TRIGGER IF NOT EXISTS library_search_insert AFTER INSERT ON saved_content BEGIN
 INSERT INTO library_search(id,title,author,description) VALUES(new.id, CASE WHEN json_valid(new.payload) THEN COALESCE(json_extract(new.payload,'$.title'),'') ELSE '' END, CASE WHEN json_valid(new.payload) THEN COALESCE(json_extract(new.payload,'$.author'),'') ELSE '' END, CASE WHEN json_valid(new.payload) THEN COALESCE(json_extract(new.payload,'$.description'),'') ELSE '' END);
END;
CREATE TRIGGER IF NOT EXISTS library_search_update AFTER UPDATE OF payload ON saved_content BEGIN
 DELETE FROM library_search WHERE id=old.id;
 INSERT INTO library_search(id,title,author,description) VALUES(new.id, CASE WHEN json_valid(new.payload) THEN COALESCE(json_extract(new.payload,'$.title'),'') ELSE '' END, CASE WHEN json_valid(new.payload) THEN COALESCE(json_extract(new.payload,'$.author'),'') ELSE '' END, CASE WHEN json_valid(new.payload) THEN COALESCE(json_extract(new.payload,'$.description'),'') ELSE '' END);
END;
CREATE TRIGGER IF NOT EXISTS library_search_delete AFTER DELETE ON saved_content BEGIN
 DELETE FROM library_search WHERE id=old.id;
END;
