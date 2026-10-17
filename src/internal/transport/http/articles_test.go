package httptransport

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"tangentry/internal/adapters/sqlite"
	"tangentry/internal/articles"
	"tangentry/internal/content"
)

type httpArticleExtractor struct{ calls int }

func (e *httpArticleExtractor) Fetch(ctx context.Context, raw string) (articles.Document, error) {
	e.calls++
	return articles.Document{SourceURL: raw, Blocks: []articles.Block{{Kind: "paragraph", Text: "从原站提取的全文😀"}}}, ctx.Err()
}

func TestArticleImageProxyIsBoundedAndSendsOnlySourceOrigin(t *testing.T) {
	imageServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Referer") != "https://www.example.com/" {
			t.Errorf("unexpected image referer: %q", r.Header.Get("Referer"))
		}
		if r.URL.Path == "/html" {
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("not an image"))
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("image fixture"))
	}))
	defer imageServer.Close()
	handler := New(nil, nil, nil, Options{ArticleImageHTTP: &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		clone := req.Clone(req.Context())
		clone.URL.Scheme = "http"
		clone.URL.Host = strings.TrimPrefix(imageServer.URL, "http://")
		return imageServer.Client().Do(clone)
	})}})
	request := func(raw string) *httptest.ResponseRecorder {
		path := "/api/articles/image?url=" + url.QueryEscape(raw) + "&referrer=" + url.QueryEscape("https://www.example.com/story?secret=discard")
		req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1"+path, nil)
		req.Header.Set("Origin", "http://127.0.0.1")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w
	}
	if w := request("https://images.example.com/ok"); w.Code != http.StatusOK || w.Header().Get("Content-Type") != "image/png" || w.Body.String() != "image fixture" {
		t.Fatal(w.Code, w.Header(), w.Body.String())
	}
	if w := request("https://images.example.com/html"); w.Code != http.StatusUnsupportedMediaType {
		t.Fatal("HTML response passed as an image", w.Code)
	}
	for _, raw := range []string{"file:///etc/passwd", "https://user:password@example.com/image.png"} {
		if w := request(raw); w.Code != http.StatusBadRequest {
			t.Fatal("unsafe image URL accepted", raw, w.Code)
		}
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestArticleHTTPExplicitFetchVersionsAndInputs(t *testing.T) {
	s, err := sqlite.Open(filepath.Join(t.TempDir(), "article.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	id := "rss:" + strings.Repeat("a", 64)
	item := content.Item{ID: id, Platform: "rss", PlatformID: strings.Repeat("a", 64), Kind: "article", Title: "RSS 摘要", URL: "https://example.com/story", Description: "摘要"}
	if _, err = s.Save(context.Background(), item); err != nil {
		t.Fatal(err)
	}
	extractor := &httpArticleExtractor{}
	h := New(content.NewService(s), nil, nil, Options{Articles: articles.New(s, extractor)})
	send := func(method, path, body, origin string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://127.0.0.1"+path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", origin)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	base := "/api/articles/" + id
	if w := send("GET", base, "", ""); w.Code != 404 || extractor.calls != 0 {
		t.Fatal(w.Code, extractor.calls)
	}
	for _, body := range []string{`{}`, `{"expectedRevision":null}`, `{"expectedRevision":-1}`, `{"expectedRevision":0,"url":"https://evil.example"}`, `{"expectedRevision":0} {}`, strings.Repeat("x", 2000)} {
		if w := send("POST", base+"/fetch", body, ""); w.Code != 400 || extractor.calls != 0 {
			t.Fatal(w.Code, w.Body.String(), extractor.calls)
		}
	}
	if w := send("POST", base+"/fetch", `{"expectedRevision":0}`, "https://evil.example"); w.Code != 403 {
		t.Fatal(w.Code)
	}
	w := send("POST", base+"/fetch", `{"expectedRevision":0}`, "")
	if w.Code != 200 || extractor.calls != 1 {
		t.Fatal(w.Code, w.Body.String())
	}
	var doc articles.Document
	if json.Unmarshal(w.Body.Bytes(), &doc) != nil || doc.ContentID != id || doc.Revision != 1 || doc.Text == item.Description {
		t.Fatal(doc)
	}
	if w = send("GET", base, "", ""); w.Code != 200 || extractor.calls != 1 {
		t.Fatal(w.Code, extractor.calls)
	}
	if w = send("POST", base+"/fetch", `{"expectedRevision":0}`, ""); w.Code != 409 || extractor.calls != 1 {
		t.Fatal(w.Code, extractor.calls)
	}
	if w = send("POST", base+"/fetch", `{"expectedRevision":1}`, ""); w.Code != 200 || extractor.calls != 2 {
		t.Fatal(w.Code, extractor.calls)
	}
	if w = send("GET", base+"?url=secret", "", ""); w.Code != 400 {
		t.Fatal(w.Code)
	}
	if w = send("POST", "/api/articles/rss:"+strings.Repeat("b", 64)+"/fetch", `{"expectedRevision":0}`, ""); w.Code != 404 {
		t.Fatal(w.Code)
	}
	item.Kind = "audio"
	s.Save(context.Background(), item)
	if w = send("POST", base+"/fetch", `{"expectedRevision":2}`, ""); w.Code != 400 || extractor.calls != 2 {
		t.Fatal(w.Code, extractor.calls)
	}
	// A document is readable locally without doing network work even if metadata changes.
	if w = send("GET", base, "", ""); w.Code != 200 || extractor.calls != 2 {
		t.Fatal(w.Code)
	}
}

func TestArticleImageProxyRetriesWithoutRefererAndSniffsUnlabelledImages(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")
	var referers []string
	handler := New(nil, nil, nil, Options{ArticleImageHTTP: &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		referers = append(referers, req.Header.Get("Referer"))
		recorder := httptest.NewRecorder()
		if req.Header.Get("Referer") != "" {
			recorder.WriteHeader(http.StatusForbidden)
		} else {
			recorder.Header().Set("Content-Type", "application/octet-stream")
			_, _ = recorder.Write(png)
		}
		return recorder.Result(), nil
	})}})
	path := "/api/articles/image?url=" + url.QueryEscape("https://cdn.example.com/a") + "&referrer=" + url.QueryEscape("https://blog.example.com/post")
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1"+path, nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "image/png" {
		t.Fatal(w.Code, w.Header(), w.Body.String())
	}
	if len(referers) != 2 || referers[0] != "https://blog.example.com/" || referers[1] != "" {
		t.Fatalf("referers = %q", referers)
	}
}
