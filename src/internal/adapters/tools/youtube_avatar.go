package tools

import (
	"context"
	"encoding/json"
	"net/url"
	"time"

	"tangentry/internal/discovery"
)

type youtubeAvatarEntry struct {
	url     string
	expires time.Time
	done    chan struct{}
}

// Optional public channel metadata. No account cookies or individual uploads are read.
func (t *Toolkit) youtubeAvatar(ctx context.Context, id string) string {
	if !discovery.YouTubeChannelID(id) || ctx.Err() != nil {
		return ""
	}
	t.youtubeAvatarMu.Lock()
	if cached, ok := t.youtubeAvatars[id]; ok {
		if cached.done != nil {
			t.youtubeAvatarMu.Unlock()
			select {
			case <-ctx.Done():
				return ""
			case <-cached.done:
			}
			t.youtubeAvatarMu.Lock()
			value := t.youtubeAvatars[id].url
			t.youtubeAvatarMu.Unlock()
			return value
		}
		if time.Now().Before(cached.expires) {
			t.youtubeAvatarMu.Unlock()
			return cached.url
		}
	}
	select {
	case t.youtubeAvatarSlots <- struct{}{}:
	default:
		t.youtubeAvatarMu.Unlock()
		return ""
	}
	for key, cached := range t.youtubeAvatars {
		if cached.done == nil && time.Now().After(cached.expires) {
			delete(t.youtubeAvatars, key)
		}
	}
	if len(t.youtubeAvatars) >= 256 {
		for key, cached := range t.youtubeAvatars {
			if cached.done == nil {
				delete(t.youtubeAvatars, key)
				break
			}
		}
	}
	done := make(chan struct{})
	t.youtubeAvatars[id] = youtubeAvatarEntry{done: done}
	t.youtubeAvatarMu.Unlock()
	value := t.readYouTubeAvatar(ctx, id)
	ttl := 24 * time.Hour
	if value == "" {
		ttl = 5 * time.Minute
	}
	t.youtubeAvatarMu.Lock()
	t.youtubeAvatars[id] = youtubeAvatarEntry{url: value, expires: time.Now().Add(ttl)}
	close(done)
	t.youtubeAvatarMu.Unlock()
	<-t.youtubeAvatarSlots
	return value
}

func (t *Toolkit) readYouTubeAvatar(ctx context.Context, id string) string {
	// Avatars are a background nicety: they never trigger the first-use
	// yt-dlp download.
	if t.ytdlp != nil {
		if _, installed := t.ytdlp.installed(); !installed {
			return ""
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	args := []string{"--ignore-config", "--no-plugin-dirs", "--no-cache-dir", "--simulate", "--skip-download", "--flat-playlist", "--playlist-items", "0", "--dump-single-json", "--socket-timeout", "3", "--retries", "0", "--", discovery.YouTubeChannelURL(id)}
	cmd, err := t.ytdlpCommand(ctx, args...)
	if err != nil {
		return ""
	}
	stdout, stderr := &metadataBuffer{}, &boundedBuffer{}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	if cmd.Run() != nil || stdout.exceeded {
		return ""
	}
	var data struct {
		ChannelID  string            `json:"channel_id"`
		Entries    []json.RawMessage `json:"entries"`
		Thumbnails []struct {
			ID  string `json:"id"`
			URL string `json:"url"`
		} `json:"thumbnails"`
	}
	if json.Unmarshal(stdout.Bytes(), &data) != nil || data.ChannelID != id || len(data.Entries) != 0 {
		return ""
	}
	for _, image := range data.Thumbnails {
		if image.ID != "avatar_uncropped" {
			continue
		}
		u, err := url.Parse(image.URL)
		if err == nil && u.Scheme == "https" && u.User == nil && u.Port() == "" && (u.Host == "yt3.ggpht.com" || u.Host == "yt3.googleusercontent.com") && len(image.URL) <= 2048 {
			return image.URL
		}
	}
	return ""
}
