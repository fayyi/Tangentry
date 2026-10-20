package bilibili

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"tangentry/internal/discovery"
	"tangentry/internal/subtitles"
)

const maxSubtitleParts = 100
const maxSubtitleTracks = 32
const maxSubtitleBody = 2 << 20
const maxSubtitleBodies = 16 << 20

var errNoSubtitleData = errors.New("subtitle API returned no subtitle data")

var subtitleCredentialText = regexp.MustCompile(`(?i)(cookie|sessdata|bili_jct|dedeuserid(?:__ckmd5)?|buvid3|sid|token|csrf|authorization)\s*[:=]\s*[^\s,;]+`)
var subtitleURLText = regexp.MustCompile(`(?i)https?://[^\s<>"']+`)

var subtitleCDNHosts = map[string]bool{
	"aisubtitle.hdslb.com": true,
	"i0.hdslb.com":         true,
	"i1.hdslb.com":         true,
	"i2.hdslb.com":         true,
	"subtitle.hdslb.com":   true,
}

type playerSubtitleResponse struct {
	Subtitle *struct {
		List      *[]playerSubtitle `json:"list"`
		Subtitles *[]playerSubtitle `json:"subtitles"`
	} `json:"subtitle"`
}

func (r playerSubtitleResponse) hasTrackList() bool {
	return r.Subtitle != nil && (r.Subtitle.Subtitles != nil || r.Subtitle.List != nil)
}

func (r playerSubtitleResponse) candidates() []playerSubtitle {
	if !r.hasTrackList() {
		return nil
	}
	var subtitlesList, list []playerSubtitle
	if r.Subtitle.Subtitles != nil {
		subtitlesList = *r.Subtitle.Subtitles
	}
	if r.Subtitle.List != nil {
		list = *r.Subtitle.List
	}
	candidates := make([]playerSubtitle, 0, len(subtitlesList)+len(list))
	seen := make(map[playerSubtitle]struct{}, cap(candidates))
	for _, group := range [][]playerSubtitle{subtitlesList, list} {
		for _, candidate := range group {
			if _, ok := seen[candidate]; ok {
				continue
			}
			seen[candidate] = struct{}{}
			candidates = append(candidates, candidate)
		}
	}
	return candidates
}

type playerSubtitle struct {
	Language string `json:"lan"`
	Label    string `json:"lan_doc"`
	URL      string `json:"subtitle_url"`
	Type     int    `json:"type"`
	AIType   int    `json:"ai_type"`
	AIStatus int    `json:"ai_status"`
}

type subtitleView struct {
	CID   int64  `json:"cid"`
	Title string `json:"title"`
	Pages []struct {
		CID   int64  `json:"cid"`
		Page  int    `json:"page"`
		Title string `json:"part"`
	} `json:"pages"`
}

type subtitleCuePayload struct {
	From    float64 `json:"from"`
	To      float64 `json:"to"`
	Content string  `json:"content"`
}

func (c *Client) ReadSubtitles(ctx context.Context, bvid string) (subtitles.Document, error) {
	return c.readSubtitles(ctx, bvid, "")
}

func (c *Client) ReadSubtitlesLanguage(ctx context.Context, bvid, language string) (subtitles.Document, error) {
	if !subtitleLanguageCode.MatchString(language) {
		return subtitles.Document{}, subtitles.ErrInvalid
	}
	return c.readSubtitles(ctx, bvid, language)
}

var subtitleLanguageCode = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

