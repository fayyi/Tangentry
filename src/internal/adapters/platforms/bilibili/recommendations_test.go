package bilibili

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHomepageRecommendationsAreNativeDeduplicatedAndExcludeBusinessCards(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/x/web-interface/index/top/feed/rcmd" || r.URL.Query().Get("ps") != "20" || r.Header.Get("Cookie") != "fixture=synthetic" {
			t.Error("unexpected recommendation request")
		}
		w.Write([]byte(`{"code":0,"data":{"item":[{"goto":"live","title":"skip"},{"goto":"av","bvid":"BV17x411w7KC","title":"A &amp; B","pic":"//i0.hdslb.com/fixture.jpg","duration":62,"owner":{"mid":123,"name":"creator"}},{"goto":"av","bvid":"BV17x411w7KC","title":"duplicate"},{"goto":"av","bvid":"BV1Pyta66EDh","title":"advert","business_info":{}},{"goto":"av","bvid":"BV1Pyta66EDh","title":"advert","is_ad":true}]}}`))
	}))
	defer server.Close()
	client := NewClient(server.Client(), "fixture=synthetic")
	client.baseURL = server.URL
	feed, err := client.Recommendations(context.Background())
	if err != nil || len(feed.Items) != 1 || feed.Items[0].Title != "A & B" || feed.Items[0].DurationSeconds != 62 {
		t.Fatal(feed, err)
	}
}
func TestMalformedRecommendationsNeverPretendToBeEmptySuccess(t *testing.T) {
	for _, raw := range []string{`{"code":-352}`, `{"code":0,"data":{}}`, `{"code":0,"data":{"item":[{"goto":"av","bvid":"invalid","title":"invalid"}]}}`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(raw)) }))
		client := NewClient(server.Client(), "")
		client.baseURL = server.URL
		if _, err := client.Recommendations(context.Background()); err == nil {
			t.Error("malformed recommendation accepted", raw)
		}
		server.Close()
	}
}

func TestRecommendationContinuationUsesPlatformPageParameters(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("fresh_idx") != "2" || r.URL.Query().Get("fresh_idx_1h") != "2" || r.URL.Query().Get("fresh_type") != "4" {
			t.Error("next recommendation batch did not advance platform parameters", r.URL.Query())
		}
		w.Write([]byte(`{"code":0,"data":{"item":[{"goto":"av","bvid":"BV17x411w7KC","title":"Next batch","owner":{"mid":123,"name":"creator","face":"https://i0.hdslb.com/avatar.jpg"}}]}}`))
	}))
	defer server.Close()
	client := NewClient(server.Client(), "")
	client.baseURL = server.URL
	feed, err := client.Recommendations(context.Background(), 2)
	if err != nil || feed.Page != 2 || !feed.HasMore || len(feed.Items) != 1 || feed.Items[0].AuthorAvatarURL == "" {
		t.Fatal(feed, err)
	}
}
