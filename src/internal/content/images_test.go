package content

import (
	"strings"
	"testing"
)

func TestAlbumValidation(t *testing.T) {
	item := Item{ID: "xiaohongshu:6411cf99000000001300b6d9", Platform: "xiaohongshu", PlatformID: "6411cf99000000001300b6d9", Kind: "image", Title: "Album", URL: "https://www.xiaohongshu.com/explore/6411cf99000000001300b6d9", Images: []Image{{URL: "https://sns-webpic-qc.xhscdn.com/one", Width: 900, Height: 1200}}}
	if Validate(item) != nil || !ValidID(item.ID) {
		t.Fatal("valid album rejected")
	}
	for _, image := range []Image{{URL: "javascript:alert(1)"}, {URL: "https://user:secret@example.com/image"}, {URL: "https://example.com/image", Width: -1}, {URL: "https://example.com/image", Height: 30001}, {URL: "https://example.com/image", Alt: strings.Repeat("x", 1001)}} {
		item.Images = []Image{image}
		if Validate(item) == nil {
			t.Fatal("invalid image accepted", image)
		}
	}
	item.Images = make([]Image, 51)
	if Validate(item) == nil {
		t.Fatal("oversize album accepted")
	}
}
