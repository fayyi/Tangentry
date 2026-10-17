package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"runtime"
	"strings"
	"time"
)

// managedRelease is one verified download of a tool executable.
type managedRelease struct {
	Version string
	URL     string
	SHA256  string
	// Member names the executable inside a zip archive; empty for a bare file.
	Member string
}

// Pinned releases are downloaded on first use instead of shipping in installers.
// Checksums come from each project's published release digests.
const pinnedYTDLPVersion = "2026.08.19"

var ytdlpAssets = map[string]struct{ name, sha256 string }{
	"darwin/arm64":  {"yt-dlp_macos", "0f192b7ec147ab6288885d6351d9ab67367640029b4377576ef46dd79cf7b202"},
	"darwin/amd64":  {"yt-dlp_macos", "0f192b7ec147ab6288885d6351d9ab67367640029b4377576ef46dd79cf7b202"},
	"windows/amd64": {"yt-dlp.exe", "66674953fe251b89f4d08c5f0e35e0728679bd67ab3d7d05c0562af101dd3e7a"},
	"windows/arm64": {"yt-dlp_arm64.exe", "05b438997bafc3affdfda9d041353c9d73e04dc842207254b655b0887c4445b0"},
	"linux/amd64":   {"yt-dlp_linux", "58162f9bfdc27458ea47bfcb311cf47028f17d8154a8bf7d689861d46399230a"},
	"linux/arm64":   {"yt-dlp_linux_aarch64", "b16e4dab368a816cd05d477d698a605a6ae87ccee1c8ffd38fa21d7254141fcc"},
}

// Deno runs YouTube's JavaScript challenges for yt-dlp when the system has
// no deno or node of its own.
const pinnedDenoVersion = "v2.9.7"

var denoAssets = map[string]struct{ name, sha256 string }{
	"darwin/arm64":  {"deno-aarch64-apple-darwin.zip", "5cd46d6268f6f78f5d88bdc7159d20bd44cdaa4b3303474839f87ec6fe7ae25c"},
	"darwin/amd64":  {"deno-x86_64-apple-darwin.zip", "95daaff11c116a52ad54785e7914c8e9c9cdcaba793c5ed929c74ca2d8e6259a"},
	"windows/amd64": {"deno-x86_64-pc-windows-msvc.zip", "a0c3101b4158d1dfb7d6a78a7bf0f3de80c96bb423c152beec8beb22786f2238"},
	"windows/arm64": {"deno-aarch64-pc-windows-msvc.zip", "c4c4ac8bfdaa37814bda5c05fc9cdf2154904e2ef8277673a30bdceaaa649807"},
	"linux/amd64":   {"deno-x86_64-unknown-linux-gnu.zip", "c6527f24f4b16031d3ae4fa9f658d5f11534c8d84ce7dc8502420280919c3490"},
	"linux/arm64":   {"deno-aarch64-unknown-linux-gnu.zip", "c832298b1ad4422481334855f6003e0f54145762c5a134f20a489511d2f65bbf"},
}

const (
	managedDownloadLimit = 256 << 20
	managedTimeout       = 30 * time.Minute
)

func platformKey() string { return runtime.GOOS + "/" + runtime.GOARCH }

func exeName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

var (
	ytdlpRelease = "https://github.com/yt-dlp/yt-dlp/releases"
	denoRelease  = "https://github.com/denoland/deno/releases"
	ytdlpLatest  = "https://api.github.com/repos/yt-dlp/yt-dlp/releases/latest"
)

func pinnedYTDLP() (managedRelease, error) {
	asset, ok := ytdlpAssets[platformKey()]
	if !ok {
		return managedRelease{}, fmt.Errorf("yt-dlp has no release for %s", platformKey())
	}
	return managedRelease{Version: pinnedYTDLPVersion, URL: ytdlpRelease + "/download/" + pinnedYTDLPVersion + "/" + asset.name, SHA256: asset.sha256}, nil
}

func pinnedDeno() (managedRelease, error) {
	asset, ok := denoAssets[platformKey()]
	if !ok {
		return managedRelease{}, fmt.Errorf("deno has no release for %s", platformKey())
	}
	return managedRelease{Version: pinnedDenoVersion, URL: denoRelease + "/download/" + pinnedDenoVersion + "/" + asset.name, SHA256: asset.sha256, Member: exeName("deno")}, nil
}

var releaseTag = regexp.MustCompile(`^\d{4}\.\d{2}\.\d{2}(?:\.\d+)?$`)

// latestYTDLP resolves the newest stable yt-dlp release and its published
// SHA-256 sum.
func latestYTDLP(ctx context.Context, client *http.Client) (managedRelease, error) {
	asset, ok := ytdlpAssets[platformKey()]
	if !ok {
		return managedRelease{}, fmt.Errorf("yt-dlp has no release for %s", platformKey())
	}
	body, err := fetchText(ctx, client, ytdlpLatest, 1<<20)
	if err != nil {
		return managedRelease{}, err
	}
	var release struct {
		Tag string `json:"tag_name"`
	}
	if err := json.Unmarshal(body, &release); err != nil || !releaseTag.MatchString(release.Tag) {
		return managedRelease{}, errors.New("unexpected yt-dlp release metadata")
	}
	sums, err := fetchText(ctx, client, ytdlpRelease+"/download/"+release.Tag+"/SHA2-256SUMS", 1<<20)
	if err != nil {
		return managedRelease{}, err
	}
	sum := checksumFor(string(sums), asset.name)
	if sum == "" {
		return managedRelease{}, fmt.Errorf("yt-dlp %s publishes no checksum for %s", release.Tag, asset.name)
	}
	return managedRelease{Version: release.Tag, URL: ytdlpRelease + "/download/" + release.Tag + "/" + asset.name, SHA256: sum}, nil
}

var sha256Pattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

func checksumFor(sums, name string) string {
	scanner := bufio.NewScanner(strings.NewReader(sums))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == name && sha256Pattern.MatchString(strings.ToLower(fields[0])) {
			return strings.ToLower(fields[0])
		}
	}
	return ""
}

func fetchText(ctx context.Context, client *http.Client, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: HTTP %d", url, response.StatusCode)
	}
	return io.ReadAll(io.LimitReader(response.Body, limit))
}
