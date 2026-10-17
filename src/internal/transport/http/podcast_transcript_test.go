package httptransport

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"tangentry/internal/adapters/sqlite"
	"tangentry/internal/content"
	"tangentry/internal/subscriptions"
	"tangentry/internal/subtitles"
)

func TestPodcastTranscriptResolvesRSSWithoutChangingIdentity(t *testing.T) {
	for _, location := range []string{"inbox", "library"} {
		t.Run(location, func(t *testing.T) {
			store, err := sqlite.Open(filepath.Join(t.TempDir(), "podcast.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			ctx := context.Background()
			episode := "686000000000000000000001"
			item := content.Item{ID: "rss:" + strings.Repeat("a", 64), Platform: "rss", PlatformID: strings.Repeat("a", 64), Kind: "audio", Title: "节目", URL: "https://www.xiaoyuzhoufm.com/episode/" + episode, MediaURL: "https://example.com/audio.mp3"}
			library := content.NewService(store)
			if location == "library" {
				if _, err := library.Save(ctx, item); err != nil {
					t.Fatal(err)
				}
			} else {
				source, _, err := subscriptions.NewService(store, nil).Create(ctx, subscriptions.Input{Type: "rss", Query: "https://example.com/feed"})
				if err != nil {
					t.Fatal(err)
				}
				if _, err = store.UpdateSubscription(ctx, source.ID, subscriptions.Snapshot{Items: []content.Item{item}, Total: 1}, 10); err != nil {
					t.Fatal(err)
				}
			}
			calls := 0
			h := New(library, nil, nil, Options{SavedSubtitles: subtitles.NewSavedService(store), SubscriptionInbox: subscriptions.NewInboxService(store), PodcastEpisodeID: func(_ context.Context, id string) string {
				if id == item.ID {
					return episode
				}
				return ""
			}, PodcastCaptions: func(_ context.Context, id string) (subtitles.Document, error) {
				calls++
				if id != episode {
					t.Fatalf("wrong episode: %s", id)
				}
				return subtitles.Document{VideoID: "xiaoyuzhou:" + id, Part: 1, Status: "available", Tracks: []subtitles.Track{{Language: "zh-Hans", Label: "文稿", Cues: []subtitles.Cue{{Start: 0, End: 1, Text: "正文"}}}}}, nil
			}})
			w := storageRequest(h, "GET", "/api/subtitles/podcast?contentId="+item.ID, "", "")
			var doc subtitles.Document
			if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &doc) != nil || doc.VideoID != item.ID || doc.Status != "available" || calls != 1 {
				t.Fatalf("unexpected response %d %s", w.Code, w.Body)
			}
			// A new handler must read the persisted document, not call the platform.
			reopened := New(library, nil, nil, Options{SavedSubtitles: subtitles.NewSavedService(store), PodcastCaptions: func(context.Context, string) (subtitles.Document, error) {
				t.Fatal("cached transcript requested platform")
				return subtitles.Document{}, nil
			}})
			w = storageRequest(reopened, "GET", "/api/subtitles/podcast?contentId="+item.ID, "", "")
			if w.Code != 200 || !strings.Contains(w.Body.String(), "正文") {
				t.Fatal("persisted transcript unavailable", w.Code, w.Body)
			}
			w = storageRequest(h, "GET", "/api/subtitles/podcast?contentId=rss:"+strings.Repeat("b", 64), "", "")
			if w.Code != 200 || calls != 1 {
				t.Fatal("unknown RSS invoked platform", w.Code)
			}
			w = storageRequest(h, "PUT", "/api/saved-subtitles/"+item.ID+"/zh", `{"revision":0,"track":{"language":"zh","label":"本机字幕","automatic":false,"cues":[{"start":0,"end":1,"text":"已保存"}]}}`, "application/json")
			if w.Code != 200 {
				t.Fatal("save transcript", w.Code, w.Body)
			}
			w = storageRequest(h, "GET", "/api/subtitles/podcast?contentId="+item.ID, "", "")
			if w.Code != 200 || calls != 1 || !strings.Contains(w.Body.String(), "已保存") {
				t.Fatal("saved transcript must take precedence", w.Code, w.Body)
			}
		})
	}
}

func TestPodcastTranscriptConcurrentEditWins(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "podcast.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	saved := subtitles.NewSavedService(store)
	id := "xiaoyuzhou:686000000000000000000001"
	track := subtitles.Track{Language: "zh-Hans", Label: "编辑文稿", Cues: []subtitles.Cue{{Start: 0, End: 1, Text: "用户编辑"}}}
	h := New(nil, nil, nil, Options{SavedSubtitles: saved, PodcastCaptions: func(ctx context.Context, _ string) (subtitles.Document, error) {
		if _, err := saved.Save(ctx, id, track, 0); err != nil {
			t.Fatal(err)
		}
		platform := track
		platform.Cues = []subtitles.Cue{{Start: 0, End: 1, Text: "平台文稿"}}
		return subtitles.Document{Status: "available", Tracks: []subtitles.Track{platform}}, nil
	}})
	w := storageRequest(h, "GET", "/api/subtitles/podcast?contentId="+id, "", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "用户编辑") || strings.Contains(w.Body.String(), "平台文稿") {
		t.Fatal("concurrent edit overwritten", w.Code, w.Body)
	}
}

func TestPublicRSSTranscriptPersistsAndPreservesUserEdits(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "public-podcast.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	library := content.NewService(store)
	saved := subtitles.NewSavedService(store)
	item := content.Item{ID: "rss:" + strings.Repeat("c", 64), Platform: "rss", PlatformID: strings.Repeat("c", 64), Kind: "audio", Title: "Public podcast", URL: "https://example.com/episode", Transcripts: []content.Transcript{{URL: "https://example.com/captions.vtt", Type: "text/vtt", Language: "en"}}}
	if _, err := library.Save(context.Background(), item); err != nil {
		t.Fatal(err)
	}
	calls := 0
	h := New(library, nil, nil, Options{SavedSubtitles: saved, PublicPodcastCaptions: func(ctx context.Context, resolved content.Item) (subtitles.Document, error) {
		calls++
		if resolved.ID != item.ID || len(resolved.Transcripts) != 1 {
			t.Fatal("metadata lost", resolved)
		}
		track := subtitles.Track{Language: "en", Label: "Edited", Cues: []subtitles.Cue{{Start: .125, End: 1.25, Text: "User edit"}}}
		if _, err := saved.Save(ctx, item.ID, track, 0); err != nil {
			t.Fatal(err)
		}
		track.Cues[0].Text = "Publisher caption"
		return subtitles.Document{Status: "available", Tracks: []subtitles.Track{track}}, nil
	}})
	for i := 0; i < 2; i++ {
		w := storageRequest(h, "GET", "/api/subtitles/podcast?contentId="+item.ID, "", "")
		if w.Code != 200 || !strings.Contains(w.Body.String(), "User edit") || strings.Contains(w.Body.String(), "Publisher caption") || calls != 1 {
			t.Fatal(w.Code, w.Body, calls)
		}
	}
}

func TestPublicRSSTranscriptFailureCanRetryWithoutSavingEmptyResult(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "retry-podcast.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	library := content.NewService(store)
	saved := subtitles.NewSavedService(store)
	item := content.Item{ID: "rss:" + strings.Repeat("d", 64), Platform: "rss", PlatformID: strings.Repeat("d", 64), Kind: "audio", Title: "Public podcast", URL: "https://example.com/episode", Transcripts: []content.Transcript{{URL: "https://example.com/captions.vtt", Type: "text/vtt"}}}
	if _, err := library.Save(context.Background(), item); err != nil {
		t.Fatal(err)
	}
	calls := 0
	h := New(library, nil, nil, Options{SavedSubtitles: saved, PublicPodcastCaptions: func(context.Context, content.Item) (subtitles.Document, error) {
		calls++
		if calls == 1 {
			return subtitles.Document{}, subtitles.ErrPublicUnavailable
		}
		return subtitles.Document{Status: "available", Tracks: []subtitles.Track{{Language: "und", Label: "Public", Cues: []subtitles.Cue{{Start: 0, End: 1, Text: "Recovered"}}}}}, nil
	}})
	path := "/api/subtitles/podcast?contentId=" + item.ID
	w := storageRequest(h, "GET", path, "", "")
	if w.Code != 502 || !strings.Contains(w.Body.String(), "public_subtitles_unavailable") || strings.Contains(w.Body.String(), "登录") {
		t.Fatal("misleading public subtitle failure", w.Code, w.Body)
	}
	tracks, err := saved.List(context.Background(), item.ID)
	if err != nil || len(tracks) != 0 {
		t.Fatal("failed fetch persisted captions", tracks, err)
	}
	for i := 0; i < 2; i++ {
		w = storageRequest(h, "GET", path, "", "")
		if w.Code != 200 || !strings.Contains(w.Body.String(), "Recovered") || calls != 2 {
			t.Fatal("retry or cache failed", w.Code, w.Body, calls)
		}
	}
}
