package notes

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"unicode/utf8"
)

// SummaryAnchor attaches a note to a stable node in the folded summary tree.
// It is separate from source anchors so regenerated summaries can resolve the
// note without changing the original article/video/PDF location.
type SummaryAnchor struct {
	NodeID      string   `json:"nodeId"`
	UnitID      string   `json:"unitId"`
	Level       int      `json:"level"`
	SentenceIDs []string `json:"sentenceIds,omitempty"`
	Quote       string   `json:"quote"`
	Shown       string   `json:"shown"`
}

func validSummaryID(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && utf8.ValidString(value) && utf8.RuneCountInString(value) <= 200 && !strings.ContainsAny(value, "\r\n\x00")
}

func validateSummaryAnchor(anchor *SummaryAnchor) error {
	if anchor == nil {
		return nil
	}
	if !validSummaryID(anchor.NodeID) || !validSummaryID(anchor.UnitID) || anchor.Level < 0 || anchor.Level > 4 ||
		!utf8.ValidString(anchor.Quote) || !utf8.ValidString(anchor.Shown) || utf8.RuneCountInString(anchor.Quote) > 2000 || utf8.RuneCountInString(anchor.Shown) > 2000 ||
		strings.TrimSpace(anchor.Quote) == "" || strings.TrimSpace(anchor.Shown) == "" || len(anchor.SentenceIDs) > 20 {
		return ErrInvalid
	}
	seen := make(map[string]bool, len(anchor.SentenceIDs))
	for _, id := range anchor.SentenceIDs {
		if !validSummaryID(id) || seen[id] {
			return ErrInvalid
		}
		seen[id] = true
	}
	return nil
}

// ValidateSummaryAnchor is exported for persistence adapters that decode an
// anchor from storage before returning it to the API.
func ValidateSummaryAnchor(anchor *SummaryAnchor) error { return validateSummaryAnchor(anchor) }

func decodeSummaryAnchor(raw []byte) (*SummaryAnchor, error) {
	var anchor *SummaryAnchor
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&anchor) != nil || decoder.Decode(new(any)) != io.EOF {
		return nil, ErrInvalid
	}
	if err := validateSummaryAnchor(anchor); err != nil {
		return nil, err
	}
	return anchor, nil
}
