package xiaoyuzhou

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestPublicEpisodeMappingAndPrivateMedia(t *testing.T) {
	id := "661d14a948c40e2fb076b1f4"
	raw := `<script id="__NEXT_DATA__">{"props":{"pageProps":{"episode":{"eid":"661d14a948c40e2fb076b1f4","title":"测试单集","description":"<p>节目说明</p>","pubDate":"2024-04-15T11:51:04.267Z","media":{"source":{"mode":"PUBLIC","url":"https://media.xyzcdn.net/test.mp3"}},"podcast":{"title":"测试节目"}}}}}</script>`
	item, err := Parse([]byte(raw), id)
	if err != nil || item.Platform != "xiaoyuzhou" || item.Kind != "audio" || item.MediaURL == "" || item.PublishedAt == 0 || item.Description != "节目说明" {
		t.Fatal(item, err)
	}
	private := strings.Replace(raw, `"title":"测试单集"`, `"isPrivateMedia":true,"title":"测试单集"`, 1)
	item, err = Parse([]byte(private), id)
	if err != nil || item.MediaURL != "" {
		t.Fatal("private media exposed")
	}
	if _, err := Parse([]byte(raw), "000000000000000000000000"); err == nil {
		t.Fatal("wrong identity")
	}
	for _, url := range []string{"https://xiaoyuzhoufm.com.evil.test/episode/" + id, "https://www.xiaoyuzhoufm.com/podcast/" + id, "file:///episode/" + id} {
		if _, _, err := URL(url); err == nil {
			t.Fatal("invalid URL accepted")
		}
	}
}

func TestPublicEpisodeMappingFallsBackToImageURL(t *testing.T) {
	id := "661d14a948c40e2fb076b1f4"
	raw := `<script id="__NEXT_DATA__">{"props":{"pageProps":{"episode":{"eid":"661d14a948c40e2fb076b1f4","title":"封面兼容","image":{"url":"//image.xyzcdn.net/cover.jpg"},"podcast":{"title":"测试节目"}}}}}</script>`
	item, err := Parse([]byte(raw), id)
	if err != nil || item.ThumbnailURL != "https://image.xyzcdn.net/cover.jpg" {
		t.Fatalf("image fallback missing: %+v %v", item, err)
	}
}

func TestPublicEpisodeFallsBackToNestedPodcastImage(t *testing.T) {
	id := "661d14a948c40e2fb076b1f4"
	raw := `<script id="__NEXT_DATA__">{"props":{"pageProps":{"episode":{"eid":"661d14a948c40e2fb076b1f4","title":"节目封面","podcast":{"title":"测试节目","image":{"picUrl":"https://image.xyzcdn.net/podcast.jpg"}}}}}}</script>`
	item, err := Parse([]byte(raw), id)
	if err != nil || item.ThumbnailURL != "https://image.xyzcdn.net/podcast.jpg" {
		t.Fatalf("podcast cover missing: %+v %v", item, err)
	}
}

func TestPublicAudioMetadataFallbackWithoutLogin(t *testing.T) {
	id := "661d14a948c40e2fb076b1f4"
	episode := `<script id="__NEXT_DATA__">{"props":{"pageProps":{"episode":{"eid":"661d14a948c40e2fb076b1f4","title":"公开音频","podcast":{"title":"测试节目"}}}}}</script>`
	for _, meta := range []string{
		`<meta property="og:audio" content="https://media.xyzcdn.net/public.mp3">`,
		`<meta content="https://media.xyzcdn.net/public.mp3" property="og:audio:secure_url">`,
		`<meta property="og:audio" content="https://media.xyzcdn.net/public.mp3" />`,
	} {
		for _, page := range []string{meta + episode, episode + meta} {
			item, err := Parse([]byte(page), id)
			if err != nil || item.MediaURL != "https://media.xyzcdn.net/public.mp3" {
				t.Fatalf("public audio metadata missing: %+v %v", item, err)
			}
		}
	}
	for _, tc := range []struct {
		page string
		want string
	}{
		{episode + `<meta property="og:audio" content="https://attacker.test/audio.mp3">`, ""},
		{strings.Replace(episode, `"title":"公开音频"`, `"isPrivateMedia":true,"title":"公开音频"`, 1) + `<meta property="og:audio" content="https://media.xyzcdn.net/public.mp3">`, ""},
		{strings.Replace(episode, `"title":"公开音频"`, `"enclosure":{"url":"https://media.xyzcdn.net/primary.mp3"},"title":"公开音频"`, 1) + `<meta property="og:audio" content="https://media.xyzcdn.net/public.mp3">`, "https://media.xyzcdn.net/primary.mp3"},
	} {
		item, err := Parse([]byte(tc.page), id)
		if err != nil || item.MediaURL != tc.want {
			t.Fatalf("audio fallback changed priority or privacy: %+v %v", item, err)
		}
	}
}

