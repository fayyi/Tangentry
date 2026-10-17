package subscriptions_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"tangentry/internal/adapters/feeds"
	"tangentry/internal/adapters/sqlite"
	"tangentry/internal/content"
	"tangentry/internal/discovery"
	"tangentry/internal/readstate"
	"tangentry/internal/subscriptions"
	"testing"
	"time"
)

func rssSnapshot(title string) subscriptions.Snapshot {
	return subscriptions.Snapshot{Title: "feed", Total: 1, Items: []content.Item{{ID: "rss:" + strings.Repeat("a", 64), Platform: "rss", PlatformID: strings.Repeat("a", 64), Kind: "article", Title: title, URL: "https://example.com/article"}}}
}

func TestRefreshHealthStatusMapping(t *testing.T) {
	now := int64(1800000000)
	cases := []struct {
		name    string
		state   subscriptions.RefreshState
		updated int64
		want    subscriptions.RefreshStatus
	}{
		{"paused", subscriptions.RefreshState{Enabled: false}, 0, subscriptions.StatusPaused},
		{"never checked", subscriptions.RefreshState{Enabled: true}, 0, subscriptions.StatusNeverChecked},
		{"healthy", subscriptions.RefreshState{Enabled: true, LastCheckedAt: now - 60}, now - 60, subscriptions.StatusHealthy},
		{"stale", subscriptions.RefreshState{Enabled: true, LastCheckedAt: now - 60}, now - 31*24*60*60, subscriptions.StatusStale},
		{"auth", subscriptions.RefreshState{Enabled: true, Failures: 1, Status: subscriptions.StatusAuthRequired}, now - 60, subscriptions.StatusAuthRequired},
		{"limited", subscriptions.RefreshState{Enabled: true, Failures: 1, Status: subscriptions.StatusRateLimited}, now - 60, subscriptions.StatusRateLimited},
		{"not found", subscriptions.RefreshState{Enabled: true, Failures: 1, Status: subscriptions.StatusNotFound}, now - 60, subscriptions.StatusNotFound},
		{"upstream", subscriptions.RefreshState{Enabled: true, Failures: 1, Status: subscriptions.StatusUpstreamErr}, now - 60, subscriptions.StatusUpstreamErr},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := subscriptions.StatusFor(tc.state, tc.updated, now); got != tc.want {
				t.Fatalf("status = %q, want %q", got, tc.want)
			}
		})
	}
}
func TestRSSChecksPersistValidatorsAndOnlyChangeSnapshotWhenChanged(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "feeds.db")
	store, err := sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { store.Close() }()
	now := time.Unix(1800000000, 0)
	calls := 0
	validators := subscriptions.Validators{ETag: `"first"`, ResourceURL: "https://example.com/feed"}
	current := subscriptions.ConditionalResult{Snapshot: rssSnapshot("first"), Validators: validators}
	var failure error
	fetch := func(_ context.Context, _ string, v subscriptions.Validators) (subscriptions.ConditionalResult, error) {
		calls++
		if calls == 1 && v != (subscriptions.Validators{}) || calls > 1 && v != validators {
			t.Fatal("wrong validators", v)
		}
		return current, failure
	}
	service := subscriptions.NewService(store, nil, subscriptions.WithClock(func() time.Time { return now }), subscriptions.WithConditionalFeeds(fetch))
	source, created, err := service.Create(ctx, subscriptions.Input{Type: "rss", Query: validators.ResourceURL})
	if err != nil || !created || calls != 0 || !source.Refresh.Enabled || source.Refresh.IntervalSeconds != subscriptions.DefaultInterval {
		t.Fatal(source, err, calls)
	}
	first, err := service.Update(ctx, source.ID)
	if err != nil || first.Source.Refresh.ContentRevision != 1 {
		t.Fatal(first, err)
	}
	if _, err = readstate.New(store).Set(ctx, []string{first.Items[0].ID}, true); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Minute)
	current = subscriptions.ConditionalResult{NotModified: true, Validators: validators}
	same, err := service.Update(ctx, source.ID)
	if err != nil || !same.NotModified || same.Source.LastUpdatedAt != first.Source.LastUpdatedAt || !reflect.DeepEqual(same.Items, first.Items) || same.Source.Refresh.LastCheckedAt != now.Unix() || same.Source.Refresh.ContentRevision != 1 {
		t.Fatal(same, err)
	}
	// A server without 304 support can still return identical content.
	now = now.Add(time.Minute)
	current = subscriptions.ConditionalResult{Snapshot: rssSnapshot("first"), Validators: validators}
	same, err = service.Update(ctx, source.ID)
	if err != nil || !same.NotModified || same.Source.LastUpdatedAt != first.Source.LastUpdatedAt {
		t.Fatal(same, err)
	}
	current.Snapshot = rssSnapshot("changed")
	changed, err := service.Update(ctx, source.ID)
	if err != nil || changed.NotModified || changed.Source.Refresh.ContentRevision != 2 || changed.Items[0].Title != "changed" {
		t.Fatal(changed, err)
	}
	states, err := readstate.New(store).Batch(ctx, []string{changed.Items[0].ID})
	if err != nil || len(states) != 1 || !states[0].Read {
		t.Fatal("read state reset", states, err)
	}
	bytes, _ := json.Marshal(changed.Source)
	if strings.Contains(string(bytes), "ETag") || strings.Contains(string(bytes), "ResourceURL") {
		t.Fatal("private validators exposed")
	}
	service.Close()
	store.Close()
	store, err = sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	service = subscriptions.NewService(store, nil, subscriptions.WithClock(func() time.Time { return now }), subscriptions.WithConditionalFeeds(fetch))
	defer service.Close()
	persisted, err := store.GetSubscriptionRefresh(ctx, source.ID)
	if err != nil || persisted.Validators != validators || persisted.State.ContentRevision != 2 {
		t.Fatal("validators/revision lost", persisted, err)
	}
	failure = &feeds.FetchError{Message: "private-secret-from-upstream", Restricted: true, RetryAfterSeconds: 7200}
	if _, err = service.ConfigureRefresh(ctx, source.ID, subscriptions.RefreshPolicy{Enabled: true, IntervalSeconds: 900}); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Update(ctx, source.ID); err == nil {
		t.Fatal("failure hidden")
	}
	cached, err := service.Get(ctx, source.ID)
	if err != nil || !reflect.DeepEqual(cached.Items, changed.Items) || cached.Source.LastUpdatedAt != changed.Source.LastUpdatedAt || cached.Source.Refresh.LastCheckedAt != changed.Source.Refresh.LastCheckedAt || cached.Source.Refresh.NextCheckAt != now.Unix()+7200 || strings.Contains(cached.Source.Refresh.Message, "secret") {
		t.Fatal("failed check changed cached data", cached, err)
	}
	for _, scenario := range []struct {
		status int
		want   string
		state  subscriptions.RefreshStatus
	}{{401, "访问密钥", subscriptions.StatusAuthRequired}, {403, "鉴权配置", subscriptions.StatusAuthRequired}, {404, "HTTP 404", subscriptions.StatusNotFound}, {429, "请求过于频繁", subscriptions.StatusRateLimited}, {503, "HTTP 503", subscriptions.StatusUpstreamErr}} {
		failure = &feeds.FetchError{Message: "private-secret-from-upstream", StatusCode: scenario.status}
		if _, err = service.Update(ctx, source.ID); err == nil {
			t.Fatal("failure hidden", scenario.status)
		}
		cached, err = service.Get(ctx, source.ID)
		if err != nil || !strings.Contains(cached.Source.Refresh.Message, scenario.want) || cached.Source.Refresh.Status != scenario.state || strings.Contains(cached.Source.Refresh.Message, "secret") || !reflect.DeepEqual(cached.Items, changed.Items) {
			t.Fatal("missing safe persisted diagnostic", scenario.status, err)
		}
	}
	failure = &feeds.FetchError{TimedOut: true, Message: "private-secret-timeout"}
	if _, err = service.Update(ctx, source.ID); err == nil {
		t.Fatal("timeout hidden")
	}
	persisted, err = store.GetSubscriptionRefresh(ctx, source.ID)
	if err != nil || !strings.Contains(persisted.State.Message, "响应超时") || persisted.State.Status != subscriptions.StatusUpstreamErr || strings.Contains(persisted.State.Message, "secret") {
		t.Fatal("timeout diagnosis not safely persisted", err)
	}
	failure = nil
	current = subscriptions.ConditionalResult{NotModified: true, Validators: validators}
	recovered, err := service.Update(ctx, source.ID)
	if err != nil || recovered.Source.Refresh.Failures != 0 || recovered.Source.Refresh.Message != "" || recovered.Source.Refresh.NextCheckAt != now.Unix()+900 {
		t.Fatal(recovered, err)
	}
	if err = service.Delete(ctx, source.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = store.GetSubscriptionRefresh(ctx, source.ID); !errors.Is(err, subscriptions.ErrNotFound) {
		t.Fatal("orphan refresh", err)
	}
	if _, err = store.CommitSubscriptionRefresh(ctx, source.ID, nil, 0, persisted); !errors.Is(err, subscriptions.ErrNotFound) {
		t.Fatal("deleted source resurrected", err)
	}
	states, _ = readstate.New(store).Batch(ctx, []string{changed.Items[0].ID})
	if !states[0].Read {
		t.Fatal("delete reset read state")
	}
}
func TestRSSOptInSchedulerDueTimingAndShutdown(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "schedule.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	now := time.Unix(1800000000, 0)
	var calls int32
	service := subscriptions.NewService(store, func(_ context.Context, uid string) (subscriptions.Snapshot, error) {
		atomic.AddInt32(&calls, 1)
		if uid == "12345" {
			return subscriptions.Snapshot{Items: []content.Item{video()}, Total: 1}, nil
		}
		return rssSnapshot("scheduled"), nil
	}, subscriptions.WithClock(func() time.Time { return now }))
	defer service.Close()
	first, _, _ := service.Create(ctx, subscriptions.Input{Type: "rss", Query: "https://example.com/a"})
	second, _, _ := service.Create(ctx, subscriptions.Input{Type: "rss", Query: "https://example.com/b"})
	bili, _, _ := service.Create(ctx, subscriptions.Input{Query: "12345"})
	if err = service.RefreshDue(ctx, now.Add(100*time.Hour)); err != nil || calls != 3 {
		t.Fatal("default-enabled sources did not refresh", calls)
	}
	atomic.StoreInt32(&calls, 0)
	for _, policy := range []subscriptions.RefreshPolicy{{Enabled: true, IntervalSeconds: 0}, {Enabled: true, IntervalSeconds: 899}, {Enabled: true, IntervalSeconds: 604801}} {
		if _, err = service.ConfigureRefresh(ctx, first.ID, policy); !errors.Is(err, subscriptions.ErrInvalid) {
			t.Fatal("invalid policy")
		}
	}
	if _, err = service.ConfigureRefresh(ctx, bili.ID, subscriptions.RefreshPolicy{Enabled: false, IntervalSeconds: 900}); err != nil {
		t.Fatal("B站 refresh policy should be configurable", err)
	}
	enabled, err := service.ConfigureRefresh(ctx, first.ID, subscriptions.RefreshPolicy{Enabled: true, IntervalSeconds: 900})
	if err != nil || enabled.Refresh.NextCheckAt != now.Unix()+900 {
		t.Fatal("enable fetched early", enabled, err)
	}
	_, _ = service.ConfigureRefresh(ctx, second.ID, subscriptions.RefreshPolicy{Enabled: true, IntervalSeconds: 900})
	now = now.Add(899 * time.Second)
	_ = service.RefreshDue(ctx, now)
	if calls != 0 {
		t.Fatal("fetched before due")
	}
	same, _ := service.ConfigureRefresh(ctx, first.ID, subscriptions.RefreshPolicy{Enabled: true, IntervalSeconds: 900})
	if same.Refresh.NextCheckAt != enabled.Refresh.NextCheckAt {
		t.Fatal("same policy postponed")
	}
	now = now.Add(time.Second)
	if err = service.RefreshDue(ctx, now); err != nil || calls != 2 {
		t.Fatal("one tick must fetch every due source", calls, err)
	}
	if err = service.RefreshDue(ctx, now); err != nil || calls != 2 {
		t.Fatal("same tick must not refetch sources", calls, err)
	}
	_, _ = service.ConfigureRefresh(ctx, first.ID, subscriptions.RefreshPolicy{Enabled: false, IntervalSeconds: 900})
	_, _ = service.ConfigureRefresh(ctx, second.ID, subscriptions.RefreshPolicy{Enabled: false, IntervalSeconds: 900})
	now = now.Add(24 * time.Hour)
	_ = service.RefreshDue(ctx, now)
	if calls != 2 {
		t.Fatal("disable didn't take effect")
	}
	service.Close()
	if _, err = service.Update(ctx, first.ID); !errors.Is(err, context.Canceled) {
		t.Fatal("closed service starts update")
	}
	// Close cancels active manual network calls before the repository is closed.
	entered := make(chan struct{})
	done := make(chan error)
	blocking := subscriptions.NewService(store, func(ctx context.Context, _ string) (subscriptions.Snapshot, error) {
		close(entered)
		<-ctx.Done()
		return subscriptions.Snapshot{}, ctx.Err()
	})
	go func() { _, err := blocking.Update(ctx, first.ID); done <- err }()
	<-entered
	sources, _ := blocking.List(ctx)
	active := false
	for _, source := range sources {
		if source.ID == first.ID {
			active = source.Active
		}
	}
	if !active {
		t.Fatal("active source missing")
	}
	blocking.StartRefresh()
	blocking.StartRefresh()
	blocking.Close()
	if err = <-done; !errors.Is(err, context.Canceled) {
		t.Fatal("shutdown didn't cancel")
	}
	final, _ := store.GetSubscription(ctx, first.ID)
	if final.Source.Active || final.Source.Refresh.Failures != 0 {
		t.Fatal("shutdown persisted runtime active/error")
	}
}

