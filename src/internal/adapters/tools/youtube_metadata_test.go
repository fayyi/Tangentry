package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"tangentry/internal/config"
)

func TestYouTubeMetadataUsesStableChannelIdentityAndNameFallback(t *testing.T) {
	if os.PathSeparator != '/' {
		t.Skip("POSIX process fixture")
	}
	const channel = "UCaaaaaaaaaaaaaaaaaaaaaa"
	for _, tc := range []struct {
		name, uploader, handle, channelName, channelID, wantName, wantID string
	}{
		{"channel fallback", "", "@channel", "Actual channel", channel, "Actual channel", channel},
		{"uploader preserved", "Actual uploader", "@channel", "Other label", channel, "Actual uploader", channel},
		{"legacy stable identity", "Author", channel, "", "", "Author", channel},
		{"handle is not identity", "Author", "@channel", "", "", "Author", ""},
		{"invalid channel", "Author", "@channel", "", "PLplaylist", "Author", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, err := json.Marshal(map[string]any{"id": "NZLbTuBDhJg", "title": "YouTube metadata", "uploader": tc.uploader, "uploader_id": tc.handle, "channel": tc.channelName, "channel_id": tc.channelID})
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "metadata-fixture")
			// The JSON fixture contains no quotes interpreted by the shell.
			if err := os.WriteFile(path, []byte("#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo fixture; exit 0; fi\nprintf '%s\\n' '"+string(data)+"'\n"), 0700); err != nil {
				t.Fatal(err)
			}
			toolkit := New(config.Config{YTDLPPath: path, FFmpegPath: filepath.Join(t.TempDir(), "absent")})
			item, err := toolkit.YouTube(context.Background(), "https://www.youtube.com/watch?v=NZLbTuBDhJg")
			if err != nil || item.Author != tc.wantName || item.AuthorID != tc.wantID {
				t.Fatalf("metadata: %+v, %v", item, err)
			}
		})
	}
}
