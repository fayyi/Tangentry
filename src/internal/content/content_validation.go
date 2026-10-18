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
	if item.ID == "" || len(item.ID) > 180 || item.PlatformID == "" || item.Platform == "" || (item.Kind != "video" && item.Kind != "article" && item.Kind != "audio" && item.Kind != "pdf" && item.Kind != "image") || strings.TrimSpace(item.Title) == "" || len(item.Title) > 2000 || len(item.Description) > maxDescription || item.DurationSeconds < 0 || item.ReplyCount < 0 || item.ViewCount < 0 || item.LikeCount < 0 {
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
	if item.Discussion != nil {
		if item.Discussion.TopicID == "" || len(item.Discussion.Posts) > 200 || len(item.Discussion.PostIDs) > 10000 || item.Discussion.TotalPosts < len(item.Discussion.Posts) || item.Discussion.LoadedPosts != len(item.Discussion.Posts) {
			return ErrInvalid
		}
		for _, post := range item.Discussion.Posts {
			if post.ID <= 0 || post.Number <= 0 || strings.TrimSpace(post.Username) == "" || post.LikeCount < 0 || post.ReplyToNumber < 0 || len(post.Blocks) > 4000 {
				return ErrInvalid
			}
			for _, block := range post.Blocks {
				if !validArticleBlock(block) {
					return ErrInvalid
				}
			}
		}
	}
	for _, block := range item.ArticleBlocks {
		if !validArticleBlock(block) {
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

func validArticleBlock(block ArticleBlock) bool {
	if block.Group < 0 || block.Group > 100000 || len(block.Summary) > 2000 || len(block.InlineImages) > 100 {
		return false
	}
	for _, image := range block.InlineImages {
		u, err := url.Parse(image.URL)
		if err != nil || len(image.URL) > 4096 || u.Hostname() == "" || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") || image.Offset < 0 || image.Width < 0 || image.Height < 0 || image.Width > 30000 || image.Height > 30000 || len(image.Alt) > 1000 {
			return false
		}
	}
	switch block.Kind {
	case "paragraph", "quote", "code", "table":
		return block.Level == 0 && !block.Ordered && strings.TrimSpace(block.Text) != "" && len(block.Text) <= 20000
	case "list":
		return block.Level >= 0 && block.Level <= 16 && strings.TrimSpace(block.Text) != "" && len(block.Text) <= 20000
	case "heading":
		return block.Level >= 2 && block.Level <= 4 && !block.Ordered && strings.TrimSpace(block.Text) != "" && len(block.Text) <= 2000
	case "details", "spoiler":
		return block.Level == 0 && !block.Ordered && strings.TrimSpace(block.Text) != "" && len(block.Text) <= 20000 && strings.TrimSpace(block.Summary) != ""
	case "onebox":
		return block.Level == 0 && !block.Ordered && strings.TrimSpace(block.Text) != "" && len(block.Text) <= 2000 && validArticleURL(block.URL)
	case "image", "emoji":
		u, err := url.Parse(block.URL)
		return err == nil && len(block.URL) <= 4096 && u.Hostname() != "" && u.User == nil && (u.Scheme == "https" || u.Scheme == "http") && block.Level == 0 && !block.Ordered && block.Summary == "" && block.Width >= 0 && block.Height >= 0 && block.Width <= 30000 && block.Height <= 30000 && len(block.Alt) <= 1000
	default:
		return false
	}
}

func validArticleURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && len(raw) <= 4096 && u.Hostname() != "" && u.User == nil && (u.Scheme == "https" || u.Scheme == "http")
}
