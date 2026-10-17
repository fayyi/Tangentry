package articles

import (
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"golang.org/x/net/html"
	"golang.org/x/text/encoding/simplifiedchinese"
	"tangentry/internal/articles"
)

func articleHTML() string {
	return `<html><head><title>阅读与批注</title></head><body><nav>无关导航</nav><article><h1>阅读与批注</h1><p>` + strings.Repeat("正文😀的第一段，可以记录自己的想法。", 45) + `</p><h2>走进正文</h2><p>第二段的 <strong>加粗文字</strong> 与 <a href="javascript:alert(1)">链接文字</a>。<img src="http://127.0.0.1/secret" onerror="alert(1)"></p><blockquote><p>引用中的想法。</p></blockquote><ul><li>第一个要点</li><li>第二个要点</li></ul><pre>one
<span class="comment">// retain this code comment</span>
two</pre><p>` + strings.Repeat("结束这篇文章，保留结构和引用。", 30) + `</p><script>危险脚本</script><iframe src="http://127.0.0.1/private"></iframe></article></body></html>`
}
func TestExtractionUsesAnonymousBoundedHTMLAndSafeBlocks(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
			t.Error("credentials leaked")
		}
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "/story", 302)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		io.WriteString(w, articleHTML())
	}))
	defer server.Close()
	jar, _ := cookiejar.New(nil)
	u, _ := url.Parse("http://article-fixture.example")
	jar.SetCookies(u, []*http.Cookie{{Name: "secret", Value: "private"}})
	client := articleTestClient(server)
	client.Jar = jar
	doc, err := (&Extractor{HTTP: client}).Fetch(context.Background(), "http://article-fixture.example"+"/redirect")
	if err != nil {
		t.Fatal(err)
	}
	if requests != 2 || doc.SourceURL != "http://article-fixture.example"+"/story" || len(doc.Blocks) < 6 || doc.SourceHash == "" {
		t.Fatal(doc, requests)
	}
	if strings.Contains(doc.Text, "危险脚本") || strings.Contains(doc.Text, "127.0.0.1") || strings.Contains(doc.Text, "无关导航") {
		t.Fatal(doc.Text)
	}
	if !strings.Contains(doc.Text, "链接文字") || !strings.Contains(doc.Text, "😀") {
		t.Fatal(doc.Text)
	}
	types := map[string]bool{}
	parts := []string{}
	for _, b := range doc.Blocks {
		types[b.Kind] = true
		if b.Kind != "image" {
			parts = append(parts, b.Text)
		}
	}
	if !types["heading"] || !types["code"] || !types["list"] || doc.Text != strings.Join(parts, "\n\n") {
		t.Fatal(doc)
	}
	if !strings.Contains(doc.Text, "// retain this code comment") {
		t.Fatal("syntax highlighted code comment was lost")
	}
	if client.Jar != jar {
		t.Fatal("modified shared client")
	}
}
func TestExtractionRejectsInvalidResponsesAndCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/pdf":
			w.Header().Set("Content-Type", "application/pdf")
			io.WriteString(w, "%PDF-1")
		case "/large":
			w.Header().Set("Content-Type", "text/html")
			io.WriteString(w, strings.Repeat("x", maxHTML+1))
		case "/private":
			w.WriteHeader(403)
		case "/empty":
			w.Header().Set("Content-Type", "text/html")
			io.WriteString(w, "<html><body></body></html>")
		case "/redirect":
			http.Redirect(w, r, "file:///tmp/private", 302)
		case "/wait":
			<-r.Context().Done()
		}
	}))
	defer server.Close()
	e := &Extractor{HTTP: articleTestClient(server)}
	for _, path := range []string{"/pdf", "/large", "/private", "/empty", "/redirect"} {
		t.Run(path, func(t *testing.T) {
			if _, err := e.Fetch(context.Background(), "http://article-fixture.example"+path); !errors.Is(err, articles.ErrUnavailable) {
				t.Fatal(err)
			}
		})
	}
	if _, err := e.Fetch(context.Background(), "https://user:pass@example.com"); !errors.Is(err, articles.ErrInvalid) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := e.Fetch(ctx, "http://article-fixture.example"+"/wait"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestArticleRedirectRemainsAnonymousAfterSharedPolicy(t *testing.T) {
	for _, custom := range []bool{false, true} {
		t.Run(map[bool]string{false: "automatic-referrer", true: "custom-policy"}[custom], func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if r.Header.Get("Referer") != "" || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
					t.Error("article redirect leaked private headers")
				}
				if r.URL.Path == "/start" {
					if r.URL.Query().Get("key") != "private-fixture-key" {
						t.Error("original access key removed")
					}
					http.Redirect(w, r, "https://other-article.example/story", 302)
					return
				}
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				io.WriteString(w, articleHTML())
			}))
			defer server.Close()
			client := articleTestClient(server)
			if custom {
				client.CheckRedirect = func(r *http.Request, via []*http.Request) error {
					r.Header.Set("Referer", "https://private.example/?key=private-fixture-key")
					r.Header.Set("Cookie", "session=private-fixture")
					r.Header.Set("Authorization", "Bearer private-fixture")
					return nil
				}
			}
			doc, err := (&Extractor{HTTP: client}).Fetch(context.Background(), "https://article-fixture.example/start?key=private-fixture-key")
			if err != nil || requests != 2 || doc.SourceURL != "https://other-article.example/story" || !strings.Contains(doc.Text, "正文😀") {
				t.Fatal("anonymous redirect extraction failed", err, requests)
			}
		})
	}
}

