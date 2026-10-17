package douyin

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"tangentry/internal/discovery"
	"testing"
)

type trip func(*http.Request) (*http.Response, error)

func (f trip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestShareResolutionIsAnonymousAndIdentityOnly(t *testing.T) {
	calls := 0
	checks := 0
	c := &Client{HTTP: &http.Client{Transport: trip(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
			t.Fatal("credential forwarded")
		}
		if calls == 1 {
			return &http.Response{StatusCode: 302, Header: http.Header{"Location": {"https://www.douyin.com/video/6961737553342991651?share=tracking"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("not parsed")), Request: r}, nil
	}), CheckRedirect: func(*http.Request, []*http.Request) error { checks++; return nil }}}
	ref, err := c.Resolve(context.Background(), "https://v.douyin.com/AbcD123/")
	if err != nil || ref.ID != "6961737553342991651" || calls != 2 || checks != 1 {
		t.Fatal(ref, calls, checks, err)
	}
	if _, err = c.Resolve(context.Background(), ref.URL); err != nil || calls != 2 {
		t.Fatal("direct input fetched")
	}
}
func TestShareResolutionRejectsOtherHostsAndCancellation(t *testing.T) {
	for _, target := range []string{"https://evil.example/private", "http://www.douyin.com/video/6961737553342991651", "https://www.douyin.com:443/video/6961737553342991651", "https://www.douyin.com/user/123"} {
		calls := 0
		c := &Client{HTTP: &http.Client{Transport: trip(func(r *http.Request) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: 302, Header: http.Header{"Location": {target}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
		})}}
		if _, err := c.Resolve(context.Background(), "https://v.douyin.com/AbcD123/"); err == nil || calls > 7 {
			t.Fatal("bad redirect", err, calls)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c := &Client{HTTP: &http.Client{Transport: trip(func(r *http.Request) (*http.Response, error) { return nil, r.Context().Err() })}}
	if _, err := c.Resolve(ctx, "https://v.douyin.com/AbcD123/"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := c.Resolve(ctx, "https://127.0.0.1/private"); !errors.Is(err, discovery.ErrInvalidQuery) {
		t.Fatal(err)
	}
}
