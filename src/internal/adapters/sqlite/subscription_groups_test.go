package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"tangentry/internal/content"
	"tangentry/internal/subscriptions"
	"testing"
)

func TestGroupsOrganizeAllSourcesWithoutChangingContentOrPolicies(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "groups.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	service := subscriptions.NewService(s, nil)
	defer service.Close()
	groups := subscriptions.NewGroupService(s)
	group, created, err := groups.Create(ctx, "  科技 Tech  ")
	if err != nil || !created || group.Name != "科技 Tech" || group.Revision != 1 {
		t.Fatal(group, err)
	}
	duplicate, created, err := groups.Create(ctx, "科技 TECH")
	if err != nil || created || duplicate != group {
		t.Fatal("duplicate changed group", duplicate, err)
	}
	other, _, err := groups.Create(ctx, "播客")
	if err != nil {
		t.Fatal(err)
	}
	inputs := []subscriptions.Input{{Query: "12345"}, {Type: "rss", Query: "https://example.com/podcast.xml"}, {Type: "youtube", Query: "UCaaaaaaaaaaaaaaaaaaaaaa"}}
	for _, input := range inputs {
		source, _, err := service.Create(ctx, input)
		if err != nil {
			t.Fatal(err)
		}
		item := content.Item{ID: "bilibili:BV17x411w7KC", Platform: "bilibili", PlatformID: "BV17x411w7KC", Kind: "video", URL: "https://www.bilibili.com/video/BV17x411w7KC/", Title: "fixture"}
		if _, err = s.UpdateSubscription(ctx, source.ID, subscriptions.Snapshot{Items: []content.Item{item}, Total: 1}, 20); err != nil {
			t.Fatal(err)
		}
		record := subscriptions.DefaultRefresh()
		record.State.Enabled = input.Type != ""
		record.State.NextCheckAt = 99999
		record.State.ContentRevision = 3
		record.Validators.ETag = `"fixture"`
		if _, err = s.CommitSubscriptionRefresh(ctx, source.ID, nil, 0, record); err != nil {
			t.Fatal(err)
		}
		before, _ := service.Get(ctx, source.ID)
		membership, err := groups.Assign(ctx, source.ID, group.ID, "")
		if err != nil || membership.GroupID != group.ID {
			t.Fatal(membership, err)
		}
		after, err := service.Get(ctx, source.ID)
		before.Source.GroupID = group.ID
		if err != nil || !reflect.DeepEqual(before, after) {
			t.Fatal("assignment changed snapshot or policy", before, after, err)
		}
		if _, err = groups.Assign(ctx, source.ID, other.ID, ""); !errors.Is(err, subscriptions.ErrGroupConflict) {
			t.Fatal("stale move overwrote assignment", err)
		}
		renamed, err := service.Rename(ctx, source.ID, subscriptions.Edit{Name: "renamed"})
		if err != nil || renamed.GroupID != group.ID || renamed.Refresh != before.Source.Refresh {
			t.Fatal("source rename lost membership/policy", renamed, err)
		}
		refresh, err := s.CommitSubscriptionRefresh(ctx, source.ID, nil, 0, record)
		if err != nil || refresh.Source.GroupID != group.ID {
			t.Fatal("refresh lost membership", err)
		}
		if _, err = groups.Assign(ctx, source.ID, group.ID, group.ID); err != nil {
			t.Fatal("same assignment should be idempotent", err)
		}
	}
	renamed, err := groups.Rename(ctx, group.ID, subscriptions.GroupEdit{Name: "科技与学习", Revision: group.Revision})
	if err != nil || renamed.Revision != 2 || renamed.CreatedAt != group.CreatedAt {
		t.Fatal(renamed, err)
	}
	if _, err = groups.Rename(ctx, group.ID, subscriptions.GroupEdit{Name: "stale", Revision: 1}); !errors.Is(err, subscriptions.ErrGroupConflict) {
		t.Fatal("stale rename", err)
	}
	if _, err = groups.Rename(ctx, group.ID, subscriptions.GroupEdit{Name: other.Name, Revision: 2}); !errors.Is(err, subscriptions.ErrGroupName) {
		t.Fatal("duplicate rename merged groups", err)
	}
	if err = groups.Delete(ctx, group.ID, 1); !errors.Is(err, subscriptions.ErrGroupConflict) {
		t.Fatal("stale dissolution", err)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	groups = subscriptions.NewGroupService(s)
	list, err := groups.List(ctx)
	if err != nil || len(list) != 2 || list[0] != renamed {
		t.Fatal("groups not persisted", list, err)
	}
	before, err := s.ListSubscriptions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = groups.Delete(ctx, group.ID, renamed.Revision); err != nil {
		t.Fatal(err)
	}
	after, err := s.ListSubscriptions(ctx)
	for i := range before {
		before[i].GroupID = ""
	}
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("dissolution changed sources", err)
	}
	if err = s.DeleteSubscription(ctx, after[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err = groups.Assign(ctx, after[0].ID, other.ID, ""); !errors.Is(err, subscriptions.ErrNotFound) {
		t.Fatal("deleted source recreated", err)
	}
}

func TestGroupsValidationConcurrencyAndAtomicRollback(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "atomic.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	g := subscriptions.NewGroupService(s)
	for _, name := range []string{"", " ", "line\nbreak", "tab\t", "null\x00", string([]byte{0xff}), strings.Repeat("中", 61)} {
		if _, _, err = g.Create(ctx, name); !errors.Is(err, subscriptions.ErrGroupInvalid) {
			t.Fatalf("accepted %q: %v", name, err)
		}
	}
	var wg sync.WaitGroup
	results := make(chan subscriptions.Group, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			group, _, err := g.Create(ctx, "Concurrent")
			if err != nil {
				t.Error(err)
			}
			results <- group
		}()
	}
	wg.Wait()
	close(results)
	var id string
	for group := range results {
		if id == "" {
			id = group.ID
		}
		if group.ID != id {
			t.Fatal("concurrent duplicate", group)
		}
	}
	service := subscriptions.NewService(s, nil)
	defer service.Close()
	source, _, err := service.Create(ctx, subscriptions.Input{Query: "12345"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = g.Assign(ctx, source.ID, id, ""); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec(`CREATE TRIGGER reject_group_delete BEFORE DELETE ON subscription_groups BEGIN SELECT RAISE(ABORT,'fixture failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err = g.Delete(ctx, id, 1); err == nil {
		t.Fatal("fixture failure missing")
	}
	current, err := s.GetSubscription(ctx, source.ID)
	if err != nil || current.Source.GroupID != id {
		t.Fatal("partial dissolve", current, err)
	}
	if _, err = s.db.Exec(`CREATE TRIGGER reject_member_delete BEFORE DELETE ON subscription_group_members BEGIN SELECT RAISE(ABORT,'fixture failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err = service.Delete(ctx, source.ID); err == nil {
		t.Fatal("failed source deletion not reported")
	}
	if _, err = s.GetSubscription(ctx, source.ID); err != nil {
		t.Fatal("partial source deletion", err)
	}
	if _, err = g.Assign(ctx, source.ID, "", id); err == nil {
		t.Fatal("failed ungrouping not reported")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, _, err = g.Create(canceled, "canceled"); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled creation", err)
	}
	if _, err = g.Assign(ctx, source.ID, "", id); err == nil {
		t.Fatal("rollback failed")
	}
	for _, input := range [][3]string{{"invalid", id, ""}, {source.ID, "invalid", id}, {source.ID, id, "invalid"}} {
		if _, err = g.Assign(ctx, input[0], input[1], input[2]); !errors.Is(err, subscriptions.ErrGroupInvalid) {
			t.Fatal("invalid membership accepted", err)
		}
	}
	if _, err = s.db.Exec(`DROP TRIGGER reject_member_delete`); err != nil {
		t.Fatal(err)
	}
	if err = service.Delete(ctx, source.ID); err != nil {
		t.Fatal(err)
	}
	var members int
	if err = s.db.QueryRow(`SELECT COUNT(*) FROM subscription_group_members`).Scan(&members); err != nil || members != 0 {
		t.Fatal("orphan membership", members, err)
	}
	if list, err := g.List(ctx); err != nil || len(list) != 1 {
		t.Fatal("source deletion removed group", list, err)
	}
}

func TestGroupMigrationPreservesVersion12RowsAndAddsNoMemberships(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`CREATE TABLE schema_version(version INTEGER PRIMARY KEY)`, `INSERT INTO schema_version VALUES(12)`,
		`CREATE TABLE subscriptions(id TEXT PRIMARY KEY,creator_id TEXT NOT NULL UNIQUE,name TEXT NOT NULL,created_at INTEGER NOT NULL,updated_at INTEGER NOT NULL,snapshot TEXT NOT NULL)`,
		`INSERT INTO subscriptions VALUES('bilibili:creator:12345','12345','我的来源',12,20,'')`,
		`INSERT INTO subscriptions VALUES('rss:feed:` + strings.Repeat("a", 64) + `','https://example.com/feed','RSS 来源',12,20,'')`,
		`CREATE TABLE subscription_refresh(id TEXT PRIMARY KEY,payload TEXT NOT NULL)`,
		`INSERT INTO subscription_refresh VALUES('bilibili:creator:12345','{"state":{"enabled":true,"intervalSeconds":3600}}')`,
	} {
		if _, err = db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	const query = `SELECT json_array(s.id,s.creator_id,s.name,s.created_at,s.updated_at,s.snapshot,r.payload) FROM subscriptions s JOIN subscription_refresh r ON r.id=s.id WHERE s.id='bilibili:creator:12345'`
	var before string
	if err = db.QueryRow(query).Scan(&before); err != nil {
		t.Fatal(err)
	}
	db.Close()
	for range 2 {
		s, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		var after string
		if err = s.db.QueryRow(query).Scan(&after); err != nil || before != after {
			t.Fatal("migration changed rows", after, err)
		}
		var rssPayload string
		if err = s.db.QueryRow(`SELECT payload FROM subscription_refresh WHERE id=?`, "rss:feed:"+strings.Repeat("a", 64)).Scan(&rssPayload); err != nil || !strings.Contains(rssPayload, "7200") || !strings.Contains(rssPayload, "nextCheckAt") {
			t.Fatal("migration did not add default polling policy", rssPayload, err)
		}
		var count int
		if err = s.db.QueryRow(`SELECT (SELECT COUNT(*) FROM subscription_groups)+(SELECT COUNT(*) FROM subscription_group_members)`).Scan(&count); err != nil || count != 0 {
			t.Fatal("migration invented organization", err)
		}
		if err = s.db.QueryRow(`SELECT MAX(version) FROM schema_version`).Scan(&count); err != nil || count != latestVersion(t) {
			t.Fatal("schema", err)
		}
		s.Close()
	}
}
