package subscriptions

import (
	"bytes"
	"context"
	"encoding/xml"
	"io"
	"strings"
)

func validateOPML(ctx context.Context, raw []byte) error {
	decoder := xml.NewDecoder(bytes.NewReader(raw))
	depth, roots, sources := 0, 0, 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		token, err := decoder.Token()
		if err == io.EOF {
			if depth != 0 || roots != 1 {
				return ErrInvalid
			}
			return nil
		}
		if err != nil {
			return ErrInvalid
		}
		switch value := token.(type) {
		case xml.StartElement:
			if depth == 0 {
				roots++
				if roots != 1 || value.Name.Local != "opml" || value.Name.Space != "" {
					return ErrInvalid
				}
			}
			depth++
			if depth > maxOPMLDepth {
				return ErrInvalid
			}
			if value.Name.Local == "outline" {
				for _, attribute := range value.Attr {
					if (attribute.Name.Local == "xmlUrl" || attribute.Name.Local == "htmlUrl") && strings.TrimSpace(attribute.Value) != "" {
						sources++
						break
					}
				}
				if sources > maxOPMLOutlines {
					return ErrInvalid
				}
			}
		case xml.EndElement:
			depth--
		case xml.CharData:
			if depth == 0 && strings.TrimSpace(string(value)) != "" {
				return ErrInvalid
			}
		case xml.Directive:
			return ErrInvalid
		}
	}
}
