package xiaoyuzhou

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type authTransport func(*http.Request) (*http.Response, error)

func (f authTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestAuthenticatedRequestsDoNotForwardTokensOnRedirect(t *testing.T) {
	calls := 0
	auth := &AuthSession{HTTP: &http.Client{Transport: authTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		h := make(http.Header)
		h.Set("Location", "https://elsewhere.example/transcript")
		return &http.Response{StatusCode: 302, Body: io.NopCloser(strings.NewReader("")), Header: h}, nil
	})}}
	req, _ := http.NewRequest(http.MethodGet, "https://api.xiaoyuzhoufm.com/v1/episode/get", nil)
	setAppHeaders(req, "synthetic-token")
	res, err := auth.Do(context.Background(), req, "synthetic-token")
	if err != nil || res.StatusCode != 302 || calls != 1 {
		t.Fatalf("authenticated redirect followed: calls=%d err=%v", calls, err)
	}
	res.Body.Close()
}

func TestAuthRefreshReplaysPostBody(t *testing.T) {
	access, refresh := "old-access", "old-refresh"
	calls := 0
	client := &http.Client{Transport: authTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		switch calls {
		case 1:
			return &http.Response{StatusCode: 401, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
		case 2:
			if r.URL.Path != "/app_auth_tokens.refresh" || r.Header.Get("x-jike-refresh-token") != refresh {
				t.Fatalf("unexpected refresh request: %s", r.URL)
			}
			if r.Header.Get("Market") != "AppStore" || r.Header.Get("BundleID") != "app.podcast.cosmos" || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded; charset=utf-8" {
				t.Fatal("refresh request missing native app headers")
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"success":true,"x-jike-access-token":"new-access","x-jike-refresh-token":"new-refresh"}`)), Header: make(http.Header)}, nil
		case 3:
			body, _ := io.ReadAll(r.Body)
			if string(body) != `{"eid":"episode"}` || r.Header.Get("x-jike-access-token") != "new-access" {
				t.Fatalf("request was not replayed safely: body=%q token=%q", body, r.Header.Get("x-jike-access-token"))
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{}`)), Header: make(http.Header)}, nil
		default:
			t.Fatal("unexpected request")
			return nil, nil
		}
	})}
	auth := &AuthSession{HTTP: client, Tokens: func() (string, string) { return access, refresh }, Save: func(oldAccess, oldRefresh, nextAccess, nextRefresh string) error {
		if oldAccess != access || oldRefresh != refresh {
			t.Fatal("wrong CAS snapshot")
		}
		access, refresh = nextAccess, nextRefresh
		return nil
	}}
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://api.xiaoyuzhoufm.com/v1/episode-transcript/get", strings.NewReader(`{"eid":"episode"}`))
	req.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader(`{"eid":"episode"}`)), nil }
	res, err := auth.Do(context.Background(), req, "old-access")
	if err != nil || res.StatusCode != 200 || calls != 3 {
		t.Fatalf("refresh flow failed: %v calls=%d", err, calls)
	}
}