func (c *Client) readSubtitles(ctx context.Context, bvid, selectedLanguage string) (subtitles.Document, error) {
	if len(bvid) != 12 || !strings.HasPrefix(bvid, "BV") {
		return subtitles.Document{}, subtitles.ErrInvalid
	}
	var view subtitleView
	if err := c.get(ctx, "/x/web-interface/view", url.Values{"bvid": {bvid}}, &view, false); err != nil {
		if ctx.Err() != nil {
			return subtitles.Document{}, ctx.Err()
		}
		status := "error"
		if isSubtitleRateLimited(err) {
			status = "rate_limited"
		}
		code, message := platformSubtitleError(err)
		return subtitles.Document{VideoID: bvid, Part: 1, Status: status, ErrorCode: code, ErrorMessage: message, Tracks: []subtitles.Track{}}, nil
	}
	parts := view.Pages
	if len(parts) == 0 && view.CID > 0 {
		parts = []struct {
			CID   int64  `json:"cid"`
			Page  int    `json:"page"`
			Title string `json:"part"`
		}{{CID: view.CID, Page: 1, Title: view.Title}}
	}
	if len(parts) == 0 || len(parts) > maxSubtitleParts {
		return subtitles.Document{}, &discovery.PlatformError{Message: "B站没有返回有效的分 P 信息，请重试。"}
	}
	if selectedLanguage != "" {
		return c.readSelectedSubtitleLanguage(ctx, bvid, view, parts[0], selectedLanguage)
	}

	doc := subtitles.Document{VideoID: bvid, Part: 1, Status: "error", Tracks: []subtitles.Track{}, Parts: make([]subtitles.Part, 0, len(parts))}
	coolingDown := false
	var restrictionCode int
	remainingBodyBytes := maxSubtitleBodies
	for i, page := range parts {
		part := subtitles.Part{CID: page.CID, Page: page.Page, Title: safeSubtitleText(page.Title, 120), Status: "error", Languages: []subtitles.Language{}, Tracks: []subtitles.Track{}}
		if part.Page < 1 {
			part.Page = i + 1
		}
		if page.CID <= 0 {
			part.ErrorMessage = "B站没有返回这一个分 P 的 CID。"
			doc.Parts = append(doc.Parts, part)
			continue
		}
		if coolingDown {
			part.Status = "rate_limited"
			part.ErrorMessage = "B站暂时限制访问（" + strconv.Itoa(restrictionCode) + "），请稍后重试。"
			part.ErrorCode = restrictionCode
			doc.Parts = append(doc.Parts, part)
			continue
		}
		title := safeSubtitleText(view.Title+" "+page.Title, 240)
		result, err := c.readSubtitlePart(ctx, bvid, page.CID, title, i == 0, selectedLanguage, &remainingBodyBytes)
		if err != nil {
			if ctx.Err() != nil {
				return subtitles.Document{}, ctx.Err()
			}
			part.ErrorCode, part.ErrorMessage = platformSubtitleError(err)
			if isSubtitleRateLimited(err) {
				part.Status = "rate_limited"
				coolingDown = true
				restrictionCode = part.ErrorCode
			}
			doc.Parts = append(doc.Parts, part)
			continue
		}
		part.Languages, part.Tracks, part.Status, part.AIMismatch = result.Languages, result.Tracks, result.Status, result.AIMismatch
		part.ErrorCode, part.ErrorMessage = result.ErrorCode, result.ErrorMessage
		doc.Parts = append(doc.Parts, part)
	}
	if len(doc.Parts) > 0 {
		first := doc.Parts[0]
		doc.Part, doc.Status, doc.ErrorCode, doc.ErrorMessage = first.Page, first.Status, first.ErrorCode, first.ErrorMessage
		doc.Languages, doc.Tracks = first.Languages, first.Tracks
	}
	return doc, nil
}

