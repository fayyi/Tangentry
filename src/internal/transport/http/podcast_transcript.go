package httptransport

import (
	"context"
	"net/http"
	"strings"
	"time"

	"tangentry/internal/content"
	"tangentry/internal/subtitles"
)

func (h *podcastHandler) podcastTranscript(w http.ResponseWriter, r *http.Request) {
	if h.podcast.SavedSubtitles == nil {
		fail(w, subtitles.ErrSavedInvalid)
		return
	}
	id := r.URL.Query().Get("contentId")
	allowed := strings.HasPrefix(id, "rss:") || strings.HasPrefix(id, "xiaoyuzhou:")
	if r.URL.Path == "/api/subtitles/local-media" {
		allowed = strings.HasPrefix(id, "douyin:") || strings.HasPrefix(id, "xiaohongshu:")
	}
	if !allowed {
		fail(w, subtitles.ErrSavedInvalid)
		return
	}
	values, err := h.podcast.SavedSubtitles.List(r.Context(), id)
	if err != nil {
		fail(w, err)
		return
	}
	doc := subtitles.Document{VideoID: id, Part: 1, Status: "none", Tracks: []subtitles.Track{}}
	for _, v := range values {
		doc.Tracks = append(doc.Tracks, v.Track)
	}
	if len(values) > 0 {
		doc.Status = "available"
	}
	episodeID := ""
	publicItem := content.Item{}
	if len(values) == 0 && h.podcast.PublicCaptions != nil {
		publicItem = h.publicPodcastItem(r.Context(), id)
	}
	if len(values) == 0 && h.podcast.Captions != nil {
		if h.podcast.EpisodeID != nil {
			episodeID = h.podcast.EpisodeID(r.Context(), id)
		} else if strings.HasPrefix(id, "xiaoyuzhou:") {
			episodeID = strings.TrimPrefix(id, "xiaoyuzhou:")
		}
	}
	usePublic := len(values) == 0 && h.podcast.PublicCaptions != nil && len(publicItem.Transcripts) > 0
	if episodeID != "" || usePublic {
		select {
		case h.browsing <- struct{}{}:
			defer func() { <-h.browsing }()
		default:
			fail(w, subtitles.ErrBusy)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 28*time.Second)
		defer cancel()
		if usePublic {
			doc, err = h.podcast.PublicCaptions(ctx, publicItem)
		} else {
			doc, err = h.podcast.Captions(ctx, episodeID)
		}
		if err != nil {
			fail(w, err)
			return
		}
		doc.VideoID = id
		if doc.Status == "available" {
			for _, track := range doc.Tracks {
				// Revision zero only inserts a new language; concurrent edits win.
				if _, saveErr := h.podcast.SavedSubtitles.Save(ctx, id, track, 0); saveErr != nil {
					if saveErr != subtitles.ErrSavedConflict {
						fail(w, saveErr)
						return
					}
				}
			}
			cached, readErr := h.podcast.SavedSubtitles.List(ctx, id)
			if readErr != nil {
				fail(w, readErr)
				return
			}
			doc.Tracks = []subtitles.Track{}
			for _, value := range cached {
				doc.Tracks = append(doc.Tracks, value.Track)
			}
		}
	}
	write(w, 200, doc)
}

func (h *podcastHandler) publicPodcastItem(ctx context.Context, id string) content.Item {
	if !strings.HasPrefix(id, "rss:") {
		return content.Item{}
	}
	if h.podcast.Inbox != nil {
		if item, err := h.podcast.Inbox.Item(ctx, id); err == nil && item.Kind == "audio" && len(item.Transcripts) > 0 {
			return item
		}
	}
	if h.podcast.Content != nil {
		if item, err := h.podcast.Content.Get(ctx, id); err == nil && item.Kind == "audio" {
			return item
		}
	}
	return content.Item{}
}
