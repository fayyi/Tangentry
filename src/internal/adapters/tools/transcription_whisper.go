package tools

import (
	_ "embed"
	"encoding/json"
	"strings"

	"github.com/longbridgeapp/opencc"
	"tangentry/internal/subtitles"
	"tangentry/internal/transcription"
)

//go:embed faster_whisper.py
var fasterScript []byte

//go:embed qwen_asr.py
var qwenScript []byte

func ParseWhisper(data []byte, limit int) (subtitles.Track, error) {
	return parseWhisper(data, limit, "auto")
}

func parseWhisper(data []byte, limit int, language string) (subtitles.Track, error) {
	var value struct {
		Result struct {
			Language string `json:"language"`
		} `json:"result"`
		Segments []struct {
			Offsets struct {
				From int64 `json:"from"`
				To   int64 `json:"to"`
			} `json:"offsets"`
			Text string `json:"text"`
		} `json:"transcription"`
	}
	if json.Unmarshal(data, &value) != nil || len(value.Segments) == 0 || len(value.Segments) > 10000 {
		return subtitles.Track{}, subtitles.ErrFormat
	}
	label := "本机语音转写"
	if limit > 0 {
		label = "前 60 秒 · 本机语音转写"
	}
	track := subtitles.Track{Language: "whisper", Label: label, Automatic: true, Cues: []subtitles.Cue{}}
	var converter *opencc.OpenCC
	if language == "zh" || (language == "auto" && value.Result.Language == "zh") {
		var err error
		converter, err = opencc.New("t2s")
		if err != nil {
			return subtitles.Track{}, transcription.ErrUnavailable
		}
		track.Label = "简体中文 · " + label
	}
	var previous int64 = -1
	for _, segment := range value.Segments {
		text := strings.TrimSpace(segment.Text)
		if text == "" {
			continue
		}
		if converter != nil {
			var err error
			text, err = converter.Convert(text)
			if err != nil {
				return subtitles.Track{}, transcription.ErrUnavailable
			}
		}
		if segment.Offsets.From < 0 || segment.Offsets.From < previous || segment.Offsets.To <= segment.Offsets.From || segment.Offsets.To > 6*3600*1000 || (limit > 0 && segment.Offsets.To > int64(limit*1000)) {
			return subtitles.Track{}, subtitles.ErrFormat
		}
		track.Cues = append(track.Cues, subtitles.Cue{Start: float64(segment.Offsets.From) / 1000, End: float64(segment.Offsets.To) / 1000, Text: text})
		previous = segment.Offsets.From
	}
	if len(track.Cues) == 0 {
		return subtitles.Track{}, subtitles.ErrFormat
	}
	return track, nil
}
