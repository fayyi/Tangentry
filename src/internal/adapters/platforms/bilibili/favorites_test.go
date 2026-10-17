package bilibili

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"tangentry/internal/discovery"
	"tangentry/internal/favoritesync"
)

func TestFavoriteFoldersAndPagesUseDynamicCredentialsAndCanonicalVideoIdentity(t *testing.T) {
	cookie := "explicit-test-cookie"
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Cookie") != cookie {
			t.Error("credential provider ignored")
		}
		switch r.URL.Path {
		case "/x/web-interface/nav":
			w.Write([]byte(`{"code":0,"data":{"isLogin":true,"mid":12345}}`))
		case "/x/v3/fav/folder/created/list-all":
			if r.URL.Query().Get("up_mid") != "12345" {
				t.Error(r.URL.RawQuery)
			}
			w.Write([]byte(`{"code":0,"data":{"count":1,"list":[{"id":123456,"title":"学习 &amp; 收藏","media_count":3}]}}`))
		case "/x/v3/fav/resource/list":
			q := r.URL.Query()
			if q.Get("media_id") != "123456" || q.Get("ps") != "20" || q.Get("pn") != "2" || q.Get("order") != "mtime" {
				t.Error(q)
			}
			w.Write([]byte(`{"code":0,"data":{"info":{"id":123456,"title":"学习 &amp; 收藏","media_count":3},"has_more":false,"medias":[{"type":2,"attr":0,"bv_id":"BV17x411w7KC","title":"真实映射测试","cover":"//i0.hdslb.com/cover.jpg","duration":62,"upper":{"mid":345,"name":"创作者"},"pubtime":123},{"type":2,"attr":9,"title":"deleted"},{"type":12,"attr":0,"title":"audio"}]}}`))
		default:
			t.Errorf("unexpected endpoint %s", r.URL.Path)
		}
	}))
	defer server.Close()
	client := NewClient(server.Client(), "")
	client.baseURL = server.URL
	client.SetCookieProvider(func(raw string) string { return cookie })
	folders, err := client.Folders(context.Background())
	if err != nil || len(folders) != 1 || folders[0].Title != "学习 & 收藏" {
		t.Fatal(folders, err)
	}
	cookie = "another-explicit-test-cookie"
	page, err := client.Page(context.Background(), "123456", 2)
	if err != nil || len(page.Items) != 1 || page.Excluded != 2 || page.Entries != 3 || page.HasMore || page.Items[0].ID != "bilibili:BV17x411w7KC" || page.Items[0].AuthorID != "345" {
		t.Fatal(page, err)
	}
	if calls != 3 {
		t.Fatal(calls)
	}
}
func TestFavoriteMalformedOrRestrictedResponsesNeverAppearAsEmptySuccess(t *testing.T) {
	for _, data := range []string{
		`{"code":0,"data":{}}`,
		`{"code":0,"data":{"info":{"id":123,"title":"test","media_count":1},"medias":[]}}`,
		`{"code":0,"data":{"info":{"id":999,"title":"test","media_count":0},"has_more":false,"medias":[]}}`,
		`{"code":0,"data":{"info":{"id":123,"title":"test","media_count":1},"has_more":false,"medias":[{"type":2,"attr":0,"bvid":"not-a-bvid","title":"test"}]}}`,
		`{"code":-101,"data":null}`, `{"code":-352,"data":null}`,
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(data)) }))
		client := NewClient(server.Client(), "")
		client.baseURL = server.URL
		if _, err := client.Page(context.Background(), "123", 1); err == nil {
			t.Fatal("accepted", data)
		}
		server.Close()
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(412) }))
	defer server.Close()
	client := NewClient(server.Client(), "")
	client.baseURL = server.URL
	_, err := client.Folders(context.Background())
	var platform *discovery.PlatformError
	if !errors.As(err, &platform) || !platform.Restricted {
		t.Fatal(err)
	}
	if _, err := client.Page(context.Background(), "../bad", 1); !errors.Is(err, favoritesync.ErrInvalid) {
		t.Fatal(err)
	}
}
