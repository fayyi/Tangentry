package httptransport

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"tangentry/internal/adapters/platforms/xiaohongshu"
	"tangentry/internal/adapters/sqlite"
	"tangentry/internal/content"
	"tangentry/internal/discovery"
)

type xhsImageTransport struct {
	calls   int
	picture []byte
}

type testImageCache struct{ cache *xiaohongshu.Images }

func (c testImageCache) Read(ctx context.Context, raw string) (ImageData, error) {
	value, err := c.cache.Read(ctx, raw)
	return ImageData{Bytes: value.Bytes, Type: value.Type}, err
}

func (t *xhsImageTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.calls++
	return &http.Response{StatusCode: 200, Request: req, Header: http.Header{"Content-Type": {"image/png"}}, Body: io.NopCloser(bytes.NewReader(t.picture))}, nil
}

func TestXiaohongshuImagesUseOnlyRegisteredSourcesAndKeepOldCollectedVersions(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "xhs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	item := content.Item{ID: "xiaohongshu:6411cf99000000001300b6d9", Platform: "xiaohongshu", PlatformID: "6411cf99000000001300b6d9", Kind: "image", Title: "图集", URL: "https://www.xiaohongshu.com/explore/6411cf99000000001300b6d9", Images: []content.Image{{URL: "https://sns-webpic-qc.xhscdn.com/old"}}}
	if _, err := store.Save(ctx, item); err != nil {
		t.Fatal(err)
	}
	item.Images[0].URL = "https://sns-webpic-qc.xhscdn.com/current"
	if err := store.SaveXiaohongshuDetail(ctx, item); err != nil {
		t.Fatal(err)
	}
	cover := item
	cover.Images = nil
	cover.ThumbnailURL = "https://sns-webpic-qc.xhscdn.com/feed-cover"
	if err := store.EnqueueRecommendations(ctx, discovery.QueueScope{Platform: "xiaohongshu", Account: discovery.PersonalAccount}, []content.Item{cover}, 101); err != nil {
		t.Fatal(err)
	}
	var picture bytes.Buffer
	if err := png.Encode(&picture, image.NewRGBA(image.Rect(0, 0, 2, 3))); err != nil {
		t.Fatal(err)
	}
	remote := &xhsImageTransport{picture: picture.Bytes()}
	cache, err := xiaohongshu.OpenImages(t.TempDir(), &http.Client{Transport: remote})
	if err != nil {
		t.Fatal(err)
	}
	h := New(content.NewService(store), nil, nil, Options{XiaohongshuImageItems: store.XiaohongshuImageCandidates, XiaohongshuImages: testImageCache{cache}, XiaohongshuImageVersion: xiaohongshu.ImageVersion})
	send := func(version string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "http://127.0.0.1/api/xiaohongshu/images/"+item.ID+"/"+version, nil))
		return w
	}
	if send("invalid").Code != 400 || send(xiaohongshu.ImageVersion("https://evil.example/private")).Code != 404 || remote.calls != 0 {
		t.Fatal("unregistered image requested")
	}
	for _, raw := range []string{item.Images[0].URL, "https://sns-webpic-qc.xhscdn.com/old", cover.ThumbnailURL} {
		w := send(xiaohongshu.ImageVersion(raw))
		if w.Code != 200 || w.Header().Get("Content-Type") != "image/png" || w.Header().Get("X-Content-Type-Options") != "nosniff" || !bytes.Equal(w.Body.Bytes(), picture.Bytes()) {
			t.Fatal("registered image failed", w.Code)
		}
		if send(xiaohongshu.ImageVersion(raw)).Code != 200 {
			t.Fatal("cached image failed")
		}
	}
	if remote.calls != 3 {
		t.Fatal("cached image fetched again", remote.calls)
	}
}

func TestXiaohongshuRefreshAcceptsOnlyStrictSameNoteRequest(t *testing.T) {
	const id = "xiaohongshu:6411cf99000000001300b6d9"
	calls := 0
	h := New(nil, nil, nil, Options{XiaohongshuRefresh: func(_ context.Context, expected, raw string) (content.Item, error) {
		calls++
		if expected != id || raw != "https://xhslink.com/a/Fixture" {
			t.Fatal("wrong refresh input")
		}
		return content.Item{ID: id}, nil
	}})
	send := func(body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("POST", "http://127.0.0.1/api/xiaohongshu/details/"+id+"/refresh", strings.NewReader(body)))
		return w
	}
	for _, body := range []string{`null`, `{}`, `{"url":""}`, `{"url":1}`, `{"url":"https://xhslink.com/a/Fixture","extra":true}`, `{"url":"https://xhslink.com/a/Fixture"}{}`} {
		if send(body).Code != 400 || calls != 0 {
			t.Fatal("bad refresh body accepted", calls)
		}
	}
	if send(`{"url":"https://xhslink.com/a/Fixture"}`).Code != 200 || calls != 1 {
		t.Fatal("valid refresh not accepted")
	}
}
