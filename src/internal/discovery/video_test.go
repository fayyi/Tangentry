package discovery

import "testing"

func TestYouTubeURLNormalization(t *testing.T) {
	for _, raw := range []string{"https://youtu.be/jNQXAC9IVRw?t=1", "https://www.youtube.com/watch?v=jNQXAC9IVRw&list=123", "https://m.youtube.com/shorts/jNQXAC9IVRw"} {
		id, url, err := ParseYouTube(raw)
		if err != nil || id != "jNQXAC9IVRw" || url != "https://www.youtube.com/watch?v=jNQXAC9IVRw" {
			t.Fatal(raw, id, url, err)
		}
	}
	for _, raw := range []string{"https://youtube.com.evil.test/watch?v=jNQXAC9IVRw", "https://user:secret@youtube.com/watch?v=jNQXAC9IVRw", "https://youtube.com/playlist?list=123", "https://youtube.com/watch?v=bad", "https://youtube.com:8080/watch?v=jNQXAC9IVRw"} {
		if _, _, err := ParseYouTube(raw); err == nil {
			t.Fatal("invalid accepted", raw)
		}
	}
}
