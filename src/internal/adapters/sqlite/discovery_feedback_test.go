package sqlite

import (
	"context"
	"path/filepath"
	"testing"

	"tangentry/internal/discovery"
)

func TestDiscoveryFeedbackPersistsAndFiltersExpiredRows(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "feedback.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	if err := store.SaveDiscoveryFeedback(ctx, discovery.Feedback{ID: "hide:item-1", Type: discovery.FeedbackHide, Target: discovery.TargetItem, Value: "youtube:item-1", CreatedAt: 10}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveDiscoveryFeedback(ctx, discovery.Feedback{ID: "less:creator-1", Type: discovery.FeedbackLess, Target: discovery.TargetCreator, Value: "creator-1", ExpiresAt: 20, CreatedAt: 11}); err != nil {
		t.Fatal(err)
	}
	values, err := store.ListDiscoveryFeedback(ctx, 15)
	if err != nil || len(values) != 2 {
		t.Fatalf("active feedback: %#v %v", values, err)
	}
	values, err = store.ListDiscoveryFeedback(ctx, 20)
	if err != nil || len(values) != 1 || values[0].ID != "hide:item-1" {
		t.Fatalf("expired feedback: %#v %v", values, err)
	}
	if err := store.DeleteDiscoveryFeedback(ctx, "hide:item-1"); err != nil {
		t.Fatal(err)
	}
	if err := store.ClearDiscoveryFeedback(ctx); err != nil {
		t.Fatal(err)
	}
}
