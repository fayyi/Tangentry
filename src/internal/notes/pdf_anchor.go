package notes

import (
	"bytes"
	"encoding/json"
	"io"
	"math"
	"unicode/utf8"
)

// PDFRect uses normalized PDF page coordinates, with its origin at the bottom
// left of the unrotated crop box. Browser zoom and rotation are not persisted.
type PDFRect struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

type PDFAnchor struct {
	DocumentID string    `json:"documentId"`
	Page       int       `json:"page"`
	Quote      string    `json:"quote"`
	Rects      []PDFRect `json:"rects"`
}

func validatePDFAnchor(anchor *PDFAnchor) error {
	if anchor == nil {
		return nil
	}
	if !sourceHashPattern.MatchString(anchor.DocumentID) || anchor.Page < 1 || anchor.Page > 100000 ||
		!utf8.ValidString(anchor.Quote) || utf8.RuneCountInString(anchor.Quote) == 0 || utf8.RuneCountInString(anchor.Quote) > 2000 ||
		len(anchor.Rects) == 0 || len(anchor.Rects) > 200 {
		return ErrInvalid
	}
	for _, rect := range anchor.Rects {
		for _, value := range []float64{rect.X, rect.Y, rect.Width, rect.Height} {
			if math.IsNaN(value) || math.IsInf(value, 0) {
				return ErrInvalid
			}
		}
		if rect.X < 0 || rect.Y < 0 || rect.Width <= 0 || rect.Height <= 0 || rect.X+rect.Width > 1.000000001 || rect.Y+rect.Height > 1.000000001 {
			return ErrInvalid
		}
	}
	return nil
}

func decodePDFAnchor(raw []byte) (*PDFAnchor, error) {
	var anchor *PDFAnchor
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&anchor) != nil || d.Decode(new(any)) != io.EOF {
		return nil, ErrInvalid
	}
	if err := validatePDFAnchor(anchor); err != nil {
		return nil, err
	}
	return anchor, nil
}
