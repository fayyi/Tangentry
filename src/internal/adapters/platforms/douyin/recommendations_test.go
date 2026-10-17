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

func TestRecommendationMappingAndCookieIsolation(t *testing.T) {
	calls := 0
	c := &Client{Cookie: func(raw string) string {
		if raw != "https://www.douyin.com/aweme/v2/web/module/feed/" {
			t.Fatal(raw)
		}
		return "sessionid=fixture"
	}, HTTP: &http.Client{Transport: trip(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != "POST" || r.URL.Hostname() != "www.douyin.com" || r.Header.Get("Cookie") != "sessionid=fixture" || r.URL.Query().Get("refresh_index") != "2" || r.URL.Query().Get("pull_type") != "2" {
			t.Fatal("invalid request")
		}
		return &http.Response{StatusCode: 200, Request: r, Body: io.NopCloser(strings.NewReader(`{"status_code":0,"has_more":1,"aweme_list":[{"aweme_id":"6961737553342991651","desc":"推荐测试","create_time":123,"author":{"uid":"12345","nickname":"测试作者"},"video":{"duration":12345,"cover":{"url_list":["javascript:bad","https://p3.douyinpic.com/cover.jpg"]}}},{"aweme_id":"6961737553342991651","desc":"重复"},{"aweme_id":"invalid","desc":"错误"}]}`))}, nil
	})}}
	feed, err := c.Recommendations(context.Background(), 2)
	if err != nil || calls != 1 || len(feed.Items) != 1 || !feed.HasMore || feed.Items[0].DurationSeconds != 12 || feed.Items[0].ThumbnailURL != "https://p3.douyinpic.com/cover.jpg" {
		t.Fatal(feed, calls, err)
	}
}

func TestRecommendationRejectionsNeverRetryOrFollowRedirects(t *testing.T) {
	for _, body := range []string{"", "blocked", `<html>login</html>`, `{"status_code":8}`, `{"status_code":0}`, `{"status_code":0,"aweme_list":[{"aweme_id":"bad"}]}`} {
		calls := 0
		c := &Client{Cookie: func(string) string { return "sessionid=fixture" }, HTTP: &http.Client{Transport: trip(func(r *http.Request) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: 200, Request: r, Body: io.NopCloser(strings.NewReader(body))}, nil
		})}}
		_, err := c.Recommendations(context.Background(), 1)
		var platform *discovery.PlatformError
		if !errors.As(err, &platform) || !platform.Restricted || calls != 1 {
			t.Fatal(calls, err)
		}
	}
	calls := 0
	c := &Client{Cookie: func(string) string { return "sessionid=fixture" }, HTTP: &http.Client{Transport: trip(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": {"https://other.example/"}}, Request: r, Body: io.NopCloser(strings.NewReader(""))}, nil
	})}}
	if _, err := c.Recommendations(context.Background(), 1); err == nil || calls != 1 {
		t.Fatal(calls, err)
	}
	c.Cookie = nil
	if _, err := c.Recommendations(context.Background(), 1); err == nil || calls != 1 {
		t.Fatal("anonymous request attempted", err)
	}
}
