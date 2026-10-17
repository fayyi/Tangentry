package subscriptions_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"tangentry/internal/adapters/sqlite"
	"tangentry/internal/subscriptions"
)

func TestBatchOperationsKeepPerSourceFailuresAndApplyPolicies(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "batch.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service := subscriptions.NewService(store, nil)
	defer service.Close()

	first, created, err := service.Create(ctx, subscriptions.Input{Type: "rss", Query: "https://example.com/one"})
	if err != nil || !created {
		t.Fatal(first, created, err)
	}
	second, created, err := service.Create(ctx, subscriptions.Input{Type: "rss", Query: "https://example.com/two"})
	if err != nil || !created {
		t.Fatal(second, created, err)
	}
	group, _, err := subscriptions.NewGroupService(store).Create(ctx, "批量测试")
	if err != nil {
		t.Fatal(err)
	}

	missing := "rss:feed:" + strings.Repeat("a", 64)
	results, err := service.BatchAssignGroup(ctx, []string{first.ID, missing}, group.ID, "")
	if err != nil || len(results) != 2 || results[0].Err != nil || !errors.Is(results[1].Err, subscriptions.ErrNotFound) {
		t.Fatalf("group batch = %+v, %v", results, err)
	}
	updated, err := service.Get(ctx, first.ID)
	if err != nil || updated.Source.GroupID != group.ID {
		t.Fatalf("group was not assigned: %+v, %v", updated.Source, err)
	}

	results, err = service.BatchConfigureRefresh(ctx, []string{first.ID, second.ID}, subscriptions.RefreshPolicy{Enabled: false, IntervalSeconds: 900})
	if err != nil || len(results) != 2 || results[0].Err != nil || results[1].Err != nil {
		t.Fatalf("refresh batch = %+v, %v", results, err)
	}
	for _, id := range []string{first.ID, second.ID} {
		source, getErr := service.Get(ctx, id)
		if getErr != nil || source.Source.Refresh.Enabled || source.Source.Refresh.IntervalSeconds != 900 {
			t.Fatalf("policy not applied to %s: %+v, %v", id, source.Source.Refresh, getErr)
		}
	}

	results, err = service.BatchDelete(ctx, []string{first.ID, missing})
	if err != nil || len(results) != 2 || results[0].Err != nil || !errors.Is(results[1].Err, subscriptions.ErrNotFound) {
		t.Fatalf("delete batch = %+v, %v", results, err)
	}
	if _, err = service.Get(ctx, first.ID); !errors.Is(err, subscriptions.ErrNotFound) {
		t.Fatal("successful batch delete did not remove source")
	}
}

func TestNewPollingSourcesUseTwoHourDefault(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "defaults.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service := subscriptions.NewService(store, nil)
	defer service.Close()
	source, created, err := service.Create(ctx, subscriptions.Input{Type: "rss", Query: "https://example.com/default"})
	if err != nil || !created || !source.Refresh.Enabled || source.Refresh.IntervalSeconds != subscriptions.DefaultInterval {
		t.Fatalf("unexpected default refresh state: %+v, %v", source.Refresh, err)
	}
	if subscriptions.DefaultInterval != 7200 {
		t.Fatalf("default interval = %d", subscriptions.DefaultInterval)
	}
}
