package youtube

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type playerRoundTrip func(*http.Request) (*http.Response, error)

func (f playerRoundTrip) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func playerResponse(status int, body string, req *http.Request) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: req}
}

func TestPlayerUsesSignedSessionAndFiltersTracks(t *testing.T) {
	const cookie = "SAPISID=synthetic-player-secret; SID=fixture"
	var seen *http.Request
	bootstrapSeen := false
	client := &http.Client{Transport: playerRoundTrip(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == "/sw.js_data" {
			bootstrapSeen = true
			if !strings.Contains(req.Header.Get("Cookie"), "SAPISID=synthetic-player-secret") || req.Header.Get("Authorization") != "" {
				t.Fatal("bootstrap credentials were not scoped to the YouTube origin")
			}
			return playerResponse(500, "fixture", req), nil
		}
		seen = req.Clone(req.Context())
		seen.Header = req.Header.Clone()
		body, _ := io.ReadAll(req.Body)
		var payload map[string]any
		if json.Unmarshal(body, &payload) != nil || payload["videoId"] != "jNQXAC9IVRw" {
			t.Fatal("invalid player payload")
		}
		return playerResponse(200, `{"playabilityStatus":{"status":"OK"},"videoDetails":{"videoId":"jNQXAC9IVRw"},"captions":{"playerCaptionsTracklistRenderer":{"captionTracks":[{"baseUrl":"https://www.youtube.com/api/timedtext?lang=en&v=jNQXAC9IVRw","languageCode":"en","name":{"simpleText":"English"}},{"baseUrl":"https://www.youtube.com/api/timedtext?lang=zh-Hans&v=jNQXAC9IVRw","languageCode":"zh-Hans","kind":"asr"},{"baseUrl":"https://evil.example/api/timedtext?lang=ja&v=jNQXAC9IVRw","languageCode":"ja"},{"baseUrl":"https://www.youtube.com/api/timedtext?lang=ja&tlang=en&v=jNQXAC9IVRw","languageCode":"ja"}]}}}`, req), nil
	})}
	player := NewPlayer(client)
	player.Now = func() time.Time { return time.Unix(1_800_000_000, 0) }
	result := player.Fetch(context.Background(), "jNQXAC9IVRw", cookie)
	if result.Error != "" || len(result.Tracks) != 2 || result.Tracks[1].Kind != "asr" {
		t.Fatalf("unexpected tracks: %+v", result)
	}
	if !bootstrapSeen || seen.URL.Path != "/youtubei/v1/player" || seen.Header.Get("Cookie") != cookie || !strings.HasPrefix(seen.Header.Get("Authorization"), "SAPISIDHASH ") {
		t.Fatal("cookie or signature not scoped to player")
	}
	if strings.Contains(result.Tracks[0].URL, "synthetic-player-secret") {
		t.Fatal("credential leaked")
	}
}

func TestPlayerClassifiesAuthAndRestriction(t *testing.T) {
	for _, test := range []struct {
		status int
		body   string
		want   string
	}{
		{403, `{"playabilityStatus":{"status":"LOGIN_REQUIRED","reason":"Sign in to confirm your age"}}`, "auth"},
		{200, `{"playabilityStatus":{"status":"ERROR","reason":"Sign in to confirm you're not a bot"}}`, "restricted"},
		{429, "private synthetic-player-secret", "restricted"},
	} {
		client := &http.Client{Transport: playerRoundTrip(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path == "/sw.js_data" {
				return playerResponse(500, "fixture", req), nil
			}
			return playerResponse(test.status, test.body, req), nil
		})}
		if result := NewPlayer(client).Fetch(context.Background(), "jNQXAC9IVRw", "SID=synthetic"); result.Error != test.want {
			t.Fatalf("status %d: %+v", test.status, result)
		}
	}
}

func TestPlayerBoundsTracksAndCancellation(t *testing.T) {
	tracks := make([]map[string]any, playerTrackLimit+1)
	for i := range tracks {
		tracks[i] = map[string]any{"baseUrl": "https://www.youtube.com/api/timedtext?lang=en", "languageCode": "en"}
	}
	body, _ := json.Marshal(map[string]any{"videoDetails": map[string]any{"videoId": "jNQXAC9IVRw"}, "captions": map[string]any{"playerCaptionsTracklistRenderer": map[string]any{"captionTracks": tracks}}})
	client := &http.Client{Transport: playerRoundTrip(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == "/sw.js_data" {
			return playerResponse(500, "fixture", req), nil
		}
		return playerResponse(200, string(body), req), nil
	})}
	if result := NewPlayer(client).Fetch(context.Background(), "jNQXAC9IVRw", ""); result.Error != "format" {
		t.Fatal("track bound not enforced", result)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if result := NewPlayer(client).Fetch(ctx, "jNQXAC9IVRw", ""); result.Error != "network" && result.Error != "format" {
		t.Fatal("cancelled request did not stop", result)
	}
}

func TestPlayerRefusesRedirectsAndOversizedResponses(t *testing.T) {
	calls := 0
	client := &http.Client{Transport: playerRoundTrip(func(req *http.Request) (*http.Response, error) {
		calls++
		if req.URL.Path == "/sw.js_data" {
			return playerResponse(500, "fixture", req), nil
		}
		return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": []string{"https://outside.example/player"}}, Body: io.NopCloser(strings.NewReader("redirect")), Request: req}, nil
	})}
	if result := NewPlayer(client).Fetch(context.Background(), "jNQXAC9IVRw", "SID=synthetic"); result.Error != "network" || calls != 2 {
		t.Fatalf("redirect was followed or misclassified: %+v calls=%d", result, calls)
	}
	client = &http.Client{Transport: playerRoundTrip(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == "/sw.js_data" {
			return playerResponse(500, "fixture", req), nil
		}
		return playerResponse(http.StatusOK, strings.Repeat("x", innertubeBodyLimit+1), req), nil
	})}
	if result := NewPlayer(client).Fetch(context.Background(), "jNQXAC9IVRw", ""); result.Error != "network" {
		t.Fatal("oversized player response was accepted", result)
	}
}

func TestPlayerStopsOnContextCancellationDuringRequest(t *testing.T) {
	started := make(chan struct{})
	client := &http.Client{Transport: playerRoundTrip(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == "/sw.js_data" {
			return playerResponse(500, "fixture", req), nil
		}
		close(started)
		<-req.Context().Done()
		return nil, req.Context().Err()
	})}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan PlayerResult, 1)
	go func() { done <- NewPlayer(client).Fetch(ctx, "jNQXAC9IVRw", "") }()
	<-started
	cancel()
	if result := <-done; result.Error != "network" {
		t.Fatal("cancelled player request continued", result)
	}
}
