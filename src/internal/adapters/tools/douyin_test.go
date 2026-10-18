package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"tangentry/internal/config"
	"tangentry/internal/credentials"
	"testing"
)

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
