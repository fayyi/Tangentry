package tools

import (
	"context"
	"encoding/json"
	"errors"
	"html"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"tangentry/internal/adapters/platforms/youtube"
	"tangentry/internal/discovery"
	"tangentry/internal/subtitles"
)

type youtubeCaption struct {
	Ext  string `json:"ext"`
	URL  string `json:"url"`
	Name string `json:"name"`
}
type youtubeCaptionMetadata struct {
	ID        string                      `json:"id"`
	Subtitles map[string][]youtubeCaption `json:"subtitles"`
	Automatic map[string][]youtubeCaption `json:"automatic_captions"`
}

type youtubeCaptionStage int

const (
	youtubeManualCaptions youtubeCaptionStage = iota
	youtubeAnyCaptions
)

// YouTube captions are remote resources. yt-dlp fetches exactly one selected
// existing track into a private disposable directory; media is never downloaded.
func (t *Toolkit) ReadYouTubeSubtitles(ctx context.Context, id, language string) (subtitles.Document, error) {
	parsed, canonical, err := discovery.ParseYouTube("https://www.youtube.com/watch?v=" + id)
	if err != nil || parsed != id || language != "" && !languagePattern.MatchString(language) {
		return subtitles.Document{}, subtitles.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 28*time.Second)
	defer cancel()
	cookie := ""
	if t.credentials != nil {
		cookie = t.credentials.Header("youtube", "https://www.youtube.com/youtubei/v1/player")
	}
	var priorErrors []error
	confirmedNoCaptions := false
	if t.ytdlpReady() {
		doc, found, err := t.readYTDLPWithCredentials(ctx, id, canonical, language, youtubeManualCaptions)
		if err == nil && found {
			return doc, nil
		}
		if err != nil {
			priorErrors = append(priorErrors, err)
		}
		if ctx.Err() != nil {
			return subtitles.Document{}, ctx.Err()
		}
	}
	if t.youtubePlayer != nil {
		player := t.youtubePlayer(ctx, id, cookie)
		if player.Error != "" {
			priorErrors = append(priorErrors, playerError(player.Error))
		} else {
			confirmedNoCaptions = len(player.Tracks) == 0
			for _, automatic := range []bool{false, true} {
				track, ok := choosePlayerCaption(player.Tracks, language, automatic)
				if !ok {
					continue
				}
				captionCookie := cookie
				if t.credentials != nil {
					captionCookie = t.credentials.Header("youtube", track.URL)
				}
				doc, err := t.readInnerTubeCaption(ctx, id, track, captionCookie, player.Tracks)
				if err == nil {
					return doc, nil
				}
				priorErrors = append(priorErrors, err)
			}
		}
		if ctx.Err() != nil {
			return subtitles.Document{}, ctx.Err()
		}
	}
	if t.ytdlpReady() {
		doc, found, err := t.readYTDLPWithCredentials(ctx, id, canonical, language, youtubeAnyCaptions)
		if err == nil && found {
			return doc, nil
		}
		if err != nil {
			priorErrors = append(priorErrors, err)
		} else {
			confirmedNoCaptions = true
		}
	}
	if ctx.Err() != nil {
		return subtitles.Document{}, ctx.Err()
	}
	if classified := strongestYouTubeCaptionError(priorErrors); classified != nil {
		return subtitles.Document{}, classified
	}
	if language != "" {
		return subtitles.Document{}, subtitles.ErrInvalid
	}
	if !t.ytdlpReady() && t.youtubePlayer == nil {
		return subtitles.Document{}, subtitles.ErrTools
	}
	if confirmedNoCaptions {
		return subtitles.Document{VideoID: id, Part: 1, Status: "none", Tracks: []subtitles.Track{}, Languages: []subtitles.Language{}}, nil
	}
	return subtitles.Document{}, subtitles.ErrUnavailable
}