func TestRefreshDueContinuesAfterOneSourceFails(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "partial-failure.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Unix(1800000000, 0)
	var calls int32
	service := subscriptions.NewService(store, func(_ context.Context, feed string) (subscriptions.Snapshot, error) {
		atomic.AddInt32(&calls, 1)
		if strings.Contains(feed, "/failed") {
			return subscriptions.Snapshot{}, discovery.ErrUnavailable
		}
		return rssSnapshot(feed), nil
	}, subscriptions.WithClock(func() time.Time { return now }))
	defer service.Close()
	ctx := context.Background()
	for _, address := range []string{"https://example.com/ok-a", "https://example.com/failed", "https://example.com/ok-b"} {
		source, created, createErr := service.Create(ctx, subscriptions.Input{Type: "rss", Query: address})
		if createErr != nil || !created {
			t.Fatal(source, created, createErr)
		}
		if _, createErr = service.ConfigureRefresh(ctx, source.ID, subscriptions.RefreshPolicy{Enabled: true, IntervalSeconds: 900}); createErr != nil {
			t.Fatal(createErr)
		}
	}
	err = service.RefreshDue(ctx, now.Add(900*time.Second))
	if !errors.Is(err, discovery.ErrUnavailable) || atomic.LoadInt32(&calls) != 3 {
		t.Fatalf("partial refresh = calls %d, err %v", calls, err)
	}
	sources, err := service.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range sources {
		if strings.Contains(source.URL, "/failed") {
			if source.LastUpdatedAt != 0 || source.Refresh.Failures == 0 {
				t.Fatalf("failed source was committed as successful: %+v", source)
			}
			continue
		}
		if source.LastUpdatedAt == 0 || source.Refresh.Failures != 0 {
			t.Fatalf("healthy source did not refresh: %+v", source)
		}
	}
}