func TestAuthenticatedAudioUsesStructuredEnclosure(t *testing.T) {
	id := "661d14a948c40e2fb076b1f4"
	for _, tt := range []struct {
		body string
		want string
	}{
		{`{"data":{"eid":"` + id + `","enclosure":{"url":"https://media.xyzcdn.net/opaque-file"}}}`, "https://media.xyzcdn.net/opaque-file"},
		{`{"data":{"eid":"` + id + `","enclosure":{"url":"https://tk.wavpub.com/audio"}}}`, "https://tk.wavpub.com/audio"},
		{`{"data":{"eid":"` + id + `","isPrivateMedia":true,"enclosure":{"url":"https://media.xyzcdn.net/opaque-file"}}}`, ""},
		{`{"data":{"eid":"` + id + `","enclosure":{"url":"https://attacker.test/audio.mp3"}}}`, ""},
		{`{"data":{"eid":"000000000000000000000000","enclosure":{"url":"https://media.xyzcdn.net/audio.mp3"}}}`, ""},
	} {
		c := &Client{HTTP: &http.Client{Transport: transcriptTransport(func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(tt.body)), Header: make(http.Header)}, nil
		})}, Auth: &AuthSession{Tokens: func() (string, string) { return "synthetic", "" }}}
		c.Auth.HTTP = c.HTTP
		if got := c.episodeMedia(context.Background(), id); got != tt.want {
			t.Fatalf("audio URL: got %q want %q", got, tt.want)
		}
	}
}

func TestPublicEpisodeMappingUsesEnclosureURL(t *testing.T) {
	id := "661d14a948c40e2fb076b1f4"
	raw := `<script id="__NEXT_DATA__">{"props":{"pageProps":{"episode":{"eid":"661d14a948c40e2fb076b1f4","title":"测试单集","enclosure":{"url":"https://media.xyzcdn.net/test.m4a"},"podcast":{"title":"测试节目"}}}}}</script>`
	item, err := Parse([]byte(raw), id)
	if err != nil || item.MediaURL != "https://media.xyzcdn.net/test.m4a" {
		t.Fatalf("enclosure URL not mapped: %+v %v", item, err)
	}
}

func TestAudioHostingValidationAndPublicFallback(t *testing.T) {
	for _, raw := range []string{
		"http://tk.wavpub.com/audio",
		"https://tk.wavpub.com.evil.example/audio",
		"https://evilwavpub.com/audio",
		"https://unknown.wavpub.com/audio",
		"https://" + "user" + "@" + "tk.wavpub.com/audio",
		"https://tk.wavpub.com:8443/audio",
		"https://127.0.0.1/audio",
	} {
		if validAudioURL(raw) {
			t.Fatalf("unsafe audio hosting accepted: %s", raw)
		}
	}
	id := "661d14a948c40e2fb076b1f4"
	raw := `<script id="__NEXT_DATA__">{"props":{"pageProps":{"episode":{"eid":"661d14a948c40e2fb076b1f4","title":"外部托管播客","enclosure":{"url":"https://unsupported.example/audio"},"media":{"source":{"mode":"PUBLIC","url":"https://tk.wavpub.com/audio"}},"podcast":{"title":"测试节目"}}}}}</script>`
	item, err := Parse([]byte(raw), id)
	if err != nil || item.MediaURL != "https://tk.wavpub.com/audio" {
		t.Fatalf("valid public fallback missing: %+v %v", item, err)
	}
	item, err = Parse([]byte(strings.Replace(raw, `"title":"外部托管播客"`, `"isPrivateMedia":true,"title":"外部托管播客"`, 1)), id)
	if err != nil || item.MediaURL != "" {
		t.Fatal("private externally hosted audio exposed")
	}
}