func (c *Client) readSelectedSubtitleLanguage(ctx context.Context, bvid string, view subtitleView, page struct {
	CID   int64  `json:"cid"`
	Page  int    `json:"page"`
	Title string `json:"part"`
}, language string) (subtitles.Document, error) {
	part := subtitles.Part{CID: page.CID, Page: page.Page, Title: safeSubtitleText(page.Title, 120), Status: "error", Languages: []subtitles.Language{}, Tracks: []subtitles.Track{}}
	if part.Page < 1 {
		part.Page = 1
	}
	if page.CID <= 0 {
		part.ErrorMessage = "B站没有返回第一分 P 的 CID。"
	} else {
		title := safeSubtitleText(view.Title+" "+page.Title, 240)
		result, err := c.readSubtitlePart(ctx, bvid, page.CID, title, true, language, nil)
		if err != nil {
			if ctx.Err() != nil {
				return subtitles.Document{}, ctx.Err()
			}
			part.ErrorCode, part.ErrorMessage = platformSubtitleError(err)
		} else {
			part.Languages, part.Tracks, part.Status, part.AIMismatch = result.Languages, result.Tracks, result.Status, result.AIMismatch
			part.ErrorCode, part.ErrorMessage = result.ErrorCode, result.ErrorMessage
		}
	}
	return subtitles.Document{
		VideoID: bvid, Part: part.Page, Status: part.Status, ErrorCode: part.ErrorCode, ErrorMessage: part.ErrorMessage,
		Languages: part.Languages, Tracks: part.Tracks, Parts: []subtitles.Part{part},
	}, nil
}

func (c *Client) readSubtitlePart(ctx context.Context, bvid string, cid int64, title string, readBodies bool, selectedLanguage string, remainingBodyBytes *int) (subtitles.Part, error) {
	params := url.Values{"bvid": {bvid}, "cid": {strconv.FormatInt(cid, 10)}}
	first, firstErr := c.playerSubtitles(ctx, "/x/player/v2", params)
	if isSubtitleRateLimited(firstErr) {
		return subtitles.Part{}, firstErr
	}
	var firstCandidates []playerSubtitle
	var firstPart subtitles.Part
	firstPartBuilt := false
	if firstErr == nil {
		firstCandidates = first.candidates()
	}
	if len(firstCandidates) > 0 {
		if hasSubtitleURL(firstCandidates) {
			firstPart = c.buildSubtitlePart(ctx, firstCandidates, title, readBodies, selectedLanguage, remainingBodyBytes)
			firstPartBuilt = true
			// A declared URL can still yield an empty/invalid payload. Treat that
			// as missing subtitle data and give the signed endpoint a chance.
			if !readBodies || len(firstPart.Tracks) > 0 {
				return firstPart, nil
			}
		}
	}

	key, keyErr := c.mixinKey(ctx)
	if keyErr != nil {
		if isSubtitleRateLimited(keyErr) {
			return subtitles.Part{}, keyErr
		}
		if firstErr != nil {
			return subtitles.Part{}, firstErr
		}
		if len(firstCandidates) > 0 {
			return c.subtitleFallbackError(ctx, firstCandidates, title, keyErr, readBodies, selectedLanguage, remainingBodyBytes), nil
		}
		return subtitles.Part{}, keyErr
	}
	signed := url.Values{"bvid": {bvid}, "cid": {strconv.FormatInt(cid, 10)}}
	sign(signed, key, time.Now().Unix())
	second, secondErr := c.playerSubtitles(ctx, "/x/player/wbi/v2", signed)
	if secondErr != nil {
		if isSubtitleRateLimited(secondErr) {
			return subtitles.Part{}, secondErr
		}
		if len(firstCandidates) > 0 {
			if errors.Is(secondErr, errNoSubtitleData) {
				if firstPartBuilt {
					return firstPart, nil
				}
				return c.buildSubtitlePart(ctx, firstCandidates, title, readBodies, selectedLanguage, remainingBodyBytes), nil
			}
			return c.subtitleFallbackError(ctx, firstCandidates, title, secondErr, readBodies, selectedLanguage, remainingBodyBytes), nil
		}
		if errors.Is(secondErr, errNoSubtitleData) && firstErr == nil && first.hasTrackList() {
			return c.buildSubtitlePart(ctx, firstCandidates, title, readBodies, selectedLanguage, remainingBodyBytes), nil
		}
		if firstErr != nil && !errors.Is(firstErr, errNoSubtitleData) {
			return subtitles.Part{}, firstErr
		}
		return subtitles.Part{}, secondErr
	}
	if !second.hasTrackList() {
		if len(firstCandidates) > 0 {
			if firstPartBuilt {
				return firstPart, nil
			}
			return c.buildSubtitlePart(ctx, firstCandidates, title, readBodies, selectedLanguage, remainingBodyBytes), nil
		}
		return subtitles.Part{}, errors.New("subtitle API returned no subtitle data")
	}
	secondCandidates := second.candidates()
	if len(secondCandidates) == 0 {
		if len(firstCandidates) > 0 {
			if firstPartBuilt {
				return firstPart, nil
			}
			return c.buildSubtitlePart(ctx, firstCandidates, title, readBodies, selectedLanguage, remainingBodyBytes), nil
		}
		if firstErr != nil && !errors.Is(firstErr, errNoSubtitleData) {
			return subtitles.Part{}, firstErr
		}
		return c.buildSubtitlePart(ctx, secondCandidates, title, readBodies, selectedLanguage, remainingBodyBytes), nil
	}
	return c.buildSubtitlePart(ctx, secondCandidates, title, readBodies, selectedLanguage, remainingBodyBytes), nil
}

