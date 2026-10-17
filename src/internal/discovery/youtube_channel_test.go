package discovery

import (
	"strings"
	"testing"
)

func TestYouTubeChannelLocatorsNormalizeWithoutNetwork(t *testing.T) {
	id := "UC" + strings.Repeat("a", 22)
	for _, raw := range []string{id, "  " + id + "  ", "https://youtube.com/channel/" + id, "http://m.youtube.com/channel/" + id + "/videos?si=share#x", "https://WWW.YOUTUBE.COM/channel/" + id + "/shorts", YouTubeChannelFeed(id)} {
		r, err := ParseYouTubeChannel(raw)
		if err != nil || r.ID != id || r.URL != YouTubeChannelURL(id) {
			t.Fatal(raw, r, err)
		}
	}
	for _, raw := range []string{"@Creator_Name", "https://www.youtube.com/@Creator_Name/videos?si=share", "https://youtube.com/@Creator_Name/shorts", "https://m.youtube.com/@Creator_Name/streams", "https://www.youtube.com/@Creator_Name/about"} {
		r, err := ParseYouTubeChannel(raw)
		if err != nil || r.ID != "" || r.URL != "https://www.youtube.com/@Creator_Name" {
			t.Fatal(raw, r, err)
		}
	}
	if _, err := ParseYouTubeChannel("@中文频道"); err != nil {
		t.Fatal("international handle rejected", err)
	}
	for _, raw := range []string{"", "UCshort", "@", "@bad space", "@-leading", "@trailing.", "@a/b", "@a?x", "@" + strings.Repeat("a", 31), "https://youtu.be/abcdefghijk", "https://www.youtube.com/watch?v=abcdefghijk", "https://www.youtube.com/playlist?list=foo", "https://www.youtube.com.evil.example/@name", "https://user:secret@youtube.com/@name", "https://youtube.com:443/@name", "https://youtube.com/channel/" + id + "/videos/extra", "https://youtube.com/@name%2Fwatch", "https://youtube.com/feeds/videos.xml?channel_id=" + id + "&channel_id=" + id, "https://youtube.com/feeds/videos.xml?channel_id=" + id + "&playlist_id=foo", strings.Repeat("x", 2049)} {
		if r, err := ParseYouTubeChannel(raw); err == nil {
			t.Fatal("accepted invalid source", raw, r)
		}
	}
}
