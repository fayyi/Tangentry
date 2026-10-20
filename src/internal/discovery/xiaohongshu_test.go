package discovery

import (
	"strings"
	"testing"
)

func TestXiaohongshuShareNormalization(t *testing.T) {
	id := "6411cf99000000001300b6d9"
	if ref, err := ParseXiaohongshu(id); err != nil || ref.ID != id || strings.Contains(ref.URL, "xsec_token") {
		t.Fatalf("bare note ID was not normalized: %#v %v", ref, err)
	}
	for _, raw := range []string{"https://www.xiaohongshu.com/explore/" + id, "http://xiaohongshu.com/discovery/item/" + id + "?share=discard&xsec_token=fixture+access&xsec_source=pc_share#ignored", "复制 https://www.xiaohongshu.com/explore/" + id + " 这条分享"} {
		ref, err := ParseXiaohongshu(raw)
		if err != nil || ref.ID != id || !strings.HasPrefix(ref.URL, "https://www.xiaohongshu.com/explore/"+id) || strings.Contains(ref.URL, "discard") {
			t.Fatal(raw, ref, err)
		}
		if !SameVideo(ref.URL, "https://www.xiaohongshu.com/explore/"+id+"?xsec_token=other") {
			t.Fatal("share parameter changed identity")
		}
	}
	for _, raw := range []string{"https://xhslink.com/Abc123", "复制 https://xhslink.com/a/Abc123 打开小红书", "http://xhslink.com/o/Abc123", "https://xhslink.com/m/Abc123"} {
		ref, err := ParseXiaohongshu(raw)
		if err != nil || ref.ID != "" {
			t.Fatal(ref, err)
		}
		if _, _, _, err := VideoURL(raw); err == nil {
			t.Fatal("unresolved short link admitted")
		}
	}
}
func TestXiaohongshuRejectsUnsafeAndAmbiguousLocators(t *testing.T) {
	id := "6411cf99000000001300b6d9"
	for _, raw := range []string{"https://www.xiaohongshu.com/user/profile/" + id, "https://www.xiaohongshu.com/explore/short", "https://www.xiaohongshu.com.evil.example/explore/" + id, "https://user:secret@www.xiaohongshu.com/explore/" + id, "https://www.xiaohongshu.com:443/explore/" + id, "https://www.xiaohongshu.com/explore/" + id + "?xsec_token=one&xsec_token=two", "https://xhslink.com/a/code/extra", "https://www.xiaohongshu.com/explore/" + id + " https://example.com/other", "https://www.xiaohongshu.com/explore/" + id + "?xsec_token=" + strings.Repeat("x", 513)} {
		if _, err := ParseXiaohongshu(raw); err == nil {
			t.Fatal("invalid accepted", raw)
		}
	}
	if SameVideo("bad", "bad") {
		t.Fatal("invalid identity matched")
	}
}
