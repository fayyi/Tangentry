package tools

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"tangentry/internal/content"
	"tangentry/internal/netpolicy"
	"tangentry/internal/subtitles"
)

var podcastLanguage = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// PublicPodcastCaptions fetches publisher-provided timed transcripts without credentials.
func PublicPodcastCaptions(ctx context.Context, client *http.Client, item content.Item) (subtitles.Document, error) {
	doc := subtitles.Document{VideoID: item.ID, Part: 1, Status: "none", Tracks: []subtitles.Track{}}
	if item.Kind != "audio" || content.Validate(item) != nil {
		return doc, subtitles.ErrInvalid
	}
	if client == nil {
		client = netpolicy.NewClient()
	}
	// Never inherit a caller's cookie jar or redirect credentials.
	public := *client
	public.Jar = nil
	redirect := client.CheckRedirect
	public.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return http.ErrUseLastResponse
		}
		if _, err := netpolicy.URL(req.URL.String()); err != nil {
			return err
		}
		if redirect != nil {
			if err := redirect(req, via); err != nil {
				return err
			}
		}
		req.Header.Del("Cookie")
		req.Header.Del("Authorization")
		return nil
	}
	seen := map[string]bool{}
	limitedHosts := map[string]bool{}
	for _, reference := range item.Transcripts {
		language := reference.Language
		if language == "" {
			language = "und"
		}
		if !podcastLanguage.MatchString(language) || seen[language] {
			continue
		}
		address, err := netpolicy.URL(reference.URL)
		if err != nil {
			continue
		}
		parsed, err := url.Parse(address)
		if err != nil || limitedHosts[strings.ToLower(parsed.Hostname())] {
			continue
		}
		requestCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, address, nil)
		if err != nil {
			cancel()
			return doc, subtitles.ErrUnavailable
		}
		request.Header.Set("Accept", reference.Type)
		response, err := public.Do(request)
		if err != nil {
			cancel()
			if ctx.Err() != nil {
				return doc, ctx.Err()
			}
			continue
		}
		// Avoid immediately trying another address on a server that asked us to stop.
		if response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusServiceUnavailable && response.Header.Get("Retry-After") != "" {
			limitedHosts[strings.ToLower(parsed.Hostname())] = true
			if response.Request != nil && response.Request.URL != nil {
				limitedHosts[strings.ToLower(response.Request.URL.Hostname())] = true
			}
			response.Body.Close()
			cancel()
			continue
		}
		data, readErr := io.ReadAll(io.LimitReader(response.Body, (2<<20)+1))
		response.Body.Close()
		cancel()
		if response.StatusCode != http.StatusOK || readErr != nil || len(data) > 2<<20 || !utf8.Valid(data) {
			continue
		}
		var cues []subtitles.Cue
		switch reference.Type {
		case "text/vtt":
			cues, err = parseWebVTT(string(data))
		case "application/x-subrip":
			cues, err = parseSRT(string(data))
		case "application/json":
			cues, err = parsePodcastJSON(data)
		default:
			continue
		}
		if err != nil || len(cues) == 0 {
			continue
		}
		seen[language] = true
		doc.Tracks = append(doc.Tracks, subtitles.Track{Language: language, Label: "公开字幕 · " + language, Cues: cues})
	}
	if ctx.Err() != nil {
		return doc, ctx.Err()
	}
	if len(doc.Tracks) > 0 {
		doc.Status = "available"
	} else if len(item.Transcripts) > 0 {
		return doc, subtitles.ErrPublicUnavailable
	}
	return doc, nil
}

func parsePodcastJSON(data []byte) ([]subtitles.Cue, error) {
	var transcript struct {
		Version  string `json:"version"`
		Segments []struct {
			Start *float64 `json:"startTime"`
			End   *float64 `json:"endTime"`
			Body  string   `json:"body"`
		} `json:"segments"`
	}
	if len(data) > 2<<20 || !utf8.Valid(data) || json.Unmarshal(data, &transcript) != nil || transcript.Version != "1.0.0" || len(transcript.Segments) == 0 || len(transcript.Segments) > 10000 {
		return nil, subtitles.ErrFormat
	}
	cues := make([]subtitles.Cue, 0, len(transcript.Segments))
	for _, segment := range transcript.Segments {
		text := strings.TrimSpace(segment.Body)
		if segment.Start == nil || segment.End == nil || math.IsNaN(*segment.Start) || math.IsInf(*segment.Start, 0) || math.IsNaN(*segment.End) || math.IsInf(*segment.End, 0) || *segment.Start < 0 || *segment.End <= *segment.Start || *segment.End > 7*24*3600 || text == "" || len(text) > 8000 {
			return nil, subtitles.ErrFormat
		}
		cues = append(cues, subtitles.Cue{Start: *segment.Start, End: *segment.End, Text: text})
	}
	sort.SliceStable(cues, func(i, j int) bool { return cues[i].Start < cues[j].Start })
	return cues, nil
}
