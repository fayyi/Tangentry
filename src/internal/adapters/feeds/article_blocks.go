package feeds

import (
	"bytes"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf16"

	"golang.org/x/net/html"
	"tangentry/internal/adapters/htmlimage"
	"tangentry/internal/content"
)

// ParseArticleBlocks keeps document structure without exposing raw HTML.
// Image URLs are represented as data and fetched through the local image proxy.
func ParseArticleBlocks(raw, base string) []content.ArticleBlock {
	root, err := html.Parse(bytes.NewReader([]byte(raw)))
	if err != nil {
		return nil
	}
	blocks := make([]content.ArticleBlock, 0, 32)
	imageCount := 0
	group := 0
	appendImage := func(node *html.Node, original string, kind string) {
		if imageCount >= 200 || len(blocks) >= 4000 {
			return
		}
		attrs := htmlimage.Attrs(node)
		rawURL := htmlimage.Source(attrs)
		if original != "" {
			rawURL = original
		} else if responsive := imageSourceSet(attrs["srcset"], base); responsive != "" {
			rawURL = responsive
		}
		if image := htmlimage.Resolve(rawURL, base); image != "" {
			block := content.ArticleBlock{Kind: kind, URL: image, Alt: textLimit(attrs["alt"], 1000), Width: htmlimage.Dimension(attrs["width"]), Height: htmlimage.Dimension(attrs["height"])}
			blocks = append(blocks, block)
			imageCount++
		}
	}
	appendText := func(kind string, level int, text string) {
		text = strings.TrimSpace(textLimit(text, 20000))
		if text != "" && len(blocks) < 4000 {
			blocks = append(blocks, content.ArticleBlock{Kind: kind, Level: level, Text: text})
		}
	}
	classHas := func(node *html.Node, class string) bool {
		for _, attr := range node.Attr {
			if attr.Key == "class" {
				for _, value := range strings.Fields(attr.Val) {
					if value == class {
						return true
					}
				}
			}
		}
		return false
	}
	attr := func(node *html.Node, key string) string {
		for _, value := range node.Attr {
			if value.Key == key {
				return strings.TrimSpace(value.Val)
			}
		}
		return ""
	}
	plain := func(node *html.Node) string {
		var result strings.Builder
		var walk func(*html.Node)
		walk = func(n *html.Node) {
			if n.Type == html.TextNode {
				result.WriteString(n.Data)
				return
			}
			if n.Type == html.ElementNode && (n.Data == "svg" || n.Data == "script" || n.Data == "style") {
				return
			}
			for child := n.FirstChild; child != nil; child = child.NextSibling {
				walk(child)
			}
		}
		walk(node)
		return result.String()
	}
	resolveLink := func(raw string) string {
		if raw == "" {
			return ""
		}
		if strings.HasPrefix(raw, "//") {
			raw = "https:" + raw
		}
		u, err := url.Parse(raw)
		if err != nil {
			return ""
		}
		if !u.IsAbs() {
			parent, err := url.Parse(base)
			if err != nil {
				return ""
			}
			u = parent.ResolveReference(u)
		}
		if (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil {
			return ""
		}
		return u.String()
	}
	var walk func(*html.Node, int, bool)
	var appendList func(string, int, bool, int)
	walk = func(node *html.Node, depth int, inCode bool) {
		if len(blocks) >= 4000 {
			return
		}
		if node.Type == html.TextNode {
			if value := strings.TrimSpace(textLimit(node.Data, 20000)); value != "" {
				appendText("paragraph", 0, value)
			}
			return
		}
		if node.Type != html.ElementNode {
			for child := node.FirstChild; child != nil; child = child.NextSibling {
				walk(child, depth, inCode)
			}
			return
		}
		if node.Data == "head" || node.Data == "title" || node.Data == "meta" || node.Data == "script" || node.Data == "style" || node.Data == "noscript" || node.Data == "iframe" || node.Data == "svg" || node.Data == "form" || node.Data == "button" {
			return
		}
		if node.Data == "a" && classHas(node, "anchor") {
			return
		}
		if node.Data == "img" {
			if inCode {
				return
			}
			kind := "image"
			if classHas(node, "emoji") {
				kind = "emoji"
			}
			appendImage(node, "", kind)
			return
		}
		if node.Data == "a" && classHas(node, "lightbox") {
			var image *html.Node
			var findImage func(*html.Node)
			findImage = func(n *html.Node) {
				if image != nil || n.Type == html.ElementNode && (n.Data == "svg" || classHas(n, "meta")) {
					return
				}
				if n.Type == html.ElementNode && n.Data == "img" {
					image = n
					return
				}
				for child := n.FirstChild; child != nil; child = child.NextSibling {
					findImage(child)
				}
			}
			findImage(node)
			if image != nil {
				appendImage(image, attr(node, "href"), "image")
			}
			return
		}
		if node.Data == "div" && classHas(node, "lightbox-wrapper") {
			for child := node.FirstChild; child != nil; child = child.NextSibling {
				walk(child, depth, inCode)
			}
			return
		}
		if node.Data == "div" && classHas(node, "meta") || node.Data == "svg" {
			return
		}
		if node.Data == "aside" && classHas(node, "onebox") {
			target := ""
			var findLink func(*html.Node)
			findLink = func(n *html.Node) {
				if target != "" {
					return
				}
				if n.Type == html.ElementNode && n.Data == "a" {
					target = attr(n, "href")
					return
				}
				for child := n.FirstChild; child != nil; child = child.NextSibling {
					findLink(child)
				}
			}
			findLink(node)
			target = resolveLink(target)
			label := ""
			var findTitle func(*html.Node)
			findTitle = func(n *html.Node) {
				if label != "" {
					return
				}
				if n.Type == html.ElementNode && n.Data == "a" {
					label = strings.TrimSpace(plain(n))
					return
				}
				for child := n.FirstChild; child != nil; child = child.NextSibling {
					findTitle(child)
				}
			}
			findTitle(node)
			if target != "" {
				if label == "" {
					label = target
				}
				appendText("onebox", 0, textLimit(label, 2000))
				blocks[len(blocks)-1].URL = target
			}
			return
		}
		if node.Data == "details" {
			summary := "更多内容"
			for child := node.FirstChild; child != nil; child = child.NextSibling {
				if child.Type == html.ElementNode && child.Data == "summary" {
					summary = strings.TrimSpace(plain(child))
					break
				}
			}
			text := strings.TrimSpace(plain(node))
			if text != "" {
				if summary != "" && strings.HasPrefix(text, summary) {
					text = strings.TrimSpace(strings.TrimPrefix(text, summary))
				}
				if text != "" {
					blocks = append(blocks, content.ArticleBlock{Kind: "details", Summary: textLimit(summary, 2000), Text: textLimit(text, 20000)})
				}
			}
			return
		}
		if node.Data == "div" && classHas(node, "spoiler") || node.Data == "span" && classHas(node, "spoiler") {
			text := strings.TrimSpace(plain(node))
			if text != "" {
				blocks = append(blocks, content.ArticleBlock{Kind: "spoiler", Summary: "显示剧透内容", Text: textLimit(text, 20000)})
			}
			return
		}
		if node.Data == "aside" && classHas(node, "quote") {
			text := strings.TrimSpace(plain(node))
			if text != "" {
				appendText("quote", 0, text)
			}
			return
		}
		if node.Data == "ul" || node.Data == "ol" {
			ordered := node.Data == "ol"
			group++
			listGroup := group
			for child := node.FirstChild; child != nil; child = child.NextSibling {
				if child.Type == html.ElementNode && child.Data == "li" {
					parseListItem(child, depth, ordered, listGroup, walk, appendList)
				}
			}
			return
		}
		if node.Data == "table" {
			for child := node.FirstChild; child != nil; child = child.NextSibling {
				walk(child, depth, inCode)
			}
			return
		}
		if node.Data == "tr" {
			cells := make([]string, 0, 4)
			for child := node.FirstChild; child != nil; child = child.NextSibling {
				if child.Type == html.ElementNode && (child.Data == "td" || child.Data == "th") {
					cells = append(cells, strings.TrimSpace(plain(child)))
				}
			}
			appendText("table", 0, strings.Join(cells, "\t"))
			return
		}
		if node.Data == "pre" {
			code := strings.TrimSpace(plain(node))
			if code != "" {
				blocks = append(blocks, content.ArticleBlock{Kind: "code", Text: textLimit(code, 20000)})
			}
			return
		}
		kind, level := feedBlockKind(node.Data)
		if node.Data == "blockquote" || kind == "quote" {
			appendText("quote", 0, plain(node))
			return
		}
		if kind == "heading" || node.Data == "p" || node.Data == "figcaption" || node.Data == "dt" || node.Data == "dd" {
			text := strings.Builder{}
			inlineImages := make([]content.ArticleInlineImage, 0)
			var inline func(*html.Node)
			inline = func(n *html.Node) {
				if n.Type == html.TextNode {
					text.WriteString(n.Data)
					return
				}
				if n.Type != html.ElementNode {
					return
				}
				if n.Data == "img" {
					if classHas(n, "emoji") {
						attrs := htmlimage.Attrs(n)
						raw := htmlimage.Source(attrs)
						if responsive := imageSourceSet(attrs["srcset"], base); responsive != "" {
							raw = responsive
						}
						if image := htmlimage.Resolve(raw, base); image != "" && imageCount < 200 {
							inlineImages = append(inlineImages, content.ArticleInlineImage{Offset: len(utf16.Encode([]rune(text.String()))), URL: image, Alt: textLimit(attrs["alt"], 1000), Width: htmlimage.Dimension(attrs["width"]), Height: htmlimage.Dimension(attrs["height"])})
							imageCount++
						}
						return
					}
					before := strings.TrimSpace(text.String())
					if before != "" {
						appendText("paragraph", 0, before)
						text.Reset()
					}
					imageKind := "image"
					if classHas(n, "emoji") {
						imageKind = "emoji"
					}
					appendImage(n, "", imageKind)
					return
				}
				if n.Data == "a" && classHas(n, "lightbox") {
					var image *html.Node
					var findImage func(*html.Node)
					findImage = func(child *html.Node) {
						if image != nil || child.Type == html.ElementNode && (child.Data == "svg" || classHas(child, "meta")) {
							return
						}
						if child.Type == html.ElementNode && child.Data == "img" {
							image = child
							return
						}
						for nested := child.FirstChild; nested != nil; nested = nested.NextSibling {
							findImage(nested)
						}
					}
					findImage(n)
					if image != nil {
						before := strings.TrimSpace(text.String())
						if before != "" {
							appendText("paragraph", 0, before)
							text.Reset()
						}
						appendImage(image, attr(n, "href"), "image")
					}
					return
				}
				if n.Data == "a" && classHas(n, "anchor") || n.Data == "svg" || n.Data == "div" && classHas(n, "meta") {
					return
				}
				if n.Data == "br" {
					text.WriteByte('\n')
					return
				}
				for child := n.FirstChild; child != nil; child = child.NextSibling {
					inline(child)
				}
			}
			for child := node.FirstChild; child != nil; child = child.NextSibling {
				inline(child)
			}
			value := strings.TrimSpace(textLimit(text.String(), 20000))
			if kind == "heading" {
				if value != "" {
					blocks = append(blocks, content.ArticleBlock{Kind: kind, Level: level, Text: value, InlineImages: inlineImages})
				}
			} else {
				if value != "" {
					blocks = append(blocks, content.ArticleBlock{Kind: "paragraph", Text: value, InlineImages: inlineImages})
				} else if len(inlineImages) > 0 {
					for _, image := range inlineImages {
						blocks = append(blocks, content.ArticleBlock{Kind: "emoji", URL: image.URL, Alt: image.Alt, Width: image.Width, Height: image.Height})
					}
				}
			}
			return
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child, depth, inCode || node.Data == "code")
		}
	}
	appendList = func(text string, depth int, ordered bool, listGroup int) {
		text = strings.TrimSpace(textLimit(text, 20000))
		if text != "" && len(blocks) < 4000 {
			blocks = append(blocks, content.ArticleBlock{Kind: "list", Level: depth, Group: listGroup, Ordered: ordered, Text: text})
		}
	}
	walk(root, 0, false)
	if len(blocks) > 4000 {
		blocks = blocks[:4000]
	}
	return blocks
}

