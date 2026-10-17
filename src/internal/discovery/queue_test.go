package discovery

import (
	"context"
	"errors"
	"strings"
	"testing"

	"tangentry/internal/content"
)

type queueFixture struct {
	writes, reads, marks int
	onRead, onWrite      func()
}

func (r *queueFixture) EnqueueRecommendations(context.Context, QueueScope, []content.Item, int64) error {
	r.writes++
	if r.onWrite != nil {
		r.onWrite()
	}
	return nil
}
func (r *queueFixture) ListQueuedRecommendations(context.Context, QueueScope, int) ([]QueuedItem, error) {
	r.reads++
	if r.onRead != nil {
		r.onRead()
	}
	return []QueuedItem{}, nil
}

func TestQueueDiscardsResponsesWhenAccountChangesDuringStorage(t *testing.T) {
	for _, operation := range []string{"read", "write"} {
		t.Run(operation, func(t *testing.T) {
			scope := QueueScope{Platform: "youtube", Account: strings.Repeat("a", 64)}
			change := func() { scope.Account = strings.Repeat("b", 64) }
			r := &queueFixture{}
			q := NewRecommendationQueue(r, func(string) QueueScope { return scope })
			if operation == "read" {
				r.onRead = change
				items, err := q.Pending(context.Background(), "youtube", 20)
				if !errors.Is(err, ErrUnavailable) || items != nil {
					t.Fatal("old account inventory returned", items, err)
				}
			} else {
				r.onWrite = change
				feed, err := q.Fetch(context.Background(), "youtube", func(context.Context) (Feed, error) {
					return Feed{Items: []content.Item{{ID: "youtube:abcdefghijk"}}}, nil
				})
				if !errors.Is(err, ErrUnavailable) || feed.Items != nil {
					t.Fatal("old account response returned", feed, err)
				}
			}
		})
	}
}
func (r *queueFixture) MarkQueuedRecommendation(context.Context, QueueScope, string, bool, int64) error {
	r.marks++
	return nil
}

func TestQueueRejectsAccountChangesAndUsesLocalReads(t *testing.T) {
	r := &queueFixture{}
	scope := QueueScope{Platform: "youtube", Account: strings.Repeat("a", 64)}
	q := NewRecommendationQueue(r, func(string) QueueScope { return scope })
	ctx := context.Background()
	if _, err := q.Pending(ctx, "youtube", 20); err != nil || r.reads != 1 {
		t.Fatal(err, r)
	}
	if err := q.Mark(ctx, "youtube", "bilibili:BV17x411w7KC", true); !errors.Is(err, ErrInvalidQuery) || r.marks != 0 {
		t.Fatal(err, r)
	}
	if err := q.Mark(ctx, "youtube", "youtube:abcdefghijk", false); err != nil || r.marks != 1 {
		t.Fatal(err, r)
	}
	_, err := q.Fetch(ctx, "youtube", func(context.Context) (Feed, error) {
		scope.Account = strings.Repeat("b", 64)
		return Feed{Items: []content.Item{}}, nil
	})
	if !errors.Is(err, ErrUnavailable) || r.writes != 0 {
		t.Fatal("old account response persisted", err, r)
	}
	_, err = q.Fetch(ctx, "youtube", func(context.Context) (Feed, error) { return Feed{Items: []content.Item{}}, nil })
	if err != nil || r.writes != 1 {
		t.Fatal(err, r)
	}
	_, err = q.Fetch(ctx, "youtube", func(context.Context) (Feed, error) { return Feed{}, ErrUnavailable })
	if !errors.Is(err, ErrUnavailable) || r.writes != 1 {
		t.Fatal(err, r)
	}
}
