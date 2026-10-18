package bilibili

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSearchMapsSignedVideoResults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/x/web-interface/wbi/search/type" || r.URL.Query().Get("search_type") != "video" || r.URL.Query().Get("keyword") != "测试" || r.URL.Query().Get("w_rid") == "" {
			t.Fatalf("unexpected search request: %s", r.URL.String())
		}
		_, _ = w.Write([]byte(`{"code":0,"data":{"result":[{"bvid":"BV17x411w7KC","title":"<em>标题</em>","author":"作者","mid":42,"pic":"//cdn.example/cover.jpg","description":"说明","duration":"01:02","pubdate":1700000000},{"bvid":"bad","title":"忽略"}],"pageinfo":{"pages":2,"num":1}}}`))
	}))
	defer server.Close()
	client := NewClient(server.Client(), "synthetic=fixture")
	client.baseURL = server.URL
	client.key, client.keyExpires = "fixture-key", time.Now().Add(time.Hour)
	page, err := client.Search(context.Background(), "测试", "")
	if err != nil || len(page.Items) != 1 || !page.HasMore || page.Cursor != "2" {
		t.Fatalf("unexpected search page: %+v, %v", page, err)
	}
	item := page.Items[0]
	if item.ID != "bilibili:BV17x411w7KC" || item.Title != "标题" || item.AuthorID != "42" || item.DurationSeconds != 62 || item.ThumbnailURL != "https://cdn.example/cover.jpg" {
		t.Fatalf("unexpected item: %+v", item)
	}
}
