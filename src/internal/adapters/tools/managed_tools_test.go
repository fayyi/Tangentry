package tools

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"tangentry/internal/config"
	"tangentry/internal/downloads"
)

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// releaseServer serves fixture yt-dlp and deno releases from GitHub-like paths.
type releaseServer struct {
	mu      sync.Mutex
	files   map[string][]byte
	hits    map[string]int
	gate    chan struct{}
	gated   string
	started chan struct{}
}

func newReleaseServer(t *testing.T) (*releaseServer, *httptest.Server) {
	r := &releaseServer{files: map[string][]byte{}, hits: map[string]int{}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.mu.Lock()
		data, ok := r.files[req.URL.Path]
		r.hits[req.URL.Path]++
		gate, gated := r.gate, r.gated == req.URL.Path
		r.mu.Unlock()
		if !ok {
			http.NotFound(w, req)
			return
		}
		if gated && gate != nil {
			half := len(data) / 2
			w.Write(data[:half])
			w.(http.Flusher).Flush()
			close(r.started)
			<-gate
			w.Write(data[half:])
			return
		}
		w.Write(data)
	}))
	t.Cleanup(server.Close)
	previous := []string{ytdlpRelease, denoRelease, ytdlpLatest}
	yt, deno := ytdlpAssets[platformKey()], denoAssets[platformKey()]
	ytdlpRelease, denoRelease, ytdlpLatest = server.URL+"/yt-dlp/releases", server.URL+"/deno/releases", server.URL+"/latest"
	t.Cleanup(func() {
		ytdlpRelease, denoRelease, ytdlpLatest = previous[0], previous[1], previous[2]
		ytdlpAssets[platformKey()], denoAssets[platformKey()] = yt, deno
	})
	return r, server
}

func (r *releaseServer) set(path string, data []byte) {
	r.mu.Lock()
	r.files[path] = data
	r.mu.Unlock()
}

func fakeYTDLP(version string) []byte {
	return []byte("#!/bin/sh\nif [ \"$1\" = --version ]; then echo " + version + "; exit 0; fi\necho \"$@\"\n")
}

func publishYTDLP(r *releaseServer, version string, data []byte, pinned bool) {
	name := ytdlpAssets[platformKey()].name
	r.set("/yt-dlp/releases/download/"+version+"/"+name, data)
	if pinned {
		ytdlpAssets[platformKey()] = struct{ name, sha256 string }{name, digest(data)}
	}
}

