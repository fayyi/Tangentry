package sqlite

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"tangentry/internal/content"
	"tangentry/internal/subscriptions"
	"testing"
)

func TestRefreshCommitRollbackAndMetadataOnlyKeepsExactSnapshot(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "atomic.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	id := "rss:feed:" + strings.Repeat("b", 64)
	_, _, err = s.CreateSubscription(ctx, subscriptions.Source{ID: id, CreatorID: "https://example.com/feed", Name: "fixture", CreatedAt: 1})
	if err != nil {
		t.Fatal(err)
	}
	var n int
	s.db.QueryRow(`SELECT COUNT(*) FROM subscription_refresh`).Scan(&n)
	if n != 0 {
		t.Fatal("default read created metadata")
	}
	snapshot := subscriptions.Snapshot{Items: []content.Item{}, Total: 0, Title: "old"}
	_, err = s.UpdateSubscription(ctx, id, snapshot, 10)
	if err != nil {
		t.Fatal(err)
	}
	var before string
	s.db.QueryRow(`SELECT snapshot FROM subscriptions WHERE id=?`, id).Scan(&before)
	record := subscriptions.DefaultRefresh()
	record.State.LastCheckedAt = 20
	record.Validators.ETag = `"one"`
	_, err = s.CommitSubscriptionRefresh(ctx, id, nil, 0, record)
	if err != nil {
		t.Fatal(err)
	}
	var after string
	var at int64
	s.db.QueryRow(`SELECT snapshot,updated_at FROM subscriptions WHERE id=?`, id).Scan(&after, &at)
	if before != after || at != 10 {
		t.Fatal("metadata commit rewrote snapshot")
	}
	_, err = s.db.Exec(`CREATE TRIGGER reject_refresh BEFORE UPDATE ON subscription_refresh BEGIN SELECT RAISE(ABORT,'explicit fixture failure'); END`)
	if err != nil {
		t.Fatal(err)
	}
	snapshot.Title = "must rollback"
	changed := record
	changed.Validators.ETag = `"two"`
	changed.State.ContentRevision = 2
	if _, err = s.CommitSubscriptionRefresh(ctx, id, &snapshot, 30, changed); err == nil {
		t.Fatal("metadata failure not reported")
	}
	s.db.QueryRow(`SELECT snapshot,updated_at FROM subscriptions WHERE id=?`, id).Scan(&after, &at)
	actual, err := s.GetSubscriptionRefresh(ctx, id)
	if err != nil || !reflect.DeepEqual(actual, record) || after != before || at != 10 {
		t.Fatal("partial commit", actual, err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err = s.CommitSubscriptionRefresh(canceled, id, nil, 0, changed); err == nil {
		t.Fatal("cancellation committed")
	}
}