func (c *Client) subtitleFallbackError(ctx context.Context, candidates []playerSubtitle, title string, err error, readBodies bool, selectedLanguage string, remainingBodyBytes *int) subtitles.Part {
	part := c.buildSubtitlePart(ctx, candidates, title, readBodies, selectedLanguage, remainingBodyBytes)
	part.Status = "error"
	part.ErrorCode, part.ErrorMessage = platformSubtitleError(err)
	return part
}

func (c *Client) playerSubtitles(ctx context.Context, endpoint string, params url.Values) (playerSubtitleResponse, error) {
	body, err := c.fetchEnvelope(ctx, c.baseURL, "https://www.bilibili.com/", endpoint, params)
	if err != nil {
		return playerSubtitleResponse{}, err
	}
	if body.Code != 0 {
		if body.Code == -352 || body.Code == -412 || body.Code == -799 || body.Code == -509 {
			return playerSubtitleResponse{}, c.restrictedAPI(body.Code)
		}
		return playerSubtitleResponse{}, apiSubtitleError{code: body.Code, message: body.Message}
	}
	if len(body.Data) == 0 || string(body.Data) == "null" {
		return playerSubtitleResponse{}, errors.New("subtitle API returned no data")
	}
	var response playerSubtitleResponse
	if json.Unmarshal(body.Data, &response) != nil {
		return playerSubtitleResponse{}, errors.New("subtitle API response could not be decoded")
	}
	if !response.hasTrackList() {
		return playerSubtitleResponse{}, errNoSubtitleData
	}
	return response, nil
}

type apiSubtitleError struct {
	code    int
	message string
}

func (e apiSubtitleError) Error() string { return "bilibili subtitle API error" }

func platformSubtitleError(err error) (int, string) {
	var apiErr apiSubtitleError
	if errors.As(err, &apiErr) {
		code := apiErr.code
		if code < -100000 || code > 100000 {
			code = 0
		}
		return code, safeSubtitleText(apiErr.message, 160)
	}
	var platform *discovery.PlatformError
	if errors.As(err, &platform) {
		code, _ := strconv.Atoi(platform.Code)
		if code < -100000 || code > 100000 {
			code = 0
		}
		return code, safeSubtitleText(platform.Message, 160)
	}
	return 0, safeSubtitleText(err.Error(), 160)
}