func TestExtractionAcceptsLargePageWithSmallReadableBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		io.WriteString(w, strings.Replace(articleHTML(), "</head>", "<script>"+strings.Repeat(" ", 3<<20)+"</script></head>", 1))
	}))
	defer server.Close()
	doc, err := (&Extractor{HTTP: articleTestClient(server)}).Fetch(context.Background(), "https://article-fixture.example/large-readable")
	if err != nil || !strings.Contains(doc.Text, "正文😀") || len(doc.Text) > 20000 {
		t.Fatal(doc, err)
	}
}

func TestExtractionBoundsDecompressedPagesAndAcceptsCompressedArticles(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept-Encoding") != "gzip" {
			t.Error("transport did not negotiate gzip")
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Encoding", "gzip")
		compressed := gzip.NewWriter(w)
		defer compressed.Close()
		if r.URL.Path == "/oversized" {
			io.WriteString(compressed, strings.Repeat(" ", maxHTML+1))
			return
		}
		io.WriteString(compressed, strings.Replace(articleHTML(), "</head>", "<script>"+strings.Repeat(" ", 3<<20)+"</script></head>", 1))
	}))
	defer server.Close()
	extractor := &Extractor{HTTP: articleTestClient(server)}
	doc, err := extractor.Fetch(context.Background(), "https://article-fixture.example/compressed")
	if err != nil || !strings.Contains(doc.Text, "正文😀") || !strings.Contains(doc.Text, "结束这篇文章") {
		t.Fatal("compressed article lost its readable body", err)
	}
	doc, err = extractor.Fetch(context.Background(), "https://article-fixture.example/oversized")
	if !errors.Is(err, articles.ErrUnavailable) || !strings.Contains(err.Error(), "16 MB") || doc.Text != "" {
		t.Fatal("decompressed size limit did not reject the complete response", err)
	}
}

