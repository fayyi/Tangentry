package sqlite

import (
	"context"
	"path/filepath"
	"strings"
	"tangentry/internal/content"
	"tangentry/internal/readstate"
	"tangentry/internal/subscriptions"
	"testing"
)

func TestReadStateSurvivesSnapshotsAndReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "states.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	a := "rss:" + strings.Repeat("a", 64)
	b := "rss:" + strings.Repeat("b", 64)
	service := readstate.New(store)
	states, err := service.Batch(ctx, []string{a, b})
	if err != nil || len(states) != 2 || states[0].Read || states[0].UpdatedAt != 0 {
		t.Fatal("unseen state should be unread")
	}
	if _, err := service.Set(ctx, []string{a, b}, false); err != nil {
		t.Fatal(err)
	}
	var count int
	store.db.QueryRow(`SELECT COUNT(*) FROM read_state`).Scan(&count)
	if count != 0 {
		t.Fatal("passive reads created records")
	}
	first, err := store.SetReadStates(ctx, []string{a}, true, 100)
	if err != nil {
		t.Fatal(err)
	}
	again, err := store.SetReadStates(ctx, []string{a}, true, 200)
	if err != nil || again[0] != first[0] {
		t.Fatal("idempotent mark changed timestamp")
	}
	id := "rss:feed:" + strings.Repeat("d", 64)
	store.CreateSubscription(ctx, subscriptions.Source{ID: id, CreatorID: "https://feed.example/rss", Name: "fixture"})
	item := content.Item{ID: a, Platform: "rss", Kind: "article", Title: "changed metadata", URL: "https://feed.example/a"}
	store.UpdateSubscription(ctx, id, subscriptions.Snapshot{Items: []content.Item{item}, Total: 1}, 10)
	store.UpdateSubscription(ctx, id, subscriptions.Snapshot{Items: []content.Item{}, Total: 0}, 20)
	store.UpdateSubscription(ctx, id, subscriptions.Snapshot{Items: []content.Item{item}, Total: 1}, 30)
	store.DeleteSubscription(ctx, id)
	store.Close()
	store, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	states, err = readstate.New(store).Batch(ctx, []string{a, b})
	if err != nil || !states[0].Read || states[1].Read || states[0].UpdatedAt != 100 {
		t.Fatal("snapshot or deletion lost state")
	}
	if _, err := store.SetReadStates(ctx, []string{a}, false, 300); err != nil {
		t.Fatal(err)
	}
	states, _ = readstate.New(store).Batch(ctx, []string{a})
	if states[0].Read || states[0].UpdatedAt != 300 {
		t.Fatal("explicit unread failed")
	}
	for _, table := range []string{"saved_content", "notes", "downloads", "reading_progress"} {
		store.db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count)
		if count != 0 {
			t.Fatalf("read state changed %s", table)
		}
	}
	store.db.QueryRow(`SELECT MAX(version) FROM schema_version`).Scan(&count)
	if count != latestVersion(t) {
		t.Fatal("schema version")
	}
}

func TestReadStateBulkTransactionRollsBack(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "atomic.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	a := "youtube:dQw4w9WgXcQ"
	b := "bilibili:BV17x411w7KC"
	_, err = store.db.Exec(`CREATE TRIGGER reject_second BEFORE INSERT ON read_state WHEN NEW.content_id='bilibili:BV17x411w7KC' BEGIN SELECT RAISE(ABORT,'explicit test storage failure'); END`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := readstate.New(store).Set(ctx, []string{a, b}, true); err == nil {
		t.Fatal("storage failure lost")
	}
	states, err := readstate.New(store).Batch(ctx, []string{a, b})
	if err != nil || states[0].Read || states[1].Read {
		t.Fatal("part of failed batch persisted")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := readstate.New(store).Set(canceled, []string{a}, true); err == nil {
		t.Fatal("canceled batch persisted")
	}
}