func TestManagedYTDLPDownloadsOnFirstUseVerifiesAndRecovers(t *testing.T) {
	if os.PathSeparator != '/' {
		t.Skip("POSIX fixture")
	}
	releases, _ := newReleaseServer(t)
	good := fakeYTDLP(pinnedYTDLPVersion)
	publishYTDLP(releases, pinnedYTDLPVersion, good, true)
	// The release first serves a corrupted file: nothing may be installed.
	releases.set("/yt-dlp/releases/download/"+pinnedYTDLPVersion+"/"+ytdlpAssets[platformKey()].name, []byte("corrupted"))
	dir := t.TempDir()
	ffmpeg := filepath.Join(t.TempDir(), "ffmpeg")
	if err := os.WriteFile(ffmpeg, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, ".yt-dlp-download-stale"), []byte("partial"), 0600)
	kit := New(config.Config{ToolsDir: dir, FFmpegPath: ffmpeg})
	if _, err := os.Stat(filepath.Join(dir, ".yt-dlp-download-stale")); !os.IsNotExist(err) {
		t.Fatal("interrupted download staging survived a restart")
	}
	report := kit.Status()
	if !kit.Ready() || !report.DownloadReady || !report.Tools[0].Managed || report.Tools[0].Installed || !report.Tools[0].Available {
		t.Fatal("a downloadable yt-dlp must count as ready but not installed", report.Tools[0])
	}
	if _, err := kit.YTDLP(context.Background()); err == nil {
		t.Fatal("unverified yt-dlp accepted")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatal("failed download left files behind", entries)
	}
	if state := kit.Status().Tools[0]; state.Error == "" || state.Installed || state.Downloading {
		t.Fatal("download failure not reported", state)
	}
	publishYTDLP(releases, pinnedYTDLPVersion, good, true)
	releases.gate, releases.gated, releases.started = make(chan struct{}), "/yt-dlp/releases/download/"+pinnedYTDLPVersion+"/"+ytdlpAssets[platformKey()].name, make(chan struct{})
	release := sync.OnceFunc(func() { close(releases.gate) })
	defer release()
	result := make(chan error, 1)
	canceled, cancel := context.WithCancel(context.Background())
	go func() { _, err := kit.YTDLP(canceled); result <- err }()
	<-releases.started
	deadline := time.Now().Add(5 * time.Second)
	for {
		if state := kit.Status().Tools[0]; state.Downloading && state.Received > 0 && state.Error == "" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("download progress not reported", kit.Status().Tools[0])
		}
		time.Sleep(5 * time.Millisecond)
	}
	// A canceled request stops waiting; the download itself continues.
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	release()
	path, err := kit.YTDLP(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if contents, _ := os.ReadFile(path); !bytes.Equal(contents, good) {
		t.Fatal("installed yt-dlp differs from the verified release")
	}
	if info, _ := os.Stat(path); info.Mode()&0111 == 0 {
		t.Fatal("installed yt-dlp is not executable")
	}
	if state := kit.Status().Tools[0]; !state.Installed || state.Version != pinnedYTDLPVersion || state.Error != "" || state.Downloading {
		t.Fatal(state)
	}
	if hits := releases.hits[releases.gated]; hits != 2 {
		t.Fatal("download repeated or not retried", hits)
	}
	// A truncated executable is never trusted.
	os.WriteFile(path, []byte("#!/bin/sh\n"), 0755)
	if _, ok := kit.ytdlp.installed(); ok {
		t.Fatal("a modified executable was trusted")
	}
}

func TestManagedYTDLPGetsJavaScriptRuntimeForYouTubeOnly(t *testing.T) {
	if os.PathSeparator != '/' {
		t.Skip("POSIX fixture")
	}
	releases, _ := newReleaseServer(t)
	publishYTDLP(releases, pinnedYTDLPVersion, fakeYTDLP(pinnedYTDLPVersion), true)
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	member, _ := writer.Create("deno")
	member.Write([]byte("#!/bin/sh\necho deno\n"))
	writer.Close()
	deno := denoAssets[platformKey()]
	releases.set("/deno/releases/download/"+pinnedDenoVersion+"/"+deno.name, archive.Bytes())
	denoAssets[platformKey()] = struct{ name, sha256 string }{deno.name, digest(archive.Bytes())}
	previous := lookupRuntime
	defer func() { lookupRuntime = previous }()
	lookupRuntime = func(string) string { return "" }
	dir := t.TempDir()
	kit := New(config.Config{ToolsDir: dir})
	cmd, err := kit.ytdlpCommand(context.Background(), "--dump-single-json", "--", "https://www.youtube.com/watch?v=jNQXAC9IVRw")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Args[1] != "--js-runtimes" || cmd.Args[2] != "deno:"+filepath.Join(dir, "deno") {
		t.Fatal("YouTube extraction lacks the downloaded JavaScript runtime", cmd.Args)
	}
	if contents, _ := os.ReadFile(filepath.Join(dir, "deno")); string(contents) != "#!/bin/sh\necho deno\n" {
		t.Fatal("deno was not extracted from the verified archive")
	}
	for _, args := range [][]string{
		{"--flat-playlist", "--", "https://www.youtube.com/channel/UC" + strings.Repeat("a", 22)},
		{"--", "https://www.bilibili.com/video/BV1QraJ6ZESK/"},
	} {
		cmd, err := kit.ytdlpCommand(context.Background(), args...)
		if err != nil || strings.Contains(strings.Join(cmd.Args, " "), "--js-runtimes") {
			t.Fatal("JavaScript runtime added where it is not needed", cmd.Args, err)
		}
	}
	lookupRuntime = func(name string) string {
		if name == "node" {
			return "/usr/bin/node-fixture"
		}
		return ""
	}
	cmd, _ = kit.ytdlpCommand(context.Background(), "--", "https://youtu.be/jNQXAC9IVRw")
	if cmd.Args[2] != "node:/usr/bin/node-fixture" {
		t.Fatal("a system runtime was not preferred", cmd.Args)
	}
	explicit := New(config.Config{YTDLPPath: "/bin/sh", ToolsDir: t.TempDir()})
	if explicit.ytdlp != nil {
		t.Fatal("an explicit yt-dlp path was replaced by a managed download")
	}
	if cmd, _ := explicit.ytdlpCommand(context.Background(), "--", "https://youtu.be/jNQXAC9IVRw"); len(cmd.Args) != 3 {
		t.Fatal("explicit yt-dlp received extra arguments", cmd.Args)
	}
}

