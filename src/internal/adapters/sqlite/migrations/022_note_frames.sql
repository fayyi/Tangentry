-- Early builds keyed note_frames by note alone; rebuild with one row per frame.
CREATE TABLE IF NOT EXISTS note_frames (note_id TEXT NOT NULL, frame_id TEXT NOT NULL, data BLOB NOT NULL CHECK(length(data)<=1048576), PRIMARY KEY(note_id,frame_id));
CREATE TABLE note_frames_next (note_id TEXT NOT NULL, frame_id TEXT NOT NULL, data BLOB NOT NULL CHECK(length(data)<=1048576), PRIMARY KEY(note_id,frame_id));
INSERT INTO note_frames_next(note_id,frame_id,data) SELECT note_id,frame_id,data FROM note_frames;
DROP TABLE note_frames;
ALTER TABLE note_frames_next RENAME TO note_frames;
CREATE TABLE IF NOT EXISTS note_creations (id TEXT PRIMARY KEY, fingerprint TEXT NOT NULL, note_id TEXT NOT NULL);
