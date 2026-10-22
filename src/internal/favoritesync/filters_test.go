package favoritesync

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"tangentry/internal/content"
)

func TestDownloadFilters(t *testing.T) {
	item := content.Item{Title: "AI 教程：Go Web 实战", Author: "技术频道"}
	for _, tc := range []struct {
		name string
		p    Policy
		want bool
	}{
		{"legacy empty rules", Policy{}, true},
		{"case insensitive alternatives", Policy{TitleInclude: "Rust, ai 教程"}, true},
		{"Chinese commas and lines", Policy{TitleInclude: "访谈，\n Go Web \n"}, true},
		{"exclusion wins", Policy{TitleInclude: "教程", TitleExclude: "实战"}, false},
		{"author and title together", Policy{TitleInclude: "教程", AuthorInclude: "频道"}, true},
		{"author mismatch", Policy{TitleInclude: "教程", AuthorInclude: "其他作者"}, false},
		{"title mismatch", Policy{TitleInclude: "Rust", AuthorInclude: "技术"}, false},
		{"whitespace fields", Policy{TitleInclude: " , ， \n", TitleExclude: " , "}, true},
		{"literal not regexp", Policy{TitleInclude: ".*"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.p.matchesDownload(item); got != tc.want {
				t.Fatalf("matched=%v want=%v", got, tc.want)
			}
		})
	}
	if (Policy{AuthorInclude: "技术"}).matchesDownload(content.Item{Title: item.Title}) {
		t.Fatal("missing author matched an author rule")
	}
}

func TestFilterValidationDoesNotChangeSavedPolicy(t *testing.T) {
	s, _, _, _ := newFixture(t)
	ctx := context.Background()
	before, err := s.Create(ctx, Input{Query: "12345"})
	if err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{strings.Repeat("中", 301), strings.Repeat("a,", 21), "a\x00b", string([]byte{0xff})} {
		p := DefaultPolicy()
		p.TitleInclude = invalid
		if _, err := s.Configure(ctx, "12345", p); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid filter accepted: %v", err)
		}
		after, _ := s.Get(ctx, "12345")
		if !reflect.DeepEqual(after.Policy, before.Policy) {
			t.Fatal("failed validation replaced saved policy")
		}
	}
}

func TestFilteredSyncRetainsEveryItemAndOnlyDownloadsMatches(t *testing.T) {
	s, _, remote, d := newFixture(t)
	items := remote.pages[0].Items
	items[0].Title, items[0].Author = "Go 教程", "技术频道"
	items[1].Title, items[1].Author = "Go 教程 预告", "技术频道"
	items[2].Title, items[2].Author = "访谈", "技术频道"
	p := DefaultPolicy()
	p.AutoDownload = true
	p.TitleInclude, p.TitleExclude, p.AuthorInclude = "教程", "预告", "技术"
	if _, err := s.Create(context.Background(), Input{Query: "12345", Policy: p}); err != nil {
		t.Fatal(err)
	}
	source := syncOnce(t, s, "12345")
	if len(source.Items) != 6 || d.started != 1 || len(source.Pending) != 0 ||
		d.jobs[0].URL != "https://www.bilibili.com/video/"+items[0].PlatformID+"/" ||
		!strings.Contains(source.Message, "5 个视频未匹配自动下载规则") {
		t.Fatalf("filter changed snapshot or admitted wrong videos: %+v jobs=%+v", source, d.jobs)
	}
	// Relaxing rules does not backfill items discovered under the previous policy.
	p.TitleInclude, p.TitleExclude, p.AuthorInclude = "", "", ""
	if _, err := s.Configure(context.Background(), "12345", p); err != nil {
		t.Fatal(err)
	}
	syncOnce(t, s, "12345")
	if d.started != 1 {
		t.Fatal("rule relaxation unexpectedly downloaded existing items")
	}
}

func TestNewRulesPrunePendingAndPreventFailedDownloadRetry(t *testing.T) {
	s, _, _, d := newFixture(t)
	ctx := context.Background()
	p := DefaultPolicy()
	p.AutoDownload, p.MaxPerRun = true, 1
	if _, err := s.Create(ctx, Input{Query: "12345", Policy: p}); err != nil {
		t.Fatal(err)
	}
	before := syncOnce(t, s, "12345")
	if len(before.Pending) != 5 || d.started != 1 {
		t.Fatal("fixture did not create pending downloads", before)
	}
	d.mu.Lock()
	d.jobs[0].State = "failed"
	d.mu.Unlock()
	p.TitleExclude = "测试"
	updated, err := s.Configure(ctx, "12345", p)
	if err != nil || len(updated.Pending) != 0 || len(updated.Items) != 6 || len(updated.Transfers) != 1 {
		t.Fatal("rule update lost metadata or retained excluded queue", updated, err)
	}
	after := syncOnce(t, s, "12345")
	if d.started != 1 || len(after.Pending) != 0 || len(after.Items) != 6 {
		t.Fatal("excluded failed transfer was retried", after)
	}
}
