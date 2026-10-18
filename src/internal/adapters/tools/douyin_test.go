package tools

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"tangentry/internal/config"
	"tangentry/internal/credentials"
	"tangentry/internal/downloads"
	"tangentry/internal/playback"
	"testing"
)

type douyinTransport func(*http.Request) (*http.Response, error)

func (f douyinTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestClassifyDouyinFreshCookieFailureDistinguishesConfiguration(t *testing.T) {
	raw := "ERROR: HTTP Error 403: Forbidden; Fresh cookies (not necessarily logged in) are needed"
	withoutCookie := classifyDouyin(raw, false).Error()
	withCookie := classifyDouyin(raw, true).Error()
	if !strings.Contains(withoutCookie, "未配置抖音 Cookie") {
		t.Fatalf("missing-cookie classification: %s", withoutCookie)
	}
	if !strings.Contains(withCookie, "解析器被抖音接口拒绝") || strings.Contains(withCookie, "Cookie 无效") {
		t.Fatalf("parser-rejection classification: %s", withCookie)
	}
}

func TestClassifyDouyinTimeoutIsNetworkError(t *testing.T) {
	err := classifyDouyin("ERROR: [Douyin] Unable to download detail JSON: Operation timed out", true)
	if err == nil || !strings.Contains(err.Error(), "网络或超时") || strings.Contains(err.Error(), "Cookie") {
		t.Fatalf("timeout classification: %v", err)
	}
}

func TestDouyinDownloadUsesWebViewFallbackWithoutYTDLPAndBackup(t *testing.T) {
	if os.PathSeparator != '/' {
		t.Skip("POSIX fixture")
	}
	tk := New(config.Config{YTDLPPath: filepath.Join(t.TempDir(), "missing"), FFmpegPath: filepath.Join(t.TempDir(), "missing-ffmpeg")})
	tk.SetDouyinPlaybackResolver(func(context.Context, string) (playback.Source, error) {
		return playback.Source{
			URL:      "https://primary.douyinvod.com/video.mp4?deadline=4102444800",
			Backups:  []string{"https://backup.douyinvod.com/video.mp4?deadline=4102444800"},
			Platform: "douyin",
		}, nil
	})
	tk.SetDouyinHTTPClient(&http.Client{Transport: douyinTransport(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" || r.Header.Get("Referer") != "https://www.douyin.com/video/6961737553342991651" {
			t.Fatalf("unsafe fallback request: %+v", r.Header)
		}
		if r.URL.Hostname() == "primary.douyinvod.com" {
			return &http.Response{StatusCode: http.StatusForbidden, Header: http.Header{"Content-Type": {"text/html"}}, Body: io.NopCloser(strings.NewReader("forbidden"))}, nil
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"video/mp4"}}, Body: io.NopCloser(strings.NewReader("fixture-video"))}, nil
	})})
	directory := t.TempDir()
	result, err := tk.DownloadMedia(context.Background(), "https://www.douyin.com/video/6961737553342991651", directory, 0, downloads.Options{Mode: "video"})
	if err != nil {
		t.Fatal(err)
	}
	if result.FileName != "6961737553342991651.mp4" {
		t.Fatalf("unexpected file: %+v", result)
	}
	body, err := os.ReadFile(filepath.Join(directory, result.FileName))
	if err != nil || string(body) != "fixture-video" {
		t.Fatalf("fallback file = %q, %v", body, err)
	}
}

func TestDouyinDownloadFallbackRejectsAudioOption(t *testing.T) {
	tk := New(config.Config{YTDLPPath: filepath.Join(t.TempDir(), "missing")})
	tk.SetDouyinPlaybackResolver(func(context.Context, string) (playback.Source, error) {
		return playback.Source{URL: "https://fixture.douyinvod.com/video.mp4"}, nil
	})
	_, err := tk.DownloadMedia(context.Background(), "https://www.douyin.com/video/6961737553342991651", t.TempDir(), 0, downloads.Options{Mode: "audio"})
	if err == nil || !strings.Contains(err.Error(), "仅音频") {
		t.Fatalf("audio fallback was not rejected: %v", err)
	}
}