func TestUpdateYTDLPInstallsTheLatestVerifiedRelease(t *testing.T) {
	if os.PathSeparator != '/' {
		t.Skip("POSIX fixture")
	}
	releases, _ := newReleaseServer(t)
	publishYTDLP(releases, pinnedYTDLPVersion, fakeYTDLP(pinnedYTDLPVersion), true)
	kit := New(config.Config{ToolsDir: t.TempDir()})
	if _, err := kit.YTDLP(context.Background()); err != nil {
		t.Fatal(err)
	}
	newer := fakeYTDLP("2026.10.01")
	name := ytdlpAssets[platformKey()].name
	releases.set("/latest", []byte(`{"tag_name":"2026.10.01"}`))
	publishYTDLP(releases, "2026.10.01", newer, false)
	releases.set("/yt-dlp/releases/download/2026.10.01/SHA2-256SUMS", []byte(strings.Repeat("0", 64)+"  "+name+"\n"))
	wait := func() downloads.ToolStatus {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if state := kit.Status().Tools[0]; !state.Downloading {
				return state
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatal("update did not finish")
		return downloads.ToolStatus{}
	}
	if err := kit.UpdateYTDLP(); err != nil {
		t.Fatal(err)
	}
	if state := wait(); state.Error == "" || state.Version != pinnedYTDLPVersion || !state.Installed {
		t.Fatal("a checksum mismatch replaced the working yt-dlp", state)
	}
	releases.set("/yt-dlp/releases/download/2026.10.01/SHA2-256SUMS", []byte(digest(newer)+"  "+name+"\n"+digest([]byte("other"))+"  yt-dlp_other\n"))
	if err := kit.UpdateYTDLP(); err != nil {
		t.Fatal(err)
	}
	if err := kit.UpdateYTDLP(); err != nil && !errors.Is(err, downloads.ErrToolBusy) {
		t.Fatal(err)
	}
	if state := wait(); state.Error != "" || state.Version != "2026.10.01" {
		t.Fatal("update not installed", state)
	}
	path, _ := kit.YTDLP(context.Background())
	if contents, _ := os.ReadFile(path); !bytes.Equal(contents, newer) {
		t.Fatal("updated executable not published")
	}
	if New(config.Config{YTDLPPath: "/bin/sh"}).UpdateYTDLP() == nil {
		t.Fatal("an unmanaged yt-dlp cannot be updated")
	}
}

func TestBackgroundAvatarLookupsNeverStartTheFirstUseDownload(t *testing.T) {
	releases, _ := newReleaseServer(t)
	publishYTDLP(releases, pinnedYTDLPVersion, fakeYTDLP(pinnedYTDLPVersion), true)
	dir := t.TempDir()
	kit := New(config.Config{ToolsDir: dir})
	if avatar := kit.readYouTubeAvatar(context.Background(), "UC"+strings.Repeat("a", 22)); avatar != "" {
		t.Fatal(avatar)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 || len(releases.hits) != 0 {
		t.Fatal("an avatar lookup downloaded yt-dlp", entries, releases.hits)
	}
}
