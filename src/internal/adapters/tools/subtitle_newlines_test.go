package tools

import (
	"reflect"
	"strings"
	"testing"

	"tangentry/internal/subtitles"
)

func TestTimedSubtitlesAcceptAllLineTerminatorsWithoutChangingCues(t *testing.T) {
	formats := []struct {
		name  string
		raw   string
		parse func(string) ([]subtitles.Cue, error)
	}{
		{"vtt", "\ufeffWEBVTT\n\nNOTE publisher note\nignored\n\nSTYLE \t\n::cue { color: white; }\n\nREGION\t \nid:area\n\nfirst\n00:00.125 --> 00:01.250 align:start\n<v Host>第一行\nSecond &amp; line\n\n00:02.125 --> 00:04.250\nFinal line", parseWebVTT},
		{"srt", "\ufeff1\n00:00:00,125 --> 00:00:01,250\n第一行\nSecond &amp; line\n\n2\n00:00:02,125 --> 00:00:04,250\nFinal line", parseSRT},
	}
	expected := []subtitles.Cue{{Start: .125, End: 1.25, Text: "第一行\nSecond & line"}, {Start: 2.125, End: 4.25, Text: "Final line"}}
	for _, format := range formats {
		for _, newline := range []string{"\n", "\r\n", "\r"} {
			t.Run(format.name+"/"+strings.ReplaceAll(strings.ReplaceAll(newline, "\r", "CR"), "\n", "LF"), func(t *testing.T) {
				cues, err := format.parse(strings.ReplaceAll(format.raw, "\n", newline))
				if err != nil || !reflect.DeepEqual(cues, expected) {
					t.Fatal("line terminator changed timing or text", cues, err)
				}
			})
		}
		mixed := strings.Replace(format.raw, "\n\n", "\r\n\r", 1)
		if cues, err := format.parse(mixed); err != nil || !reflect.DeepEqual(cues, expected) {
			t.Fatal("mixed line terminators rejected", cues, err)
		}
	}
}