func (t *Toolkit) readYTDLPWithCredentials(ctx context.Context, id, canonical, language string, stage youtubeCaptionStage) (subtitles.Document, bool, error) {
	cookieArgs, clean, err := t.cookieArguments(canonical)
	if err != nil {
		return subtitles.Document{}, false, subtitles.ErrUnavailable
	}
	defer clean()
	return t.readYTDLPYouTubeCaptions(ctx, id, canonical, language, stage, cookieArgs)
}

func (t *Toolkit) readYTDLPYouTubeCaptions(ctx context.Context, id, canonical, language string, stage youtubeCaptionStage, cookieArgs []string) (subtitles.Document, bool, error) {
	if ctx.Err() != nil {
		return subtitles.Document{}, false, ctx.Err()
	}
	base := []string{"--ignore-config", "--no-playlist", "--no-cache-dir", "--skip-download", "--dump-single-json", "--socket-timeout", "10", "--retries", "0", "--extractor-args", "youtube:skip=translated_subs"}
	run := func(extra []string) ([]byte, error) {
		args := append(append(append([]string{}, base...), cookieArgs...), extra...)
		args = append(args, "--", canonical)
		cmd, err := t.ytdlpCommand(ctx, args...)
		if err != nil {
			return nil, subtitles.ErrTools
		}
		stdout, stderr := &metadataBuffer{limit: 16 << 20}, &boundedBuffer{}
		cmd.Stdout, cmd.Stderr = stdout, stderr
		if err := cmd.Run(); err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, youtubeCaptionFailure(stderr.String())
		}
		if stdout.exceeded {
			return nil, subtitles.ErrFormat
		}
		warnings := stderr.String()
		if len(warnings) >= 16384 {
			return nil, subtitles.ErrUnavailable
		}
		for _, line := range strings.Split(warnings, "\n") {
			lower := strings.ToLower(line)
			if strings.Contains(line, "WARNING:") && (strings.Contains(lower, "subtitle") || strings.Contains(lower, "caption")) {
				return nil, youtubeCaptionFailure(line)
			}
		}
		return stdout.Bytes(), nil
	}
	raw, err := run([]string{"--simulate"})
	if err != nil {
		return subtitles.Document{}, false, err
	}
	choices, err := youtubeLanguages(raw, id)
	if err != nil {
		return subtitles.Document{}, false, err
	}
	chosenIndex := -1
	for i := range choices {
		if language != "" && choices[i].Language != language || stage == youtubeManualCaptions && choices[i].Automatic {
			continue
		}
		if chosenIndex == -1 || stage == youtubeAnyCaptions && choices[chosenIndex].Automatic && !choices[i].Automatic ||
			choices[i].Automatic == choices[chosenIndex].Automatic && youtubeLanguageScore(choices[i]) < youtubeLanguageScore(choices[chosenIndex]) {
			chosenIndex = i
		}
	}
	if chosenIndex == -1 {
		return subtitles.Document{VideoID: id, Part: 1, Status: "none", Tracks: []subtitles.Track{}, Languages: choices}, false, nil
	}
	chosen := choices[chosenIndex]
	directory, err := os.MkdirTemp("", "tangentry-youtube-captions-*")
	if err != nil {
		return subtitles.Document{}, false, subtitles.ErrUnavailable
	}
	defer os.RemoveAll(directory)
	// dump-single-json implies simulation by default. Explicitly allow the subtitle
	// file write while skip-download continues to forbid media downloads.
	writeFlag := "--write-subs"
	if chosen.Automatic {
		writeFlag = "--write-auto-subs"
	}
	raw, err = run([]string{"--no-simulate", writeFlag, "--sub-langs", "^" + regexp.QuoteMeta(chosen.Language) + "$", "--sub-format", "json3/srt/vtt", "--paths", directory, "--output", "caption.%(ext)s"})
	if err != nil {
		return subtitles.Document{}, false, err
	}
	var received struct {
		ID        string `json:"id"`
		Requested map[string]struct {
			Ext string `json:"ext"`
			URL string `json:"url"`
		} `json:"requested_subtitles"`
	}
	if json.Unmarshal(raw, &received) != nil || received.ID != id || len(received.Requested) != 1 || !supportedYouTubeCaptionFormat(received.Requested[chosen.Language].Ext) {
		return subtitles.Document{}, false, subtitles.ErrFormat
	}
	if !youtubeCaptionURLMatches(received.Requested[chosen.Language].URL, strings.TrimSuffix(chosen.Language, "-orig"), id) {
		return subtitles.Document{}, false, subtitles.ErrFormat
	}
	extension := received.Requested[chosen.Language].Ext
	path := filepath.Join(directory, "caption."+chosen.Language+"."+extension)
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 2<<20 || info.Size() == 0 {
		return subtitles.Document{}, false, subtitles.ErrFormat
	}
	file, err := os.Open(path)
	if err != nil {
		return subtitles.Document{}, false, subtitles.ErrUnavailable
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, (2<<20)+1))
	if err != nil || len(data) > 2<<20 {
		return subtitles.Document{}, false, subtitles.ErrFormat
	}
	var cues []subtitles.Cue
	switch extension {
	case "json3":
		cues, err = parseJSON3(data)
	case "srt":
		cues, err = parseSRT(string(data))
	case "vtt":
		cues, err = parseWebVTT(string(data))
	}
	if err != nil {
		return subtitles.Document{}, false, err
	}
	if chosen.Automatic {
		cues = mergeYouTubeAutoCues(cues)
	}
	doc := subtitles.Document{VideoID: id, Part: 1, Status: "available", Languages: choices, Tracks: []subtitles.Track{{Language: chosen.Language, Label: chosen.Label, Automatic: chosen.Automatic, Cues: cues}}}
	return doc, true, nil
}

