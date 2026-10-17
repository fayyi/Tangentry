package ai

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"regexp"
	"strings"
	"unicode/utf8"

	"tangentry/internal/content"
	"tangentry/internal/subtitles"
)

const promptVersion = 1

var language = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
var keyPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

func validate(in Input) bool {
	validID := content.SpeechID(in.ContentID)
	if in.Source != "" {
		if in.Source != "title" && in.Source != "article" {
			return false
		}
		validID = content.ValidID(in.ContentID)
		if in.Track.Language != "text-"+in.Source || (in.Source == "title" && len(in.Track.Cues) != 1) {
			return false
		}
	} else if strings.HasPrefix(in.Track.Language, "text-") {
		return false
	}
	if !validID || !language.MatchString(in.Track.Language) || len(in.Track.Cues) == 0 || len(in.Track.Cues) > 10000 {
		return false
	}
	previous, total := -1.0, 0
	for _, cue := range in.Track.Cues {
		if math.IsNaN(cue.Start) || math.IsNaN(cue.End) || math.IsInf(cue.Start, 0) || math.IsInf(cue.End, 0) || cue.Start < 0 || cue.Start < previous || cue.End <= cue.Start || cue.End > 7*24*3600 || !utf8.ValidString(cue.Text) || strings.TrimSpace(cue.Text) == "" || len(cue.Text) > 8000 {
			return false
		}
		previous = cue.Start
		total += len(cue.Text)
		if total > 2<<20 {
			return false
		}
	}
	return true
}

func translationKind(in Input) string {
	if in.Source == "title" {
		return kindTitle
	}
	return kindTranslation
}

// textsKey identifies the exact texts sent for translation, independent of
// content, timing, provider and model.
func textsKey(texts []string) string {
	return hashSource(struct {
		Version int
		Texts   []string
	}{promptVersion, texts})
}

func identity(in Input, c Config) Result {
	texts := make([]string, len(in.Track.Cues))
	for i, cue := range in.Track.Cues {
		texts[i] = cue.Text
	}
	return Result{Key: textsKey(texts), ContentID: in.ContentID, Language: in.Track.Language, BaseURL: c.BaseURL, Model: c.Model, Total: len(in.Track.Cues), Texts: []string{}}
}

// legacyIdentity is the file key used before results moved to the database.
func legacyIdentity(in Input, c Config) string {
	data, _ := json.Marshal(struct {
		Version                             int
		ContentID, Language, BaseURL, Model string
		Cues                                []subtitles.Cue
	}{promptVersion, in.ContentID, in.Track.Language, c.BaseURL, c.Model, in.Track.Cues})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func validResult(result Result) bool {
	if result.Total <= 0 || result.Total > 10000 || result.Texts == nil || result.Model == "" || len(result.Model) > 200 || len(result.Texts) > result.Total || !content.ValidID(result.ContentID) || !language.MatchString(result.Language) || !ValidEndpoint(result.BaseURL) {
		return false
	}
	total := 0
	for _, text := range result.Texts {
		total += len(text)
		if text == "" || !utf8.ValidString(text) || len(text) > 24000 || total > 8<<20 {
			return false
		}
	}
	return true
}