func TestHeaderlessXHTMLExtractionAndNonWebXMLRejection(t *testing.T) {
	for _, prefix := range []string{`<?xml version="1.0" encoding="UTF-8"?>`, "\ufeff<?xml version=\"1.0\"?>\n<!-- publisher -->"} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header()["Content-Type"] = nil
			io.WriteString(w, prefix+articleHTML())
		}))
		doc, err := (&Extractor{HTTP: articleTestClient(server)}).Fetch(context.Background(), "https://article-fixture.example/xhtml")
		server.Close()
		if err != nil || !strings.Contains(doc.Text, "正文😀") {
			t.Fatal("headerless XHTML article rejected", err)
		}
	}
	for _, data := range []string{
		`<?xml version="1.0"?><rss><channel><title>Not an article</title></channel></rss>`,
		`<?xml version="1.0"?><feed xmlns="http://www.w3.org/2005/Atom"></feed>`,
		`<?xml version="1.0"?><svg><text>not HTML</text></svg>`,
		`plain text <html><body>not a document</body></html>`,
	} {
		if isWebDocument([]byte(data)) {
			t.Fatal("non-web document accepted", data)
		}
	}
	if isWebDocument([]byte(`<?xml version="1.0"?>` + strings.Repeat("<!-- preamble -->", 33) + `<html></html>`)) {
		t.Fatal("excessive XML preamble accepted")
	}
}

func TestXHTMLDeclaredEncodingAndHTTPPrecedence(t *testing.T) {
	page := `<?xml version="1.0" encoding="GBK"?>` + strings.ReplaceAll(articleHTML(), "😀", "")
	encoded, err := simplifiedchinese.GBK.NewEncoder().Bytes([]byte(page))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xhtml+xml")
		w.Write(encoded)
	}))
	defer server.Close()
	doc, err := (&Extractor{HTTP: articleTestClient(server)}).Fetch(context.Background(), "https://article-fixture.example/gbk")
	if err != nil || !strings.Contains(doc.Text, "正文的第一段") || doc.Title != "阅读与批注" {
		t.Fatal("XML encoding not respected", doc.Title, err)
	}
	for _, data := range [][]byte{[]byte(page), append([]byte{0xef, 0xbb, 0xbf}, []byte(page)...)} {
		reader, err := articleReader(data, "application/xhtml+xml; charset=utf-8")
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := io.ReadAll(reader)
		if err != nil || !strings.Contains(string(decoded), "阅读与批注") {
			t.Fatal("HTTP encoding precedence lost", err)
		}
	}
	reader, err := articleReader(encoded, "")
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := io.ReadAll(reader)
	if err != nil || !strings.Contains(string(decoded), "阅读与批注") {
		t.Fatal("headerless XML charset lost", err)
	}
	if _, err := articleReader([]byte(`<?xml version="1.0" encoding="unknown-encoding"?><html>`), "application/xhtml+xml"); err == nil {
		t.Fatal("unsupported declared encoding silently accepted")
	}
}
func TestExtractionExcludesArchiveWidgetWithoutDeletingArticleWordsOrCode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		io.WriteString(w, strings.Replace(articleHTML(), "</article>", `<p>The phrase No posts is part of this article.</p><pre><span class="portable-archive-list">archive class in a code example</span></pre><div class="widget portable-archive-list"><p>Unrelated archive empty state</p></div><div class="portable-archive-list-example"><p>This similarly named element is real content.</p></div></article>`, 1))
	}))
	defer server.Close()
	doc, err := (&Extractor{HTTP: articleTestClient(server)}).Fetch(context.Background(), "https://article-fixture.example/archive-widget")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc.Text, "Unrelated archive empty state") {
		t.Fatal("archive widget leaked into article")
	}
	for _, preserved := range []string{"The phrase No posts", "archive class in a code example", "similarly named element"} {
		if !strings.Contains(doc.Text, preserved) {
			t.Fatalf("removed article content: %s", preserved)
		}
	}
}
func TestBlocksPreserveCanonicalUnicodeCodeAndIgnoreUntrustedElements(t *testing.T) {
	node, err := html.Parse(strings.NewReader(`<article><p>A😀<br>B</p><blockquote><p>quoted</p></blockquote><pre>line1
  line2</pre><script>no</script><p hidden>secret</p><p><svg>bad</svg>end</p></article>`))
	if err != nil {
		t.Fatal(err)
	}
	doc := articles.Document{Blocks: blocks(node, "")}
	if err := articles.Prepare(&doc); err != nil {
		t.Fatal(err)
	}
	if doc.Text != "A😀 B\n\nquoted\n\nline1\n  line2\n\nend" {
		t.Fatal(doc.Text)
	}
}

