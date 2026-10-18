package content

import (
	"strings"
	"testing"
)

func TestSharedIdentityBoundaries(t *testing.T) {
	for _, id := range []string{"bilibili:BV17x411w7KC", "youtube:abcdefghijk", "douyin:6961737553342991651", "rss:" + strings.Repeat("a", 64), "xiaoyuzhou:" + strings.Repeat("b", 24), "arxiv:2610.22322", "arxiv:hep-th/9901001"} {
		if !ValidID(id) || !SpeechID(id) {
			t.Fatal("missing identity", id)
		}
	}
	pdf := "pdf:" + strings.Repeat("c", 64)
	if !ValidID(pdf) || SpeechID(pdf) {
		t.Fatal("PDF speech boundary")
	}
	for _, id := range []string{"memo:" + strings.Repeat("a", 32), "youtube:channel:UCaaaaaaaaaaaaaaaaaaaaaa", "rss:feed:" + strings.Repeat("a", 64), "douyin:123", "douyin:0961737553342991651", "douyin:6961737553342991651:extra", "arxiv:2610.22322v2", "arxiv:bad", "RSS:" + strings.Repeat("a", 64)} {
		if ValidID(id) || SpeechID(id) {
			t.Fatal("invalid identity", id)
		}
	}
}
