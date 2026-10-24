package worker

import "testing"

func TestMergerReplacesPartialAndDeduplicatesFinal(t *testing.T) {
	merger := NewMerger()
	if err := merger.Apply(Event{Type: "partial", Sequence: 1, Start: 0, End: 1, Text: "你"}); err != nil {
		t.Fatal(err)
	}
	if err := merger.Apply(Event{Type: "partial", Sequence: 1, Start: 0, End: 1.2, Text: "你好"}); err != nil {
		t.Fatal(err)
	}
	final := Event{Type: "final", Sequence: 1, Start: 0, End: 1.2, Text: "你好"}
	if err := merger.Apply(final); err != nil {
		t.Fatal(err)
	}
	if err := merger.Apply(final); err != nil {
		t.Fatal(err)
	}
	track := merger.Track("zh", "自动字幕")
	if len(track.Cues) != 1 || track.Cues[0].Text != "你好" || track.Cues[0].End != 1.2 {
		t.Fatalf("track = %#v", track)
	}
}

func TestMergerRejectsInvalidCue(t *testing.T) {
	merger := NewMerger()
	if err := merger.Apply(Event{Type: "final", Sequence: 1, Start: 2, End: 1, Text: "错"}); err != ErrInvalidCue {
		t.Fatalf("error = %v", err)
	}
}
