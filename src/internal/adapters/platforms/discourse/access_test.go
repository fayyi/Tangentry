package discourse

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"tangentry/internal/discovery"
	"tangentry/internal/platformfetch"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func response(r *http.Request, status int, raw string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(raw)), Header: http.Header{}, Request: r}
}

func TestRankedFeedPagingFallbackAndRestrictedRequests(t *testing.T) {
	for _, status := range []int{200, 404, 401, 403, 429} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			calls := []string{}
			c := &Client{Cookie: func(string) string { return "_t=test-owned-session" }, HTTP: &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
				calls = append(calls, r.URL.Path)
				if r.URL.Host != "linux.do" || r.Header.Get("Cookie") != "_t=test-owned-session" || r.URL.Query().Get("page") != "1" {
					t.Fatal("unexpected request", r.URL)
				}
				if r.URL.Path == "/hot.json" && status != 200 {
					return response(r, status, `{}`), nil
				}
				return response(r, 200, `{"topic_list":{"more_topics_url":"/hot.json?page=2","topics":[{"id":42,"title":"话题"}]}}`), nil
			})}}
			feed, err := c.Recommendations(context.Background(), 2)
			if status == 200 || status == 404 {
				if err != nil || len(feed.Items) != 1 || feed.Page != 2 || !feed.HasMore {
					t.Fatal(feed, err)
				}
				want := 1
				if status == 404 {
					want = 2
				}
				if len(calls) != want {
					t.Fatal(calls)
				}
			} else {
				var blocked *discovery.PlatformError
				if !errors.As(err, &blocked) || !blocked.Restricted || len(calls) != 1 {
					t.Fatal(feed, calls, err)
				}
			}
		})
	}
}

func TestNativeFeedFiltersUseDiscourseRoutes(t *testing.T) {
	for _, filter := range []string{"latest", "top", "new", "unread"} {
		t.Run(filter, func(t *testing.T) {
			c := &Client{Cookie: func(string) string { return "_t=fixture" }, HTTP: &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path != "/"+filter+".json" || r.URL.Query().Get("page") != "1" {
					t.Fatalf("unexpected filter request: %s", r.URL.String())
				}
				return response(r, 200, `{"topic_list":{"more_topics_url":"","topics":[{"id":9,"title":"话题"}]}}`), nil
			})}}
			feed, err := c.Feed(context.Background(), filter, 2)
			if err != nil || len(feed.Items) != 1 || feed.Page != 2 {
				t.Fatalf("filter %s: %+v %v", filter, feed, err)
			}
		})
	}
}

func TestTopPageZeroWithUsersJSONIsNotCloudflare(t *testing.T) {
	c := &Client{Page: pageFetcherFunc(func(_ context.Context, platform, path string, request platformfetch.Request) (platformfetch.Response, error) {
		if platform != "linuxdo" || path != "/top.json?page=0" || request.Method != http.MethodGet {
			t.Fatalf("unexpected top request: %s %s", platform, path)
		}
		return platformfetch.Response{Status: http.StatusOK, URL: "https://linux.do/top.json", ContentType: "application/json; charset=utf-8", Body: `{"users":[{"id":1,"username":"reader"}],"topic_list":{"more_topics_url":"/top.json?page=1","topics":[{"id":3035608,"title":"热门话题","reply_count":2}]}}`}, nil
	})}
	feed, err := c.Feed(context.Background(), "top", 1)
	if err != nil || len(feed.Items) != 1 || feed.Items[0].ID != "linuxdo:3035608" || !feed.HasMore {
		t.Fatalf("top page JSON was not parsed: %+v, %v", feed, err)
	}
}

func TestCloudflareRecognitionRequiresHTMLOrChallengeFeatures(t *testing.T) {
	if !looksLikeCloudflareResponse(http.StatusOK, "", []byte(`<title>Just a moment...</title>`)) {
		t.Fatal("HTML challenge with missing content type was not recognized")
	}
	if looksLikeCloudflareResponse(http.StatusOK, "application/json; charset=utf-8", []byte(`{"cloudflare":"field"}`)) {
		t.Fatal("valid JSON was classified as Cloudflare")
	}
}

