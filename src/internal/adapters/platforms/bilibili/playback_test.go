package bilibili

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"tangentry/internal/content"
	"tangentry/internal/playback"
)

type playbackFallback struct{ calls int }

func (f *playbackFallback) Video(context.Context, string) (content.Item, error) {
	return content.Item{}, nil
}
func (f *playbackFallback) Playback(context.Context, string) (playback.Source, error) {
	f.calls++
	return playback.Source{URL: "https://fallback.bilivideo.com/video.mp4", Quality: 32, Duration: 17}, nil
}

func TestPlaybackFirstPartCombinedMP4(t *testing.T) {
	for _, format := range []string{"mp4720", "dash", "segmented", "preview"} {
		t.Run(format, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Cookie") != "fixture=private" {
					t.Error("cookie not restricted to API request")
				}
				if r.URL.Path == "/x/web-interface/view" {
					w.Write([]byte(`{"code":0,"data":{"cid":123456789}}`))
					return
				}
				q := r.URL.Query()
				if q.Get("bvid") != "BV17x411w7KC" || q.Get("cid") != "123456789" || q.Get("platform") != "html5" || q.Get("fnval") != "1" {
					t.Error("incorrect combined stream request")
				}
				body := `{"code":0,"data":{"format":"mp4720","quality":64,"durl":[{"url":"https://media.bilivideo.com/a.mp4"}]}}`
				switch format {
				case "dash":
					body = `{"code":0,"data":{"format":"dash","quality":64,"dash":{"video":[]}}}`
				case "segmented":
					body = `{"code":0,"data":{"format":"mp4","durl":[{"url":"https://media.bilivideo.com/a"},{"url":"https://media.bilivideo.com/b"}]}}`
				case "preview":
					body = `{"code":0,"data":{"format":"mp4","is_preview":1,"durl":[{"url":"https://media.bilivideo.com/a"}]}}`
				}
				w.Write([]byte(body))
			}))
			defer server.Close()
			client := NewClient(server.Client(), "fixture=private")
			client.baseURL = server.URL
			value, err := client.Playback(context.Background(), "BV17x411w7KC")
			if format == "mp4720" {
				if err != nil || value.Quality != 64 {
					t.Fatal("valid stream rejected", err)
				}
			} else if err == nil {
				t.Fatal("unsupported stream accepted")
			}
		})
	}
}

func TestPlaybackUsesFirstPageCIDAndFallsBackForSegmentedDURL(t *testing.T) {
	fallback := &playbackFallback{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/x/web-interface/view" {
			w.Write([]byte(`{"code":0,"data":{"pages":[{"cid":987,"duration":17}]}}`))
			return
		}
		w.Write([]byte(`{"code":0,"data":{"format":"mp4","quality":64,"durl":[{"url":"https://media.bilivideo.com/a"},{"url":"https://media.bilivideo.com/b"}]}}`))
	}))
	defer server.Close()
	client := NewClient(server.Client(), "", fallback)
	client.baseURL = server.URL
	source, err := client.Playback(context.Background(), "BV17x411w7KC")
	if err != nil || source.URL != "https://fallback.bilivideo.com/video.mp4" || fallback.calls != 1 {
		t.Fatalf("unexpected fallback result: %+v %v calls=%d", source, err, fallback.calls)
	}
}