func TestRSSRejectsUnconfirmed304AndInvalidChangedSnapshot(t *testing.T) {
	store, _ := sqlite.Open(filepath.Join(t.TempDir(), "invalid.db"))
	defer store.Close()
	result := subscriptions.ConditionalResult{NotModified: true}
	service := subscriptions.NewService(store, nil, subscriptions.WithConditionalFeeds(func(context.Context, string, subscriptions.Validators) (subscriptions.ConditionalResult, error) {
		return result, nil
	}))
	defer service.Close()
	ctx := context.Background()
	source, _, _ := service.Create(ctx, subscriptions.Input{Type: "rss", Query: "https://example.com/empty"})
	if _, err := service.Update(ctx, source.ID); !errors.Is(err, subscriptions.ErrSnapshot) {
		t.Fatal("unconfirmed304")
	}
	detail, _ := service.Get(ctx, source.ID)
	if detail.Source.LastUpdatedAt != 0 || detail.Source.Refresh.LastCheckedAt != 0 {
		t.Fatal("unconfirmed cache stored")
	}
	result = subscriptions.ConditionalResult{Snapshot: subscriptions.Snapshot{Items: []content.Item{}, Total: 0}}
	empty, err := service.Update(ctx, source.ID)
	if err != nil || empty.Source.LastUpdatedAt == 0 || empty.Items == nil {
		t.Fatal("confirmed empty feed not stored", err)
	}
	result.Snapshot = rssSnapshot("invalid")
	result.Snapshot.Items[0].Platform = "bilibili"
	if _, err = service.Update(ctx, source.ID); !errors.Is(err, subscriptions.ErrSnapshot) {
		t.Fatal("invalid feed accepted")
	}
	final, _ := service.Get(ctx, source.ID)
	if len(final.Items) != 0 || final.Source.LastUpdatedAt != empty.Source.LastUpdatedAt {
		t.Fatal("invalid snapshot replaced cache")
	}
}

