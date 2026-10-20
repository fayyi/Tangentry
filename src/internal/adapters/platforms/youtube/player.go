package youtube

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const playerTrackLimit = 200

type CaptionTrack struct {
	Language string
	Label    string
	URL      string
	Kind     string
}

type PlayerResult struct {
	Tracks []CaptionTrack
	Error  string
}

// Player retrieves native caption tracks from a player response. Credentials
// are only sent to the fixed YouTube origin and redirects are never followed.
type Player struct {
	HTTP      *http.Client
	Now       func() time.Time
	UserAgent string
}

func NewPlayer(client *http.Client) *Player {
	if client == nil {
		client = http.DefaultClient
	}
	cloned := *client
	cloned.Jar = nil
	cloned.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Player{HTTP: &cloned}
}

var playerVideoID = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)
var bcp47Language = regexp.MustCompile(`^[A-Za-z]{2,8}(?:-[A-Za-z0-9]{1,8})*$`)

func (p *Player) Fetch(ctx context.Context, videoID, cookie string) PlayerResult {
	if !playerVideoID.MatchString(videoID) {
		return PlayerResult{Error: "invalid"}
	}
	if ctx.Err() != nil {
		return PlayerResult{Error: "network"}
	}
	client := p.HTTP
	if client == nil {
		client = NewPlayer(nil).HTTP
	}
	client = clonePlayerHTTP(client)
	now := time.Now
	if p.Now != nil {
		now = p.Now
	}
	r := &requester{http: client, cookie: cookie, now: now, userAgent: p.UserAgent}
	session := r.newSession(ctx)
	bootstrapError := r.code
	if ctx.Err() != nil {
		return PlayerResult{Error: "network"}
	}
	payload, err := json.Marshal(map[string]any{
		"videoId":        videoID,
		"context":        session.Context,
		"contentCheckOk": true,
		"racyCheckOk":    true,
	})
	if err != nil {
		return PlayerResult{Error: "network"}
	}
	query := url.Values{"prettyPrint": {"false"}, "alt": {"json"}}
	if session.APIKey != "" && len(session.APIKey) <= 256 {
		query.Set("key", session.APIKey)
	}
	endpoint := &url.URL{Scheme: "https", Host: "www.youtube.com", Path: "/youtubei/" + session.APIVersion + "/player", RawQuery: query.Encode()}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(payload))
	if err != nil {
		return PlayerResult{Error: "network"}
	}
	clientContext, _ := session.Context["client"].(map[string]any)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Language", "zh-CN")
	req.Header.Set("User-Agent", firstNonEmpty(stringAt(clientContext, "userAgent"), p.UserAgent, innertubeUserAgent))
	req.Header.Set("Origin", innertubeBase)
	req.Header.Set("X-Goog-Visitor-Id", stringAt(clientContext, "visitorData"))
	req.Header.Set("X-Youtube-Client-Version", stringAt(clientContext, "clientVersion"))
	req.Header.Set("X-Youtube-Client-Name", "1")
	signRequest(req.Header, req.URL, cookie, now())
	r.code = "network"
	raw, err := r.do(ctx, req)
	if err != nil {
		return PlayerResult{Error: r.code}
	}
	var data map[string]any
	if json.Unmarshal(raw, &data) != nil {
		if restrictedSignal(string(raw)) {
			return PlayerResult{Error: "restricted"}
		}
		if loginSignal(string(raw)) {
			return PlayerResult{Error: "auth"}
		}
		return PlayerResult{Error: "format"}
	}
	if rejectedCookieSession(data, cookie) {
		return PlayerResult{Error: "auth"}
	}
	playability, _ := path(data, "playabilityStatus").(map[string]any)
	status := strings.ToUpper(stringAt(playability, "status"))
	reason := strings.ToLower(strings.Join(reasonStrings(playability), " "))
	if strings.Contains(reason, "confirm you're not a bot") || strings.Contains(reason, "confirm you’re not a bot") || strings.Contains(reason, "verify you are") || strings.Contains(reason, "unusual traffic") {
		return PlayerResult{Error: "restricted"}
	}
	if status == "LOGIN_REQUIRED" || status == "AGE_VERIFICATION_REQUIRED" || status == "AGE_CHECK_REQUIRED" || strings.Contains(reason, "sign in") || strings.Contains(reason, "log in") || strings.Contains(reason, "age-restricted") {
		return PlayerResult{Error: "auth"}
	}
	if status == "ERROR" {
		return PlayerResult{Error: "network"}
	}
	if status != "" && status != "OK" {
		return PlayerResult{Error: "network"}
	}
	if stringAtValue(path(data, "videoDetails", "videoId")) != videoID {
		return PlayerResult{Error: "format"}
	}
	tracksValue := path(data, "captions", "playerCaptionsTracklistRenderer", "captionTracks")
	if tracksValue == nil {
		if bootstrapError == "auth" || bootstrapError == "restricted" {
			return PlayerResult{Error: bootstrapError}
		}
		return PlayerResult{Tracks: []CaptionTrack{}}
	}
	items, ok := tracksValue.([]any)
	if !ok || len(items) > playerTrackLimit {
		return PlayerResult{Error: "format"}
	}
	tracks := make([]CaptionTrack, 0, len(items))
	for _, item := range items {
		track, ok := item.(map[string]any)
		if !ok {
			return PlayerResult{Error: "format"}
		}
		kind := stringAt(track, "kind")
		if kind != "" && kind != "asr" {
			continue
		}
		language := stringAt(track, "languageCode")
		if !bcp47Language.MatchString(language) || language == "live_chat" || len(language) > 35 {
			continue
		}
		baseURL := stringAt(track, "baseUrl")
		parsed, err := url.Parse(baseURL)
		if err != nil || !validTimedTextURL(parsed) {
			continue
		}
		query, err := url.ParseQuery(parsed.RawQuery)
		if err != nil || len(query["tlang"]) != 0 || len(query["lang"]) != 1 || query.Get("lang") != language || len(query["v"]) != 1 || query.Get("v") != videoID {
			continue
		}
		label := captionTrackLabel(track, language)
		tracks = append(tracks, CaptionTrack{Language: language, Label: label, URL: parsed.String(), Kind: kind})
	}
	return PlayerResult{Tracks: tracks}
}

