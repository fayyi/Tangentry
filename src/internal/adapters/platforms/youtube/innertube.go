package youtube

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// InnerTube protocol constants for the desktop web client. They mirror the
// values YouTube.js used before the Node helper was replaced.
const (
	innertubeBase       = "https://www.youtube.com"
	innertubeAPIVersion = "v1"
	innertubeVersion    = "2.20260623.01.00"
	innertubeUserAgent  = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Safari/537.36"
	innertubeBodyLimit  = 8 << 20
	innertubeTimeout    = 12 * time.Second
)

// innertubeError carries the diagnosis reported to the toolkit. Hard errors
// (a rejected cookie session or an oversized batch) stop fallbacks.
type innertubeError struct {
	code string
	hard bool
}

func (e *innertubeError) Error() string { return "youtube innertube: " + e.code }

func hardError(err error) bool {
	var failure *innertubeError
	return errors.As(err, &failure) && failure.hard
}

type session struct {
	Context      map[string]any
	APIKey       string
	APIVersion   string
	AccountIndex int
}

// requester performs InnerTube requests for one helper invocation and keeps the
// latest transport diagnosis, as the Node helper's fetch wrapper did.
type requester struct {
	http      *http.Client
	cookie    string
	now       func() time.Time
	code      string
	userAgent string
}

func youtubeHost(u *url.URL) bool {
	return u.Scheme == "https" && u.User == nil && u.Port() == "" && (u.Hostname() == "www.youtube.com" || u.Hostname() == "youtube.com")
}

var libraryAuthorization = regexp.MustCompile(`^SAPISIDHASH \d+_[a-f0-9]+$`)

// signRequest attaches saved cookies and SAPISID signatures to InnerTube API
// requests only. It matches yt-dlp's first/third-party signatures as well as
// the secure SID fallback, and never replaces OAuth authorization.
func signRequest(headers http.Header, u *url.URL, cookie string, now time.Time) {
	if !youtubeHost(u) || !strings.HasPrefix(u.Path, "/youtubei/v1/") {
		return
	}
	if cookie != "" {
		headers.Set("Cookie", cookie)
	}
	if existing := headers.Get("Authorization"); existing != "" && !libraryAuthorization.MatchString(existing) {
		return
	}
	values := cookieValues(cookie)
	timestamp := now.Unix()
	var signatures []string
	for _, candidate := range [][2]string{
		{"SAPISIDHASH", firstNonEmpty(values["SAPISID"], values["__Secure-3PAPISID"])},
		{"SAPISID1PHASH", values["__Secure-1PAPISID"]},
		{"SAPISID3PHASH", values["__Secure-3PAPISID"]},
	} {
		if candidate[1] == "" {
			continue
		}
		sum := sha1.Sum([]byte(fmt.Sprintf("%d %s %s", timestamp, candidate[1], innertubeBase)))
		signatures = append(signatures, fmt.Sprintf("%s %d_%s", candidate[0], timestamp, hex.EncodeToString(sum[:])))
	}
	if len(signatures) == 0 {
		return
	}
	headers.Set("Authorization", strings.Join(signatures, " "))
	headers.Set("X-Origin", innertubeBase)
	if headers.Get("X-Goog-Authuser") == "" {
		headers.Set("X-Goog-Authuser", "0")
	}
}