func (t *Toolkit) readInnerTubeCaption(ctx context.Context, id string, track youtube.CaptionTrack, cookie string, available []youtube.CaptionTrack) (subtitles.Document, error) {
	if ctx.Err() != nil {
		return subtitles.Document{}, ctx.Err()
	}
	u, err := url.Parse(track.URL)
	if err != nil || !youtubeCaptionURLMatches(track.URL, track.Language, id) {
		return subtitles.Document{}, subtitles.ErrFormat
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil || len(query["tlang"]) != 0 || query.Get("lang") != track.Language || query.Get("v") != id {
		return subtitles.Document{}, subtitles.ErrFormat
	}
	query.Set("fmt", "json3")
	u.RawQuery = query.Encode()
	client := t.youtubeHTTP
	if client == nil {
		client = http.DefaultClient
	}
	cloned := *client
	cloned.Jar = nil
	cloned.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return subtitles.Document{}, subtitles.ErrFormat
	}
	req.Header.Set("Accept", "application/json,text/plain,*/*")
	req.Header.Set("User-Agent", t.youtubeUserAgentValue())
	req.Header.Set("Referer", "https://www.youtube.com/watch?v="+id)
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	response, err := cloned.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return subtitles.Document{}, ctx.Err()
		}
		return subtitles.Document{}, subtitles.ErrUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode > 299 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return subtitles.Document{}, youtubeCaptionHTTPFailure(response.StatusCode, string(body))
	}
	if response.ContentLength > 2<<20 {
		return subtitles.Document{}, subtitles.ErrFormat
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, (2<<20)+1))
	if err != nil {
		return subtitles.Document{}, subtitles.ErrUnavailable
	}
	if len(raw) == 0 || len(raw) > 2<<20 {
		return subtitles.Document{}, subtitles.ErrFormat
	}
	if contentType := response.Header.Get("Content-Type"); contentType != "" {
		mediaType, _, parseErr := mime.ParseMediaType(contentType)
		if parseErr != nil || mediaType != "application/json" && mediaType != "text/plain" {
			if failure := youtubeCaptionBodyFailure(string(raw)); failure != nil {
				return subtitles.Document{}, failure
			}
			return subtitles.Document{}, subtitles.ErrFormat
		}
	}
	cues, err := parseJSON3(raw)
	if err != nil {
		if failure := youtubeCaptionBodyFailure(string(raw)); failure != nil {
			return subtitles.Document{}, failure
		}
		return subtitles.Document{}, err
	}
	if track.Kind == "asr" {
		cues = mergeYouTubeAutoCues(cues)
	}
	if len(cues) == 0 {
		return subtitles.Document{}, subtitles.ErrFormat
	}
	label := languageLabel(track.Language)
	languages := make([]subtitles.Language, 0, len(available))
	for _, candidate := range available {
		languages = append(languages, subtitles.Language{Language: candidate.Language, Label: candidate.Label, Automatic: candidate.Kind == "asr"})
	}
	return subtitles.Document{VideoID: id, Part: 1, Status: "available", Languages: languages, Tracks: []subtitles.Track{{Language: track.Language, Label: label, Automatic: track.Kind == "asr", Cues: cues}}}, nil
}

