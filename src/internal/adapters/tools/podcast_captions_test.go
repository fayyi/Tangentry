package tools

import (
	"context"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"testing"
	"time"

	"tangentry/internal/content"
	"tangentry/internal/subtitles"
)

type podcastTransport func(*http.Request) (*http.Response, error)

func (f podcastTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestPublicPodcastCaptionsFormatsAndFallback(t *testing.T) {
	for _, format := range []string{"text/vtt", "application/x-subrip", "application/json"} {
		t.Run(format, func(t *testing.T) {
			item := content.Item{ID: "rss:" + strings.Repeat("a", 64), Platform: "rss", PlatformID: strings.Repeat("a", 64), Kind: "audio", Title: "Podcast", URL: "https://example.com/episode", Transcripts: []content.Transcript{
				{URL: "https://example.com/bad", Type: format, Language: "en"},
				{URL: "https://example.com/good", Type: format, Language: "en"},
				{URL: "https://example.com/duplicate", Type: format, Language: "en"},
			}}
			calls := 0
			client := &http.Client{Transport: podcastTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
					t.Fatal("public transcript carried credentials")
				}
				body := "<html>unavailable</html>"
				if r.URL.Path == "/good" {
					body = "WEBVTT\n\n00:00.125 --> 00:01.250\nHello\n"
					if format == "application/x-subrip" {
						body = "1\n00:00:00,125 --> 00:00:01,250\nHello\n"
					}
					if format == "application/json" {
						body = `{"version":"1.0.0","segments":[{"startTime":0.125,"endTime":1.25,"body":"Hello","speaker":"Host"}]}`
					}
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
			})}
			doc, err := PublicPodcastCaptions(context.Background(), client, item)
			if err != nil || doc.VideoID != item.ID || doc.Status != "available" || len(doc.Tracks) != 1 || calls != 2 {
				t.Fatal(doc, err, calls)
			}
			cue := doc.Tracks[0].Cues[0]
			if cue.Start != .125 || cue.End != 1.25 || cue.Text != "Hello" {
				t.Fatal("timings changed", cue)
			}
		})
	}
}

func TestPodcastJSONRejectsMissingOrInvalidTimes(t *testing.T) {
	for _, raw := range []string{
		`{"version":"1.0.0","segments":[{"endTime":2,"body":"missing start"}]}`,
		`{"version":"1.0.0","segments":[{"startTime":0,"body":"missing end"}]}`,
		`{"version":"1.0.0","segments":[{"startTime":-1,"endTime":2,"body":"negative"}]}`,
		`{"version":"1.0.0","segments":[{"startTime":2,"endTime":1,"body":"reversed"}]}`,
		`{"version":"1.0.0","segments":[{"startTime":0,"endTime":1,"body":" "}]}`,
		`{"version":"2.0.0","segments":[{"startTime":0,"endTime":1,"body":"future"}]}`,
		`{"version":"1.0.0","segments":[]}`,
		`{"version":"1.0.0","segments":[{"startTime":0,"endTime":1,"body":"valid"}]} {}`,
	} {
		if _, err := parsePodcastJSON([]byte(raw)); err != subtitles.ErrFormat {
			t.Fatalf("invalid transcript accepted: %s, %v", raw, err)
		}
	}
	cues, err := parsePodcastJSON([]byte(`{"version":"1.0.0","segments":[{"startTime":2.125,"endTime":3.25,"body":"第二句"},{"startTime":0,"endTime":1.5,"body":"第一句"}]}`))
	if err != nil || len(cues) != 2 || cues[0].Start != 0 || cues[1].Start != 2.125 || cues[1].End != 3.25 || cues[1].Text != "第二句" {
		t.Fatal("precise timing or source text changed", cues, err)
	}
}