func (c *Client) buildSubtitlePart(ctx context.Context, candidates []playerSubtitle, title string, readBodies bool, selectedLanguage string, remainingBodyBytes *int) subtitles.Part {
	part := subtitles.Part{Title: title, Status: "none", Languages: []subtitles.Language{}, Tracks: []subtitles.Track{}}
	failedURL := false
	if len(candidates) > maxSubtitleTracks {
		candidates = candidates[:maxSubtitleTracks]
	}
	for _, candidate := range candidates {
		language := strings.TrimSpace(candidate.Language)
		if language == "" || len(language) > 64 {
			continue
		}
		label := safeSubtitleText(html.UnescapeString(candidate.Label), 200)
		if label == "" {
			label = subtitleLanguageLabel(language)
			if label == "" {
				label = language
			}
		}
		source := subtitleSource(candidate)
		hasURL := strings.TrimSpace(candidate.URL) != ""
		part.Languages = append(part.Languages, subtitles.Language{Language: language, Label: label, Automatic: source == "ai", Source: source, HasURL: hasURL})
		selected := selectedLanguage == "" || language == selectedLanguage
		if !hasURL || !readBodies || !selected || remainingBodyBytes != nil && *remainingBodyBytes <= 0 {
			continue
		}
		budget := maxSubtitleBody
		if remainingBodyBytes != nil && *remainingBodyBytes < budget {
			budget = *remainingBodyBytes
		}
		cues, bytesRead, err := c.fetchSubtitleCuesBudget(ctx, candidate.URL, budget)
		if remainingBodyBytes != nil {
			*remainingBodyBytes -= bytesRead
		}
		if err != nil || len(cues) == 0 {
			failedURL = true
			if err != nil {
				part.ErrorMessage = safeSubtitleText(err.Error(), 160)
			}
			continue
		}
		part.Tracks = append(part.Tracks, subtitles.Track{Language: language, Label: label, Automatic: source == "ai", Source: source, Cues: cues})
	}
	sort.SliceStable(part.Tracks, func(i, j int) bool {
		return subtitleTrackPriority(part.Tracks[i]) < subtitleTrackPriority(part.Tracks[j])
	})
	sort.SliceStable(part.Languages, func(i, j int) bool {
		return subtitleLanguagePriority(part.Languages[i]) < subtitleLanguagePriority(part.Languages[j])
	})
	if len(part.Tracks) > 0 {
		for i := range part.Tracks {
			if part.Tracks[i].Automatic && aiSubtitleMismatch(part.Tracks[i], part.Title) {
				part.Tracks[i].Mismatch = true
			}
		}
		for _, track := range part.Tracks {
			part.AIMismatch = part.AIMismatch || track.Source == "ai" && track.Mismatch
		}
		part.Status = part.Tracks[0].Source
		if part.Tracks[0].Source == "ai" && part.Tracks[0].Mismatch {
			part.Status = "ai_mismatch"
		}
	} else if len(part.Languages) > 0 {
		hasURL := false
		for _, language := range part.Languages {
			hasURL = hasURL || language.HasURL
		}
		if readBodies && failedURL {
			part.Status = "error"
			if part.ErrorMessage == "" {
				part.ErrorMessage = "字幕内容为空或格式无效。"
			}
		} else if !hasURL {
			part.Status = "api_empty"
		} else {
			for _, language := range part.Languages {
				if language.HasURL {
					part.Status = language.Source
					break
				}
			}
		}
	}
	return part
}

func isSubtitleRateLimited(err error) bool {
	var platform *discovery.PlatformError
	return errors.As(err, &platform) && platform.Restricted
}

func subtitleSource(candidate playerSubtitle) string {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(candidate.Language)), "ai-") || candidate.AIType > 0 || candidate.AIStatus > 0 || candidate.Type == 1 {
		return "ai"
	}
	return "cc"
}

func hasSubtitleURL(candidates []playerSubtitle) bool {
	for _, candidate := range candidates {
		if strings.TrimSpace(candidate.URL) != "" {
			return true
		}
	}
	return false
}

func subtitleLanguagePriority(language subtitles.Language) int {
	return subtitlePriority(language.Language, language.Source)
}

