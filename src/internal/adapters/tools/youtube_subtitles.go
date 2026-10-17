package tools

import (
	"context"
	"encoding/json"
	"html"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"tangentry/internal/discovery"
	"tangentry/internal/subtitles"
)

type youtubeCaption struct {
	Ext  string `json:"ext"`
	URL  string `json:"url"`
	Name string `json:"name"`
}
type youtubeCaptionMetadata struct {
	ID        string                      `json:"id"`
	Subtitles map[string][]youtubeCaption `json:"subtitles"`
	Automatic map[string][]youtubeCaption `json:"automatic_captions"`
}

// YouTube captions are remote resources. yt-dlp fetches exactly one selected
// existing track into a private disposable directory; media is never downloaded.
func (t *Toolkit) ReadYouTubeSubtitles(ctx context.Context, id, language string) (subtitles.Document, error) {
	parsed, canonical, err := discovery.ParseYouTube("https://www.youtube.com/watch?v=" + id)
	if err != nil || parsed != id || language != "" && !languagePattern.MatchString(language) {
		return subtitles.Document{}, subtitles.ErrInvalid
	}
	if !t.ytdlpReady() {
		return subtitles.Document{}, subtitles.ErrTools
	}
	ctx, cancel := context.WithTimeout(ctx, 28*time.Second)
	defer cancel()
	cookieArgs, clean, err := t.cookieArguments(canonical)
	if err != nil {
		return subtitles.Document{}, subtitles.ErrUnavailable
	}
	defer clean()
	base := []string{"--ignore-config", "--no-playlist", "--no-cache-dir", "--skip-download", "--dump-single-json", "--socket-timeout", "10", "--retries", "0", "--extractor-args", "youtube:skip=translated_subs"}
	run := func(extra []string) ([]byte, error) {
		args := append(append(append([]string{}, base...), cookieArgs...), extra...)
		args = append(args, "--", canonical)
		cmd, err := t.ytdlpCommand(ctx, args...)
		if err != nil {
			return nil, subtitles.ErrTools
		}
		stdout, stderr := &metadataBuffer{limit: 16 << 20}, &boundedBuffer{}
		cmd.Stdout, cmd.Stderr = stdout, stderr
		if err := cmd.Run(); err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, youtubeCaptionFailure(stderr.String())
		}
		if stdout.exceeded {
			return nil, subtitles.ErrFormat
		}
		warnings := stderr.String()
		if len(warnings) >= 16384 {
			return nil, subtitles.ErrUnavailable
		}
		for _, line := range strings.Split(warnings, "\n") {
			lower := strings.ToLower(line)
			if strings.Contains(line, "WARNING:") && (strings.Contains(lower, "subtitle") || strings.Contains(lower, "caption")) {
				return nil, youtubeCaptionFailure(line)
			}
		}
		return stdout.Bytes(), nil
	}
	raw, err := run([]string{"--simulate"})
	if err != nil {
		return subtitles.Document{}, err
	}
	choices, err := youtubeLanguages(raw, id)
	if err != nil {
		return subtitles.Document{}, err
	}
	doc := subtitles.Document{VideoID: id, Part: 1, Status: "none", Tracks: []subtitles.Track{}, Languages: choices}
	if len(choices) == 0 {
		if language != "" {
			return subtitles.Document{}, subtitles.ErrInvalid
		}
		return doc, nil
	}
	if language == "" {
		language = preferredYouTubeLanguage(choices)
	}
	var chosen *subtitles.Language
	for i := range choices {
		if choices[i].Language == language {
			chosen = &choices[i]
			break
		}
	}
	if chosen == nil {
		return subtitles.Document{}, subtitles.ErrInvalid
	}
	directory, err := os.MkdirTemp("", "tangentry-youtube-captions-*")
	if err != nil {
		return subtitles.Document{}, subtitles.ErrUnavailable
	}
	defer os.RemoveAll(directory)
	// dump-single-json implies simulation by default. Explicitly allow the subtitle
	// file write while skip-download continues to forbid media downloads.
	raw, err = run([]string{"--no-simulate", "--write-subs", "--write-auto-subs", "--sub-langs", "^" + regexp.QuoteMeta(language) + "$", "--sub-format", "json3/srt/vtt", "--paths", directory, "--output", "caption.%(ext)s"})
	if err != nil {
		return subtitles.Document{}, err
	}
	// Check the second extraction identity as well. Its metadata never crosses API.
	var received struct {
		ID        string `json:"id"`
		Requested map[string]struct {
			Ext string `json:"ext"`
			URL string `json:"url"`
		} `json:"requested_subtitles"`
	}
	if json.Unmarshal(raw, &received) != nil || received.ID != id || len(received.Requested) != 1 || !supportedYouTubeCaptionFormat(received.Requested[language].Ext) {
		return subtitles.Document{}, subtitles.ErrFormat
	}
	if !nativeYouTubeCaptionURL(received.Requested[language].URL) {
		return subtitles.Document{}, subtitles.ErrFormat
	}
	extension := received.Requested[language].Ext
	path := filepath.Join(directory, "caption."+language+"."+extension)
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 2<<20 || info.Size() == 0 {
		return subtitles.Document{}, subtitles.ErrFormat
	}
	file, err := os.Open(path)
	if err != nil {
		return subtitles.Document{}, subtitles.ErrUnavailable
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, (2<<20)+1))
	if err != nil || len(data) > 2<<20 {
		return subtitles.Document{}, subtitles.ErrFormat
	}
	var cues []subtitles.Cue
	switch extension {
	case "json3":
		cues, err = parseJSON3(data)
	case "srt":
		cues, err = parseSRT(string(data))
	case "vtt":
		cues, err = parseWebVTT(string(data))
	}
	if err != nil {
		return subtitles.Document{}, err
	}
	doc.Status = "available"
	doc.Tracks = []subtitles.Track{{Language: language, Label: chosen.Label, Automatic: chosen.Automatic, Cues: cues}}
	return doc, nil
}