func TestPublicPodcastIncompleteJSONFallsBackToTimedVTT(t *testing.T) {
	item := content.Item{ID: "rss:" + strings.Repeat("a", 64), Platform: "rss", PlatformID: strings.Repeat("a", 64), Kind: "audio", Title: "Podcast", URL: "https://example.com/episode", Transcripts: []content.Transcript{
		{URL: "https://example.com/incomplete.json", Type: "application/json", Language: "zh-CN"},
		{URL: "https://example.com/complete.vtt", Type: "text/vtt", Language: "zh-CN"},
	}}
	var paths []string
	client := &http.Client{Transport: podcastTransport(func(r *http.Request) (*http.Response, error) {
		paths = append(paths, r.URL.Path)
		body := `{"version":"1.0.0","segments":[{"startTime":0,"body":"No end time"}]}`
		if r.URL.Path == "/complete.vtt" {
			body = "WEBVTT\n\n00:02.125 --> 00:04.250\nComplete timed caption\n"
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}
	doc, err := PublicPodcastCaptions(context.Background(), client, item)
	if err != nil || doc.Status != "available" || len(doc.Tracks) != 1 || len(paths) != 2 || paths[0] != "/incomplete.json" || paths[1] != "/complete.vtt" {
		t.Fatal("incomplete JSON blocked timed alternative", doc, paths, err)
	}
	cue := doc.Tracks[0].Cues[0]
	if cue.Start != 2.125 || cue.End != 4.25 || cue.Text != "Complete timed caption" {
		t.Fatal("fallback timing changed", cue)
	}
}

func TestPublicPodcastCaptionsRejectsOversizeAndHonorsCancellation(t *testing.T) {
	item := content.Item{ID: "rss:" + strings.Repeat("a", 64), Platform: "rss", PlatformID: strings.Repeat("a", 64), Kind: "audio", Title: "Podcast", URL: "https://example.com/episode", Transcripts: []content.Transcript{{URL: "https://example.com/captions", Type: "text/vtt"}}}
	client := &http.Client{Transport: podcastTransport(func(r *http.Request) (*http.Response, error) {
		if err := r.Context().Err(); err != nil {
			return nil, err
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(strings.Repeat("x", (2<<20)+1)))}, nil
	})}
	doc, err := PublicPodcastCaptions(context.Background(), client, item)
	if err != subtitles.ErrPublicUnavailable || doc.Status != "none" || len(doc.Tracks) != 0 {
		t.Fatal("oversize accepted", doc, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := PublicPodcastCaptions(ctx, client, item); err != context.Canceled {
		t.Fatal("cancellation lost", err)
	}
}

func TestPublicPodcastCaptionsDefaultClientBlocksPrivateDestination(t *testing.T) {
	item := content.Item{ID: "rss:" + strings.Repeat("a", 64), Platform: "rss", PlatformID: strings.Repeat("a", 64), Kind: "audio", Title: "Podcast", URL: "https://example.com/episode", Transcripts: []content.Transcript{{URL: "http://127.0.0.1/captions", Type: "text/vtt"}}}
	doc, err := PublicPodcastCaptions(context.Background(), nil, item)
	if err != subtitles.ErrPublicUnavailable || doc.Status != "none" || len(doc.Tracks) != 0 {
		t.Fatal("private destination produced captions", doc, err)
	}
}

func TestPublicPodcastCaptionsMissingReferenceIsNotFetchFailure(t *testing.T) {
	item := content.Item{ID: "rss:" + strings.Repeat("a", 64), Platform: "rss", PlatformID: strings.Repeat("a", 64), Kind: "audio", Title: "Podcast", URL: "https://example.com/episode"}
	client := &http.Client{Transport: podcastTransport(func(*http.Request) (*http.Response, error) {
		t.Fatal("missing transcript triggered network request")
		return nil, nil
	})}
	doc, err := PublicPodcastCaptions(context.Background(), client, item)
	if err != nil || doc.Status != "none" || len(doc.Tracks) != 0 {
		t.Fatal("absence became retrieval error", doc, err)
	}
}

func TestPublicPodcastCaptionsBoundsEachAddressAndKeepsSuccessfulTracks(t *testing.T) {
	item := content.Item{ID: "rss:" + strings.Repeat("a", 64), Platform: "rss", PlatformID: strings.Repeat("a", 64), Kind: "audio", Title: "Podcast", URL: "https://example.com/episode", Transcripts: []content.Transcript{
		{URL: "https://example.com/slow-first", Type: "text/vtt", Language: "en"},
		{URL: "https://example.com/good", Type: "text/vtt", Language: "en"},
		{URL: "https://example.com/slow-last", Type: "text/vtt", Language: "zh"},
	}}
	calls := 0
	client := &http.Client{Transport: podcastTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		deadline, ok := r.Context().Deadline()
		if !ok || time.Until(deadline) > 3*time.Second || time.Until(deadline) <= 0 {
			t.Fatal("individual address has no bounded deadline", deadline)
		}
		if r.URL.Path != "/good" {
			if r.URL.Path == "/slow-first" {
				<-r.Context().Done()
				return nil, r.Context().Err()
			}
			return nil, context.DeadlineExceeded
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("WEBVTT\n\n00:00.125 --> 00:01.250\nHello\n"))}, nil
	})}
	doc, err := PublicPodcastCaptions(context.Background(), client, item)
	if err != nil || doc.Status != "available" || len(doc.Tracks) != 1 || doc.Tracks[0].Language != "en" || calls != 3 {
		t.Fatal("slow backup lost successful captions", doc, err, calls)
	}
}

func TestPublicPodcastCaptionsStripsJarAndRedirectCredentials(t *testing.T) {
	item := content.Item{ID: "rss:" + strings.Repeat("a", 64), Platform: "rss", PlatformID: strings.Repeat("a", 64), Kind: "audio", Title: "Podcast", URL: "https://example.com/episode", Transcripts: []content.Transcript{{URL: "https://example.com/captions", Type: "text/vtt"}}}
	jar, _ := cookiejar.New(nil)
	u, _ := url.Parse("https://example.com")
	jar.SetCookies(u, []*http.Cookie{{Name: "session", Value: "test-only-secret"}})
	calls := 0
	client := &http.Client{Jar: jar, CheckRedirect: func(r *http.Request, _ []*http.Request) error {
		r.Header.Set("Authorization", "Bearer test-only-secret")
		r.Header.Set("Cookie", "session=test-only-secret")
		return nil
	}, Transport: podcastTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
			t.Fatal("public request leaked credentials", r.URL.Path)
		}
		if calls == 1 {
			return &http.Response{StatusCode: 302, Header: http.Header{"Location": {"https://example.com/redirected"}}, Body: io.NopCloser(strings.NewReader(""))}, nil
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("WEBVTT\n\n00:00.125 --> 00:01.250\nHello\n"))}, nil
	})}
	doc, err := PublicPodcastCaptions(context.Background(), client, item)
	if err != nil || doc.Status != "available" || calls != 2 {
		t.Fatal(doc, err, calls)
	}
	if client.Jar != jar || len(jar.Cookies(u)) != 1 {
		t.Fatal("public fetch changed caller credentials")
	}
}

