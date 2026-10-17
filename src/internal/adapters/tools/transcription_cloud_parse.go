package tools

import (
	"encoding/json"
	"math"
	"regexp"
	"strings"

	"tangentry/internal/subtitles"
	"tangentry/internal/transcription"
)

var senseVoiceMarker = regexp.MustCompile(`<\|[a-zA-Z0-9_]+\|>`)

// Text-only recognition gets the real chunk window, never invented word/phrase timestamps.
func parseSiliconflowSpeech(data []byte, language string, duration float64) ([]subtitles.Cue, error) {
	var result struct {
		Text *string `json:"text"`
	}
	if json.Unmarshal(data, &result) != nil || result.Text == nil {
		return nil, subtitles.ErrFormat
	}
	text := strings.TrimSpace(senseVoiceMarker.ReplaceAllString(*result.Text, ""))
	if language == "auto" && strings.Contains(*result.Text, "<|zh|>") {
		language = "zh"
	}
	if text == "" {
		return []subtitles.Cue{}, nil
	}
	if len(text) > 8000 || duration <= 0 {
		return nil, subtitles.ErrFormat
	}
	raw, err := json.Marshal(map[string]any{"language": "", "segments": []map[string]any{{"start": 0, "end": duration, "text": text}}})
	if err != nil {
		return nil, err
	}
	return parseCloudSpeech(raw, language, duration)
}

func parseCloudSpeech(data []byte, language string, duration float64) ([]subtitles.Cue, error) {
	if math.IsNaN(duration) || math.IsInf(duration, 0) || duration <= 0 || duration > 21600.5 {
		return nil, subtitles.ErrFormat
	}
	type timedSegment struct {
		Start *float64 `json:"start"`
		End   *float64 `json:"end"`
		Text  string   `json:"text"`
	}
	var result struct {
		Language string  `json:"language"`
		VTT      string  `json:"vtt"`
		Text     *string `json:"text"`
		Info     struct {
			Language string `json:"language"`
		} `json:"transcription_info"`
		Segments []timedSegment `json:"segments"`
	}
	if json.Unmarshal(data, &result) != nil || len(result.Segments) > 10000 {
		return nil, subtitles.ErrFormat
	}
	if len(result.Segments) == 0 {
		if result.VTT == "" {
			if result.Text != nil && strings.TrimSpace(*result.Text) == "" {
				return []subtitles.Cue{}, nil
			}
			return nil, transcription.Failed("no_timestamps", "云端未返回带时间戳的字幕片段，未保存不完整结果。")
		}
		cues, err := parseWebVTT(result.VTT)
		if err != nil {
			return nil, err
		}
		for _, cue := range cues {
			result.Segments = append(result.Segments, timedSegment{Start: &cue.Start, End: &cue.End, Text: cue.Text})
		}
	}
	detected := strings.ToLower(result.Language)
	if detected == "" {
		detected = strings.ToLower(result.Info.Language)
	}
	if detected == "chinese" {
		detected = "zh"
	}
	// Reuse local recognition's timing validation and Chinese normalization.
	type offsets struct {
		From int64 `json:"from"`
		To   int64 `json:"to"`
	}
	type segment struct {
		Offsets offsets `json:"offsets"`
		Text    string  `json:"text"`
	}
	converted := struct {
		Result   map[string]string `json:"result"`
		Segments []segment         `json:"transcription"`
	}{Result: map[string]string{"language": detected}}
	var previous float64 = -1
	for _, value := range result.Segments {
		if value.Start == nil || value.End == nil {
			return nil, subtitles.ErrFormat
		}
		start, end := *value.Start, *value.End
		if math.IsNaN(start) || math.IsNaN(end) || math.IsInf(start, 0) || math.IsInf(end, 0) || start < 0 || start < previous || end <= start || end > duration+2 {
			return nil, subtitles.ErrFormat
		}
		previous = start
		if start >= duration {
			continue
		}
		end = math.Min(end, duration)
		converted.Segments = append(converted.Segments, segment{offsets{int64(math.Round(start * 1000)), int64(math.Round(end * 1000))}, value.Text})
	}
	raw, err := json.Marshal(converted)
	if err != nil {
		return nil, err
	}
	track, err := parseWhisper(raw, 0, language)
	if err == nil && !validCachedSpeech(track.Cues, duration) {
		return nil, subtitles.ErrFormat
	}
	return track.Cues, err
}