func youtubeCaptionLanguage(value string) bool {
	if len(value) < 2 || len(value) > 35 || value == "live_chat" {
		return false
	}
	base := strings.TrimSuffix(value, "-orig")
	parts := strings.Split(base, "-")
	if len(parts[0]) < 2 || len(parts[0]) > 8 {
		return false
	}
	for _, char := range parts[0] {
		if char < 'A' || char > 'Z' {
			if char < 'a' || char > 'z' {
				return false
			}
		}
	}
	for _, part := range parts[1:] {
		if len(part) == 0 || len(part) > 8 {
			return false
		}
		for _, char := range part {
			if (char < 'A' || char > 'Z') && (char < 'a' || char > 'z') && (char < '0' || char > '9') {
				return false
			}
		}
	}
	return true
}

func (t *Toolkit) youtubeUserAgentValue() string {
	if t.youtubeUserAgent != nil {
		return t.youtubeUserAgent()
	}
	return "Mozilla/5.0 (compatible; Tangentry/0.1)"
}

func youtubeCaptionHTTPFailure(status int, body string) error {
	text := strings.ToLower(body)
	switch {
	case status == http.StatusTooManyRequests || status == http.StatusForbidden && isYouTubeRestrictionSignal(text):
		return subtitles.ErrRestricted
	case status == http.StatusUnauthorized || status == http.StatusForbidden && isYouTubeLoginSignal(text):
		return subtitles.ErrLogin
	default:
		return subtitles.ErrUnavailable
	}
}

func youtubeCaptionBodyFailure(body string) error {
	text := strings.ToLower(body)
	if isYouTubeRestrictionSignal(text) {
		return subtitles.ErrRestricted
	}
	if isYouTubeLoginSignal(text) {
		return subtitles.ErrLogin
	}
	return nil
}

func isYouTubeRestrictionSignal(text string) bool {
	return strings.Contains(text, "not a bot") || strings.Contains(text, "unusual traffic") || strings.Contains(text, "verify you are") || strings.Contains(text, "automated queries")
}

func isYouTubeLoginSignal(text string) bool {
	return strings.Contains(text, "sign in") || strings.Contains(text, "log in") || strings.Contains(text, "login required") || strings.Contains(text, "authentication required") || strings.Contains(text, "age verification required")
}

func playerError(code string) error {
	switch code {
	case "auth":
		return subtitles.ErrLogin
	case "restricted":
		return subtitles.ErrRestricted
	case "format":
		return subtitles.ErrFormat
	case "invalid":
		return subtitles.ErrInvalid
	default:
		return subtitles.ErrUnavailable
	}
}

