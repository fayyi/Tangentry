package ai

import (
	"strings"
	"unicode"
)

func chineseSubtitles(in Input) bool {
	if in.Source != "" && in.Source != "title" {
		return false
	}
	lang := strings.ReplaceAll(strings.TrimPrefix(strings.ToLower(in.Track.Language), "ai-"), "_", "-")
	base := strings.SplitN(lang, "-", 2)[0]
	switch base {
	case "zh", "cmn", "zho", "chi":
		return true
	}
	han, latin, kana, count := 0, 0, 0, 0
	samples := min(20, len(in.Track.Cues))
	for i := 0; i < samples; i++ {
		index := 0
		if samples > 1 {
			index = i * (len(in.Track.Cues) - 1) / (samples - 1)
		}
		cue := in.Track.Cues[index]
		for _, ch := range cue.Text {
			count++
			if count > 4000 {
				break
			}
			switch {
			case unicode.Is(unicode.Han, ch):
				han++
			case unicode.Is(unicode.Latin, ch):
				latin++
			case unicode.Is(unicode.Hiragana, ch) || unicode.Is(unicode.Katakana, ch):
				kana++
			}
		}
		if count > 4000 {
			break
		}
	}
	return kana == 0 && han >= 4 && han*10 >= (han+latin)*6
}

func originalSubtitleResult(in Input, c Config) Result {
	result := identity(in, c)
	for _, cue := range in.Track.Cues {
		result.Texts = append(result.Texts, cue.Text)
	}
	return result
}