func subtitleTrackPriority(track subtitles.Track) int {
	return subtitlePriority(track.Language, track.Source)
}

func subtitlePriority(language, source string) int {
	automaticChinese := strings.HasPrefix(strings.ToLower(strings.TrimSpace(language)), "ai-")
	language = normalizedSubtitleLanguage(language)
	simplifiedChinese := isSimplifiedChineseSubtitleLanguage(language)
	if automaticChinese && (language == "zh" || language == "cmn" || language == "zho" || language == "chi") {
		if source == "ai" {
			return 1
		}
	}
	if simplifiedChinese && source == "cc" {
		return 0
	}
	if simplifiedChinese && source == "ai" {
		return 1
	}
	return 2
}

func normalizedSubtitleLanguage(language string) string {
	language = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(language), "_", "-"))
	language = strings.TrimPrefix(language, "ai-")
	return strings.TrimSuffix(language, "-orig")
}

func isChineseSubtitleLanguage(language string) bool {
	language = normalizedSubtitleLanguage(language)
	return language == "zh" || strings.HasPrefix(language, "zh-")
}

func isSimplifiedChineseSubtitleLanguage(language string) bool {
	language = normalizedSubtitleLanguage(language)
	return language == "zh-hans" || strings.HasPrefix(language, "zh-hans-") || language == "zh-cn" || strings.HasPrefix(language, "zh-cn-") || language == "zh-sg" || strings.HasPrefix(language, "zh-sg-")
}

func isTraditionalChineseSubtitleLanguage(language string) bool {
	language = normalizedSubtitleLanguage(language)
	return language == "zh-hant" || strings.HasPrefix(language, "zh-hant-") || language == "zh-tw" || strings.HasPrefix(language, "zh-tw-") || language == "zh-hk" || strings.HasPrefix(language, "zh-hk-") || language == "zh-mo" || strings.HasPrefix(language, "zh-mo-")
}

func subtitleLanguageLabel(language string) string {
	if isTraditionalChineseSubtitleLanguage(language) {
		return "繁体中文"
	}
	if isSimplifiedChineseSubtitleLanguage(language) {
		return "简体中文"
	}
	if isChineseSubtitleLanguage(language) {
		return "中文"
	}
	return ""
}

func (c *Client) fetchSubtitleCues(ctx context.Context, raw string) ([]subtitles.Cue, error) {
	cues, _, err := c.fetchSubtitleCuesBudget(ctx, raw, maxSubtitleBody)
	return cues, err
}

func (c *Client) fetchSubtitleCuesBudget(ctx context.Context, raw string, budget int) ([]subtitles.Cue, int, error) {
	validated, err := validateSubtitleURL(raw)
	if err != nil {
		return nil, 0, err
	}
	if budget <= 0 {
		return nil, 0, errors.New("subtitle body budget exhausted")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, validated, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Referer", "https://www.bilibili.com/")
	req.Header.Set("Accept", "application/json")
	client := *c.http
	client.Timeout = 12 * time.Second
	client.Jar = nil
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := client.Do(req)
	if err != nil {
		return nil, 0, errors.New("subtitle CDN request failed")
	}
	defer response.Body.Close()
	contentType := safeSubtitleText(response.Header.Get("Content-Type"), 80)
	if response.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("字幕 CDN 返回 HTTP %d（%s）。", response.StatusCode, contentType)
	}
	if !strings.Contains(strings.ToLower(contentType), "json") {
		return nil, 0, fmt.Errorf("字幕 CDN 返回了非 JSON 内容（%s）。", contentType)
	}
	limit := min(maxSubtitleBody, budget)
	if response.ContentLength > int64(limit) {
		return nil, 0, errors.New("字幕 CDN 响应超过大小限制或总量预算。")
	}
	// Do not read the usual +1 sentinel here: when this is the final shared
	// budget, even that probe would exceed the document-wide byte cap.
	rawBody, err := io.ReadAll(io.LimitReader(response.Body, int64(limit)))
	if err != nil || len(rawBody) >= limit && response.ContentLength < 0 {
		return nil, len(rawBody), errors.New("字幕 CDN 响应超过大小限制或总量预算。")
	}
	var payload struct {
		Body []subtitleCuePayload `json:"body"`
	}
	if json.Unmarshal(rawBody, &payload) != nil || len(payload.Body) == 0 || len(payload.Body) > 10000 {
		return nil, len(rawBody), fmt.Errorf("字幕 CDN 内容无效（HTTP %d，%s）。", response.StatusCode, contentType)
	}
	cues := make([]subtitles.Cue, 0, len(payload.Body))
	previous := -1.0
	for _, cue := range payload.Body {
		text := safeSubtitleText(cue.Content, 8000)
		if cue.From < 0 || cue.From < previous || cue.To <= cue.From || cue.To > 7*24*3600 || text == "" {
			return nil, len(rawBody), errors.New("subtitle cue data is invalid")
		}
		cues = append(cues, subtitles.Cue{Start: cue.From, End: cue.To, Text: text})
		previous = cue.From
	}
	return cues, len(rawBody), nil
}