func youtubeCaptionFailure(warning string) error {
	text := strings.ToLower(warning)
	switch {
	case strings.Contains(text, "http error 429"), strings.Contains(text, "too many requests"), strings.Contains(text, "confirm you're not a bot"), strings.Contains(text, "confirm you’re not a bot"):
		return subtitles.ErrRestricted
	case strings.Contains(text, "sign in"), strings.Contains(text, "login required"), strings.Contains(text, "cookies are no longer valid"), strings.Contains(text, "cookies have been rotated"):
		return subtitles.ErrLogin
	default:
		return subtitles.ErrUnavailable
	}
}

func nativeYouTubeCaptionURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" || (u.Hostname() != "www.youtube.com" && u.Hostname() != "youtube.com") || u.Path != "/api/timedtext" {
		return false
	}
	query, err := url.ParseQuery(u.RawQuery)
	return err == nil && query.Get("tlang") == ""
}

func supportedYouTubeCaptionFormat(extension string) bool {
	return extension == "json3" || extension == "srt" || extension == "vtt"
}

func youtubeLanguages(raw []byte, id string) ([]subtitles.Language, error) {
	var data youtubeCaptionMetadata
	if json.Unmarshal(raw, &data) != nil || data.ID != id || data.Subtitles == nil && data.Automatic == nil {
		return nil, subtitles.ErrFormat
	}
	if len(data.Subtitles) > 200 || len(data.Automatic) > 200 {
		return nil, subtitles.ErrFormat
	}
	choices := make([]subtitles.Language, 0)
	seen := map[string]bool{}
	for index, tracks := range []map[string][]youtubeCaption{data.Subtitles, data.Automatic} {
		native := map[string]bool{}
		translated := map[string]bool{}
		for lang, versions := range tracks {
			if len(versions) > 2000 {
				return nil, subtitles.ErrFormat
			}
			for _, version := range versions {
				if !supportedYouTubeCaptionFormat(version.Ext) {
					continue
				}
				u, err := url.Parse(version.URL)
				// yt-dlp also reports HLS caption manifests alongside timedtext.
				// They are not downloadable by this reader; do not reject usable tracks.
				if err == nil && u.Scheme == "https" && u.User == nil && u.Port() == "" && u.Hostname() == "manifest.googlevideo.com" && strings.HasPrefix(u.Path, "/api/manifest/hls_timedtext_playlist/") {
					continue
				}
				if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" || (u.Hostname() != "www.youtube.com" && u.Hostname() != "youtube.com") || u.Path != "/api/timedtext" {
					return nil, subtitles.ErrFormat
				}
				query, err := url.ParseQuery(u.RawQuery)
				if err != nil {
					return nil, subtitles.ErrFormat
				}
				if query.Get("tlang") == "" {
					native[lang] = true
				} else {
					translated[lang] = true
				}
			}
		}
		for lang := range tracks {
			if lang == "live_chat" || seen[lang] {
				continue
			}
			if !languagePattern.MatchString(lang) {
				return nil, subtitles.ErrFormat
			}
			if !native[lang] {
				if index == 0 && translated[lang] {
					return nil, subtitles.ErrFormat
				}
				continue
			}
			baseLang := strings.TrimSuffix(lang, "-orig")
			// Mixed base lists can make yt-dlp select a translated variant. Its
			// original alias is the unambiguous download key when available.
			preferOriginal := index == 1 && native[baseLang+"-orig"] && translated[baseLang]
			if preferOriginal && lang == baseLang {
				continue
			}
			// The extractor may expose a native auto track twice (en / en-orig).
			if index == 1 && strings.HasSuffix(lang, "-orig") {
				if (!preferOriginal && native[baseLang]) || seen[baseLang] {
					continue
				}
			}
			choices = append(choices, subtitles.Language{Language: lang, Label: languageLabel(baseLang), Automatic: index == 1})
			seen[lang] = true
		}
	}
	if len(choices) > 200 {
		return nil, subtitles.ErrFormat
	}
	sort.Slice(choices, func(i, j int) bool { return choices[i].Language < choices[j].Language })
	return choices, nil
}

