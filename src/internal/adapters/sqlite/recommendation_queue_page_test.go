package sqlite

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"tangentry/internal/content"
	"tangentry/internal/discovery"
	"testing"
	"time"
)

func TestQueueCursorDoesNotSkipItemsAfterExposureChanges(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "queue.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	scope := discovery.QueueScope{Platform: "youtube", Account: strings.Repeat("a", 64)}
	for batch := 0; batch < 2; batch++ {
		items := make([]content.Item, 300)
		for i := range items {
			id := fmt.Sprintf("queue%06d", batch*300+i)
			items[i] = content.Item{ID: "youtube:" + id, Platform: "youtube", PlatformID: id, Kind: "video", Title: id, URL: "https://www.youtube.com/watch?v=" + id}
		}
		if err := s.EnqueueRecommendations(ctx, scope, items, 100); err != nil {
			t.Fatal(err)
		}
	}
	cursor := discovery.QueueCursor{Account: scope.Account, Snapshot: 200}
	seen := map[string]bool{}
	for {
		page, err := s.PageQueuedRecommendations(ctx, scope, cursor, 60)
		if err != nil {
			t.Fatal(err)
		}
		if len(page) == 0 {
			break
		}
		for _, entry := range page {
			if seen[entry.Item.ID] {
				t.Fatal("duplicate", entry.Item.ID)
			}
			seen[entry.Item.ID] = true
			if err := s.MarkQueuedRecommendation(ctx, scope, entry.Item.ID, false, 300); err != nil {
				t.Fatal(err)
			}
		}
		last := page[len(page)-1]
		cursor.ID, cursor.FetchedAt = last.Item.ID, last.FetchedAt
	}
	if len(seen) != 600 {
		t.Fatal("inventory skipped", len(seen))
	}
}

func TestQueueServicePagesMixedBrowsingStatesWithStableSnapshot(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "queue.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	scope := discovery.QueueScope{Platform: "youtube", Account: strings.Repeat("a", 64)}
	items := make([]content.Item, 5)
	for i := range items {
		id := fmt.Sprintf("mixed%06d", i)
		items[i] = content.Item{ID: "youtube:" + id, Platform: "youtube", PlatformID: id, Kind: "video", Title: id, URL: "https://www.youtube.com/watch?v=" + id}
	}
	before := time.Now().Add(-time.Minute).UnixMilli()
	if err := s.EnqueueRecommendations(ctx, scope, items[:4], before); err != nil {
		t.Fatal(err)
	}
	for _, i := range []int{0, 2} {
		if err := s.MarkQueuedRecommendation(ctx, scope, items[i].ID, false, before+1); err != nil {
			t.Fatal(err)
		}
	}
	queue := discovery.NewRecommendationQueue(s, func(string) discovery.QueueScope { return scope })
	first, err := queue.PendingPage(ctx, "youtube", 2, "")
	if err != nil || len(first.Items) != 2 || first.Next == "" || first.Items[0].Item.ID != items[1].ID || first.Items[1].Item.ID != items[3].ID {
		t.Fatal(first, err)
	}
	// New arrivals belong to the next browsing snapshot, not this cursor chain.
	after := time.Now().Add(time.Minute).UnixMilli()
	for _, entry := range first.Items {
		if err := s.MarkQueuedRecommendation(ctx, scope, entry.Item.ID, false, after); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.MarkQueuedRecommendation(ctx, scope, items[2].ID, true, after); err != nil {
		t.Fatal(err)
	}
	if err := s.EnqueueRecommendations(ctx, scope, items[4:], after); err != nil {
		t.Fatal(err)
	}
	second, err := queue.PendingPage(ctx, "youtube", 2, first.Next)
	if err != nil || len(second.Items) != 1 || second.Next != "" || second.Items[0].Item.ID != items[0].ID {
		t.Fatal("mixed-state continuation skipped or repeated content", second, err)
	}
}

func TestQueuePageSkipsMismatchedPayloadWithoutHidingValidRows(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "queue.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	scope := discovery.QueueScope{Platform: "youtube", Account: strings.Repeat("a", 64)}
	items := make([]content.Item, 3)
	for i := range items {
		id := fmt.Sprintf("queue%06d", i)
		items[i] = content.Item{ID: "youtube:" + id, Platform: "youtube", PlatformID: id, Kind: "video", Title: id, URL: "https://www.youtube.com/watch?v=" + id}
	}
	if err := s.EnqueueRecommendations(ctx, scope, items, 100); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(items[2])
	if _, err := s.db.Exec(`UPDATE recommendation_queue SET payload=? WHERE content_id=?`, string(raw), items[0].ID); err != nil {
		t.Fatal(err)
	}
	page, err := s.PageQueuedRecommendations(ctx, scope, discovery.QueueCursor{Account: scope.Account, Snapshot: 200}, 2)
	if err != nil || len(page) != 2 || page[0].Item.ID != items[1].ID || page[1].Item.ID != items[2].ID {
		t.Fatal(page, err)
	}
}
