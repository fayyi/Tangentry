package articles

import (
	"bytes"
	"encoding/xml"
	"io"
	"mime"
	"net/http"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"
)

func articleReader(data []byte, contentType string) (io.Reader, error) {
	_, parameters, _ := mime.ParseMediaType(contentType)
	_, _, certain := charset.DetermineEncoding(data, contentType)
	if parameters["charset"] == "" && !certain && bytes.HasPrefix(bytes.TrimSpace(data), []byte("<?xml")) {
		label := ""
		decoder := xml.NewDecoder(io.LimitReader(bytes.NewReader(data), 4096))
		decoder.CharsetReader = func(name string, input io.Reader) (io.Reader, error) {
			label = name
			return input, nil
		}
		if token, err := decoder.Token(); err == nil {
			if declaration, ok := token.(xml.ProcInst); ok && declaration.Target == "xml" && label != "" {
				return charset.NewReaderLabel(label, bytes.NewReader(data))
			}
		}
	}
	return charset.NewReader(bytes.NewReader(data), contentType)
}

// Headerless XHTML starts with an XML declaration, so MIME sniffing alone
// reports text/xml. Only an actual html root qualifies, never a feed or SVG.
func isWebDocument(data []byte) bool {
	if http.DetectContentType(data) == "text/html; charset=utf-8" {
		return true
	}
	tokens := html.NewTokenizer(bytes.NewReader(data))
	for count := 0; count < 32; count++ {
		switch tokens.Next() {
		case html.ErrorToken:
			return false
		case html.TextToken:
			if strings.TrimSpace(strings.TrimPrefix(string(tokens.Text()), "\ufeff")) != "" {
				return false
			}
		case html.StartTagToken, html.SelfClosingTagToken:
			name, _ := tokens.TagName()
			return string(name) == "html"
		case html.CommentToken, html.DoctypeToken:
			// XML declarations are bogus comments in the HTML tokenizer.
		default:
			return false
		}
	}
	return false
}