func captionTrackLabel(track map[string]any, fallback string) string {
	name, _ := track["name"].(map[string]any)
	if label := strings.TrimSpace(stringAt(name, "simpleText")); label != "" && len(label) <= 120 {
		return label
	}
	if runs, ok := name["runs"].([]any); ok {
		var label strings.Builder
		for _, value := range runs {
			run, _ := value.(map[string]any)
			label.WriteString(stringAt(run, "text"))
			if label.Len() > 120 {
				return fallback
			}
		}
		if value := strings.TrimSpace(label.String()); value != "" {
			return value
		}
	}
	return fallback
}

func stringAtValue(value any) string {
	result, _ := value.(string)
	return result
}

func clonePlayerHTTP(client *http.Client) *http.Client {
	clone := *client
	clone.Jar = nil
	clone.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &clone
}

func validTimedTextURL(u *url.URL) bool {
	return u.Scheme == "https" && u.User == nil && u.Port() == "" && (u.Hostname() == "www.youtube.com" || u.Hostname() == "youtube.com") && u.Path == "/api/timedtext" && u.Fragment == ""
}

func reasonStrings(value any) []string {
	var values []string
	switch item := value.(type) {
	case string:
		values = append(values, item)
	case map[string]any:
		for key, nested := range item {
			if key == "reason" || key == "subreason" {
				values = append(values, reasonStrings(nested)...)
			}
		}
	case []any:
		for _, nested := range item {
			values = append(values, reasonStrings(nested)...)
		}
	}
	return values
}

func restrictedSignal(value string) bool {
	lower := strings.ToLower(value)
	return strings.Contains(lower, "not a bot") || strings.Contains(lower, "unusual traffic") || strings.Contains(lower, "verify you are") || strings.Contains(lower, "confirm you're not a bot") || strings.Contains(lower, "confirm you’re not a bot") || strings.Contains(lower, "automated queries")
}
