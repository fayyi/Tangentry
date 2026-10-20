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
	if !validTargetLanguage(inputTargetLanguage(in)) {
		return false
	}
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
	target := inputTargetLanguage(in)
	key := targetTextsKey(target, texts)
	return Result{Key: key, ContentID: in.ContentID, Language: in.Track.Language, TargetLanguage: target, BaseURL: c.BaseURL, Model: c.Model, Total: len(in.Track.Cues), Texts: []string{}}
}

func targetTextsKey(target string, texts []string) string {
	if target == "" || target == targetChinese {
		return textsKey(texts)
	}
	return hashSource(struct {
		Version int
		Target  string
		Texts   []string
	}{promptVersion, target, texts})
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
	if result.TargetLanguage == "" {
		result.TargetLanguage = targetChinese
	}
	if result.Total <= 0 || result.Total > 10000 || result.Texts == nil || result.Model == "" || len(result.Model) > 200 || len(result.Texts) > result.Total || !content.ValidID(result.ContentID) || !language.MatchString(result.Language) || !validTargetLanguage(result.TargetLanguage) || !ValidEndpoint(result.BaseURL) {
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

func inputTargetLanguage(in Input) string {
	if in.TargetLanguage == "" {
		return targetChinese
	}
	target := strings.ToLower(strings.ReplaceAll(in.TargetLanguage, "_", "-"))
	switch target {
	case "zh-hans", "zh-cn", "zh-sg":
		return "zh-Hans"
	case "zh-hant", "zh-tw", "zh-hk", "zh-mo":
		return "zh-Hant"
	default:
		return target
	}
}

func subtitleAlreadyTarget(in Input) bool {
	target := inputTargetLanguage(in)
	if target != "zh-Hans" && target != "zh-Hant" {
		return false
	}
	if in.Source == "article" {
		return false
	}
	language := strings.ToLower(strings.ReplaceAll(in.Track.Language, "_", "-"))
	language = strings.TrimPrefix(language, "ai-")
	switch target {
	case "zh-Hans":
		if isSimplifiedTargetLanguage(language) {
			return true
		}
		if language == "zh" || language == "cmn" || language == "zho" || language == "chi" || isTraditionalChineseLanguage(language) {
			return false
		}
		if in.Source == "" && language != "und" && language != "unknown" {
			return false
		}
		return chineseSubtitles(in)
	case "zh-Hant":
		return isTraditionalChineseLanguage(language)
	default:
		return false
	}
}

func isSimplifiedTargetLanguage(language string) bool {
	return language == "zh-hans" || strings.HasPrefix(language, "zh-hans-") || language == "zh-cn" || strings.HasPrefix(language, "zh-cn-") || language == "zh-sg" || strings.HasPrefix(language, "zh-sg-")
}

func isTraditionalChineseLanguage(language string) bool {
	return language == "zh-hant" || strings.HasPrefix(language, "zh-hant-") || language == "zh-tw" || strings.HasPrefix(language, "zh-tw-") || language == "zh-hk" || strings.HasPrefix(language, "zh-hk-") || language == "zh-mo" || strings.HasPrefix(language, "zh-mo-")
}

func TargetLanguageName(code string) string {
	if code == "" {
		return "简体中文"
	}
	switch strings.ToLower(strings.ReplaceAll(code, "_", "-")) {
	case "zh-hans", "zh-cn", "zh-sg":
		return "简体中文"
	case "zh-hant", "zh-tw", "zh-hk", "zh-mo":
		return "繁体中文"
	case "en", "en-us", "en-gb":
		return "英语"
	case "ja":
		return "日语"
	case "ko":
		return "韩语"
	case "fr":
		return "法语"
	case "de":
		return "德语"
	case "es":
		return "西班牙语"
	case "pt":
		return "葡萄牙语"
	case "ru":
		return "俄语"
	case "it":
		return "意大利语"
	case "ar":
		return "阿拉伯语"
	case "hi":
		return "印地语"
	case "th":
		return "泰语"
	case "vi":
		return "越南语"
	case "id":
		return "印度尼西亚语"
	default:
		return code
	}
}

func validTargetLanguage(value string) bool {
	if len(value) < 2 || len(value) > 35 || !language.MatchString(value) {
		return false
	}
	for _, part := range strings.Split(strings.ReplaceAll(value, "_", "-"), "-") {
		if part == "" || len(part) > 8 {
			return false
		}
	}
	return true
}
