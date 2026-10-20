package tools

import (
	"context"
	"encoding/json"
	"html"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"tangentry/internal/discovery"
	"tangentry/internal/subtitles"
)

// ReadSubtitles requests existing platform captions only. --simulate prevents media and caption file writes.
// Bilibili's yt-dlp extractor provides inline SRT, so Tangentry does not fetch arbitrary caption URLs.
func (t *Toolkit) ReadSubtitles(ctx context.Context, bvid string) (subtitles.Document, error) {
	kind, id, err := discovery.ParseQuery(bvid)
	if err != nil || kind != "video" {
		return subtitles.Document{}, subtitles.ErrInvalid
	}
	if !t.ytdlpReady() {
		return subtitles.Document{}, subtitles.ErrTools
	}
	ctx, cancel := context.WithTimeout(ctx, 28*time.Second)
	defer cancel()
	args := []string{"--ignore-config", "--no-playlist", "--no-cache-dir", "--simulate", "--skip-download", "--write-subs", "--write-auto-subs", "--sub-langs", "all,-danmaku", "--dump-single-json", "--socket-timeout", "10", "--retries", "0"}
	cookieArgs, clean, err := t.cookieArguments("https://www.bilibili.com/video/" + id + "/")
	if err != nil {
		return subtitles.Document{}, err
	}
	defer clean()
	args = append(args, cookieArgs...)
	args = append(args, "--", "https://www.bilibili.com/video/"+id+"/")
	cmd, err := t.ytdlpCommand(ctx, args...)
	if err != nil {
		return subtitles.Document{}, subtitles.ErrTools
	}
	stdout, stderr := &metadataBuffer{}, &boundedBuffer{}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return subtitles.Document{}, ctx.Err()
		}
		return subtitles.Document{}, subtitles.ErrUnavailable
	}
	if stdout.exceeded {
		return subtitles.Document{}, subtitles.ErrFormat
	}
	if len(stderr.String()) >= 16384 {
		return subtitles.Document{}, subtitles.ErrUnavailable // Truncated warnings cannot prove a complete retrieval.
	}
	return parseSubtitles(stdout.Bytes(), stderr.String(), id)
}

type captionPayload struct {
	Ext  string `json:"ext"`
	Data string `json:"data"`
}

var languagePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,48}$`)

func parseSubtitles(raw []byte, warnings, id string) (subtitles.Document, error) {
	var metadata struct {
		ID        string                      `json:"id"`
		Subtitles map[string][]captionPayload `json:"subtitles"`
		Automatic map[string][]captionPayload `json:"automatic_captions"`
	}
	if json.Unmarshal(raw, &metadata) != nil || (metadata.ID != id && metadata.ID != id+"_p1") || (metadata.Subtitles == nil && metadata.Automatic == nil) {
		return subtitles.Document{}, subtitles.ErrFormat
	}
	doc := subtitles.Document{VideoID: id, Part: 1, Status: "none", Tracks: []subtitles.Track{}}
	seen := map[string]bool{}
	for index, tracks := range []map[string][]captionPayload{metadata.Subtitles, metadata.Automatic} {
		if len(tracks) > 32 {
			return subtitles.Document{}, subtitles.ErrFormat
		}
		for language, versions := range tracks {
			if language == "danmaku" || language == "live_chat" || seen[language] {
				continue
			}
			if !languagePattern.MatchString(language) {
				return subtitles.Document{}, subtitles.ErrFormat
			}
			var srt string
			for _, version := range versions {
				if version.Ext == "srt" && version.Data != "" {
					srt = version.Data
					break
				}
			}
			// A returned caption track that cannot be read is not evidence of no captions.
			if srt == "" {
				return subtitles.Document{}, subtitles.ErrFormat
			}
			cues, err := parseSRT(srt)
			if err != nil {
				return subtitles.Document{}, err
			}
			if len(cues) == 0 {
				return subtitles.Document{}, subtitles.ErrFormat
			}
			doc.Tracks = append(doc.Tracks, subtitles.Track{Language: language, Label: languageLabel(language), Automatic: index == 1 || strings.HasPrefix(language, "ai-"), Cues: cues})
			seen[language] = true
		}
	}
	// yt-dlp's extractor reports need_login_subtitle as a warning instead of a fatal error.
	// Any other warning is treated as an incomplete retrieval, even if partial tracks exist.
	loginRequired := false
	for _, line := range strings.Split(warnings, "\n") {
		if !strings.Contains(line, "WARNING:") {
			continue
		}
		if strings.Contains(line, "Subtitles are only available when logged in") {
			loginRequired = true
		} else {
			return subtitles.Document{}, subtitles.ErrUnavailable
		}
	}
	sort.Slice(doc.Tracks, func(i, j int) bool {
		a, b := subtitlePriority(doc.Tracks[i]), subtitlePriority(doc.Tracks[j])
		if a == b {
			return doc.Tracks[i].Language < doc.Tracks[j].Language
		}
		return a < b
	})
	if len(doc.Tracks) > 0 {
		doc.Status = "available"
	} else if loginRequired {
		doc.Status = "login_required"
	}
	return doc, nil
}

func subtitlePriority(track subtitles.Track) int {
	rawLanguage := strings.ReplaceAll(strings.ToLower(track.Language), "_", "-")
	lang := strings.TrimPrefix(rawLanguage, "ai-")
	if isSimplifiedChineseLanguage(lang) {
		if track.Automatic {
			return 1
		}
		return 0
	}
	if strings.HasPrefix(rawLanguage, "ai-") && (lang == "zh" || lang == "cmn" || lang == "zho" || lang == "chi") && track.Automatic {
		return 1
	}
	if strings.HasPrefix(lang, "zh-") || lang == "zh" {
		return 2
	}
	if lang == "en" || strings.HasPrefix(lang, "en-") {
		if track.Automatic {
			return 4
		}
		return 3
	}
	return 5
}

func languageLabel(language string) string {
	language = strings.ToLower(strings.ReplaceAll(strings.TrimPrefix(language, "ai-"), "_", "-"))
	switch language {
	case "zh-hans", "zh-cn", "zh-sg":
		return "简体中文"
	case "zh", "cmn", "zho", "chi":
		return "简体中文"
	case "zh-hant", "zh-tw", "zh-hk", "zh-mo":
		return "繁体中文"
	case "en", "en-us", "en-gb":
		return "英语"
	case "ja":
		return "日语"
	default:
		return language
	}
}

func isSimplifiedChineseLanguage(language string) bool {
	return language == "zh-hans" || strings.HasPrefix(language, "zh-hans-") || language == "zh-cn" || strings.HasPrefix(language, "zh-cn-") || language == "zh-sg" || strings.HasPrefix(language, "zh-sg-")
}

var timecodePattern = regexp.MustCompile(`^(\d{2,6}):([0-5]\d):([0-5]\d)[,.](\d{3})$`)
var formatTagPattern = regexp.MustCompile(`(?i)</?(?:b|i|u|font)(?:\s+[^>]*)?>`)
var blankLinePattern = regexp.MustCompile(`\n[\t ]*\n+`)

func parseSRT(raw string) ([]subtitles.Cue, error) {
	if len(raw) > 2<<20 {
		return nil, subtitles.ErrFormat
	}
	text := strings.TrimSpace(strings.TrimPrefix(subtitleNewlines(raw), "\ufeff"))
	if text == "" {
		return nil, subtitles.ErrFormat
	}
	cues := make([]subtitles.Cue, 0)
	for _, block := range blankLinePattern.Split(text, -1) {
		lines := strings.Split(block, "\n")
		if len(lines) > 1 && !strings.Contains(lines[0], "-->") {
			if _, err := strconv.ParseUint(strings.TrimSpace(lines[0]), 10, 32); err != nil {
				return nil, subtitles.ErrFormat
			}
			lines = lines[1:]
		}
		if len(lines) < 2 {
			return nil, subtitles.ErrFormat
		}
		timestamps := strings.Split(lines[0], "-->")
		if len(timestamps) != 2 {
			return nil, subtitles.ErrFormat
		}
		start, startOK := parseTimecode(strings.TrimSpace(timestamps[0]))
		end, endOK := parseTimecode(strings.TrimSpace(timestamps[1]))
		caption := strings.TrimSpace(html.UnescapeString(formatTagPattern.ReplaceAllString(strings.Join(lines[1:], "\n"), "")))
		if !startOK || !endOK || end <= start || end > 7*24*3600 || caption == "" || len(caption) > 8000 || len(cues) >= 10000 {
			return nil, subtitles.ErrFormat
		}
		cues = append(cues, subtitles.Cue{Start: start, End: end, Text: caption})
	}
	sort.SliceStable(cues, func(i, j int) bool { return cues[i].Start < cues[j].Start })
	return cues, nil
}

func parseTimecode(raw string) (float64, bool) {
	parts := timecodePattern.FindStringSubmatch(raw)
	if parts == nil {
		return 0, false
	}
	var values [4]int
	for i := range values {
		values[i], _ = strconv.Atoi(parts[i+1])
	}
	return float64(values[0]*3600+values[1]*60+values[2]) + float64(values[3])/1000, true
}
