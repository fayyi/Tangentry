package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"tangentry/internal/articles"
	"tangentry/internal/content"
	"tangentry/internal/notes"
	"tangentry/internal/subscriptions"
	"testing"
)

type articleExtractorFunc func(context.Context, string) (articles.Document, error)

func (f articleExtractorFunc) Fetch(ctx context.Context, url string) (articles.Document, error) {
	return f(ctx, url)
}
func TestOriginalTextCASFailureCancellationReopenAndExistingData(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "articles.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	item := inboxTestItem(33, "article")
	item.Description = "原 RSS 文字"
	source := inboxTestSource(t, s, "正文来源", "https://example.com/rss")
	if _, err = s.UpdateSubscription(ctx, source.ID, subscriptions.Snapshot{Items: []content.Item{item}}, 10); err != nil {
		t.Fatal(err)
	}
	if err = s.DeleteSubscription(ctx, source.ID); err != nil {
		t.Fatal(err)
	}
	note, err := notes.NewService(s).Create(ctx, notes.Input{ContentID: item.ID, Body: "## 已有笔记"})
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	extract := articleExtractorFunc(func(ctx context.Context, raw string) (articles.Document, error) {
		calls++
		if raw != item.URL {
			t.Fatal(raw)
		}
		return articles.Document{SourceURL: raw, Title: "原文", Blocks: []articles.Block{{Kind: "paragraph", Text: "全文😀"}}}, nil
	})
	service := articles.New(s, extract)
	if _, err = service.Get(ctx, item.ID); !errors.Is(err, articles.ErrNotFound) || calls != 0 {
		t.Fatal(err, calls)
	}
	doc, err := service.Fetch(ctx, item.ID, 0)
	if err != nil || doc.Revision != 1 {
		t.Fatal(doc, err)
	}
	if _, err = service.Fetch(ctx, item.ID, 0); !errors.Is(err, articles.ErrConflict) || calls != 1 {
		t.Fatal(err, calls)
	}
	if _, err = articles.New(s, articleExtractorFunc(func(context.Context, string) (articles.Document, error) {
		return articles.Document{}, articles.ErrUnavailable
	})).Fetch(ctx, item.ID, 1); !errors.Is(err, articles.ErrUnavailable) {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err = service.Fetch(canceled, item.ID, 1); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	unchanged, _ := service.Get(ctx, item.ID)
	if !reflect.DeepEqual(doc, unchanged) {
		t.Fatal(unchanged)
	}
	// Two writers with the same base revision cannot silently overwrite each other.
	next := doc
	next.Revision = 2
	next.Blocks = []articles.Block{{Kind: "paragraph", Text: "新的全文"}}
	if _, err = s.SaveArticle(ctx, next, 1); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveArticle(ctx, next, 1); !errors.Is(err, articles.ErrConflict) {
		t.Fatal(err)
	}
	var snapshot string
	s.db.QueryRow(`SELECT payload FROM subscription_inbox WHERE content_id=?`, item.ID).Scan(&snapshot)
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	loaded, err := s.GetArticle(ctx, item.ID)
	if err != nil || loaded.Revision != 2 || loaded.Text != "新的全文" {
		t.Fatal(loaded, err)
	}
	old, err := s.ArticleSource(ctx, item.ID)
	if err != nil || old.Description != item.Description {
		t.Fatal(old, err)
	}
	rows, err := notes.NewService(s).List(ctx, item.ID)
	if err != nil || len(rows) != 1 || !reflect.DeepEqual(note, rows[0]) {
		t.Fatal(rows, err)
	}
	var after string
	s.db.QueryRow(`SELECT payload FROM subscription_inbox WHERE content_id=?`, item.ID).Scan(&after)
	if after != snapshot {
		t.Fatal("changed feed")
	}
}