func TestTopJSONForbiddenDoesNotClaimCloudflare(t *testing.T) {
	c := &Client{HTTP: &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		res := response(r, http.StatusForbidden, `{"error":"forbidden"}`)
		res.Header.Set("Content-Type", "application/json; charset=utf-8")
		return res, nil
	})}}
	_, err := c.Feed(context.Background(), "top", 1)
	var platformErr *discovery.PlatformError
	if !errors.As(err, &platformErr) || platformErr.Restricted || strings.Contains(platformErr.Message, "疑似 Cloudflare") {
		t.Fatalf("JSON error response was misclassified: %v", err)
	}
}

func TestBrowserCookieWinsOverApiKeyForFeedRequests(t *testing.T) {
	c := &Client{
		Cookie:  func(string) string { return "_t=browser; cf_clearance=clearance" },
		UserKey: func() (string, string) { return "api-key-fixture", "client-fixture" },
		HTTP: &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
			if r.Header.Get("Cookie") != "_t=browser; cf_clearance=clearance" || r.Header.Get("User-Api-Key") != "" || r.Header.Get("User-Api-Client-Id") != "" {
				t.Fatalf("browser session was replaced by api key: cookie=%q key=%q", r.Header.Get("Cookie"), r.Header.Get("User-Api-Key"))
			}
			return response(r, 200, `{"topic_list":{"topics":[{"id":9,"title":"话题"}]}}`), nil
		})},
	}
	if _, err := c.Feed(context.Background(), "latest", 1); err != nil {
		t.Fatal(err)
	}
}

func TestCloudflareChallengeHasActionableError(t *testing.T) {
	c := &Client{Cookie: func(string) string { return "_t=browser" }, HTTP: &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		res := response(r, http.StatusForbidden, `<html><title>Just a moment...</title><script src="/cdn-cgi/challenge-platform"></script></html>`)
		res.Header.Set("cf-mitigated", "challenge")
		return res, nil
	})}}
	_, err := c.Feed(context.Background(), "latest", 1)
	var platformErr *discovery.PlatformError
	if !errors.As(err, &platformErr) || !platformErr.Restricted || !strings.Contains(platformErr.Message, "cf_clearance") {
		t.Fatalf("challenge error is not actionable: %v", err)
	}
}

type pageFetcherFunc func(context.Context, string, string, platformfetch.Request) (platformfetch.Response, error)

func (f pageFetcherFunc) Fetch(ctx context.Context, platform, path string, request platformfetch.Request) (platformfetch.Response, error) {
	return f(ctx, platform, path, request)
}

func TestPageBridgeIsPreferredForTopicReads(t *testing.T) {
	calls := 0
	c := &Client{
		Page: pageFetcherFunc(func(_ context.Context, platform, path string, request platformfetch.Request) (platformfetch.Response, error) {
			calls++
			if platform != "linuxdo" || path != "/latest.json?page=0" || request.Method != http.MethodGet {
				t.Fatalf("unexpected page request: %s %s %+v", platform, path, request)
			}
			return platformfetch.Response{Status: http.StatusOK, Body: `{"topic_list":{"topics":[{"id":42,"title":"页面话题"}]}}`}, nil
		}),
		HTTP: &http.Client{Transport: transportFunc(func(*http.Request) (*http.Response, error) {
			t.Fatal("HTTP fallback used despite page bridge")
			return nil, nil
		})},
	}
	feed, err := c.Latest(context.Background(), 1)
	if err != nil || calls != 1 || len(feed.Items) != 1 {
		t.Fatalf("page bridge read failed: %+v calls=%d err=%v", feed, calls, err)
	}
}

