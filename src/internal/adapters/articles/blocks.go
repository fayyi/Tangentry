package articles

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/net/html"
	"tangentry/internal/adapters/htmlimage"
	"tangentry/internal/articles"
)

func ignored(n *html.Node) bool {
	if n.Type != html.ElementNode {
		return false
	}
	switch n.Data {
	case "script", "style", "noscript", "iframe", "object", "embed", "svg", "math", "form", "input", "button", "select", "textarea", "img", "video", "audio", "canvas", "template":
		return true
	}
	for _, a := range n.Attr {
		if a.Key == "hidden" || a.Key == "aria-hidden" && a.Val == "true" {
			return true
		}
	}
	return false
}

func limit(text string, n int) string {
	text = clean(text, false)
	if len(text) <= n {
		return text
	}
	for n > 0 && !utf8.RuneStart(text[n]) {
		n--
	}
	return text[:n]
}

func clean(text string, code bool) string {
	text = strings.Map(func(r rune) rune {
		if r == 0 || unicode.IsControl(r) && r != '\n' && r != '\t' && r != '\r' {
			return -1
		}
		return r
	}, text)
	if code {
		return strings.Trim(strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n"), "\n")
	}
	return strings.Join(strings.Fields(text), " ")
}

func blockKind(n *html.Node) (string, int) {
	if n.Type != html.ElementNode {
		return "", 0
	}
	switch n.Data {
	case "p", "figcaption", "dt", "dd", "td", "th":
		return "paragraph", 0
	case "h1", "h2":
		return "heading", 2
	case "h3":
		return "heading", 3
	case "h4", "h5", "h6":
		return "heading", 4
	case "blockquote":
		return "quote", 0
	case "pre":
		return "code", 0
	case "li":
		return "list", 0
	}
	return "", 0
}

// blocks flattens readability's output into reading blocks. Images become
// link-only blocks in document order; the reader loads them through the proxy.
func blocks(root *html.Node, base string) []articles.Block {
	result := make([]articles.Block, 0)
	images := 0
	var walk func(*html.Node, string, int)
	walk = func(node *html.Node, inherited string, level int) {
		if ignored(node) {
			return
		}
		kind, heading := blockKind(node)
		if kind == "paragraph" && (inherited == "quote" || inherited == "list") {
			kind, heading = inherited, level
		}
		if kind == "" {
			kind, heading = inherited, level
		}
		if kind == "" {
			kind = "paragraph"
		}
		var pending strings.Builder
		flush := func() {
			text := clean(pending.String(), kind == "code")
			pending.Reset()
			if strings.TrimSpace(text) != "" {
				result = append(result, articles.Block{Kind: kind, Level: heading, Text: text})
			}
		}
		var inline func(*html.Node)
		inline = func(child *html.Node) {
			if child.Type == html.ElementNode && child.Data == "img" && kind != "code" {
				attrs := htmlimage.Attrs(child)
				if image := htmlimage.Resolve(htmlimage.Source(attrs), base); image != "" && images < 200 {
					flush()
					images++
					result = append(result, articles.Block{Kind: "image", URL: image, Alt: limit(attrs["alt"], 1000), Width: htmlimage.Dimension(attrs["width"]), Height: htmlimage.Dimension(attrs["height"])})
				}
				return
			}
			if ignored(child) {
				return
			}
			if child.Type == html.TextNode {
				pending.WriteString(child.Data)
				return
			}
			if child.Type == html.ElementNode && child.Data == "br" {
				pending.WriteString("\n")
				return
			}
			k, _ := blockKind(child)
			if kind != "code" && (k != "" || child.Type == html.ElementNode && (child.Data == "div" || child.Data == "section" || child.Data == "article" || child.Data == "ul" || child.Data == "ol" || child.Data == "table" || child.Data == "tr")) {
				flush()
				walk(child, kind, heading)
				return
			}
			for c := child.FirstChild; c != nil; c = c.NextSibling {
				inline(c)
			}
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			inline(c)
		}
		flush()
	}
	walk(root, "", 0)
	return result
}
