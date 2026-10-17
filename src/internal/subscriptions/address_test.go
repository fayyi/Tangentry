package subscriptions_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"tangentry/internal/adapters/feeds"
	"tangentry/internal/adapters/sqlite"
	"tangentry/internal/notes"
	"tangentry/internal/readstate"
	"tangentry/internal/subscriptions"
	"testing"
	"time"
)

func TestRSSAddressEditsPreserveIdentityHistoryAndRefreshPolicyAcrossReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "address.db")
	store, err := sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { store.Close() }()
	now := time.Unix(1800000000, 0)
	calls := 0
	original := "https://rsshub.example/feed?key=original-fixture-key"
	current := original
	body := `<rss version="2.0"><channel><title>feed</title><link>https://example.com</link><description>fixture</description><item><guid>one</guid><title>article</title><link>https://example.com/article</link></item></channel></rss>`
	fetch := func(_ context.Context, source subscriptions.Source, validators subscriptions.Validators) (subscriptions.ConditionalResult, error) {
		calls++
		if source.URL != current || source.CreatorID != current || source.IdentityURL != original {
			t.Fatal("fetch or parser address changed incorrectly")
		}
		if calls > 1 && validators != (subscriptions.Validators{}) {
			t.Fatal("address edit retained old validators")
		}
		feed, err := feeds.Parse([]byte(body), source.IdentityURL)
		return subscriptions.ConditionalResult{
			Snapshot:   subscriptions.Snapshot{Items: feed.Items, Title: feed.SourceTitle, Total: feed.Total},
			Validators: subscriptions.Validators{ETag: `"one"`, ResourceURL: source.URL},
		}, err
	}
	makeService := func() *subscriptions.Service {
		return subscriptions.NewService(store, nil, subscriptions.WithRSSFeeds(fetch), subscriptions.WithClock(func() time.Time { return now }))
	}
	service := makeService()
	defer func() { service.Close() }()
	source, _, err := service.Create(ctx, subscriptions.Input{Type: "rss", Query: original, Name: "private feed"})
	if err != nil {
		t.Fatal(err)
	}
	first, err := service.Update(ctx, source.ID)
	if err != nil || len(first.Items) != 1 {
		t.Fatal("initial snapshot failed", err)
	}
	entry := first.Items[0]
	if _, err := readstate.New(store).Set(ctx, []string{entry.ID}, true); err != nil {
		t.Fatal(err)
	}
	note, err := notes.NewService(store).Create(ctx, notes.Input{ContentID: entry.ID, Body: "existing reading note", Tags: []string{"keep"}})
	if err != nil {
		t.Fatal(err)
	}
	groups := subscriptions.NewGroupService(store)
	group, _, err := groups.Create(ctx, "private sources")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := groups.Assign(ctx, source.ID, group.ID, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ConfigureRefresh(ctx, source.ID, subscriptions.RefreshPolicy{Enabled: true, IntervalSeconds: 900}); err != nil {
		t.Fatal(err)
	}
	record, _ := store.GetSubscriptionRefresh(ctx, source.ID)
	record.State.Failures, record.State.Message = 3, "old failure"
	if _, err := store.CommitSubscriptionRefresh(ctx, source.ID, nil, 0, record); err != nil {
		t.Fatal(err)
	}
	for _, replacement := range []string{"https://rsshub.example/feed?key=second-fixture-key", "https://replacement.example/rss?key=third-fixture-key"} {
		now = now.Add(time.Minute)
		saved, err := service.EditAddress(ctx, source.ID, subscriptions.AddressEdit{ExpectedURL: current, URL: replacement})
		if err != nil || saved.ID != source.ID || saved.CreatedAt != source.CreatedAt || saved.LastUpdatedAt != first.Source.LastUpdatedAt || saved.GroupID != group.ID || calls != 1 {
			t.Fatal("address edit changed history or made a network request", err)
		}
		current = replacement
		record, err = store.GetSubscriptionRefresh(ctx, source.ID)
		if err != nil || record.Validators != (subscriptions.Validators{}) || !record.State.Enabled || record.State.IntervalSeconds != 900 || record.State.NextCheckAt != now.Unix()+900 || record.State.ContentRevision != 1 || record.State.Failures != 0 || record.State.Message != "" || record.State.LastCheckedAt != 0 || record.State.LastAttemptAt != 0 {
			t.Fatal("address edit did not reset only address-dependent check state", err)
		}
		cached, _ := service.Get(ctx, source.ID)
		if !reflect.DeepEqual(cached.Items, first.Items) || cached.Source.IdentityURL != original {
			t.Fatal("address edit damaged snapshot or original identity")
		}
		payload, _ := json.Marshal(cached)
		if strings.Contains(string(payload), "original-fixture-key") || strings.Contains(string(payload), "IdentityURL") {
			t.Fatal("private original identity exposed through JSON")
		}
	}
	duplicate, created, err := service.Create(ctx, subscriptions.Input{Type: "rss", Query: current, Name: "must not replace"})
	if err != nil || created || duplicate.ID != source.ID || duplicate.Name != source.Name {
		t.Fatal("current edited address did not deduplicate")
	}
	before, _ := service.Get(ctx, source.ID)
	recordBefore, _ := store.GetSubscriptionRefresh(ctx, source.ID)
	unchanged, err := service.EditAddress(ctx, source.ID, subscriptions.AddressEdit{ExpectedURL: current, URL: current + "#ignored"})
	recordAfter, _ := store.GetSubscriptionRefresh(ctx, source.ID)
	if err != nil || !reflect.DeepEqual(unchanged, before.Source) || recordAfter != recordBefore {
		t.Fatal("normalized no-op changed source or refresh state", err)
	}
	if _, err := service.EditAddress(ctx, source.ID, subscriptions.AddressEdit{ExpectedURL: original, URL: "https://example.com/stale"}); !errors.Is(err, subscriptions.ErrAddressConflict) {
		t.Fatal("stale address edit accepted")
	}
	other, _, err := service.Create(ctx, subscriptions.Input{Type: "rss", Query: "https://example.com/other"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.EditAddress(ctx, source.ID, subscriptions.AddressEdit{ExpectedURL: current, URL: other.URL}); !errors.Is(err, subscriptions.ErrAddressExists) {
		t.Fatal("duplicate target address accepted")
	}
	after, _ := service.Get(ctx, source.ID)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("failed address edit changed stored data")
	}
	service.Close()
	store.Close()
	store, err = sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	service = makeService()
	second, err := service.Update(ctx, source.ID)
	if err != nil || !second.NotModified || second.Items[0].ID != entry.ID || second.Source.Refresh.ContentRevision != 1 {
		t.Fatal("reopened edited feed changed article identity", err)
	}
	states, err := readstate.New(store).Batch(ctx, []string{entry.ID})
	if err != nil || len(states) != 1 || !states[0].Read {
		t.Fatal("read state did not survive address edit")
	}
	savedNotes, err := notes.NewService(store).List(ctx, entry.ID)
	if err != nil || len(savedNotes) != 1 || savedNotes[0].ID != note.ID || !reflect.DeepEqual(savedNotes[0].Tags, note.Tags) {
		t.Fatal("notes did not survive address edit")
	}
	inbox, err := subscriptions.NewInboxService(store).List(ctx, subscriptions.InboxQuery{SourceID: source.ID})
	if err != nil || len(inbox.Entries) != 1 || inbox.Entries[0].Item.ID != entry.ID {
		t.Fatal("address edit duplicated subscription history", err)
	}
}

func TestRSSAddressEditRejectsInvalidInputAndAnInFlightRefresh(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "busy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	entered := make(chan struct{})
	service := subscriptions.NewService(store, nil, subscriptions.WithRSSFeeds(func(ctx context.Context, _ subscriptions.Source, _ subscriptions.Validators) (subscriptions.ConditionalResult, error) {
		close(entered)
		<-ctx.Done()
		return subscriptions.ConditionalResult{}, ctx.Err()
	}))
	defer service.Close()
	ctx := context.Background()
	source, _, err := service.Create(ctx, subscriptions.Input{Type: "rss", Query: "https://example.com/feed"})
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{"", "file:///private", "https://user@example.com/feed", "https://www.youtube.com/watch?v=example", "https://www.xiaoyuzhoufm.com/episode/example", "https://youtu.be/fixture", strings.Repeat("x", 2049)} {
		if _, err := service.EditAddress(ctx, source.ID, subscriptions.AddressEdit{ExpectedURL: source.URL, URL: raw}); !errors.Is(err, subscriptions.ErrInvalid) {
			t.Fatal("invalid address accepted")
		}
	}
	if _, err := service.EditAddress(ctx, "bilibili:creator:12345", subscriptions.AddressEdit{ExpectedURL: source.URL, URL: "https://example.com/new"}); !errors.Is(err, subscriptions.ErrInvalid) {
		t.Fatal("native source address became editable")
	}
	active, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { _, err := service.Update(active, source.ID); done <- err }()
	<-entered
	edit := subscriptions.AddressEdit{ExpectedURL: source.URL, URL: "https://example.com/new"}
	if _, err := service.EditAddress(ctx, source.ID, edit); !errors.Is(err, subscriptions.ErrBusy) {
		t.Fatal("address edit raced a fetch")
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal("refresh cancellation lost")
	}
	if _, err := service.EditAddress(ctx, source.ID, edit); err != nil {
		t.Fatal("canceled refresh retained its lock", err)
	}
	routed := "https://rsshub.example/bilibili.com/feed?route=youtube.com/watch"
	saved, err := service.EditAddress(ctx, source.ID, subscriptions.AddressEdit{ExpectedURL: edit.URL, URL: routed})
	if err != nil || saved.URL != routed {
		t.Fatal("RSSHub route was mistaken for a platform webpage", err)
	}
	if _, created, err := service.Create(ctx, subscriptions.Input{Type: "rss", Query: routed}); err != nil || created {
		t.Fatal("RSSHub route creation did not use the same validation")
	}
}