func TestPageBridgeDetailFailurePreservesDiagnostics(t *testing.T) {
	c := &Client{Page: pageFetcherFunc(func(_ context.Context, platform, path string, request platformfetch.Request) (platformfetch.Response, error) {
		if platform != "linuxdo" || path != "/t/42.json" || request.Method != http.MethodGet {
			t.Fatalf("unexpected page request: %s %s %+v", platform, path, request)
		}
		return platformfetch.Response{Status: http.StatusOK, URL: "https://linux.do/t/42.json?token=secret", ContentType: "text/html", Body: "<html>verification-page token=abcdefghijklmnopqrstuvwxyz0123456789</html>"}, nil
	})}
	_, err := c.Topic(context.Background(), "42")
	var platformErr *discovery.PlatformError
	if !errors.As(err, &platformErr) || !strings.Contains(platformErr.Message, "响应不是 JSON") || !strings.Contains(platformErr.Message, "content-type=text/html") || !strings.Contains(platformErr.Message, "最终 URL=https://linux.do/t/42.json") {
		t.Fatalf("detail diagnostics lost: %v", err)
	}
	if strings.Contains(platformErr.Message, "token=secret") || strings.Contains(platformErr.Message, "abcdefghijklmnopqrstuvwxyz0123456789") {
		t.Fatalf("diagnostic leaked secret: %s", platformErr.Message)
	}
}

func TestPageBridgeUnavailableIncludesReason(t *testing.T) {
	c := &Client{Page: pageFetcherFunc(func(context.Context, string, string, platformfetch.Request) (platformfetch.Response, error) {
		return platformfetch.Response{}, errors.New("Completion handler for function call is no longer reachable")
	})}
	_, err := c.Topic(context.Background(), "42")
	var platformErr *discovery.PlatformError
	if !errors.As(err, &platformErr) || !strings.Contains(platformErr.Message, "页面请求桥不可用") || !strings.Contains(platformErr.Message, "Completion handler") {
		t.Fatalf("bridge reason lost: %v", err)
	}
}

func TestPageBridgeAllowsFourMiBTopicResponse(t *testing.T) {
	body := `{"id":42,"title":"大响应","posts_count":1,"post_stream":{"stream":[99],"posts":[{"id":99,"post_number":1,"username":"作者","cooked":"<p>正文</p>"}]}}` + strings.Repeat(" ", 3<<20)
	c := &Client{Page: pageFetcherFunc(func(context.Context, string, string, platformfetch.Request) (platformfetch.Response, error) {
		return platformfetch.Response{Status: http.StatusOK, URL: "https://linux.do/t/42.json", ContentType: "application/json", Body: body}, nil
	})}
	item, err := c.Topic(context.Background(), "42")
	if err != nil || item.ID != "linuxdo:42" {
		t.Fatalf("4 MiB bridge response rejected: item=%+v err=%v", item, err)
	}
}

func TestTopicPostsParsesRequestedFloors(t *testing.T) {
	raw := `{"post_stream":{"posts":[{"id":"101","post_number":"2","username":"回复者","created_at":"2026-01-01T00:00:00Z","reply_to_user":{"username":"楼主","name":"楼主名称"},"reply_to_post_number":"1","reply_to_post_id":"99","like_count":"7","cooked":"<p>楼层正文</p>","actions_summary":[],"link_counts":[],"polls":null,"reactions":{"heart":1},"bookmarked":false},{"id":102,"post_number":3,"username":"空回复","reply_to_user":null,"cooked":"<p>第二层</p>","user_title":{"name":"成员"},"flair_url":null}]}}`
	c := &Client{Page: pageFetcherFunc(func(_ context.Context, platform, path string, request platformfetch.Request) (platformfetch.Response, error) {
		if platform != "linuxdo" || path != "/t/42/posts.json?post_ids[]=101" || request.Method != http.MethodGet {
			t.Fatalf("unexpected posts request: %s %s", platform, path)
		}
		return platformfetch.Response{Status: http.StatusOK, URL: "https://linux.do/t/42/posts.json?post_ids%5B%5D=101", ContentType: "application/json", Body: raw}, nil
	})}
	discussion, err := c.TopicPosts(context.Background(), "42", []int64{101})
	if err != nil || discussion.TopicID != "42" || discussion.LoadedPosts != 2 || len(discussion.Posts) != 2 || discussion.Posts[0].ID != 101 {
		t.Fatalf("topic posts not parsed: %+v, %v", discussion, err)
	}
	if post := discussion.Posts[0]; post.ReplyToUser != "楼主" || post.ReplyToNumber != 1 || post.ReplyToPostID != 99 || post.LikeCount != 7 {
		t.Fatalf("reply object was not mapped: %+v", post)
	}
	if post := discussion.Posts[1]; post.ReplyToUser != "" || post.LikeCount != 0 {
		t.Fatalf("null optional fields were not tolerated: %+v", post)
	}
}

