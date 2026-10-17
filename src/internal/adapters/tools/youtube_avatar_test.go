package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tangentry/internal/config"
)

func TestYouTubeAvatarUsesExplicitPublicAvatarAndCaches(t *testing.T) {
	if os.PathSeparator != '/' {
		t.Skip("POSIX fixture")
	}
	const id = "UCaaaaaaaaaaaaaaaaaaaaaa"
	for _, tc := range []struct{ name, channel, image, want string }{
		{"valid", id, "https://yt3.googleusercontent.com/avatar", "https://yt3.googleusercontent.com/avatar"},
		{"unsafe", id, "https://evil.example/avatar", ""},
		{"wrong channel", "UCbbbbbbbbbbbbbbbbbbbbbb", "https://yt3.ggpht.com/avatar", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			path, log := filepath.Join(root, "fixture"), filepath.Join(root, "calls")
			script := "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo fixture; exit 0; fi\n"
			script += "printf 'call\\n' >> '" + log + "'\n"
			script += "printf '%s\\n' '{\"channel_id\":\"" + tc.channel + "\",\"entries\":[],\"thumbnails\":[{\"id\":\"banner_uncropped\",\"url\":\"https://yt3.ggpht.com/banner\"},{\"id\":\"avatar_uncropped\",\"url\":\"" + tc.image + "\"}]}'\n"
			if err := os.WriteFile(path, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			tk := New(config.Config{YTDLPPath: path, FFmpegPath: filepath.Join(root, "absent")})
			for range 2 {
				if value := tk.youtubeAvatar(context.Background(), id); value != tc.want {
					t.Fatalf("avatar %q", value)
				}
			}
			calls, err := os.ReadFile(log)
			if err != nil || strings.Count(string(calls), "call") != 1 {
				t.Fatal("cache miss", err)
			}
			if value := tk.youtubeAvatar(context.Background(), "@handle"); value != "" {
				t.Fatal("accepted handle")
			}
		})
	}
}
