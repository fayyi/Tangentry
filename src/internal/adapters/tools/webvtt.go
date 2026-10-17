package tools

import (
	"html"
	"regexp"
	"sort"
	"strings"

	"tangentry/internal/subtitles"
)

var vttTimePattern = regexp.MustCompile(`^(?:\d{2,6}:)?[0-5]\d:[0-5]\d\.\d{3}$`)
var vttTagPattern = regexp.MustCompile(`</?(?:c(?:\.[\w-]+)*|v|lang|b|i|u|ruby|rt)(?:[ \t]+[^>\n]*)?>|<(?:\d{2,6}:)?[0-5]\d:[0-5]\d\.\d{3}>`)

// VTT cue settings and style blocks affect video overlays, not transcript text.
func parseWebVTT(raw string) ([]subtitles.Cue, error) {
	if len(raw) > 2<<20 {
		return nil, subtitles.ErrFormat
	}
	text := strings.TrimSpace(strings.TrimPrefix(subtitleNewlines(raw), "\ufeff"))
	blocks := blankLinePattern.Split(text, -1)
	header := strings.SplitN(blocks[0], "\n", 2)[0]
	if header != "WEBVTT" && !strings.HasPrefix(header, "WEBVTT ") && !strings.HasPrefix(header, "WEBVTT\t") {
		return nil, subtitles.ErrFormat
	}
	cues := make([]subtitles.Cue, 0)
	for _, block := range blocks[1:] {
		lines := strings.Split(block, "\n")
		kind := strings.TrimRight(lines[0], " \t")
		if kind == "NOTE" || strings.HasPrefix(kind, "NOTE ") || strings.HasPrefix(kind, "NOTE\t") || kind == "STYLE" || kind == "REGION" {
			continue
		}
		if !strings.Contains(lines[0], "-->") {
			lines = lines[1:] // Optional cue identifier.
		}
		if len(lines) < 2 {
			return nil, subtitles.ErrFormat
		}
		times := strings.Split(lines[0], "-->")
		if len(times) != 2 {
			return nil, subtitles.ErrFormat
		}
		endFields := strings.Fields(times[1])
		if len(endFields) == 0 {
			return nil, subtitles.ErrFormat
		}
		start, startOK := parseVTTTime(strings.TrimSpace(times[0]))
		end, endOK := parseVTTTime(endFields[0])
		caption := strings.TrimSpace(html.UnescapeString(vttTagPattern.ReplaceAllString(strings.Join(lines[1:], "\n"), "")))
		if !startOK || !endOK || end <= start || end > 7*24*3600 || caption == "" || len(caption) > 8000 || len(cues) >= 10000 {
			return nil, subtitles.ErrFormat
		}
		cues = append(cues, subtitles.Cue{Start: start, End: end, Text: caption})
	}
	if len(cues) == 0 {
		return nil, subtitles.ErrFormat
	}
	sort.SliceStable(cues, func(i, j int) bool { return cues[i].Start < cues[j].Start })
	return cues, nil
}

func subtitleNewlines(raw string) string {
	return strings.ReplaceAll(strings.ReplaceAll(raw, "\r\n", "\n"), "\r", "\n")
}

func parseVTTTime(raw string) (float64, bool) {
	if !vttTimePattern.MatchString(raw) {
		return 0, false
	}
	if strings.Count(raw, ":") == 1 {
		raw = "00:" + raw
	}
	return parseTimecode(raw)
}
