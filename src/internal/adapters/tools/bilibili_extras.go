package tools

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"tangentry/internal/discovery"
	"tangentry/internal/downloads"
)

const maxDanmakuXML = 64 << 20
const maxDanmakuEntries = 200000

type bilibiliInfo struct {
	ID            string   `json:"id"`
	Title         string   `json:"title"`
	Description   string   `json:"description"`
	Uploader      string   `json:"uploader"`
	UploadDate    string   `json:"upload_date"`
	PlaylistIndex int      `json:"playlist_index"`
	Tags          []string `json:"tags"`
}

var episodeNumber = regexp.MustCompile(`S01E([0-9]{1,4})`)

// generateBilibiliNFO creates Kodi/Jellyfin sidecars next to a Bilibili video.
// yt-dlp's info JSON remains available as the original metadata artifact.
func generateBilibiliNFO(directory, rawURL string, videos []string) error {
	platform, bvid, _, err := discovery.VideoURL(rawURL)
	if err != nil || platform != "bilibili" {
		return nil
	}
	type entry struct {
		path string
		info bilibiliInfo
	}
	entries := make([]entry, 0, len(videos))
	for _, video := range videos {
		video, err := downloads.NormalizeArtifactPath(video)
		if err != nil {
			return err
		}
		infoPath, err := safeGeneratedPath(directory, strings.TrimSuffix(video, filepath.Ext(video))+".info.json")
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		stat, err := os.Lstat(infoPath)
		if err != nil || stat.Size() > 8<<20 {
			continue
		}
		data, err := os.ReadFile(infoPath)
		if err != nil {
			continue
		}
		var info bilibiliInfo
		if json.Unmarshal(data, &info) != nil || strings.TrimSpace(info.Title) == "" {
			continue
		}
		if info.PlaylistIndex <= 0 {
			if match := episodeNumber.FindStringSubmatch(video); len(match) == 2 {
				info.PlaylistIndex, _ = strconv.Atoi(match[1])
			}
		}
		entries = append(entries, entry{video, info})
	}
	if len(entries) == 0 {
		return nil
	}
	multi := len(entries) > 1 || strings.HasPrefix(filepath.ToSlash(entries[0].path), "Season 1/")
	if multi {
		first := entries[0].info
		show := struct {
			XMLName xml.Name `xml:"tvshow"`
			Plot    string   `xml:"plot"`
			Outline string   `xml:"outline"`
			Title   string   `xml:"title"`
			Actor   []struct {
				Name string `xml:"name"`
				Role string `xml:"role"`
			} `xml:"actor,omitempty"`
			Year     string   `xml:"year,omitempty"`
			Genre    []string `xml:"genre,omitempty"`
			UniqueID struct {
				Type  string `xml:"type,attr"`
				Value string `xml:",chardata"`
			} `xml:"uniqueid"`
			Premiered string `xml:"premiered,omitempty"`
		}{Plot: nfoPlot(bvid, first.Description), Title: first.Title, Actor: nfoActors(first.Uploader), Year: nfoYear(first.UploadDate), Genre: first.Tags, Premiered: nfoDate(first.UploadDate)}
		show.UniqueID.Type, show.UniqueID.Value = "bilibili", bvid
		if err := writeNFO(directory, "tvshow.nfo", show); err != nil {
			return err
		}
		for _, item := range entries {
			index := item.info.PlaylistIndex
			if index == 0 {
				index = 1
			}
			doc := struct {
				XMLName xml.Name `xml:"episodedetails"`
				Plot    string   `xml:"plot"`
				Outline string   `xml:"outline"`
				Title   string   `xml:"title"`
				Season  int      `xml:"season"`
				Episode int      `xml:"episode"`
			}{Title: item.info.Title, Season: 1, Episode: index}
			name := strings.TrimSuffix(item.path, filepath.Ext(item.path)) + ".nfo"
			if err := writeNFO(directory, name, doc); err != nil {
				return err
			}
		}
		return nil
	}
	item := entries[0]
	doc := struct {
		XMLName xml.Name `xml:"movie"`
		Plot    string   `xml:"plot"`
		Outline string   `xml:"outline"`
		Title   string   `xml:"title"`
		Actor   []struct {
			Name string `xml:"name"`
			Role string `xml:"role"`
		} `xml:"actor,omitempty"`
		Year     string   `xml:"year,omitempty"`
		Genre    []string `xml:"genre,omitempty"`
		UniqueID struct {
			Type  string `xml:"type,attr"`
			Value string `xml:",chardata"`
		} `xml:"uniqueid"`
		Premiered string `xml:"premiered,omitempty"`
	}{Plot: nfoPlot(bvid, item.info.Description), Title: item.info.Title, Actor: nfoActors(item.info.Uploader), Year: nfoYear(item.info.UploadDate), Genre: item.info.Tags, Premiered: nfoDate(item.info.UploadDate)}
	doc.UniqueID.Type, doc.UniqueID.Value = "bilibili", bvid
	name := strings.TrimSuffix(item.path, filepath.Ext(item.path)) + ".nfo"
	return writeNFO(directory, name, doc)
}