func TestTopicPostsEmptyResponseIsActionable(t *testing.T) {
	c := &Client{Page: pageFetcherFunc(func(context.Context, string, string, platformfetch.Request) (platformfetch.Response, error) {
		return platformfetch.Response{Status: http.StatusOK, ContentType: "application/json", Body: `{"post_stream":{"posts":[]}}`}, nil
	})}
	_, err := c.TopicPosts(context.Background(), "42", []int64{101})
	var platformErr *discovery.PlatformError
	if !errors.As(err, &platformErr) || !strings.Contains(platformErr.Message, "未返回请求的楼层正文") {
		t.Fatalf("empty posts response lost reason: %v", err)
	}
}

func TestHTTPTopicResponseSizeLimitIncludesDiagnostics(t *testing.T) {
	body := `{"id":42,"title":"话题","post_stream":{"posts":[]}}` + strings.Repeat(" ", discourseMaxResponseBody)
	c := &Client{HTTP: &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		res := response(r, http.StatusOK, body)
		res.Header.Set("Content-Type", "application/json")
		return res, nil
	})}}
	_, err := c.Topic(context.Background(), "42")
	var platformErr *discovery.PlatformError
	if !errors.As(err, &platformErr) || !strings.Contains(platformErr.Message, "响应超过 4 MiB 大小限制") || !strings.Contains(platformErr.Message, "状态码=200") || !strings.Contains(platformErr.Message, "content-type=application/json") {
		t.Fatalf("oversized detail diagnostics lost: %v", err)
	}
}

func TestSessionCheckAndUserAuthorization(t *testing.T) {
	for _, raw := range []string{`{"current_user":{"id":3,"username":"reader"}}`, `{"current_user":null}`} {
		c := &Client{HTTP: &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
			if r.Header.Get("User-Api-Key") != "private-key-fixture" || r.Header.Get("User-Api-Client-Id") != "fixture-client" || r.Header.Get("Cookie") != "" {
				t.Fatal("bad authorization")
			}
			return response(r, 200, raw), nil
		})}, Cookie: func(string) string { return "_t=old" }, UserKey: func() (string, string) { return "private-key-fixture", "fixture-client" }}
		state, err := c.CurrentSession(context.Background())
		if err != nil || state.LoggedIn != strings.Contains(raw, "reader") {
			t.Fatal(state, err)
		}
	}
}

func TestMalformedAndRedirectedDataNeverRotateSession(t *testing.T) {
	for _, status := range []int{200, 302} {
		calls := 0
		c := &Client{HTTP: &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
			calls++
			res := response(r, status, "<html>verification</html>")
			res.Header.Set("Set-Cookie", "_t=must-not-save; Secure; Path=/")
			res.Header.Set("Location", "https://other.example/")
			return res, nil
		})}, Session: func() (string, func([]*http.Cookie) error) {
			return "_t=fixture", func([]*http.Cookie) error { t.Fatal("invalid data rotated credentials"); return nil }
		}}
		if _, err := c.Recommendations(context.Background(), 1); err == nil || calls != 1 {
			t.Fatal(err, calls)
		}
	}
}

func TestTopicLinksKeepTopicIDInsteadOfReplyNumber(t *testing.T) {
	for _, raw := range []string{"https://linux.do/t/42", "https://linux.do/t/example/42/19", "https://linux.do/t/example/42.json"} {
		id, err := ParseTopicURL(raw)
		if err != nil || id != "42" {
			t.Fatal(raw, id, err)
		}
	}
	for _, raw := range []string{"https://linux.do/t/example", "https://linux.do/t/-1", "https://evil.example/t/42", "https://linux.do:8443/t/42", "http://linux.do/t/42"} {
		if _, err := ParseTopicURL(raw); err == nil {
			t.Fatal(raw)
		}
	}
}