func TestRSSIntervalChangesPreserveOutstandingFailureDelay(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "backoff.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	now := time.Unix(1800000000, 0)
	calls := 0
	var failure error = &feeds.FetchError{StatusCode: 429, RetryAfterSeconds: 7200}
	service := subscriptions.NewService(store, nil, subscriptions.WithClock(func() time.Time { return now }), subscriptions.WithConditionalFeeds(func(context.Context, string, subscriptions.Validators) (subscriptions.ConditionalResult, error) {
		calls++
		return subscriptions.ConditionalResult{Snapshot: rssSnapshot("recovered")}, failure
	}))
	defer service.Close()
	source, _, err := service.Create(ctx, subscriptions.Input{Type: "rss", Query: "https://example.com/backoff"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.ConfigureRefresh(ctx, source.ID, subscriptions.RefreshPolicy{Enabled: true, IntervalSeconds: 3600}); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Update(ctx, source.ID); err == nil {
		t.Fatal("failure hidden")
	}
	deadline := now.Unix() + 7200
	now = now.Add(time.Minute)
	if _, err = service.ConfigureRefresh(ctx, source.ID, subscriptions.RefreshPolicy{Enabled: false, IntervalSeconds: 3600}); err != nil {
		t.Fatal(err)
	}
	if err = service.RefreshDue(ctx, time.Unix(deadline+1, 0)); err != nil || calls != 1 {
		t.Fatal("disabled source retried", calls, err)
	}
	for _, interval := range []int64{900, 1800, 3600} {
		updated, err := service.ConfigureRefresh(ctx, source.ID, subscriptions.RefreshPolicy{Enabled: true, IntervalSeconds: interval})
		if err != nil || updated.Refresh.NextCheckAt != deadline || updated.Refresh.Failures != 1 {
			t.Fatal("interval shortened failure delay", updated, err)
		}
	}
	now = time.Unix(deadline-1, 0)
	if err = service.RefreshDue(ctx, now); err != nil || calls != 1 {
		t.Fatal("retried before upstream delay", calls, err)
	}
	failure = nil
	now = now.Add(time.Second)
	if err = service.RefreshDue(ctx, now); err != nil || calls != 2 {
		t.Fatal("did not recover at deadline", calls, err)
	}
	updated, err := service.ConfigureRefresh(ctx, source.ID, subscriptions.RefreshPolicy{Enabled: true, IntervalSeconds: 900})
	if err != nil || updated.Refresh.Failures != 0 || updated.Refresh.NextCheckAt != now.Unix()+900 {
		t.Fatal("successful recovery retained failure delay", updated, err)
	}
}