func nfoPlot(bvid, intro string) string {
	return fmt.Sprintf(`原始视频：<a href="https://www.bilibili.com/video/%s/">%s</a><br/><br/>%s`, bvid, bvid, intro)
}

func nfoActors(name string) []struct {
	Name string `xml:"name"`
	Role string `xml:"role"`
} {
	if strings.TrimSpace(name) == "" {
		return nil
	}
	return []struct {
		Name string `xml:"name"`
		Role string `xml:"role"`
	}{{Name: name, Role: name}}
}

func nfoDate(raw string) string {
	if len(raw) != 8 {
		return ""
	}
	for _, r := range raw {
		if r < '0' || r > '9' {
			return ""
		}
	}
	return raw[:4] + "-" + raw[4:6] + "-" + raw[6:]
}
func nfoYear(raw string) string {
	if date := nfoDate(raw); date != "" {
		return date[:4]
	}
	return ""
}
func writeNFO(directory, relative string, value any) error {
	data, err := xml.MarshalIndent(value, "", "    ")
	if err != nil {
		return err
	}
	data = append([]byte(xml.Header), data...)
	data = append(data, '\n')
	path, err := safeGeneratedPath(directory, relative)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

type danmakuEntry struct {
	Params string `xml:"p,attr"`
	Text   string `xml:",chardata"`
}
type danmakuDocument struct {
	Entries []danmakuEntry `xml:"d"`
}

func convertDanmakuXML(directory string) ([]string, error) {
	var generated []string
	err := filepath.WalkDir(directory, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".xml") {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Size() <= 0 || info.Size() > maxDanmakuXML {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		ass, err := danmakuASS(data, strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name())))
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(directory, path)
		if err != nil {
			return err
		}
		name := strings.TrimSuffix(filepath.ToSlash(relative), filepath.Ext(relative)) + ".ass"
		destination, err := safeGeneratedPath(directory, name)
		if err != nil {
			return err
		}
		if err := os.WriteFile(destination, ass, 0600); err != nil {
			return err
		}
		generated = append(generated, name)
		return nil
	})
	return generated, err
}

type assEvent struct {
	start, end int64
	text       string
	color      uint32
	mode       int
}

