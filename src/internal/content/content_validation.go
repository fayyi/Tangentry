package content

import (
	"net/url"
	"strings"
)

// Validate treats API input as untrusted; external links must not become script URLs.
func Validate(item Item) error {
	maxDescription := 20000
	if item.Platform == "rss" && item.Kind == "article" {
		maxDescription = 1 << 20
	}
	if item.ID == "" || len(item.ID) > 180 || item.PlatformID == "" || item.Platform == "" || (item.Kind != "video" && item.Kind != "article" && item.Kind != "audio" && item.Kind != "pdf" && item.Kind != "image") || strings.TrimSpace(item.Title) == "" || len(item.Title) > 2000 || len(item.Description) > maxDescription || item.DurationSeconds < 0 {
		return ErrInvalid
	}
	if item.ID != item.Platform+":"+item.PlatformID {
		return ErrInvalid
	}
	if len(item.PublishedText) > 256 || strings.ContainsAny(item.PublishedText, "\r\n\x00") {
		return ErrInvalid
	}
	if len(item.Images) > 50 {
		return ErrInvalid
	}
	if len(item.ArticleBlocks) > 4000 {
		return ErrInvalid
	}
	for _, block := range item.ArticleBlocks {
		switch block.Kind {
		case "paragraph", "quote", "code", "list":
			if block.Level != 0 || strings.TrimSpace(block.Text) == "" || len(block.Text) > 20000 {
				return ErrInvalid
			}
		case "heading":
			if block.Level < 2 || block.Level > 4 || strings.TrimSpace(block.Text) == "" || len(block.Text) > 2000 {
				return ErrInvalid
			}
		case "image":
			u, err := url.Parse(block.URL)
			if err != nil || len(block.URL) > 4096 || u.Hostname() == "" || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") || block.Width < 0 || block.Height < 0 || block.Width > 30000 || block.Height > 30000 || len(block.Alt) > 1000 {
				return ErrInvalid
			}
		default:
			return ErrInvalid
		}
	}
	if len(item.Transcripts) > 8 {
		return ErrInvalid
	}
	for _, transcript := range item.Transcripts {
		u, err := url.Parse(transcript.URL)
		if err != nil || len(transcript.URL) > 4096 || u.Hostname() == "" || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") || (transcript.Type != "text/vtt" && transcript.Type != "application/x-subrip" && transcript.Type != "application/json") || len(transcript.Language) > 64 || strings.ContainsAny(transcript.Language, "\r\n\x00") {
			return ErrInvalid
		}
	}
	for _, image := range item.Images {
		u, err := url.Parse(image.URL)
		if err != nil || len(image.URL) > 4096 || u.Hostname() == "" || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") || image.Width < 0 || image.Height < 0 || image.Width > 30000 || image.Height > 30000 || len(image.Alt) > 1000 {
			return ErrInvalid
		}
	}
	for index, raw := range []string{item.URL, item.ThumbnailURL, item.MediaURL, item.AuthorAvatarURL} {
		if index > 0 && raw == "" {
			continue
		}
		u, err := url.Parse(raw)
		if err != nil || u.Hostname() == "" || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") {
			return ErrInvalid
		}
	}
	return nil
}
