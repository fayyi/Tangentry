package xiaoyuzhou

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"

	"tangentry/internal/discovery"
	"tangentry/internal/subtitles"
)

const appAgent = "Xiaoyuzhou/2.98.0 (build:2908; iOS 26.2.1)"

func setAppHeaders(req *http.Request, access string) {
	req.Header.Set("User-Agent", appAgent)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Market", "AppStore")
	req.Header.Set("App-BuildNo", "2908")
	req.Header.Set("OS", "ios")
	req.Header.Set("BundleID", "app.podcast.cosmos")
	req.Header.Set("App-Version", "2.98.0")
	req.Header.Set("x-jike-device-id", "81ADBFD6-6921-482B-9AB9-A29E7CC7BB55")
	if access != "" {
		req.Header.Set("x-jike-access-token", access)
	}
}

// Transcript uses the user's App access token only against the official API.
func (c *Client) Transcript(ctx context.Context, id, token string) (subtitles.Document, error) {
	doc := subtitles.Document{VideoID: "xiaoyuzhou:" + id, Part: 1, Status: "none", Tracks: []subtitles.Track{}}
	if !episodeID.MatchString(id) {
		return doc, subtitles.ErrInvalid
	}
	if token == "" {
		return doc, nil
	}
	fetch := func(method, raw string, body []byte, auth bool) ([]byte, error) {
		req, _ := http.NewRequestWithContext(ctx, method, raw, bytes.NewReader(body))
		req.Header.Set("User-Agent", appAgent)
		if auth {
			setAppHeaders(req, token)
		}
		client := *c.HTTP
		// Never forward tokens or signed resources through a redirect.
		client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		var res *http.Response
		var err error
		if auth && c.Auth != nil {
			res, err = c.Auth.Do(ctx, req, token)
		} else {
			res, err = client.Do(req)
		}
		if err != nil {
			return nil, subtitles.ErrUnavailable
		}
		defer res.Body.Close()
		if res.StatusCode == 401 {
			return nil, subtitles.ErrLogin
		}
		if res.StatusCode == 403 || res.StatusCode == 429 {
			return nil, subtitles.ErrRestricted
		}
		if res.StatusCode != 200 {
			if auth && res.StatusCode == 400 {
				return nil, &discovery.PlatformError{Message: "小宇宙文稿接口暂时拒绝请求；可稍后重试，或使用本机转录。已保存文稿不受影响。"}
			}
			return nil, subtitles.ErrUnavailable
		}
		data, err := io.ReadAll(io.LimitReader(res.Body, (2<<20)+1))
		if err != nil || len(data) > 2<<20 {
			return nil, subtitles.ErrFormat
		}
		return data, nil
	}
	raw, err := fetch("GET", "https://api.xiaoyuzhoufm.com/v1/episode/get?eid="+id, nil, true)
	if err != nil {
		return doc, err
	}
	var episode struct {
		Data struct {
			ID                string  `json:"eid"`
			Duration          float64 `json:"duration"`
			TranscriptMediaID string  `json:"transcriptMediaId"`
			Transcript        struct {
				MediaID string `json:"mediaId"`
				URL     string `json:"url"`
			} `json:"transcript"`
			Media struct {
				ID string `json:"id"`
			} `json:"media"`
			Episode *struct {
				ID                string  `json:"eid"`
				Duration          float64 `json:"duration"`
				TranscriptMediaID string  `json:"transcriptMediaId"`
				Transcript        struct {
					MediaID string `json:"mediaId"`
				} `json:"transcript"`
				Media struct {
					ID string `json:"id"`
				} `json:"media"`
			} `json:"episode"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &episode) != nil {
		return doc, subtitles.ErrFormat
	}
	if episode.Data.ID == "" && episode.Data.Episode != nil {
		episode.Data.ID = episode.Data.Episode.ID
		episode.Data.Duration = episode.Data.Episode.Duration
		episode.Data.TranscriptMediaID = episode.Data.Episode.TranscriptMediaID
		episode.Data.Transcript.MediaID = episode.Data.Episode.Transcript.MediaID
		episode.Data.Media = episode.Data.Episode.Media
	}
	if episode.Data.ID != id {
		return doc, subtitles.ErrFormat
	}
	media := episode.Data.TranscriptMediaID
	if media == "" {
		media = episode.Data.Transcript.MediaID
	}
	if media == "" {
		media = episode.Data.Media.ID
	}
	if media == "" {
		return doc, nil
	}
	body, _ := json.Marshal(map[string]string{"eid": id, "mediaId": media})
	raw, err = fetch("POST", "https://api.xiaoyuzhoufm.com/v1/episode-transcript/get", body, true)
	if err != nil {
		// Older app deployments use episodeId instead of eid. Retry only the
		// schema error; auth and rate-limit failures should remain visible.
		if _, ok := err.(*discovery.PlatformError); ok {
			body, _ = json.Marshal(map[string]string{"episodeId": id, "mediaId": media})
			raw, err = fetch("POST", "https://api.xiaoyuzhoufm.com/v1/episode-transcript/get", body, true)
		}
	}
	if err != nil {
		return doc, err
	}
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if json.Unmarshal(raw, &envelope) != nil {
		return doc, subtitles.ErrFormat
	}
	var locator struct {
		URL  string `json:"transcriptUrl"`
		Alt  string `json:"url"`
		Data *struct {
			URL string `json:"transcriptUrl"`
			Alt string `json:"url"`
		} `json:"data"`
	}
	if json.Unmarshal(envelope.Data, &locator) != nil {
		return doc, subtitles.ErrFormat
	}
	if locator.Data != nil {
		locator.URL = locator.Data.URL
		locator.Alt = locator.Data.Alt
	}
	if locator.URL == "" {
		locator.URL = locator.Alt
	}
	if locator.URL == "" {
		return doc, nil
	}
	u, err := url.Parse(locator.URL)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" || !(u.Hostname() == "xiaoyuzhoufm.com" || strings.HasSuffix(u.Hostname(), ".xiaoyuzhoufm.com") || u.Hostname() == "xyzcdn.net" || strings.HasSuffix(u.Hostname(), ".xyzcdn.net")) {
		return doc, subtitles.ErrFormat
	}
	raw, err = fetch("GET", locator.URL, nil, false)
	if err != nil {
		return doc, err
	}
	cues, err := parseTranscript(raw, episode.Data.Duration)
	if err != nil {
		return doc, err
	}
	doc.Status = "available"
	doc.Tracks = []subtitles.Track{{Language: "zh-Hans", Label: "简体中文 · 平台文稿", Automatic: true, Cues: cues}}
	return doc, nil
}

func parseTranscript(raw []byte, duration float64) ([]subtitles.Cue, error) {
	var segments []struct {
		Start *float64 `json:"startMs"`
		Text  string   `json:"text"`
	}
	if json.Unmarshal(raw, &segments) != nil || len(segments) == 0 || len(segments) > 10000 || duration <= 0 || duration > 7*86400 {
		return nil, subtitles.ErrFormat
	}
	cues := make([]subtitles.Cue, 0, len(segments))
	for _, segment := range segments {
		if strings.TrimSpace(segment.Text) == "" {
			continue
		}
		if segment.Start == nil || *segment.Start < 0 || *segment.Start/1000 >= duration || len(segment.Text) > 8000 {
			return nil, subtitles.ErrFormat
		}
		start := *segment.Start / 1000
		if len(cues) > 0 {
			if start <= cues[len(cues)-1].Start {
				return nil, subtitles.ErrFormat
			}
			cues[len(cues)-1].End = start
		}
		cues = append(cues, subtitles.Cue{Start: start, End: duration, Text: segment.Text})
	}
	if len(cues) == 0 {
		return nil, subtitles.ErrFormat
	}
	return cues, nil
}
