package notes

import (
	"bytes"
	"encoding/json"
	"io"
	"math"
	"strings"
	"unicode/utf8"
)

// A reference is a snapshot of the selected subtitle, independent of the note's
// main time and of later subtitle corrections or source availability.
type SubtitleReference struct {
	Start     float64 `json:"start"`
	End       float64 `json:"end"`
	Text      string  `json:"text"`
	Language  string  `json:"language"`
	TrackHash string  `json:"trackHash,omitempty"`
}

func ValidateSubtitleReferences(references []SubtitleReference) error {
	if len(references) > 20 {
		return ErrInvalid
	}
	total := 0
	seen := map[SubtitleReference]bool{}
	for _, reference := range references {
		length := utf8.RuneCountInString(reference.Text)
		if !utf8.ValidString(reference.Text) || strings.TrimSpace(reference.Text) == "" || length > 2000 ||
			!utf8.ValidString(reference.Language) || strings.TrimSpace(reference.Language) == "" || utf8.RuneCountInString(reference.Language) > 64 ||
			reference.TrackHash != "" && !sourceHashPattern.MatchString(reference.TrackHash) ||
			math.IsNaN(reference.Start) || math.IsInf(reference.Start, 0) || math.IsNaN(reference.End) || math.IsInf(reference.End, 0) ||
			reference.Start < 0 || reference.End <= reference.Start || reference.End > 7*24*3600 || seen[reference] {
			return ErrInvalid
		}
		seen[reference] = true
		total += length
	}
	if total > 8000 {
		return ErrInvalid
	}
	return nil
}

func decodeSubtitleReferences(raw []byte) ([]SubtitleReference, error) {
	var references []SubtitleReference
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&references) != nil || decoder.Decode(new(any)) != io.EOF {
		return nil, ErrInvalid
	}
	return references, ValidateSubtitleReferences(references)
}