func danmakuASS(data []byte, title string) ([]byte, error) {
	if len(data) > maxDanmakuXML {
		return nil, fmt.Errorf("弹幕 XML 超出转换限制。")
	}
	var doc danmakuDocument
	decoder := xml.NewDecoder(bytes.NewReader(data))
	decoder.Strict = true
	if err := decoder.Decode(&doc); err != nil {
		return nil, fmt.Errorf("弹幕 XML 无法解析。")
	}
	if len(doc.Entries) > maxDanmakuEntries {
		return nil, fmt.Errorf("弹幕数量超出转换限制。")
	}
	events := make([]assEvent, 0, len(doc.Entries))
	for _, entry := range doc.Entries {
		p := strings.Split(entry.Params, ",")
		if len(p) < 5 || !utf8.ValidString(entry.Text) {
			continue
		}
		seconds, e1 := strconv.ParseFloat(p[0], 64)
		mode, e2 := strconv.Atoi(p[1])
		color, e3 := strconv.ParseUint(p[3], 10, 32)
		if e1 != nil || e2 != nil || e3 != nil || math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds < 0 || seconds > 86400 || strings.TrimSpace(entry.Text) == "" {
			continue
		}
		duration := int64(8000)
		if mode == 4 || mode == 5 {
			duration = 5000
		}
		events = append(events, assEvent{int64(seconds * 1000), int64(seconds*1000) + duration, escapeASSText(entry.Text), uint32(color), mode})
	}
	slices.SortFunc(events, func(a, b assEvent) int { return cmpInt64(a.start, b.start) })
	var out strings.Builder
	title = strings.NewReplacer("\n", " ", "\r", " ").Replace(title)
	fmt.Fprintf(&out, "[Script Info]\n; Generated from Bilibili danmaku XML\nTitle: %s\nScriptType: v4.00+\nPlayResX: 1920\nPlayResY: 1080\nWrapStyle: 2\nScaledBorderAndShadow: yes\n\n[V4+ Styles]\nFormat: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding\nStyle: Float,Arial,36,&H00FFFFFF,&H000000FF,&H80000000,&H00000000,0,0,0,0,100,100,0,0,1,2,0,7,0,0,0,1\nStyle: Top,Arial,36,&H00FFFFFF,&H000000FF,&H80000000,&H00000000,0,0,0,0,100,100,0,0,1,2,0,8,0,0,0,1\nStyle: Bottom,Arial,36,&H00FFFFFF,&H000000FF,&H80000000,&H00000000,0,0,0,0,100,100,0,0,1,2,0,2,0,0,0,1\n\n[Events]\nFormat: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text\n", title)
	lanes := make([]int64, 0, 27)
	for _, e := range events {
		style := "Float"
		color := fmt.Sprintf(`\c&H%02X%02X%02X&`, e.color&0xff, e.color>>8&0xff, e.color>>16&0xff)
		effect := "{" + color + "}"
		if e.mode == 4 {
			style = "Bottom"
		} else if e.mode == 5 {
			style = "Top"
		} else {
			lane := 0
			for lane < len(lanes) && lanes[lane] > e.start {
				lane++
			}
			if lane == len(lanes) {
				if lane >= 27 {
					continue
				}
				lanes = append(lanes, e.end)
			} else {
				lanes[lane] = e.end
			}
			y := 1080 - (lane+1)*40
			width := min(max(utf8.RuneCountInString(e.text)*24, 80), 1920)
			effect = fmt.Sprintf(`{\move(1940,%d,%d,%d)%s}`, y, -width, y, color)
		}
		fmt.Fprintf(&out, "Dialogue: 2,%s,%s,%s,,0,0,0,,%s%s\n", assTime(e.start), assTime(e.end), style, effect, e.text)
	}
	return []byte(out.String()), nil
}

func escapeASSText(raw string) string {
	raw = strings.TrimSpace(strings.NewReplacer("\r", " ", "\n", " ").Replace(raw))
	raw = strings.ReplaceAll(raw, `\`, `\\`)
	raw = strings.ReplaceAll(raw, "{", `\{`)
	return strings.ReplaceAll(raw, "}", `\}`)
}
func assTime(ms int64) string {
	cs := ms / 10
	return fmt.Sprintf("%d:%02d:%02d.%02d", cs/360000, cs/6000%60, cs/100%60, cs%100)
}
func cmpInt64(a, b int64) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}