func choosePlayerCaption(tracks []youtube.CaptionTrack, language string, automatic bool) (youtube.CaptionTrack, bool) {
	var selected youtube.CaptionTrack
	found := false
	for _, track := range tracks {
		if (track.Kind == "asr") != automatic || language != "" && track.Language != language {
			continue
		}
		candidate := subtitles.Language{Language: track.Language, Automatic: automatic}
		if !found || youtubeLanguageScore(candidate) < youtubeLanguageScore(subtitles.Language{Language: selected.Language, Automatic: automatic}) ||
			youtubeLanguageScore(candidate) == youtubeLanguageScore(subtitles.Language{Language: selected.Language, Automatic: automatic}) && track.Language < selected.Language {
			selected = track
			found = true
		}
	}
	return selected, found
}

func youtubeLanguageScore(track subtitles.Language) int {
	lang := strings.ReplaceAll(strings.TrimSuffix(strings.ToLower(track.Language), "-orig"), "_", "-")
	rank := 3
	switch lang {
	case "zh", "zh-hans", "zh-cn", "zh-sg":
		rank = 0
	case "zh-hant", "zh-tw", "zh-hk", "zh-mo":
		rank = 1
	case "en":
		rank = 2
	default:
		if strings.HasPrefix(lang, "zh-") {
			rank = 1
		} else if strings.HasPrefix(lang, "en-") {
			rank = 2
		}
	}
	return rank
}

func strongestYouTubeCaptionError(failures []error) error {
	for _, target := range []error{subtitles.ErrRestricted, subtitles.ErrLogin, subtitles.ErrFormat, subtitles.ErrUnavailable} {
		for _, err := range failures {
			if err == nil {
				continue
			}
			if target == subtitles.ErrUnavailable && !errors.Is(err, subtitles.ErrUnavailable) && !errors.Is(err, subtitles.ErrTools) {
				continue
			}
			if errors.Is(err, target) {
				return target
			}
		}
	}
	return nil
}

func mergeYouTubeAutoCues(cues []subtitles.Cue) []subtitles.Cue {
	ordered := append([]subtitles.Cue(nil), cues...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Start < ordered[j].Start })
	merged := make([]subtitles.Cue, 0, len(ordered))
	previousOriginalText := ""
	previousEnd := 0.0
	for _, cue := range ordered {
		cue.Text = strings.TrimSpace(cue.Text)
		if cue.Text == "" {
			continue
		}
		currentWords := strings.Fields(cue.Text)
		if previousOriginalText != "" && cue.Start <= previousEnd+1.5 {
			previousWords := strings.Fields(previousOriginalText)
			limit := len(previousWords)
			if len(currentWords) < limit {
				limit = len(currentWords)
			}
			for overlap := limit; overlap >= 2; overlap-- {
				// A two-word cue repeated verbatim is common natural speech,
				// not enough evidence of the rolling-window duplication that
				// automatic captions produce. Keep it unless a longer phrase
				// establishes a stronger overlap.
				if overlap == 2 && len(previousWords) <= 2 {
					continue
				}
				matches := true
				for index := 0; index < overlap; index++ {
					left := overlapToken(previousWords[len(previousWords)-overlap+index])
					right := overlapToken(currentWords[index])
					if left == "" || left != right {
						matches = false
						break
					}
				}
				if matches && overlap < len(currentWords) {
					cue.Text = strings.TrimSpace(strings.Join(currentWords[overlap:], " "))
					break
				}
			}
			if cue.Text == strings.TrimSpace(strings.Join(currentWords, " ")) && hasCJKText(previousOriginalText) && hasCJKText(cue.Text) {
				previousRunes := []rune(strings.TrimSpace(previousOriginalText))
				currentRunes := []rune(strings.TrimSpace(cue.Text))
				limit := len(previousRunes)
				if len(currentRunes) < limit {
					limit = len(currentRunes)
				}
				for overlap := limit; len(previousRunes) >= 6 && overlap >= 5; overlap-- {
					if string(previousRunes[len(previousRunes)-overlap:]) == string(currentRunes[:overlap]) && overlap < len(currentRunes) {
						cue.Text = strings.TrimSpace(string(currentRunes[overlap:]))
						break
					}
				}
			}
		}
		if cue.Text != "" {
			merged = append(merged, cue)
		}
		previousOriginalText = strings.TrimSpace(strings.Join(currentWords, " "))
		previousEnd = cue.End
	}
	return merged
}

