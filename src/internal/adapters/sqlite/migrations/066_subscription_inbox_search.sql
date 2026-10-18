CREATE VIRTUAL TABLE IF NOT EXISTS subscription_inbox_search USING fts5(content_id UNINDEXED,title,author,description,tokenize='trigram');
INSERT INTO subscription_inbox_search(content_id,title,author,description)
 SELECT content_id,title,author,CASE WHEN json_valid(payload) THEN COALESCE(json_extract(payload,'$.description'),'') ELSE '' END FROM subscription_inbox;
CREATE TRIGGER IF NOT EXISTS subscription_inbox_search_insert AFTER INSERT ON subscription_inbox BEGIN
 INSERT INTO subscription_inbox_search(content_id,title,author,description)
 VALUES(new.content_id,new.title,new.author,CASE WHEN json_valid(new.payload) THEN COALESCE(json_extract(new.payload,'$.description'),'') ELSE '' END);
END;
CREATE TRIGGER IF NOT EXISTS subscription_inbox_search_update AFTER UPDATE OF payload,title,author ON subscription_inbox BEGIN
 DELETE FROM subscription_inbox_search WHERE content_id=old.content_id;
 INSERT INTO subscription_inbox_search(content_id,title,author,description)
 VALUES(new.content_id,new.title,new.author,CASE WHEN json_valid(new.payload) THEN COALESCE(json_extract(new.payload,'$.description'),'') ELSE '' END);
END;
CREATE TRIGGER IF NOT EXISTS subscription_inbox_search_delete AFTER DELETE ON subscription_inbox BEGIN
 DELETE FROM subscription_inbox_search WHERE content_id=old.content_id;
END;