func TestCodeBlocksKeepFirstLineIndentationAndTrailingSpaces(t *testing.T) {
	const source = "    if ready:\n\tprint(\"😀\")  \n    return value  "
	if value := clean("\r\n"+strings.ReplaceAll(source, "\n", "\r\n")+"\r\n", true); value != source {
		t.Fatalf("code whitespace changed: %q", value)
	}
	node, err := html.Parse(strings.NewReader("<article><pre><code>" + source + "</code></pre></article>"))
	if err != nil {
		t.Fatal(err)
	}
	doc := articles.Document{Blocks: blocks(node, "")}
	if err := articles.Prepare(&doc); err != nil || len(doc.Blocks) != 1 || doc.Blocks[0].Kind != "code" || doc.Text != source {
		t.Fatal("canonical code or block changed", doc, err)
	}
	digest := sha256.Sum256([]byte(source))
	if doc.SourceHash != hex.EncodeToString(digest[:]) {
		t.Fatal("code anchor hash differs from retained whitespace")
	}
	empty, err := html.Parse(strings.NewReader("<article><pre> \t\n </pre><p>Actual text</p></article>"))
	if err != nil || len(blocks(empty, "")) != 1 || blocks(empty, "")[0].Text != "Actual text" {
		t.Fatal("empty code should not invalidate the article", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		io.WriteString(w, strings.Replace(articleHTML(), "</article>", "<pre><code>"+source+"</code></pre></article>", 1))
	}))
	defer server.Close()
	extracted, err := (&Extractor{HTTP: articleTestClient(server)}).Fetch(context.Background(), "https://article-fixture.example/indented-code")
	if err != nil {
		t.Fatal(err)
	}
	for _, block := range extracted.Blocks {
		if block.Kind == "code" && block.Text == source {
			return
		}
	}
	t.Fatal("readability extraction lost code whitespace", extracted.Blocks)
}

// DNS and port policy remain enabled in product; this explicit transport maps fixture I/O only.
type articleTestTransport struct{ target *url.URL }

func (t articleTestTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	clone := r.Clone(r.Context())
	target := *r.URL
	target.Scheme = t.target.Scheme
	target.Host = t.target.Host
	clone.URL = &target
	res, err := http.DefaultTransport.RoundTrip(clone)
	if res != nil {
		res.Request = r
	}
	return res, err
}
func articleTestClient(server *httptest.Server) *http.Client {
	target, _ := url.Parse(server.URL)
	return &http.Client{Transport: articleTestTransport{target}}
}

func TestBlocksKeepImagesAsLinksInDocumentOrder(t *testing.T) {
	node, err := html.Parse(strings.NewReader(`<article><p>before</p><figure><img src="/blank.gif" data-src="/a.jpg" alt="A" width="640px"></figure><p>mid <img src="https://cdn.example.com/b.png"> after</p><img src="data:image/gif;base64,xx"><pre><img src="/code.png"></pre></article>`))
	if err != nil {
		t.Fatal(err)
	}
	doc := articles.Document{Blocks: blocks(node, "https://example.com/post/1")}
	if err := articles.Prepare(&doc); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, block := range doc.Blocks {
		got = append(got, block.Kind+":"+block.Text+block.URL)
	}
	want := "paragraph:before|image:https://example.com/a.jpg|paragraph:mid|image:https://cdn.example.com/b.png|paragraph:after"
	if strings.Join(got, "|") != want {
		t.Fatalf("blocks = %q", strings.Join(got, "|"))
	}
	if doc.Blocks[1].Alt != "A" || doc.Blocks[1].Width != 640 {
		t.Fatalf("image attributes = %+v", doc.Blocks[1])
	}
	if doc.Text != "before\n\nmid\n\nafter" {
		t.Fatalf("images must not change anchored text: %q", doc.Text)
	}
}