func hasCJKText(value string) bool {
	for _, char := range value {
		if char >= 0x3400 && char <= 0x9fff || char >= 0xf900 && char <= 0xfaff || char >= 0x3040 && char <= 0x30ff || char >= 0xac00 && char <= 0xd7af {
			return true
		}
	}
	return false
}

func overlapToken(value string) string {
	return strings.Trim(value, ".,!?;:\"'()[]{}<>，。！？；：、“”‘’")
}

func youtubeCaptionFailure(warning string) error {
	text := strings.ToLower(warning)
	switch {
	case strings.Contains(text, "http error 429"), strings.Contains(text, "too many requests"), strings.Contains(text, "confirm you're not a bot"), strings.Contains(text, "confirm you’re not a bot"), strings.Contains(text, "confirm you are not a bot"), strings.Contains(text, "unusual traffic"), strings.Contains(text, "verify you are"), strings.Contains(text, "automated queries"):
		return subtitles.ErrRestricted
	case strings.Contains(text, "sign in"), strings.Contains(text, "login required"), strings.Contains(text, "cookies are no longer valid"), strings.Contains(text, "cookies have been rotated"), strings.Contains(text, "authentication required"):
		return subtitles.ErrLogin
	case strings.Contains(text, "http error 403"):
		return subtitles.ErrRestricted
	default:
		return subtitles.ErrUnavailable
	}
}

func nativeYouTubeCaptionURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" || (u.Hostname() != "www.youtube.com" && u.Hostname() != "youtube.com") || u.Path != "/api/timedtext" {
		return false
	}
	query, err := url.ParseQuery(u.RawQuery)
	return err == nil && len(query["tlang"]) == 0 && u.Fragment == "" && u.RawFragment == ""
}

func supportedYouTubeCaptionFormat(extension string) bool {
	return extension == "json3" || extension == "srt" || extension == "vtt"
}

func youtubeLanguages(raw []byte, id string) ([]subtitles.Language, error) {
	var data youtubeCaptionMetadata
	if json.Unmarshal(raw, &data) != nil || data.ID != id || data.Subtitles == nil && data.Automatic == nil {
		return nil, subtitles.ErrFormat
	}
	if len(data.Subtitles) > 200 || len(data.Automatic) > 200 {
		return nil, subtitles.ErrFormat
	}
	choices := make([]subtitles.Language, 0)
	seen := map[string]bool{}
	for index, tracks := range []map[string][]youtubeCaption{data.Subtitles, data.Automatic} {
		native := map[string]bool{}
		translated := map[string]bool{}
		for lang, versions := range tracks {
			if len(versions) > 2000 {
				return nil, subtitles.ErrFormat
			}
			for _, version := range versions {
				if !supportedYouTubeCaptionFormat(version.Ext) {
					continue
				}
				u, err := url.Parse(version.URL)
				// yt-dlp also reports HLS caption manifests alongside timedtext.
				// They are not downloadable by this reader; do not reject usable tracks.
				if err == nil && u.Scheme == "https" && u.User == nil && u.Port() == "" && u.Hostname() == "manifest.googlevideo.com" && strings.HasPrefix(u.Path, "/api/manifest/hls_timedtext_playlist/") {
					continue
				}
				if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" || (u.Hostname() != "www.youtube.com" && u.Hostname() != "youtube.com") || u.Path != "/api/timedtext" {
					return nil, subtitles.ErrFormat
				}
				query, err := url.ParseQuery(u.RawQuery)
				if err != nil {
					return nil, subtitles.ErrFormat
				}
				if query.Get("tlang") == "" {
					if !youtubeCaptionURLMatches(version.URL, strings.TrimSuffix(lang, "-orig"), id) {
						return nil, subtitles.ErrFormat
					}
					native[lang] = true
				} else {
					translated[lang] = true
				}
			}
		}
		for lang := range tracks {
			if lang == "live_chat" || seen[lang] {
				continue
			}
			if !youtubeCaptionLanguage(lang) {
				return nil, subtitles.ErrFormat
			}
			if !native[lang] {
				if index == 0 && translated[lang] {
					return nil, subtitles.ErrFormat
				}
				continue
			}
			baseLang := strings.TrimSuffix(lang, "-orig")
			// Mixed base lists can make yt-dlp select a translated variant. Its
			// original alias is the unambiguous download key when available.
			preferOriginal := index == 1 && native[baseLang+"-orig"] && translated[baseLang]
			if preferOriginal && lang == baseLang {
				continue
			}
			// The extractor may expose a native auto track twice (en / en-orig).
			if index == 1 && strings.HasSuffix(lang, "-orig") {
				if (!preferOriginal && native[baseLang]) || seen[baseLang] {
					continue
				}
			}
			choices = append(choices, subtitles.Language{Language: lang, Label: languageLabel(baseLang), Automatic: index == 1})
			seen[lang] = true
		}
	}
	if len(choices) > 200 {
		return nil, subtitles.ErrFormat
	}
	sort.Slice(choices, func(i, j int) bool { return choices[i].Language < choices[j].Language })
	return choices, nil
}

