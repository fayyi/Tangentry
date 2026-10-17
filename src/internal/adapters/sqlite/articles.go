package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"tangentry/internal/articles"
	"tangentry/internal/content"
)

func (s *Store) ArticleSource(ctx context.Context, id string) (content.Item, error) {
	var payload string
	err := s.db.QueryRowContext(ctx, `SELECT payload FROM saved_content WHERE id=? UNION ALL SELECT payload FROM subscription_inbox WHERE content_id=? LIMIT 1`, id, id).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return content.Item{}, articles.ErrNotFound
	}
	if err != nil {
		return content.Item{}, err
	}
	var item content.Item
	err = json.Unmarshal([]byte(payload), &item)
	return item, err
}
func (s *Store) GetArticle(ctx context.Context, id string) (articles.Document, error) {
	var payload string
	err := s.db.QueryRowContext(ctx, `SELECT payload FROM article_documents WHERE content_id=?`, id).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return articles.Document{}, articles.ErrNotFound
	}
	if err != nil {
		return articles.Document{}, err
	}
	var doc articles.Document
	err = json.Unmarshal([]byte(payload), &doc)
	return doc, err
}
func (s *Store) SaveArticle(ctx context.Context, doc articles.Document, expected int64) (articles.Document, error) {
	if doc.Revision != expected+1 || expected < 0 {
		return articles.Document{}, articles.ErrInvalid
	}
	if err := articles.Prepare(&doc); err != nil {
		return articles.Document{}, err
	}
	payload, err := json.Marshal(doc)
	if err != nil {
		return articles.Document{}, err
	}
	var result sql.Result
	if expected == 0 {
		result, err = s.db.ExecContext(ctx, `INSERT INTO article_documents(content_id,payload,revision) VALUES(?,?,?) ON CONFLICT(content_id) DO NOTHING`, doc.ContentID, string(payload), doc.Revision)
	} else {
		result, err = s.db.ExecContext(ctx, `UPDATE article_documents SET payload=?,revision=? WHERE content_id=? AND revision=?`, string(payload), doc.Revision, doc.ContentID, expected)
	}
	if err != nil {
		return articles.Document{}, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return articles.Document{}, err
	}
	if count != 1 {
		return articles.Document{}, articles.ErrConflict
	}
	return doc, nil
}