func TestPublicPodcastCaptionsRespectsLimitedHostAndKeepsOtherTracks(t *testing.T) {
	for _, status := range []int{429, 503} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			item := content.Item{ID: "rss:" + strings.Repeat("a", 64), Platform: "rss", PlatformID: strings.Repeat("a", 64), Kind: "audio", Title: "Podcast", URL: "https://example.com/episode", Transcripts: []content.Transcript{
				{URL: "https://good.example.com/en", Type: "text/vtt", Language: "en"},
				{URL: "https://limited.example.com/first", Type: "text/vtt", Language: "zh"},
				{URL: "https://limited.example.com/second", Type: "text/vtt", Language: "zh"},
				{URL: "https://backup.example.com/zh", Type: "text/vtt", Language: "zh"},
			}}
			calls := 0
			client := &http.Client{Transport: podcastTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.Path == "/second" {
					t.Fatal("retried limited server")
				}
				if r.URL.Hostname() == "limited.example.com" {
					return &http.Response{StatusCode: status, Header: http.Header{"Retry-After": {"120"}}, Body: io.NopCloser(strings.NewReader("private upstream error"))}, nil
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("WEBVTT\n\n00:00.125 --> 00:01.250\nHello\n"))}, nil
			})}
			doc, err := PublicPodcastCaptions(context.Background(), client, item)
			if err != nil || calls != 3 || doc.Status != "available" || len(doc.Tracks) != 2 || doc.Tracks[0].Language != "en" || doc.Tracks[1].Language != "zh" {
				t.Fatal("rate limit lost independent tracks", doc, err, calls)
			}
		})
	}
}

func TestPublicPodcastCaptionsRemembersLimitedRedirectDestination(t *testing.T) {
	item := content.Item{ID: "rss:" + strings.Repeat("a", 64), Platform: "rss", PlatformID: strings.Repeat("a", 64), Kind: "audio", Title: "Podcast", URL: "https://example.com/episode", Transcripts: []content.Transcript{
		{URL: "https://origin.example.com/first", Type: "text/vtt", Language: "en"},
		{URL: "https://cdn.example.com/second", Type: "text/vtt", Language: "en"},
		{URL: "https://origin.example.com/third", Type: "text/vtt", Language: "en"},
	}}
	calls := 0
	client := &http.Client{Transport: podcastTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Path == "/second" || r.URL.Path == "/third" {
			t.Fatal("limited redirect destination or origin retried")
		}
		if r.URL.Hostname() == "origin.example.com" {
			return &http.Response{StatusCode: 302, Header: http.Header{"Location": {"https://cdn.example.com/limited"}}, Body: io.NopCloser(strings.NewReader(""))}, nil
		}
		return &http.Response{StatusCode: 429, Request: r, Body: io.NopCloser(strings.NewReader(""))}, nil
	})}
	doc, err := PublicPodcastCaptions(context.Background(), client, item)
	if err != subtitles.ErrPublicUnavailable || calls != 2 || len(doc.Tracks) != 0 {
		t.Fatal(doc, err, calls)
	}
}
