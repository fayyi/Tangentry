package discovery

import "testing"

func TestDouyinSingleVideoNormalization(t *testing.T) {
	id := "6961737553342991651"
	for _, raw := range []string{"https://www.douyin.com/video/" + id, "http://douyin.com/video/" + id + "/?foo=tracking#fragment", "https://www.douyin.com/?modal_id=" + id, "分享我的视频 https://www.douyin.com/video/" + id + " 复制这条消息"} {
		ref, err := ParseDouyin(raw)
		if err != nil || ref.ID != id || ref.URL != "https://www.douyin.com/video/"+id {
			t.Fatal(raw, ref, err)
		}
		platform, actual, canonical, err := VideoURL(raw)
		if err != nil || platform != "douyin" || actual != id || canonical != ref.URL {
			t.Fatal("download identity", err)
		}
	}
	ref, err := ParseDouyin("3.14 打开抖音 https://v.douyin.com/AbcD123/ 复制内容")
	if err != nil || ref.ID != "" || ref.URL != "https://v.douyin.com/AbcD123/" {
		t.Fatal(ref, err)
	}
	if _, _, _, err = VideoURL(ref.URL); err == nil {
		t.Fatal("unresolved short link admitted to download queue")
	}
}
func TestDouyinRejectsNonVideoAndAmbiguousInputs(t *testing.T) {
	for _, raw := range []string{"6961737553342991651", "https://www.douyin.com/user/123", "https://www.douyin.com/note/6961737553342991651", "https://www.douyin.com/video/1", "https://www.douyin.com/video/0961737553342991651", "https://www.douyin.com.evil.example/video/6961737553342991651", "https://user:secret@www.douyin.com/video/6961737553342991651", "https://www.douyin.com:443/video/6961737553342991651", "https://www.douyin.com/?modal_id=6961737553342991651&modal_id=6982497745948921092", "https://v.douyin.com/a/b/", "https://www.douyin.com/video/6961737553342991651 https://evil.example/private"} {
		if _, err := ParseDouyin(raw); err == nil {
			t.Fatal("invalid accepted", raw)
		}
	}
}
