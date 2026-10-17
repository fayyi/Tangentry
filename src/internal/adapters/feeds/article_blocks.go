package feeds

import (
	"bytes"
	"strings"

	"golang.org/x/net/html"
	"tangentry/internal/adapters/htmlimage"
	"tangentry/internal/content"
)

// parseArticleBlocks keeps feed document structure without exposing raw HTML.
// Image URLs are represented as data and fetched through the local image proxy.
func parseArticleBlocks(raw, base string) []content.ArticleBlock {
	root, err := html.Parse(bytes.NewReader([]byte(raw)))
	if err != nil {
		return nil
	}
	blocks := make([]content.ArticleBlock, 0, 32)
	imageCount := 0
	appendImage := func(node *html.Node) {
		if imageCount >= 200 || len(blocks) >= 4000 {
			return
		}
		attrs := htmlimage.Attrs(node)
		if image := htmlimage.Resolve(htmlimage.Source(attrs), base); image != "" {
			blocks = append(blocks, content.ArticleBlock{Kind: "image", URL: image, Alt: textLimit(attrs["alt"], 1000), Width: htmlimage.Dimension(attrs["width"]), Height: htmlimage.Dimension(attrs["height"])})
			imageCount++
		}
	}
	var walk func(*html.Node, string)
	walk = func(node *html.Node, inherited string) {
		if node.Type != html.ElementNode {
			if node.Type == html.TextNode {
				value := strings.TrimSpace(textLimit(node.Data, 20000))
				if value != "" {
					blocks = append(blocks, content.ArticleBlock{Kind: "paragraph", Text: value})
				}
				return
			}
			for child := node.FirstChild; child != nil; child = child.NextSibling {
				walk(child, inherited)
			}
			return
		}
		if node.Data == "head" || node.Data == "title" || node.Data == "meta" || node.Data == "script" || node.Data == "style" || node.Data == "noscript" || node.Data == "iframe" || node.Data == "svg" || node.Data == "form" {
			return
		}
		if node.Data == "img" {
			appendImage(node)
			return
		}
		kind, heading := feedBlockKind(node.Data)
		if inherited != "" && (kind == "paragraph" || kind == "") {
			kind = inherited
		}
		if kind == "" {
			for child := node.FirstChild; child != nil; child = child.NextSibling {
				walk(child, inherited)
			}
			return
		}
		var text strings.Builder
		flush := func() {
			value := strings.TrimSpace(textLimit(text.String(), 20000))
			text.Reset()
			if value != "" {
				blocks = append(blocks, content.ArticleBlock{Kind: kind, Level: heading, Text: value})
			}
		}
		var collect func(*html.Node)
		collect = func(child *html.Node) {
			if child.Type == html.TextNode {
				text.WriteString(child.Data)
				return
			}
			if child.Type == html.ElementNode && child.Data == "br" {
				text.WriteByte('\n')
				return
			}
			if child.Type == html.ElementNode && child.Data == "img" {
				if htmlimage.URL(child, base) != "" {
					flush()
					appendImage(child)
				}
				return
			}
			for nested := child.FirstChild; nested != nil; nested = nested.NextSibling {
				collect(nested)
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			collect(child)
		}
		flush()
	}
	walk(root, "")
	if len(blocks) > 4000 {
		blocks = blocks[:4000]
	}
	return blocks
}

func feedBlockKind(tag string) (string, int) {
	switch tag {
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
	default:
		return "", 0
	}
}