func youtubeCaptionURLMatches(raw, language, videoID string) bool {
	if !nativeYouTubeCaptionURL(raw) || !youtubeCaptionLanguage(language) {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil || len(query["lang"]) != 1 || query.Get("lang") != language {
		return false
	}
	videoIDs, hasVideoID := query["v"]
	return !hasVideoID || len(videoIDs) == 1 && videoIDs[0] == videoID
}

// Prefer Chinese, then English, then other native tracks; manual captions win
// within a language. This does not request translated or generated AI text.
func preferredYouTubeLanguage(choices []subtitles.Language) string {
	if len(choices) == 0 {
		return ""
	}
	selected := choices[0]
	for _, candidate := range choices[1:] {
		if youtubeLanguageScore(candidate) < youtubeLanguageScore(selected) ||
			youtubeLanguageScore(candidate) == youtubeLanguageScore(selected) && selected.Automatic && !candidate.Automatic {
			selected = candidate
		}
	}
	return selected.Language
}

func parseJSON3(raw []byte) ([]subtitles.Cue, error) {
	if len(raw) > 2<<20 {
		return nil, subtitles.ErrFormat
	}
	var data struct {
		Events []struct {
			Start    *int64 `json:"tStartMs"`
			Duration *int64 `json:"dDurationMs"`
			Segments []struct {
				Text string `json:"utf8"`
			} `json:"segs"`
		} `json:"events"`
	}
	if json.Unmarshal(raw, &data) != nil || data.Events == nil || len(data.Events) > 20000 {
		return nil, subtitles.ErrFormat
	}
	cues := make([]subtitles.Cue, 0)
	total := 0
	for _, event := range data.Events {
		var text strings.Builder
		for _, segment := range event.Segments {
			text.WriteString(segment.Text)
		}
		caption := strings.TrimSpace(html.UnescapeString(text.String()))
		if caption == "" {
			continue
		}
		if event.Start == nil || event.Duration == nil || *event.Start < 0 || *event.Start > 7*24*3600000 || *event.Duration <= 0 || *event.Duration > 7*24*3600000-*event.Start || len(caption) > 8000 || len(cues) >= 10000 {
			return nil, subtitles.ErrFormat
		}
		total += len(caption)
		if total > 2<<20 {
			return nil, subtitles.ErrFormat
		}
		cues = append(cues, subtitles.Cue{Start: float64(*event.Start) / 1000, End: float64(*event.Start+*event.Duration) / 1000, Text: caption})
	}
	if len(cues) == 0 {
		return nil, subtitles.ErrFormat
	}
	sort.SliceStable(cues, func(i, j int) bool { return cues[i].Start < cues[j].Start })
	return cues, nil
}
