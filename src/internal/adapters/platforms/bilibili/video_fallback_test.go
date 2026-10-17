package bilibili

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"tangentry/internal/content"
)

type extractVideoFunc func(context.Context, string) (content.Item, error)

func (f extractVideoFunc) Video(ctx context.Context, id string) (content.Item, error) {
	return f(ctx, id)
}

func TestVideoFallbackValidatesIdentityAndCanRecoverIncompleteAPIData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"code":0,"data":{"bvid":"BVother00000","title":"错误的视频资料","owner":{"name":"测试作者","mid":12345}}}`))
	}))
	defer server.Close()
	const requested = "BV17x411w7KC"
	for _, valid := range []bool{true, false} {
		calls := 0
		client := NewClient(server.Client(), "", extractVideoFunc(func(ctx context.Context, id string) (content.Item, error) {
			calls++
			if id != requested {
				t.Error("wrong extractor request")
			}
			if !valid {
				id = "BVother00000"
			}
			return content.Item{ID: "bilibili:" + id, Platform: "bilibili", PlatformID: id, Kind: "video", Title: "提取器测试资料", URL: "https://www.bilibili.com/video/" + id + "/"}, nil
		}))
		client.baseURL = server.URL
		item, err := client.Video(context.Background(), requested)
		if calls != 1 || valid && (err != nil || item.ID != "bilibili:"+requested) || !valid && err == nil {
			t.Fatal(valid, item, err, calls)
		}
	}
	client := NewClient(server.Client(), "")
	client.baseURL = server.URL
	if _, err := client.Video(context.Background(), requested); err == nil {
		t.Fatal("mismatched API identity accepted without extractor")
	}
}

func TestCanceledExtractorDoesNotReturnSuccessOrHideCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	defer server.Close()
	client := NewClient(server.Client(), "", extractVideoFunc(func(context.Context, string) (content.Item, error) {
		cancel()
		return content.Item{}, errors.New("fixture cancellation")
	}))
	client.baseURL = server.URL
	if _, err := client.Video(ctx, "BV17x411w7KC"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
