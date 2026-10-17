package bilibili

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const browseFixture = `{"bvid":"BV17x411w7KC","title":"A &amp; B","pic":"//i0.hdslb.com/cover.jpg","desc":"节目说明","duration":120,"owner":{"mid":42,"name":"作者","face":"//i0.hdslb.com/avatar.jpg"}}`

func TestBrowseListsHaveDistinctPaginationAndRetainVideoMetadata(t *testing.T) {
	for _, kind := range []string{"popular", "ranking", "watchlater"} {
		t.Run(kind, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				expected := map[string]string{"popular": "/x/web-interface/popular", "ranking": "/x/web-interface/ranking/v2", "watchlater": "/x/v2/history/toview"}[kind]
				if r.URL.Path != expected || r.Header.Get("Cookie") != "synthetic=fixture" {
					t.Fatal("wrong platform request")
				}
				if kind == "popular" && (r.URL.Query().Get("pn") != "2" || r.URL.Query().Get("ps") != "24") {
					t.Fatal("wrong page")
				}
				w.Write([]byte(`{"code":0,"data":{"list":[` + browseFixture + `,` + browseFixture + `,{"bvid":"invalid","title":"omit"}],"no_more":false}}`))
			}))
			defer server.Close()
			client := NewClient(server.Client(), "synthetic=fixture")
			client.baseURL = server.URL
			page := 1
			if kind == "popular" {
				page = 2
			}
			result, err := client.Browse(context.Background(), kind, page, "", 1)
			if err != nil || len(result.Items) != 1 || result.HasMore != (kind == "popular") {
				t.Fatal(result, err)
			}
			item := result.Items[0]
			if item.Title != "A & B" || item.AuthorID != "42" || item.DurationSeconds != 120 || item.AuthorAvatarURL != "https://i0.hdslb.com/avatar.jpg" {
				t.Fatal(item)
			}
			if kind != "popular" {
				if _, err := client.Browse(context.Background(), kind, 2, "", 1); err == nil || calls != 1 {
					t.Fatal("fixed list fetched a fake second page")
				}
			}
		})
	}
}

func TestDynamicUsesSignedCursorAndMapsForwardedAndNestedVideos(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/x/polymer/web-dynamic/v1/feed/all" || r.URL.Query().Get("type") != "all" || r.URL.Query().Get("w_rid") == "" || r.URL.Query().Get("features") == "" || r.Header.Get("Referer") != "https://t.bilibili.com/" {
			t.Fatal("invalid dynamic cursor/signature")
		}
		if r.URL.Query().Get("offset") != "previous" {
			t.Fatal("unexpected offset", r.URL.Query().Get("offset"))
		}
		w.Write([]byte(`{"code":0,"data":{"items":[{"type":"1","orig":{"type":8,"bvid":"BV17x411w7KC","title":"转发的视频","duration_text":"01:02","view_count":1234,"author":{"mid":42,"name":"原作者"}}},{"type":"DYNAMIC_TYPE_AV","modules":{"module_author":{"mid":43,"name":"关注的作者","pub_ts":1},"module_dynamic":{"major":{"archive":{"bvid":"BV1Pyta66EDh","title":"关注投稿","duration_text":"01:20","stat":{"danmaku":"12","play":"3.4万"}}}}}},{"type":"8","bvid":"BV1Q541167Qg","title":"播放数回退","duration_text":"00:10","stat":{"play":9012},"author":{"mid":44,"name":"播放作者"}},{"type":"DYNAMIC_TYPE_WORD","modules":{}}],"offset":"next-page","has_more":"1"}}`))
	}))
	defer server.Close()
	client := NewClient(server.Client(), "")
	client.baseURL, client.key, client.keyExpires = server.URL, "fixture-key", time.Now().Add(time.Hour)
	result, err := client.Browse(context.Background(), "dynamic", 2, "previous", 1)
	if err != nil || len(result.Items) != 3 || result.Next != "next-page" || !result.HasMore || result.Notice != "" || result.Items[0].DurationSeconds != 62 || result.Items[0].ViewCount != 1234 || result.Items[1].Author != "关注的作者" || result.Items[1].ViewCount != 34000 || result.Items[2].ViewCount != 9012 {
		t.Fatal(result, err)
	}
	if _, err := client.Browse(context.Background(), "dynamic", 2, "", 1); err == nil {
		t.Fatal("missing cursor accepted")
	}
}

