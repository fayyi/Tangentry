package discourse

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestSearchMapsDiscourseTopicsAndCursor(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search.json" || r.URL.Query().Get("q") != "关键词" || r.URL.Query().Get("page") != "1" {
			t.Fatalf("unexpected search request: %s", r.URL.String())
		}
		_, _ = w.Write([]byte(`{"topics":[{"id":42,"title":"主题","blurb":"摘要","username":"作者","created_at":"2026-10-17T12:00:00Z","reply_count":7,"views":99,"like_count":3}],"more_full_page_results":true}`))
	}))
	defer server.Close()
	client := &Client{HTTP: server.Client()}
	// The client has a fixed origin; use a transport that redirects it to the fixture.
	client.HTTP = &http.Client{Transport: rewriteTransport{base: server.URL}}
	page, err := client.Search(context.Background(), "关键词", "")
	if err != nil || len(page.Items) != 1 || page.Cursor != "2" || !page.HasMore {
		t.Fatalf("unexpected search page: %+v, %v", page, err)
	}
	if page.Items[0].ID != "linuxdo:42" || page.Items[0].Description != "摘要" || page.Items[0].Author != "作者" || page.Items[0].AuthorID != "作者" {
		t.Fatalf("unexpected topic: %+v", page.Items[0])
	}
}

func TestCreatorMapsCreatedTopicsAndUsesUsername(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/topics/created-by/alice.json" || r.URL.Query().Get("page") != "1" {
			t.Fatalf("unexpected creator request: %s", r.URL.String())
		}
		_, _ = w.Write([]byte(`{"topic_list":{"topics":[{"id":7,"title":"作者主题","excerpt":"正文","created_at":"2026-10-17T12:00:00Z","reply_count":1,"views":2,"like_count":3}],"more_full_page_results":true}}`))
	}))
	defer server.Close()
	client := &Client{HTTP: &http.Client{Transport: rewriteTransport{base: server.URL}}}
	page, err := client.Creator(context.Background(), "alice", "")
	if err != nil || len(page.Items) != 1 || page.Cursor != "2" || page.Items[0].AuthorID != "alice" {
		t.Fatalf("unexpected creator page: %+v, %v", page, err)
	}
}

type rewriteTransport struct{ base string }

func (t rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	base, err := url.Parse(t.base)
	if err != nil {
		return nil, err
	}
	clone.URL.Scheme = base.Scheme
	clone.URL.Host = base.Host
	return http.DefaultTransport.RoundTrip(clone)
}