func parseListItem(node *html.Node, depth int, ordered bool, group int, walk func(*html.Node, int, bool), appendList func(string, int, bool, int)) {
	var text strings.Builder
	var nested []*html.Node
	var collect func(*html.Node)
	collect = func(n *html.Node) {
		if n.Type == html.TextNode {
			text.WriteString(n.Data)
			return
		}
		if n.Type != html.ElementNode {
			return
		}
		if n.Data == "ul" || n.Data == "ol" {
			nested = append(nested, n)
			return
		}
		if n.Data == "p" || n.Data == "div" {
			if text.Len() > 0 {
				text.WriteByte(' ')
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			collect(child)
		}
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		collect(child)
	}
	appendList(text.String(), depth, ordered, group)
	for _, list := range nested {
		walk(list, depth+1, false)
	}
}

func parseArticleBlocks(raw, base string) []content.ArticleBlock {
	return ParseArticleBlocks(raw, base)
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
	case "blockquote", "aside":
		return "quote", 0
	case "pre":
		return "code", 0
	case "li":
		return "list", 0
	default:
		return "", 0
	}
}

func responsiveImage(raw string, base string) string {
	return htmlimage.Resolve(raw, base)
}

func imageSourceSet(raw string, base string) string {
	bestURL := ""
	bestWidth := 0
	for _, part := range strings.Split(raw, ",") {
		fields := strings.Fields(strings.TrimSpace(part))
		if len(fields) == 0 {
			continue
		}
		width := 0
		if len(fields) > 1 && strings.HasSuffix(fields[1], "w") {
			width, _ = strconv.Atoi(strings.TrimSuffix(fields[1], "w"))
		} else if len(fields) > 1 && strings.HasSuffix(fields[1], "x") {
			density, _ := strconv.ParseFloat(strings.TrimSuffix(fields[1], "x"), 64)
			width = int(density * 10000)
		}
		if width > bestWidth {
			if resolved := responsiveImage(fields[0], base); resolved != "" {
				bestURL, bestWidth = resolved, width
			}
		}
	}
	return bestURL
}
