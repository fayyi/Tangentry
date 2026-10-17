package feeds

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/html"
	"tangentry/internal/netpolicy"
)

// Hash returns the stable identity hash used for feed items.
func Hash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

// Text strips markup and bounds feed metadata before it enters the content model.
func Text(raw string) string {
	return textLimit(raw, 20000)
}

func textLimit(raw string, limit int) string {
	z := html.NewTokenizer(strings.NewReader(raw))
	var out strings.Builder
	skip := 0
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			break
		}
		if tt == html.StartTagToken || tt == html.SelfClosingTagToken {
			t := z.Token()
			if tt == html.StartTagToken && (t.Data == "script" || t.Data == "style") {
				skip++
			}
			if skip == 0 && textBoundary(t.Data) {
				out.WriteString("\n")
			}
		}
		if tt == html.EndTagToken {
			t := z.Token()
			if (t.Data == "script" || t.Data == "style") && skip > 0 {
				skip--
			}
			if skip == 0 && textBoundary(t.Data) {
				out.WriteString("\n")
			}
			if skip == 0 && (t.Data == "td" || t.Data == "th") {
				out.WriteString("\t")
			}
		}
		if tt == html.TextToken && skip == 0 {
			out.WriteString(string(z.Text()))
		}
		if out.Len() > limit {
			break
		}
	}
	s := strings.TrimSpace(out.String())
	if len(s) > limit {
		end := limit
		for end > 0 && !utf8.RuneStart(s[end]) {
			end--
		}
		s = s[:end]
	}
	return s
}

func textBoundary(tag string) bool {
	switch tag {
	case "p", "br", "li", "div", "section", "article", "blockquote", "pre", "h1", "h2", "h3", "h4", "h5", "h6", "tr", "hr":
		return true
	}
	return false
}

func link(raw string) string {
	value, err := netpolicy.URL(raw)
	if err != nil {
		return ""
	}
	return value
}

func htmlResponse(data []byte) bool {
	tokens := html.NewTokenizer(strings.NewReader(string(data)))
	for {
		switch tokens.Next() {
		case html.ErrorToken:
			return false
		case html.StartTagToken, html.SelfClosingTagToken:
			name, _ := tokens.TagName()
			return string(name) == "html"
		}
	}
}
