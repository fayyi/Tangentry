package articles

import (
	"context"
	"errors"
	"strings"
	"sync"
	"tangentry/internal/content"
	"testing"
)

type testRepository struct{}

func (testRepository) ArticleSource(context.Context, string) (content.Item, error) {
	return content.Item{ID: "rss:" + strings.Repeat("a", 64), Platform: "rss", Kind: "article", URL: "https://example.com"}, nil
}
func (testRepository) GetArticle(context.Context, string) (Document, error) {
	return Document{}, ErrNotFound
}
func (testRepository) SaveArticle(ctx context.Context, doc Document, _ int64) (Document, error) {
	return doc, ctx.Err()
}

type waitingExtractor struct{ entered chan struct{} }

func (e waitingExtractor) Fetch(ctx context.Context, _ string) (Document, error) {
	e.entered <- struct{}{}
	<-ctx.Done()
	return Document{}, ctx.Err()
}
func TestFetchQueueIsBoundedAndCancellationReleasesSlots(t *testing.T) {
	id := "rss:" + strings.Repeat("a", 64)
	entered := make(chan struct{}, 2)
	service := New(testRepository{}, waitingExtractor{entered})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var group sync.WaitGroup
	for i := 0; i < 2; i++ {
		group.Go(func() {
			if _, err := service.Fetch(ctx, id, 0); !errors.Is(err, context.Canceled) {
				t.Error(err)
			}
		})
	}
	<-entered
	<-entered
	if _, err := service.Fetch(context.Background(), id, 0); !errors.Is(err, ErrBusy) {
		t.Fatal(err)
	}
	cancel()
	group.Wait()
	if len(service.fetching) != 0 {
		t.Fatal("slots not released")
	}
}
