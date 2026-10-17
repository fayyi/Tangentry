package sqlite

import (
	"context"
	"encoding/json"
	"strings"
	"tangentry/internal/content"
	"tangentry/internal/discovery"
	"tangentry/internal/notes"
)

// Restrict source search to identified local metadata; never search URLs or
// arbitrary payload fields. CASE also keeps malformed cached JSON harmless.
const noteSources = `WITH note_sources AS (
 SELECT n.*,COALESCE(
 CASE WHEN json_valid(c.payload) THEN CASE WHEN json_extract(c.payload,'$.id')=n.content_id THEN c.payload END END,
 CASE WHEN json_valid(i.payload) THEN CASE WHEN json_extract(i.payload,'$.id')=n.content_id THEN i.payload END END,
 CASE WHEN json_valid(x.payload) THEN CASE WHEN json_extract(x.payload,'$.id')=n.content_id AND json_extract(x.payload,'$.platform')='xiaohongshu' THEN x.payload END END,
 CASE WHEN json_valid(r.payload) THEN CASE WHEN json_extract(r.payload,'$.id')=n.content_id AND json_extract(r.payload,'$.platform')=r.platform THEN r.payload END END,
 '') AS source_payload FROM notes n
 LEFT JOIN saved_content c ON c.id=n.content_id
 LEFT JOIN subscription_inbox i ON i.content_id=n.content_id
 LEFT JOIN xiaohongshu_details x ON x.content_id=n.content_id
 LEFT JOIN recommendation_queue r ON r.content_id=n.content_id AND r.platform=substr(n.content_id,1,instr(n.content_id,':')-1) AND r.account='` + discovery.PersonalAccount + `'
) `
const noteSourceJSON = `CASE WHEN n.source_payload<>'' THEN n.source_payload ELSE '{}' END`
const noteTextMatch = `(?='' OR instr(lower(n.body),lower(?))>0 OR
 instr(lower(COALESCE(json_extract(CASE WHEN json_valid(n.text_anchor) THEN n.text_anchor ELSE '{}' END,'$.quote'),'')),lower(?))>0 OR
 instr(lower(COALESCE(json_extract(CASE WHEN json_valid(n.pdf_anchor) THEN n.pdf_anchor ELSE '{}' END,'$.quote'),'')),lower(?))>0 OR
 EXISTS (SELECT 1 FROM json_each(CASE WHEN json_valid(n.subtitle_references) THEN n.subtitle_references ELSE '[]' END)
 WHERE instr(lower(COALESCE(json_extract(value,'$.text'),'')),lower(?))>0) OR
 (json_extract(` + noteSourceJSON + `,'$.id')=n.content_id AND
 (instr(lower(COALESCE(json_extract(` + noteSourceJSON + `,'$.title'),'')),lower(?))>0 OR
 instr(lower(COALESCE(json_extract(` + noteSourceJSON + `,'$.author'),'')),lower(?))>0)))`

// A parent includes descendants only at a slash boundary. instr keeps
// underscores literal, unlike a LIKE prefix.
const noteTagMatch = `(?='' OR EXISTS (SELECT 1 FROM json_each(COALESCE(n.tags,'[]'))
 WHERE value=? OR instr(value,?||'/')=1))`

func (s *Store) SearchNotes(ctx context.Context, query notes.SearchQuery) ([]notes.Note, error) {
	// instr keeps percent, underscore and quotes literal. SQLite lower performs
	// ASCII case folding; other Unicode text is matched as a literal substring.
	columns := "n." + strings.ReplaceAll(noteColumns, ", ", ", n.")
	rows, err := s.db.QueryContext(ctx, noteSources+`SELECT `+columns+`,n.source_payload FROM note_sources n
		WHERE `+noteTextMatch+`
		AND (?=0 OR pinned=1)
		AND `+noteTagMatch+`
		AND (?=0 OR n.created_at >= ?) AND (?=0 OR n.created_at < ?)
		AND (?='' OR n.created_at < ? OR (n.created_at = ? AND n.id < ?))
		ORDER BY n.created_at DESC, n.id DESC LIMIT ?`, query.Text, query.Text, query.Text, query.Text, query.Text, query.Text, query.Text, query.PinnedOnly, query.Tag, query.Tag, query.Tag, query.FromCreatedAt, query.FromCreatedAt, query.ToCreatedAt, query.ToCreatedAt, query.BeforeID, query.BeforeCreatedAt, query.BeforeCreatedAt, query.BeforeID, notes.SearchPageSize+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]notes.Note, 0)
	for rows.Next() {
		scanner := &exportNoteScanner{rows: rows}
		item, err := scanNote(scanner)
		if err != nil {
			return nil, err
		}
		if scanner.payload != "" {
			var source content.Item
			if json.Unmarshal([]byte(scanner.payload), &source) == nil && source.ID == item.ContentID {
				item.Source = &notes.ExportSource{Title: source.Title, Author: source.Author, Platform: source.Platform, Kind: source.Kind}
			}
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) NoteTags(ctx context.Context) ([]notes.TagCount, error) {
	rows, err := s.db.QueryContext(ctx, `WITH RECURSIVE tag_paths(note_id,tag,rest) AS (
		SELECT notes.id,'',value FROM notes,json_each(COALESCE(tags,'[]')) WHERE type='text'
		UNION ALL
		SELECT note_id,
			tag || CASE WHEN tag='' THEN '' ELSE '/' END ||
			CASE WHEN instr(rest,'/')=0 THEN rest ELSE substr(rest,1,instr(rest,'/')-1) END,
			CASE WHEN instr(rest,'/')=0 THEN '' ELSE substr(rest,instr(rest,'/')+1) END
		FROM tag_paths WHERE rest<>''
	)
	SELECT tag,count(DISTINCT note_id) FROM tag_paths WHERE tag<>''
	GROUP BY tag ORDER BY count(DISTINCT note_id) DESC,tag ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]notes.TagCount, 0)
	for rows.Next() {
		var item notes.TagCount
		if err := rows.Scan(&item.Tag, &item.Count); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
