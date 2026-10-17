package ai

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

// translateBatch keeps provider requests separate from the durable paragraph
// result. Long article paragraphs are split into reusable text-keyed parts.
func (s *Service) translateBatch(ctx context.Context, c Config, in Input, segments []Segment) ([]Segment, error) {
	if in.Source != "article" || len(segments) != 1 || len(segments[0].Text) <= articleBatchBytes {
		return s.translateRequest(ctx, c, segments)
	}
	segment := segments[0]
	remaining := segment.Text
	var translated []string
	total := 0
	for remaining != "" {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		end := len(remaining)
		if end > articleBatchBytes {
			end = articleBatchBytes
			for end > 0 && !utf8.RuneStart(remaining[end]) {
				end--
			}
			// Prefer a sentence/word boundary without creating tiny requests.
			if boundary := strings.LastIndexAny(remaining[:end], " \n\t.!?。！？"); boundary >= end/2 {
				_, width := utf8.DecodeRuneInString(remaining[boundary:])
				end = boundary + width
			}
		}
		part := remaining[:end]
		remaining = remaining[end:]
		if strings.TrimSpace(part) == "" {
			continue
		}
		// A part is stored like a one-text translation, so it can be reused by
		// another article containing the same text.
		chunk := Result{Key: textsKey([]string{part}), ContentID: in.ContentID, Language: in.Track.Language, BaseURL: c.BaseURL, Model: c.Model, Total: 1, Texts: []string{}}
		text := ""
		cached, err := s.read(ctx, kindTranslation, chunk.Key)
		if err == nil && !in.Regenerate && len(cached.Texts) == 1 {
			text = cached.Texts[0]
		} else if err != nil && !errors.Is(err, ErrNotFound) {
			return nil, err
		} else {
			output, err := s.translateRequest(ctx, c, []Segment{{Index: segment.Index, Text: part}})
			if err != nil {
				return nil, err
			}
			if len(output) != 1 || output[0].Index != segment.Index || !utf8.ValidString(output[0].Text) || strings.TrimSpace(output[0].Text) == "" || len(output[0].Text) > 24000 {
				return nil, ErrFormat
			}
			text = strings.TrimSpace(output[0].Text)
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			chunk.Texts, chunk.UpdatedAt = []string{text}, time.Now().Unix()
			if err := s.write(ctx, kindTranslation, chunk); err != nil {
				return nil, err
			}
		}
		total += len(text) + 1
		if total > 24000 {
			return nil, ErrFormat
		}
		translated = append(translated, text)
	}
	return []Segment{{Index: segment.Index, Text: strings.Join(translated, "\n")}}, nil
}

func (s *Service) translateRequest(ctx context.Context, c Config, segments []Segment) ([]Segment, error) {
	requestCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	return s.provider.Translate(requestCtx, c, segments)
}