func TestDouyinMetadataAudioAndCookieIsolation(t *testing.T) {
	if os.PathSeparator != '/' {
		t.Skip("POSIX fixture")
	}
	root := t.TempDir()
	path := filepath.Join(root, "tool.py")
	log := filepath.Join(root, "args.json")
	script := `#!/usr/bin/env python3
import sys,json,pathlib
if '--version' in sys.argv: print('fixture');raise SystemExit(0)
assert '--skip-download' in sys.argv and '--no-playlist' in sys.argv
assert '--write-subs' not in sys.argv and '--paths' not in sys.argv
cookie=pathlib.Path(sys.argv[sys.argv.index('--cookies')+1])
assert cookie.stat().st_mode & 0o777 == 0o600
assert 'douyin-fixture' in cookie.read_text() and 'youtube-private' not in cookie.read_text()
pathlib.Path(__file__).with_name('args.json').write_text(json.dumps(sys.argv))
data={'id':'6961737553342991651','title':'抖音工具夹具','uploader':'测试作者','duration':12.9,'upload_date':'20261001','formats':[{'secret':'not forwarded'}]}
if '--format' in sys.argv: data.update({'url':'https://fixture.douyinvod.com/audio.mp4','protocol':'https'})
print(json.dumps(data))
`
	if err := os.WriteFile(path, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	store, err := credentials.Open(filepath.Join(root, "cookies"), "", "")
	if err != nil {
		t.Fatal(err)
	}
	if store.Save("douyin", "header", "session=douyin-fixture") != nil || store.Save("youtube", "header", "session=youtube-private") != nil {
		t.Fatal("save")
	}
	tk := New(config.Config{YTDLPPath: path, FFmpegPath: filepath.Join(root, "missing")})
	tk.SetCredentials(store)
	item, err := tk.Douyin(context.Background(), "https://www.douyin.com/video/6961737553342991651?tracking=discarded")
	if err != nil || item.ID != "douyin:6961737553342991651" || item.DurationSeconds != 12 || item.MediaURL != "" {
		t.Fatal(item, err)
	}
	raw, _ := json.Marshal(item)
	if strings.Contains(string(raw), "not forwarded") {
		t.Fatal("extractor payload escaped")
	}
	var args []string
	raw, _ = os.ReadFile(log)
	json.Unmarshal(raw, &args)
	for i, arg := range args {
		if arg == "--cookies" {
			if _, err := os.Stat(args[i+1]); !os.IsNotExist(err) {
				t.Fatal("snapshot retained")
			}
		}
	}
	audio, err := tk.DouyinAudio(context.Background(), item.PlatformID)
	if err != nil || audio.Platform != "douyin" || audio.Duration != 12 || audio.URL != "https://fixture.douyinvod.com/audio.mp4" {
		t.Fatal(audio, err)
	}
	for _, raw := range []string{"https://douyinvod.com.evil.example/audio.mp4", "http://fixture.douyinvod.com/audio.mp4", "https://127.0.0.1/private", "https://user:secret@fixture.douyinvod.com/audio.mp4"} {
		if speechMediaURL("douyin", raw) {
			t.Fatal("unsafe CDN accepted", raw)
		}
	}
	if store.Save("douyin", "netscape", ".youtube.com\tTRUE\t/\tTRUE\t0\tsession\tprivate\n") == nil {
		t.Fatal("foreign cookie accepted")
	}
}
func TestDouyinErrorsDoNotExposeToolOutput(t *testing.T) {
	if os.PathSeparator != '/' {
		t.Skip("POSIX fixture")
	}
	path := filepath.Join(t.TempDir(), "tool")
	script := "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo fixture; exit 0; fi\necho 'Fresh cookies private-secret https://user:secret@private.example/' >&2\nexit 1\n"
	os.WriteFile(path, []byte(script), 0700)
	tk := New(config.Config{YTDLPPath: path})
	_, err := tk.Douyin(context.Background(), "https://www.douyin.com/video/6961737553342991651")
	if err == nil || !strings.Contains(err.Error(), "Cookie") || strings.Contains(err.Error(), "secret") {
		t.Fatal(err)
	}
	_, err = tk.DouyinPlayback(context.Background(), "https://www.douyin.com/video/6961737553342991651")
	if err == nil || !strings.Contains(err.Error(), "Cookie") || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "private.example") {
		t.Fatal(err)
	}
}

func TestVideoMetadataDurationDoesNotInheritTranscriptionLimit(t *testing.T) {
	if os.PathSeparator != '/' {
		t.Skip("POSIX fixture")
	}
	for _, duration := range []string{"25200.5", "-1", "1e100"} {
		t.Run(duration, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "tool")
			script := "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo fixture; exit 0; fi\n" +
				"echo '{\"id\":\"jNQXAC9IVRw\",\"title\":\"Duration fixture\",\"duration\":" + duration + "}'\n"
			if err := os.WriteFile(path, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			tk := New(config.Config{YTDLPPath: path})
			item, err := tk.YouTube(context.Background(), "https://www.youtube.com/watch?v=jNQXAC9IVRw")
			if duration == "25200.5" {
				if err != nil || item.DurationSeconds != 25200 {
					t.Fatal("valid long video metadata rejected", item, err)
				}
			} else if err == nil {
				t.Fatal("invalid duration accepted", item)
			}
		})
	}
}