func validateSubtitleURL(raw string) (string, error) {
	if len(raw) == 0 || len(raw) > 2048 {
		return "", errors.New("invalid subtitle URL")
	}
	if strings.HasPrefix(raw, "//") {
		raw = "https:" + raw
	} else if strings.HasPrefix(strings.ToLower(raw), "http://") {
		raw = "https://" + raw[len("http://"):]
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Port() != "" || parsed.Hostname() == "" || !subtitleCDNHosts[strings.ToLower(parsed.Hostname())] || parsed.Opaque != "" {
		return "", errors.New("subtitle URL is not on an approved HTTPS CDN")
	}
	return parsed.String(), nil
}

func safeSubtitleText(raw string, limit int) string {
	text := strings.TrimSpace(strings.ToValidUTF8(raw, ""))
	text = subtitleCredentialText.ReplaceAllString(text, "$1=[已隐藏]")
	text = subtitleURLText.ReplaceAllString(text, "[链接已隐藏]")
	text = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, text)
	text = strings.Join(strings.Fields(text), " ")
	if utf8.RuneCountInString(text) > limit {
		runes := []rune(text)
		text = string(runes[:limit])
	}
	return text
}

func aiSubtitleMismatch(track subtitles.Track, title string) bool {
	titleTerms := subtitleTerms(title)
	if len(titleTerms) < 2 {
		return false
	}
	var sample strings.Builder
	for i, cue := range track.Cues {
		if i >= 24 || sample.Len() >= 24000 {
			break
		}
		sample.WriteString(cue.Text)
		sample.WriteByte(' ')
	}
	captionTerms := subtitleTerms(sample.String())
	if len(captionTerms) < 3 {
		return false
	}
	shared := 0
	for term := range titleTerms {
		if captionTerms[term] {
			shared++
		}
	}
	return float64(shared)/float64(len(titleTerms)) < 0.25
}

func subtitleTerms(raw string) map[string]bool {
	raw = strings.ToLower(raw)
	terms := map[string]bool{}
	var word strings.Builder
	flush := func() {
		if word.Len() >= 2 {
			terms[word.String()] = true
		}
		word.Reset()
	}
	var hanRun []rune
	flushHan := func() {
		for i := 0; i+1 < len(hanRun); i++ {
			terms[string(hanRun[i:i+2])] = true
		}
		hanRun = nil
	}
	for _, r := range raw {
		if unicode.Is(unicode.Han, r) {
			flush()
			hanRun = append(hanRun, r)
			continue
		}
		flushHan()
		if unicode.IsLetter(r) && r < unicode.MaxASCII || unicode.IsDigit(r) {
			word.WriteRune(r)
			continue
		}
		flush()
	}
	flushHan()
	flush()
	return terms
}
