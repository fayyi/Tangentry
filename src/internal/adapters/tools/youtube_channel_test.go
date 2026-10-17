package tools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"tangentry/internal/config"
	"tangentry/internal/credentials"
	"tangentry/internal/discovery"
	"testing"
	"time"
)

func TestChannelResolutionIsMetadataOnlyAndUsesNoCredentials(t *testing.T) {
	if os.PathSeparator != '/' {
		t.Skip("POSIX process fixture")
	}
	root := t.TempDir()
	exe := filepath.Join(root, "fixture.py")
	log := filepath.Join(root, "calls")
	script := `#!/usr/bin/env python3
import json,sys,os,time,pathlib
if '--version' in sys.argv: print('explicit channel fixture');raise SystemExit
assert '--ignore-config' in sys.argv and '--no-cache-dir' in sys.argv and '--simulate' in sys.argv and '--skip-download' in sys.argv and '--flat-playlist' in sys.argv
assert sys.argv[sys.argv.index('--playlist-items')+1]=='0'
assert '--cookies' not in sys.argv and '--cookies-from-browser' not in sys.argv and '--write-subs' not in sys.argv and '--paths' not in sys.argv
assert all(not k.startswith('TANGENTRY_') for k in os.environ)
assert sys.argv[-2]=='--'
pathlib.Path('CALL_LOG').write_text('metadata requested')
raw=sys.argv[-1]
if raw.endswith('@slow'):time.sleep(120)
if raw.endswith('@private'):print('Cookie synthetic-private-secret HTTP403',file=sys.stderr);raise SystemExit(1)
if raw.endswith('@large'):print('x'*((4<<20)+1));raise SystemExit
if raw.endswith('@malformed'):print('not JSON');raise SystemExit
id='UCaaaaaaaaaaaaaaaaaaaaaa'
data={'_type':'playlist','channel_id':id,'channel':'Channel fixture','channel_url':'https://www.youtube.com/channel/'+id,'entries':[],'private':'not returned'}
if raw.endswith('@wrong'):data['channel_url']='https://www.youtube.com/channel/UCbbbbbbbbbbbbbbbbbbbbbb'
if raw.endswith('@badentries'):data['entries']=[{'id':'should not extract'}]
print(json.dumps(data))
`
	script = strings.ReplaceAll(script, "CALL_LOG", log)
	if err := os.WriteFile(exe, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	jar, err := credentials.Open(filepath.Join(root, "credentials"), "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err = jar.Save("youtube", "header", "SID=synthetic-private-secret"); err != nil {
		t.Fatal(err)
	}
	tk := New(config.Config{YTDLPPath: exe, FFmpegPath: filepath.Join(root, "absent")})
	tk.SetCredentials(jar)
	channel, err := tk.YouTubeChannel(context.Background(), "https://youtube.com/@fixture/videos?share=ignored")
	if err != nil || channel.ID != "UCaaaaaaaaaaaaaaaaaaaaaa" || channel.Title != "Channel fixture" || channel.URL != discovery.YouTubeChannelURL(channel.ID) {
		t.Fatal(channel, err)
	}
	if _, err = os.Stat(log); err != nil {
		t.Fatal("tool not used")
	}
	os.Remove(log)
	direct, err := tk.YouTubeChannel(context.Background(), channel.ID)
	if err != nil || direct.ID != channel.ID {
		t.Fatal("direct ID invalid", direct, err)
	}
	if _, err = os.Stat(log); !os.IsNotExist(err) {
		t.Fatal("direct channel ID invoked tool")
	}
	for _, raw := range []string{"http://127.0.0.1/private", "https://youtube.com/watch?v=abcdefghijk"} {
		if _, err = tk.YouTubeChannel(context.Background(), raw); !errors.Is(err, discovery.ErrInvalidQuery) {
			t.Fatal("bad URL reached tool")
		}
	}
	for _, handle := range []string{"@private", "@large", "@malformed", "@wrong", "@badentries"} {
		_, err = tk.YouTubeChannel(context.Background(), handle)
		if err == nil || strings.Contains(err.Error(), "synthetic-private-secret") || strings.Contains(err.Error(), "Cookie") {
			t.Fatal("error hidden/leaked", handle, err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	if _, err = tk.YouTubeChannel(ctx, "@slow"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("cancellation hidden", err)
	}
}
func TestMissingChannelResolverStillAcceptsDirectID(t *testing.T) {
	tk := New(config.Config{YTDLPPath: filepath.Join(t.TempDir(), "absent"), FFmpegPath: filepath.Join(t.TempDir(), "absent")})
	if _, err := tk.YouTubeChannel(context.Background(), "@fixture"); err == nil {
		t.Fatal("missing tool hidden")
	}
	if _, err := tk.YouTubeChannel(context.Background(), "UCaaaaaaaaaaaaaaaaaaaaaa"); err != nil {
		t.Fatal("direct ID needs tools", err)
	}
}
func TestRealYouTubeHandleResolution(t *testing.T) {
	if os.Getenv("TANGENTRY_YOUTUBE_CHANNEL_SMOKE") != "1" {
		t.Skip("explicit anonymous channel smoke test")
	}
	tk := New(config.Config{})
	channel, err := tk.YouTubeChannel(context.Background(), "@LinusTechTips")
	if err != nil || channel.ID != "UCXuqSBlHAE6Xw-yeJA0Tunw" {
		t.Fatalf("public handle resolution unavailable: %+v %v", channel, err)
	}
	t.Logf("Anonymous handle resolved: %s (%s)", channel.Title, channel.ID)
}
