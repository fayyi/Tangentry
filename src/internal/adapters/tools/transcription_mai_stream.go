package tools

import (
	"bufio"
	"encoding/json"
	"io"
	"math"
	"sort"
	"strings"

	"tangentry/internal/transcription"
)

// findMAIString and findMAIValue isolate the tolerant envelope traversal used
// by both tRPC responses and server-sent events.
func findMAIString(data []byte, key, parent string) string {
	var root any
	if json.Unmarshal(data, &root) != nil {
		return ""
	}
	var walk func(any, bool) string
	walk = func(value any, eligible bool) string {
		switch node := value.(type) {
		case map[string]any:
			_, hasParentMarker := node[parent]
			if eligible || hasParentMarker {
				if id, ok := node[key].(string); ok && id != "" {
					return id
				}
			}
			for name, child := range node {
				if found := walk(child, name == parent); found != "" {
					return found
				}
			}
		case []any:
			for _, child := range node {
				if found := walk(child, eligible); found != "" {
					return found
				}
			}
		}
		return ""
	}
	return walk(root, false)
}

func parseMAIStream(reader io.Reader) ([]byte, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), 8<<20)
	var transcript json.RawMessage
	var fallback json.RawMessage
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		var event any
		if json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))), &event) == nil {
			if rich := findMAIValue(event, "rich_transcription"); len(rich) > 0 {
				transcript = rich
			}
			if segments := findMAIValue(event, "transcription_segments"); len(segments) > 0 {
				fallback = segments
			}
		}
	}
	if scanner.Err() != nil {
		return nil, transcription.Failed("cloud_response", "Microsoft Playground 转录流中断，未保存不完整字幕。")
	}
	var rich struct {
		Utterances []struct {
			Start *float64 `json:"start"`
			End   *float64 `json:"end"`
			Text  string   `json:"text"`
			Words []struct {
				Start *float64 `json:"start"`
				End   *float64 `json:"end"`
				Text  string   `json:"text"`
				Word  string   `json:"word"`
			} `json:"words"`
		} `json:"utterances"`
	}
	if len(transcript) == 0 && len(fallback) > 0 {
		transcript = append(append([]byte(`{"utterances":`), fallback...), '}')
	}
	if json.Unmarshal(transcript, &rich) != nil || len(rich.Utterances) == 0 || len(rich.Utterances) > 10000 {
		return nil, transcription.Failed("no_timestamps", "Microsoft Playground 未返回带时间戳的文稿。")
	}
	segments := make([]maiSegment, 0, len(rich.Utterances))
	for _, utterance := range rich.Utterances {
		if strings.TrimSpace(utterance.Text) == "" {
			continue
		}
		if utterance.Start == nil || utterance.End == nil || *utterance.Start < 0 || *utterance.End <= *utterance.Start || math.IsNaN(*utterance.Start) || math.IsNaN(*utterance.End) || math.IsInf(*utterance.Start, 0) || math.IsInf(*utterance.End, 0) {
			return nil, transcription.Failed("cloud_response", "Microsoft Playground 返回了无效的字幕时间。")
		}
		words := make([]maiWord, 0, len(utterance.Words))
		for _, word := range utterance.Words {
			text := word.Text
			if text == "" {
				text = word.Word
			}
			if word.Start == nil || word.End == nil || strings.TrimSpace(text) == "" {
				words = nil
				break
			}
			words = append(words, maiWord{start: *word.Start, end: *word.End, text: text})
		}
		if cues, ok := groupMAIWords(words, utterance.Text, *utterance.Start, *utterance.End); ok {
			segments = append(segments, cues...)
		} else {
			segments = append(segments, maiSegment{Start: *utterance.Start, End: *utterance.End, Text: strings.TrimSpace(utterance.Text)})
		}
	}
	if len(segments) == 0 {
		return nil, transcription.Failed("no_speech", "Microsoft Playground 没有识别到人声，请先试听原音频或更换模型。")
	}
	// Speaker turns can overlap or arrive grouped by speaker. Preserve their
	// real times, ordering only the display; never manufacture timing.
	sort.SliceStable(segments, func(i, j int) bool { return segments[i].Start < segments[j].Start })
	return json.Marshal(map[string]any{"segments": segments})
}

type maiWord struct {
	start float64
	end   float64
	text  string
}

type maiSegment struct {
	Start float64 `json:"start"`
	End   float64 `json:"end"`
	Text  string  `json:"text"`
}

func groupMAIWords(words []maiWord, utterance string, start, end float64) ([]maiSegment, bool) {
	if len(words) == 0 {
		return nil, false
	}
	joined := strings.Join(wordTexts(words), "")
	if strings.Join(strings.Fields(joined), " ") != strings.Join(strings.Fields(utterance), " ") {
		return nil, false
	}
	result := make([]maiSegment, 0, len(words))
	part := maiSegment{Start: words[0].start, End: words[0].end}
	var previous float64 = start
	for _, word := range words {
		if math.IsNaN(word.start) || math.IsNaN(word.end) || math.IsInf(word.start, 0) || math.IsInf(word.end, 0) || word.start < previous || word.start < start || word.end <= word.start || word.end > end {
			return nil, false
		}
		if part.Text != "" && (word.start-part.End >= .8 || word.end-part.Start > 8 || len([]rune(part.Text+word.text)) > 48) {
			result = append(result, part)
			part = maiSegment{Start: word.start, End: word.end}
		}
		if part.Text == "" {
			part.Start = word.start
		}
		part.Text += word.text
		part.End = word.end
		previous = word.start
		if strings.HasSuffix(strings.TrimSpace(part.Text), "。") || strings.HasSuffix(strings.TrimSpace(part.Text), "！") || strings.HasSuffix(strings.TrimSpace(part.Text), "？") || strings.HasSuffix(strings.TrimSpace(part.Text), ".") || strings.HasSuffix(strings.TrimSpace(part.Text), "!") || strings.HasSuffix(strings.TrimSpace(part.Text), "?") {
			result = append(result, part)
			part = maiSegment{}
		}
	}
	if part.Text != "" {
		result = append(result, part)
	}
	return result, len(result) > 0
}

func wordTexts(words []maiWord) []string {
	texts := make([]string, len(words))
	for i, word := range words {
		texts[i] = word.text
	}
	return texts
}

func findMAIValue(value any, key string) json.RawMessage {
	switch node := value.(type) {
	case map[string]any:
		if found, ok := node[key]; ok {
			data, _ := json.Marshal(found)
			return data
		}
		for _, child := range node {
			if found := findMAIValue(child, key); len(found) > 0 {
				return found
			}
		}
	case []any:
		for _, child := range node {
			if found := findMAIValue(child, key); len(found) > 0 {
				return found
			}
		}
	}
	return nil
}
