package papers

import (
	"strings"
	"testing"

	"tangentry/internal/content"
)

func TestParseReferenceVariants(t *testing.T) {
	tests := []struct {
		input, id string
		version   int
	}{
		{"2610.22322", "2610.22322", 0},
		{"arXiv:2610.22322v2", "2610.22322", 2},
		{"https://arxiv.org/abs/2610.22322v3", "2610.22322", 3},
		{"https://arxiv.org/pdf/hep-th/9901001.pdf", "hep-th/9901001", 0},
		{"https://huggingface.co/papers/2610.22322", "2610.22322", 0},
		{"huggingface.co/papers/2610.22322", "2610.22322", 0},
		{"https://alphaxiv.org/abs/2610.22322", "2610.22322", 0},
	}
	for _, test := range tests {
		ref, err := ParseReference(test.input)
		if err != nil || ref.ID != test.id || ref.Version != test.version {
			t.Fatalf("ParseReference(%q) = %+v, %v", test.input, ref, err)
		}
	}
	for _, input := range []string{"", "https://evil.example/2610.22322", "2610.22322v0", "arxiv:bad"} {
		if _, err := ParseReference(input); err == nil {
			t.Fatalf("invalid paper reference accepted: %q", input)
		}
	}
}

func TestParseArxivAtomAndMapItem(t *testing.T) {
	data := []byte(`<feed xmlns="http://www.w3.org/2005/Atom"><entry><id>https://arxiv.org/abs/2610.22322v2</id><title> A paper &amp; title </title><summary> Abstract text. </summary><published>2026-10-01T12:00:00Z</published><updated>2026-10-02T12:00:00Z</updated><author><name>Alice</name></author><author><name>Bob</name></author><category term="cs.AI"/><link rel="related" type="application/pdf" href="https://arxiv.org/pdf/2610.22322v2"/></entry></feed>`)
	items, err := ParseArxivAtom(data)
	if err != nil || len(items) != 1 {
		t.Fatalf("ParseArxivAtom: %v, %#v", err, items)
	}
	if items[0].Version != 2 || len(items[0].Authors) != 2 || items[0].Categories[0] != "cs.AI" || items[0].PublishedAt == 0 {
		t.Fatalf("atom fields missing: %#v", items[0])
	}
	item := ToItem(items[0])
	if item.ID != "arxiv:2610.22322" || item.Kind != "pdf" || item.Platform != "arxiv" || item.Description != "Abstract text." || content.Validate(item) != nil {
		t.Fatalf("invalid mapped item: %#v", item)
	}
}

func TestParseArxivRSS(t *testing.T) {
	data := []byte(`<rss version="2.0" xmlns:dc="http://purl.org/dc/elements/1.1/"><channel><item><guid>http://arxiv.org/abs/2610.22322</guid><title>RSS paper</title><description>First paragraph.</description><pubDate>Fri, 02 Oct 2026 12:00:00 +0000</pubDate><dc:creator>Alice</dc:creator><category>cs.CL</category></item></channel></rss>`)
	items, err := ParseArxivRSS(data)
	if err != nil || len(items) != 1 || items[0].Title != "RSS paper" || items[0].Authors[0] != "Alice" || items[0].Categories[0] != "cs.CL" {
		t.Fatalf("RSS mapping failed: %#v, %v", items, err)
	}
}

func TestParseHFDaily(t *testing.T) {
	data := []byte(`[ {"paper":{"id":"2610.22322"},"title":"HF paper","summary":"summary","authors":[{"name":"Alice"}],"upvotes":12,"githubRepo":"alice/project","githubStars":34,"ai_summary":"short","ai_keywords":["agents","reasoning"]} ]`)
	items, err := ParseHFDaily(data)
	if err != nil || len(items) != 1 {
		t.Fatalf("HF parse failed: %v, %#v", err, items)
	}
	// The endpoint has historically returned both a nested paper object and a
	// flattened id; malformed records should not make the whole response fail.
	if items[0].ID != "2610.22322" || items[0].Upvotes != 12 || items[0].GitHubStars != 34 || !strings.Contains(items[0].AISummary, "short") {
		t.Fatalf("HF fields missing: %#v", items[0])
	}
}
