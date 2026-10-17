package httptransport

import (
	"context"

	"tangentry/internal/articles"
	"tangentry/internal/content"
)

type ContentService interface {
	Page(context.Context, content.Query) (content.Page, error)
	Get(context.Context, string) (content.Item, error)
	Save(context.Context, content.Item) (content.Item, error)
	Delete(context.Context, string) error
}

type ArticleService interface {
	Get(context.Context, string) (articles.Document, error)
	Fetch(context.Context, string, int64) (articles.Document, error)
}
