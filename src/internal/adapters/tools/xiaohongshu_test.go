package tools

import (
	"context"
	"os"
	"path/filepath"
	"tangentry/internal/config"
	"tangentry/internal/credentials"
	"testing"
)

func TestXiaohongshuExplicitAudioKeepsShareLocatorAndOwnPrivateCookie(t *testing.T) {
	if os.PathSeparator != '/' {
		t.Skip("POSIX fixture")
	}
	root := t.TempDir()
	path := filepath.Join(root, "tool.py")
	script := `#!/usr/bin/env python3
import sys,pathlib,json
if '--version' in sys.argv: print('fixture');raise SystemExit(0)
assert '--skip-download' in sys.argv and '--no-playlist' in sys.argv and '--paths' not in sys.argv
assert 'xsec_token=fixture-access' in sys.argv[-1] and 'discard' not in sys.argv[-1]
cookie=pathlib.Path(sys.argv[sys.argv.index('--cookies')+1])
assert cookie.stat().st_mode & 0o777 == 0o600
assert '.xiaohongshu.com' in cookie.read_text() and 'own-xhs' in cookie.read_text() and 'private-youtube' not in cookie.read_text()
pathlib.Path(__file__).with_name('snapshot.txt').write_text(str(cookie))
print(json.dumps({'id':'64aaaaaaaaaaaaaaaaaaaaaa','url':'https://fixture.xhscdn.com/audio.mp4','protocol':'https','duration':12}))
`
	if err := os.WriteFile(path, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	store, err := credentials.Open(filepath.Join(root, "cookies"), "", "")
	if err != nil {
		t.Fatal(err)
	}
	if store.Save("xiaohongshu", "header", "session=own-xhs") != nil || store.Save("youtube", "header", "session=private-youtube") != nil {
		t.Fatal("save")
	}
	tk := New(config.Config{YTDLPPath: path})
	tk.SetCredentials(store)
	id := "64aaaaaaaaaaaaaaaaaaaaaa"
	audio, err := tk.XiaohongshuAudio(context.Background(), id, "https://www.xiaohongshu.com/explore/"+id+"?xsec_token=fixture-access&tracking=discard")
	if err != nil || audio.Platform != "xiaohongshu" || audio.Duration != 12 {
		t.Fatal(audio, err)
	}
	file, _ := os.ReadFile(filepath.Join(root, "snapshot.txt"))
	if _, err := os.Stat(string(file)); !os.IsNotExist(err) {
		t.Fatal("snapshot retained")
	}
	for _, raw := range []string{"https://fixture.xhscdn.com.evil.example/audio.mp4", "http://fixture.xhscdn.com/audio.mp4", "https://user:secret@fixture.xhscdn.com/audio.mp4"} {
		if speechMediaURL("xiaohongshu", raw) {
			t.Fatal("unsafe audio accepted", raw)
		}
	}
	if store.Save("xiaohongshu", "netscape", ".douyin.com\tTRUE\t/\tTRUE\t0\tsession\tforeign\n") == nil {
		t.Fatal("foreign credentials accepted")
	}
}
