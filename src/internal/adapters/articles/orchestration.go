// Package articles adapts maintained readability infrastructure to plain reading blocks.
package articles

import (
	"bytes"
	"context"
	"net/http"
	"net/url"

	readability "codeberg.org/readeck/go-readability/v2"
	"golang.org/x/net/html"
	"tangentry/internal/articles"
)

const maxHTML = 16 << 20

// Extractor turns a remote HTML document into the application's reading model.
// The HTTP client is cloned per request so redirect and cookie policy remain local.
type Extractor struct{ HTTP *http.Client }

// Fetch coordinates transport, decoding, readability extraction and block shaping.
// Each stage keeps its own resource and security limits in the file that owns it.
func (e *Extractor) Fetch(ctx context.Context, raw string) (articles.Document, error) {
	page, err := e.fetch(ctx, raw)
	if err != nil {
		return articles.Document{}, err
	}
	if err := ctx.Err(); err != nil {
		return articles.Document{}, err
	}

	base, err := url.Parse(page.finalURL)
	if err != nil {
		return articles.Document{}, articles.ErrUnavailable
	}
	parser := readability.NewParser()
	parser.MaxElemsToParse = 40_000
	input, err := html.Parse(bytes.NewReader(page.decoded))
	if err != nil {
		return articles.Document{}, articles.ErrUnavailable
	}
	if err := protectCode(ctx, input); err != nil {
		return articles.Document{}, err
	}
	article, err := parser.ParseDocument(input, base)
	if err != nil {
		return articles.Document{}, failure("未能提取原站正文，可继续阅读订阅文字或打开原内容。")
	}
	if err := ctx.Err(); err != nil {
		return articles.Document{}, err
	}
	var output bytes.Buffer
	if err := article.RenderHTML(&output); err != nil || output.Len() > maxHTML {
		return articles.Document{}, articles.ErrUnavailable
	}
	node, err := html.Parse(&output)
	if err != nil {
		return articles.Document{}, articles.ErrUnavailable
	}
	doc := articles.Document{
		SourceURL: page.finalURL,
		Title:     clean(article.Title(), false),
		Byline:    clean(article.Byline(), false),
		Blocks:    blocks(node, page.finalURL),
	}
	if len(doc.Blocks) > 0 && doc.Blocks[0].Kind == "heading" && doc.Blocks[0].Text == doc.Title {
		doc.Blocks = doc.Blocks[1:]
	}
	if err := articles.Prepare(&doc); err != nil {
		return articles.Document{}, failure("原站没有可提取的正文，可打开原内容查看。")
	}
	return doc, nil
}

type fetchedPage struct {
	decoded  []byte
	finalURL string
}

func failure(message string) error { return &articles.FetchError{Message: message} }
