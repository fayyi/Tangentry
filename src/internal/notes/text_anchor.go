package notes

import (
	"bytes"
	"encoding/json"
	"io"
	"regexp"
	"unicode/utf16"
	"unicode/utf8"
)

// TextAnchor addresses the displayed article using browser UTF-16 offsets and
// exact surrounding text. It does not assume a feed summary is the full article.
type TextAnchor struct {
	Quote      string `json:"quote"`
	Start      int    `json:"start"`
	End        int    `json:"end"`
	Prefix     string `json:"prefix"`
	Suffix     string `json:"suffix"`
	SourceHash string `json:"sourceHash"`
}

var sourceHashPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

func validateAnchor(anchor *TextAnchor) error {
	if anchor == nil {
		return nil
	}
	if !utf8.ValidString(anchor.Quote) || !utf8.ValidString(anchor.Prefix) || !utf8.ValidString(anchor.Suffix) ||
		utf8.RuneCountInString(anchor.Quote) == 0 || utf8.RuneCountInString(anchor.Quote) > 2000 ||
		utf8.RuneCountInString(anchor.Prefix) > 64 || utf8.RuneCountInString(anchor.Suffix) > 64 ||
		anchor.Start < 0 || anchor.End > 2_000_000 || anchor.End <= anchor.Start ||
		anchor.End-anchor.Start != len(utf16.Encode([]rune(anchor.Quote))) || !sourceHashPattern.MatchString(anchor.SourceHash) {
		return ErrInvalid
	}
	return nil
}

func decodeAnchor(raw []byte) (*TextAnchor, error) {
	var anchor *TextAnchor
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&anchor) != nil || decoder.Decode(new(any)) != io.EOF {
		return nil, ErrInvalid
	}
	if err := validateAnchor(anchor); err != nil {
		return nil, err
	}
	return anchor, nil
}
