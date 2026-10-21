package bilibili

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"tangentry/internal/content"
	"tangentry/internal/discovery"
)

type fallbackVideo struct{ calls int }

func (f *fallbackVideo) Video(ctx context.Context, id string) (content.Item, error) {
	f.calls++
	return content.Item{ID: "bilibili:" + id, Platform: "bilibili", PlatformID: id, Kind: "video", Title: "Extractor fixture", URL: "https://www.bilibili.com/video/" + id + "/"}, nil
}

func TestRestrictedSingleVideoUsesOptionalExtractorWithoutRepeatingBlockedAPI(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests++; w.WriteHeader(http.StatusPreconditionFailed) }))
	defer server.Close()
	fallback := &fallbackVideo{}
	client := NewClient(server.Client(), "", fallback)
	client.baseURL = server.URL
	for range 2 {
		item, err := client.Video(context.Background(), "BV17x411w7KC")
		if err != nil || item.Title != "Extractor fixture" {
			t.Fatal("fallback failed", err)
		}
	}
	if requests != 1 || fallback.calls != 2 {
		t.Fatalf("API requests %d, extractor calls %d", requests, fallback.calls)
	}
}

func TestWBISignFixedProtocolVector(t *testing.T) {
	key, err := deriveKey("7cd084941338484aae1ad9425b84077c4932caff0ff746eab6f01bf08b70ac45")
	if err != nil {
		t.Fatal(err)
	}
	params := url.Values{"foo": {"114"}, "bar": {"514"}, "baz": {"1919810"}}
	sign(params, key, 1702204169)
	// Freeze the documented key and timestamp; validate against a separately hashed canonical query.
	if params.Get("w_rid") != "6149fdadf571698ca7e6a567265cd0ee" {
		t.Fatalf("signature mismatch: %s", params.Get("w_rid"))
	}
}

func TestCreatorMappingAndAnonymousNav(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "test-cookie=not-a-real-account" {
			t.Error("configured cookie not forwarded to platform")
		}
		if r.Header.Get("Referer") != "https://space.bilibili.com/" {
			t.Error("missing referer")
		}
		switch r.URL.Path {
		case "/x/web-interface/nav":
			w.Write([]byte(`{"code":-101,"data":{"wbi_img":{"img_url":"https://i0.hdslb.com/bfs/wbi/7cd084941338484aae1ad9425b84077c.png","sub_url":"https://i0.hdslb.com/bfs/wbi/4932caff0ff746eab6f01bf08b70ac45.png"}}}`))
		case "/x/space/wbi/arc/search":
			if r.URL.Query().Get("w_rid") == "" || r.URL.Query().Get("mid") != "12345" || r.URL.Query().Get("pn") != "2" {
				t.Error("invalid signed request")
			}
			w.Write([]byte(`{"code":0,"data":{"list":{"vlist":[{"bvid":"BV17x411w7KC","title":"A &amp; B","author":"测试创作者","pic":"//i0.hdslb.com/test.jpg","description":"测试说明","length":"01:02:03","created":1702204169}]},"page":{"count":49}}}`))
		default:
			t.Fatalf("unexpected endpoint %s", r.URL.Path)
		}
	}))
	defer server.Close()
	client := NewClient(server.Client(), "test-cookie=not-a-real-account")
	client.baseURL = server.URL
	feed, err := client.CreatorVideos(context.Background(), "12345", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(feed.Items) != 1 || feed.Items[0].Title != "A & B" || feed.Items[0].DurationSeconds != 3723 || feed.Items[0].ThumbnailURL != "https://i0.hdslb.com/test.jpg" || feed.Items[0].ID != "bilibili:BV17x411w7KC" || !feed.HasMore {
		t.Fatalf("unexpected mapping: %+v", feed)
	}
}

func TestCreatorVideosFallsBackToPublicLegacyEndpointWithoutCookies(t *testing.T) {
	requests := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Path)
		if r.Header.Get("Cookie") != "" {
			t.Fatal("anonymous subscription request forwarded cookies")
		}
		switch r.URL.Path {
		case "/x/web-interface/nav":
			w.Write([]byte(`{"code":0,"data":{"wbi_img":{"img_url":"https://i0.hdslb.com/bfs/wbi/7cd084941338484aae1ad9425b84077c.png","sub_url":"https://i0.hdslb.com/bfs/wbi/4932caff0ff746eab6f01bf08b70ac45.png"}}}`))
		case "/x/space/wbi/arc/search":
			w.Write([]byte(`{"code":-400,"message":"bad request"}`))
		case "/x/space/arc/search":
			if r.URL.Query().Get("mid") != "12345" || r.URL.Query().Get("pn") != "1" {
				t.Errorf("unexpected legacy query: %s", r.URL.RawQuery)
			}
			w.Write([]byte(`{"code":0,"data":{"list":{"vlist":[{"bvid":"BV17x411w7KC","title":"公开投稿","author":"公开作者","mid":12345,"pic":"//i0.hdslb.com/test.jpg","length":"01:02","created":1702204169}]},"page":{"count":1,"pn":1,"ps":30}}}`))
		default:
			t.Fatalf("unexpected endpoint %s", r.URL.Path)
		}
	}))
	defer server.Close()
	client := NewClient(server.Client(), "")
	client.baseURL = server.URL
	feed, err := client.CreatorVideos(context.Background(), "12345", 1)
	if err != nil || len(feed.Items) != 1 || feed.Items[0].Title != "公开投稿" || feed.Items[0].ThumbnailURL != "https://i0.hdslb.com/test.jpg" || feed.Query != "12345" {
		t.Fatalf("legacy anonymous feed not mapped: %+v err=%v", feed, err)
	}
	if strings.Join(requests, ",") != "/x/web-interface/nav,/x/space/wbi/arc/search,/x/space/arc/search" {
		t.Fatalf("unexpected fallback sequence: %#v", requests)
	}
}

