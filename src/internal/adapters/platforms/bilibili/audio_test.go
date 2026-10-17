package bilibili

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSpeechAudioUsesLowBandwidthAudioAndKeepsVideoPlaybackSeparate(t *testing.T) {
	for _, scenario := range []string{"dash", "snake", "fallback", "preview", "restricted"} {
		t.Run(scenario, func(t *testing.T) {
			combined := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Cookie") != "fixture=private" {
					t.Error("platform cookie missing")
				}
				if r.URL.Path == "/x/web-interface/view" {
					w.Write([]byte(`{"code":0,"data":{"cid":123,"duration":267}}`))
					return
				}
				if r.URL.Query().Get("fnval") == "1" {
					combined++
					w.Write([]byte(`{"code":0,"data":{"format":"mp4","durl":[{"url":"https://media.bilivideo.com/video.mp4"}]}}`))
					return
				}
				if r.URL.Query().Get("fnval") != "16" || r.URL.Query().Get("cid") != "123" {
					t.Error("wrong DASH request")
				}
				body := `{"code":0,"data":{"dash":{"audio":[{"baseUrl":"https://media.bilivideo.com/high.m4s","codecs":"mp4a.40.2","bandwidth":192000},{"baseUrl":"https://media.bilivideo.com/low.m4s","codecs":"mp4a.40.2","bandwidth":64000,"backupUrl":["https://evil.invalid/a","https://backup.bilivideo.cn/low.m4s"]},{"baseUrl":"https://evil.invalid/b","codecs":"mp4a.40.2","bandwidth":1000},{"baseUrl":"https://media.bilivideo.com/opus","codecs":"opus","bandwidth":32000}]}}}`
				switch scenario {
				case "snake":
					body = strings.ReplaceAll(strings.ReplaceAll(body, "baseUrl", "base_url"), "backupUrl", "backup_url")
				case "fallback":
					body = `{"code":0,"data":{"dash":{"audio":[]}}}`
				case "preview":
					body = `{"code":0,"data":{"is_preview":1}}`
				case "restricted":
					body = `{"code":-352}`
				}
				w.Write([]byte(body))
			}))
			defer server.Close()
			c := NewClient(server.Client(), "fixture=private")
			c.baseURL = server.URL
			audio, err := c.Audio(context.Background(), "BV1QraJ6ZESK")
			if scenario == "restricted" || scenario == "preview" {
				if err == nil || combined != 0 {
					t.Fatal("invalid audio accepted or unnecessary fallback", err)
				}
				return
			}
			if err != nil || audio.Platform != "bilibili" || audio.Duration != 267 || audio.Referer != "https://www.bilibili.com/" {
				t.Fatal(audio, err)
			}
			if scenario == "fallback" {
				if combined != 1 || !strings.HasSuffix(audio.URL, "video.mp4") {
					t.Fatal(audio, combined)
				}
			} else if combined != 0 || !strings.HasSuffix(audio.URL, "low.m4s") || len(audio.Backups) != 1 {
				t.Fatal(audio, combined)
			}
		})
	}
}