// Prefer Chinese, then English, then other native tracks; manual captions win
// within a language. This does not request translated or generated AI text.
func preferredYouTubeLanguage(choices []subtitles.Language) string {
	if len(choices) == 0 {
		return ""
	}
	score := func(track subtitles.Language) int {
		return subtitlePriority(subtitles.Track{Language: strings.TrimSuffix(track.Language, "-orig"), Automatic: track.Automatic})
	}
	selected := choices[0]
	for _, candidate := range choices[1:] {
		if score(candidate) < score(selected) {
			selected = candidate
		}
	}
	return selected.Language
}

func parseJSON3(raw []byte) ([]subtitles.Cue, error) {
	if len(raw) > 2<<20 {
		return nil, subtitles.ErrFormat
	}
	var data struct {
		Events []struct {
			Start    *int64 `json:"tStartMs"`
			Duration *int64 `json:"dDurationMs"`
			Segments []struct {
				Text string `json:"utf8"`
			} `json:"segs"`
		} `json:"events"`
	}
	if json.Unmarshal(raw, &data) != nil || data.Events == nil || len(data.Events) > 20000 {
		return nil, subtitles.ErrFormat
	}
	cues := make([]subtitles.Cue, 0)
	total := 0
	for _, event := range data.Events {
		var text strings.Builder
		for _, segment := range event.Segments {
			text.WriteString(segment.Text)
		}
		caption := strings.TrimSpace(html.UnescapeString(text.String()))
		if caption == "" {
			continue
		}
		if event.Start == nil || event.Duration == nil || *event.Start < 0 || *event.Start > 7*24*3600000 || *event.Duration <= 0 || *event.Duration > 7*24*3600000-*event.Start || len(caption) > 8000 || len(cues) >= 10000 {
			return nil, subtitles.ErrFormat
		}
		total += len(caption)
		if total > 2<<20 {
			return nil, subtitles.ErrFormat
		}
		cues = append(cues, subtitles.Cue{Start: float64(*event.Start) / 1000, End: float64(*event.Start+*event.Duration) / 1000, Text: caption})
	}
	if len(cues) == 0 {
		return nil, subtitles.ErrFormat
	}
	sort.SliceStable(cues, func(i, j int) bool { return cues[i].Start < cues[j].Start })
	return cues, nil
}