func TestCreatorVideosDoesNotFallbackAfterPlatformRestriction(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path == "/x/web-interface/nav" {
			w.Write([]byte(`{"code":-101,"data":{"wbi_img":{"img_url":"https://i0.hdslb.com/bfs/wbi/7cd084941338484aae1ad9425b84077c.png","sub_url":"https://i0.hdslb.com/bfs/wbi/4932caff0ff746eab6f01bf08b70ac45.png"}}}`))
			return
		}
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()
	client := NewClient(server.Client(), "")
	client.baseURL = server.URL
	_, err := client.CreatorVideos(context.Background(), "12345", 1)
	var platform *discovery.PlatformError
	if !errors.As(err, &platform) || !platform.Restricted || requests != 2 {
		t.Fatalf("restricted request was retried or misclassified: err=%v requests=%d", err, requests)
	}
}

func TestCreatorVideosRiskResponseEntersCooldownWithoutLegacyRetry(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/x/space/wbi/arc/search" {
			t.Fatalf("risk response triggered fallback endpoint: %s", r.URL.Path)
		}
		w.Write([]byte(`{"code":0,"data":{"list":{"vlist":[]},"page":{"count":0},"is_risk":true}}`))
	}))
	defer server.Close()
	client := NewClient(server.Client(), "")
	client.baseURL, client.key, client.keyExpires = server.URL, "test-key", time.Now().Add(time.Hour)
	for range 2 {
		_, err := client.CreatorVideos(context.Background(), "12345", 1)
		var platform *discovery.PlatformError
		if !errors.As(err, &platform) || !platform.Restricted {
			t.Fatalf("risk response was not classified as a restriction: %v", err)
		}
	}
	if requests != 1 {
		t.Fatalf("risk response bypassed cooldown: requests=%d", requests)
	}
}

func TestRestrictedResponseIsAnErrorAndCoolsDown(t *testing.T) {
	for _, code := range []string{"http412", "-352", "-799"} {
		t.Run(code, func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if code == "http412" {
					w.WriteHeader(412)
					return
				}
				w.Write([]byte(`{"code":` + code + `,"message":"sensitive upstream message"}`))
			}))
			defer server.Close()
			client := NewClient(server.Client(), "")
			client.baseURL = server.URL
			for range 2 {
				_, err := client.Video(context.Background(), "BV17x411w7KC")
				var platform *discovery.PlatformError
				if !errors.As(err, &platform) || !platform.Restricted || strings.Contains(err.Error(), "sensitive") {
					t.Fatalf("unexpected error %v", err)
				}
			}
			if requests != 1 {
				t.Fatalf("cooldown made %d requests", requests)
			}
		})
	}
}

func TestVideoMappingAndDeletedContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("bvid") == "BVdeleted000" {
			w.Write([]byte(`{"code":-404}`))
			return
		}
		w.Write([]byte(`{"code":0,"data":{"bvid":"BV17x411w7KC","title":"测试视频","pic":"http://i0.hdslb.com/test.jpg","desc":"测试简介","duration":100,"pubdate":1702204169,"owner":{"name":"测试作者","mid":12345}}}`))
	}))
	defer server.Close()
	client := NewClient(server.Client(), "")
	client.baseURL = server.URL
	item, err := client.Video(context.Background(), "BV17x411w7KC")
	if err != nil || item.AuthorID != "12345" || item.DurationSeconds != 100 {
		t.Fatalf("bad item: %+v %v", item, err)
	}
	if _, err := client.Video(context.Background(), "BVdeleted000"); err == nil {
		t.Fatal("deleted content returned success")
	}
}

func TestMalformedCreatorResponseIsNotAnEmptySuccess(t *testing.T) {
	for _, data := range []string{"null", "{}", `{"list":null,"page":{"count":0}}`, `{"list":{},"page":{"count":0}}`, `{"list":{"vlist":[]},"page":{"count":0},"is_risk":true}`, `{"list":{"vlist":[]},"page":{"count":0},"gaia_res_type":1}`} {
		t.Run(data, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"code":0,"data":` + data + `}`)) }))
			defer server.Close()
			client := NewClient(server.Client(), "")
			client.baseURL, client.key, client.keyExpires = server.URL, "test-key", time.Now().Add(time.Hour)
			if _, err := client.CreatorVideos(context.Background(), "12345", 1); err == nil {
				t.Fatal("malformed response reported a successful empty feed")
			}
		})
	}
}