func cookieValues(cookie string) map[string]string {
	values := map[string]string{}
	for _, part := range strings.Split(cookie, ";") {
		name, value, ok := strings.Cut(part, "=")
		if !ok || strings.TrimSpace(name) == "" {
			continue
		}
		values[strings.TrimSpace(name)] = strings.TrimSpace(value)
	}
	return values
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// rejectedCookieSession reports HTTP 200 responses that ignore saved cookies.
func rejectedCookieSession(data map[string]any, cookie string) bool {
	if cookie == "" {
		return false
	}
	loggedOut, _ := path(data, "responseContext", "mainAppWebResponseContext", "loggedOut").(bool)
	return loggedOut
}

func (r *requester) do(ctx context.Context, req *http.Request) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, innertubeTimeout)
	defer cancel()
	response, err := r.http.Do(req.WithContext(ctx))
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	switch response.StatusCode {
	case http.StatusUnauthorized:
		r.code = "auth"
	case http.StatusForbidden:
		r.code = "auth"
	case http.StatusTooManyRequests:
		r.code = "restricted"
	}
	if response.StatusCode < 200 || response.StatusCode > 299 {
		if response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusTooManyRequests {
			body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
			if restrictedSignal(string(body)) {
				r.code = "restricted"
			} else if response.StatusCode == http.StatusForbidden && loginSignal(string(body)) {
				r.code = "auth"
			}
		}
		return nil, fmt.Errorf("youtube request failed: HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, innertubeBodyLimit+1))
	if err != nil {
		return nil, err
	}
	if len(body) > innertubeBodyLimit {
		return nil, errors.New("youtube response is too large")
	}
	return body, nil
}

func loginSignal(value string) bool {
	lower := strings.ToLower(value)
	return strings.Contains(lower, "sign in") || strings.Contains(lower, "log in") || strings.Contains(lower, "login required") || strings.Contains(lower, "authentication required") || strings.Contains(lower, "age verification required")
}

// browse posts one InnerTube browse request with the session context.
func (r *requester) browse(ctx context.Context, s session, payload map[string]any) (map[string]any, error) {
	payload["context"] = s.Context
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	endpoint := &url.URL{Scheme: "https", Host: "www.youtube.com", Path: "/youtubei/" + s.APIVersion + "/browse", RawQuery: "prettyPrint=false&alt=json"}
	req, err := http.NewRequest(http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	client, _ := s.Context["client"].(map[string]any)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Language", "*")
	req.Header.Set("User-Agent", firstNonEmpty(stringAt(client, "userAgent"), r.userAgent, innertubeUserAgent))
	req.Header.Set("Origin", innertubeBase)
	req.Header.Set("X-Goog-Visitor-Id", stringAt(client, "visitorData"))
	req.Header.Set("X-Youtube-Client-Version", stringAt(client, "clientVersion"))
	req.Header.Set("X-Youtube-Client-Name", "1")
	if r.cookie != "" && s.AccountIndex > 0 {
		req.Header.Set("X-Goog-Authuser", strconv.Itoa(s.AccountIndex))
	}
	signRequest(req.Header, req.URL, r.cookie, r.now())
	raw, err := r.do(ctx, req)
	if err != nil {
		return nil, err
	}
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, err
	}
	// Invalid cookies often receive HTTP 200 with an anonymous home nudge.
	if rejectedCookieSession(data, r.cookie) {
		r.code = "auth"
		return nil, &innertubeError{code: "auth", hard: true}
	}
	return data, nil
}

// newSession bootstraps visitor data from YouTube's service worker data, and
// falls back to a locally generated session as YouTube.js did.
func (r *requester) newSession(ctx context.Context) session {
	zone := localTimeZone()
	// YouTube's current public client key is read from sw.js_data below. Keep
	// the offline fallback free of embedded credentials; the key is not needed
	// by the context-only request path.
	fallback := session{APIVersion: innertubeAPIVersion, Context: buildContext(contextData{
		hl: "zh-CN", gl: "US", visitorData: localVisitorData(r.now()), clientVersion: innertubeVersion,
		osName: "Windows", osVersion: "10.0", timeZone: zone, browserName: "Chrome", browserVersion: "125.0.0.0",
	}, r.now())}
	req, err := http.NewRequest(http.MethodGet, innertubeBase+"/sw.js_data", nil)
	if err != nil {
		return fallback
	}
	req.Header.Set("Accept-Language", "zh-CN")
	req.Header.Set("User-Agent", firstNonEmpty(r.userAgent, innertubeUserAgent))
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Referer", innertubeBase+"/sw.js")
	cookies := "PREF=tz=" + strings.Replace(zone, "/", ".", 1) + "; VISITOR_INFO1_LIVE=" + randomString(11)
	if r.cookie != "" {
		cookies = mergeCookies(cookies, r.cookie)
	}
	req.Header.Set("Cookie", cookies)
	raw, err := r.do(ctx, req)
	if err != nil || !bytes.HasPrefix(raw, []byte(")]}'")) || len(raw) < 5 {
		return fallback
	}
	var data []any
	if json.Unmarshal(raw[5:], &data) != nil {
		return fallback
	}
	ytcfg, _ := index(index(data, 0), 2).([]any)
	device, _ := index(index(ytcfg, 0), 0).([]any)
	apiKey, _ := index(ytcfg, 1).(string)
	visitor, _ := index(device, 13).(string)
	version, _ := index(device, 16).(string)
	if device == nil || visitor == "" || version == "" {
		return fallback
	}
	text := func(i int) string { value, _ := index(device, i).(string); return value }
	installData := ""
	if config, ok := index(device, 61).([]any); ok && len(config) > 0 {
		installData, _ = config[len(config)-1].(string)
	}
	return session{APIKey: apiKey, APIVersion: innertubeAPIVersion, Context: buildContext(contextData{
		hl: "zh-CN", gl: "US", remoteHost: text(3), visitorData: visitor, clientVersion: version,
		osName: text(17), osVersion: text(18), timeZone: firstNonEmpty(text(79), zone), browserName: text(86), browserVersion: text(87),
		deviceMake: text(11), deviceModel: text(12), appInstallData: installData, deviceExperimentID: text(103), rolloutToken: text(107),
	}, r.now())}
}

// mergeCookies lets saved cookies replace bootstrap defaults with the same name.
func mergeCookies(base, saved string) string {
	var order []string
	values := map[string]string{}
	for _, part := range strings.Split(base+";"+saved, ";") {
		name, value, _ := strings.Cut(part, "=")
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, seen := values[name]; !seen {
			order = append(order, name)
		}
		values[name] = strings.TrimSpace(value)
	}
	parts := make([]string, 0, len(order))
	for _, name := range order {
		parts = append(parts, name+"="+values[name])
	}
	return strings.Join(parts, "; ")
}