func TestDynamicKeepsReadingPastImageOnlyPagesAndExplainsEmptyResults(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Write([]byte(`{"code":0,"data":{"items":[{"type":"DYNAMIC_TYPE_DRAW","id_str":"draw-1","modules":{"module_author":{"name":"作者"},"module_dynamic":{"desc":{"text":"图文"},"major":{"draw":{"items":[]}}}}},{"type":"DYNAMIC_TYPE_ARTICLE","id_str":"article-1","modules":{"module_author":{"name":"作者"},"module_dynamic":{"major":{"article":{"title":"专栏"}}}}}],"offset":"p2","has_more":true}}`))
	}))
	defer server.Close()
	client := NewClient(server.Client(), "")
	client.baseURL, client.key, client.keyExpires = server.URL, "fixture-key", time.Now().Add(time.Hour)
	result, err := client.Browse(context.Background(), "dynamic", 1, "", 1)
	if err != nil || calls != 1 || len(result.Items) != 2 || !result.HasMore || result.Notice != "" {
		t.Fatal(calls, result, err)
	}

	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"code":0,"data":{"items":[{"type":"DYNAMIC_TYPE_DRAW","modules":{}},{"type":"DYNAMIC_TYPE_DRAW","modules":{}}],"offset":"` + r.URL.Query().Get("offset") + `x","has_more":true}}`))
	}))
	defer empty.Close()
	client.baseURL = empty.URL
	result, err = client.Browse(context.Background(), "dynamic", 1, "", 1)
	if err != nil || len(result.Items) != 0 || !result.HasMore || result.Notice != "暂时没有可显示的关注动态。可点「继续浏览」查看更早的动态。" {
		t.Fatal(result, err)
	}
}

func TestDynamicHasMoreAcceptsBooleanNumberAndStringForms(t *testing.T) {
	for _, test := range []struct {
		value string
		want  bool
	}{
		{`true`, true},
		{`1`, true},
		{`"1"`, true},
		{`"true"`, true},
		{`false`, false},
		{`0`, false},
		{`"0"`, false},
		{`null`, false},
	} {
		if got := dynamicHasMore(json.RawMessage(test.value)); got != test.want {
			t.Errorf("dynamicHasMore(%s) = %t, want %t", test.value, got, test.want)
		}
	}
}

type browseTransport func(*http.Request) (*http.Response, error)

func (f browseTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestLiveUsesItsOwnOriginAndOnlyReturnsOriginalRoomLinks(t *testing.T) {
	client := NewClient(&http.Client{Transport: browseTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "api.live.bilibili.com" || r.URL.Path != "/room/v1/Area/getRoomList" || r.URL.Query().Get("parent_area_id") != "2" || r.Header.Get("Referer") != "https://live.bilibili.com/" {
			t.Fatal("invalid live endpoint")
		}
		return &http.Response{StatusCode: 200, Body: http.NoBody, Request: r}, nil
	})}, "")
	_, _ = client.Browse(context.Background(), "live", 1, "", 2)
	client.http.Transport = browseTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Request: r, Body: io.NopCloser(strings.NewReader(`{"code":0,"data":{"list":[{"roomid":123,"title":"直播标题","uname":"主播","user_cover":"//i0.hdslb.com/live.jpg"},{"room_id":124,"title":"另一场直播","cover":"javascript:alert(1)"}]}}`))}, nil
	})
	result, err := client.Browse(context.Background(), "live", 1, "", 2)
	if err != nil || len(result.Rooms) != 2 || result.Rooms[0].URL != "https://live.bilibili.com/123" || result.Rooms[0].Thumbnail != "https://i0.hdslb.com/live.jpg" || result.Rooms[1].Thumbnail != "" {
		t.Fatal(result, err)
	}
}

func TestBrowseDoesNotForwardCredentialsOnRedirectOrRetryRestrictedResponses(t *testing.T) {
	leaks := 0
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaks++ }))
	defer destination.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, 302) }))
	defer server.Close()
	client := NewClient(server.Client(), "synthetic=secret")
	client.baseURL = server.URL
	if _, err := client.Browse(context.Background(), "popular", 1, "", 1); err == nil || leaks != 0 {
		t.Fatal("followed platform redirect")
	}
	calls := 0
	client.http.Transport = browseTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 429, Body: http.NoBody, Request: r}, nil
	})
	for range 2 {
		if _, err := client.Browse(context.Background(), "popular", 1, "", 1); err == nil {
			t.Fatal("restriction hidden")
		}
	}
	if calls != 1 {
		t.Fatal("restricted platform retried")
	}
}

func TestBrowseRejectsMalformedListsAndProfileURLs(t *testing.T) {
	for _, data := range []string{`{}`, `{"list":null}`} {
		client := NewClient(&http.Client{Transport: browseTransport(func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"code":0,"data":` + data + `}`)), Request: r}, nil
		})}, "")
		if _, err := client.Browse(context.Background(), "popular", 1, "", 1); err == nil {
			t.Fatal("malformed list returned empty success")
		}
	}
	// An endless chain of forwarded posts is bounded.
	raw := json.RawMessage(`{"type":8,"bvid":"BV17x411w7KC","title":"嵌套"}`)
	for range 5 {
		raw = json.RawMessage(`{"type":1,"orig":` + string(raw) + `}`)
	}
	if _, ok := dynamicVideo(raw, 0); ok {
		t.Fatal("unbounded forwarded chain accepted")
	}
	client := NewClient(&http.Client{Transport: browseTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"code":0,"data":{"isLogin":true,"mid":42,"uname":"我的账号","face":"javascript:alert(1)","level_info":{"current_level":6}}}`)), Request: r}, nil
	})}, "")
	profile, err := client.Profile(context.Background())
	if err != nil || !profile.LoggedIn || profile.Name != "我的账号" || profile.Avatar != "" || profile.Level != 6 {
		t.Fatal(profile, err)
	}
}
