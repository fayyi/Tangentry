package discovery

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

type pageFixture struct {
	queueFixture
	calls  int
	cursor QueueCursor
	items  []QueuedItem
}

func (r *pageFixture) PageQueuedRecommendations(_ context.Context, _ QueueScope, cursor QueueCursor, _ int) ([]QueuedItem, error) {
	r.calls++
	r.cursor = cursor
	return r.items, nil
}

func TestQueuePageRejectsIncompleteAndForeignCursors(t *testing.T) {
	scope := QueueScope{Platform: "youtube", Account: strings.Repeat("a", 64)}
	r := &pageFixture{}
	q := NewRecommendationQueue(r, func(string) QueueScope { return scope })
	q.clock = func() time.Time { return time.UnixMilli(1000) }
	valid := map[string]any{"account": scope.Account, "snapshot": 900, "fetchedAt": 100, "id": "youtube:abcdefghijk", "seen": false}
	for _, field := range []string{"account", "snapshot", "fetchedAt", "id"} {
		t.Run("missing-"+field, func(t *testing.T) {
			input := make(map[string]any)
			for key, value := range valid {
				if key != field {
					input[key] = value
				}
			}
			raw, _ := json.Marshal(input)
			_, err := q.PendingPage(context.Background(), "youtube", 60, base64.RawURLEncoding.EncodeToString(raw))
			if !errors.Is(err, ErrInvalidQuery) || r.calls != 0 {
				t.Fatal("incomplete cursor reached storage", err, r.calls)
			}
		})
	}
	for _, input := range []QueueCursor{
		{Account: strings.Repeat("b", 64), Snapshot: 900, FetchedAt: 100, ID: "youtube:abcdefghijk"},
		{Account: scope.Account, Snapshot: 1100, FetchedAt: 100, ID: "youtube:abcdefghijk"},
		{Account: scope.Account, Snapshot: 900, FetchedAt: 950, ID: "youtube:abcdefghijk"},
		{Account: scope.Account, Snapshot: 900, FetchedAt: 100, ID: "bilibili:BV17x411w7KC"},
	} {
		raw, _ := json.Marshal(input)
		if _, err := q.PendingPage(context.Background(), "youtube", 60, base64.RawURLEncoding.EncodeToString(raw)); !errors.Is(err, ErrInvalidQuery) {
			t.Fatal(input, err)
		}
	}
	if r.calls != 0 {
		t.Fatal("invalid cursor reached storage")
	}
	raw, _ := json.Marshal(valid)
	if _, err := q.PendingPage(context.Background(), "youtube", 60, base64.RawURLEncoding.EncodeToString(raw)); err != nil || r.calls != 1 || r.cursor.Snapshot != 900 {
		t.Fatal(err, r)
	}
}
