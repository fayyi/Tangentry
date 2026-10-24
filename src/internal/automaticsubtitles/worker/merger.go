package worker

import (
	"errors"
	"math"
	"sort"
	"strings"

	"tangentry/internal/subtitles"
)

var (
	ErrInvalidCue = errors.New("invalid worker cue")
	ErrSequence   = errors.New("invalid worker sequence")
)

// Merger keeps one mutable cue per recognizer sequence. A partial event
// replaces the previous partial for that sequence; a final event freezes it.
// This makes retries and duplicated worker events idempotent.
type Merger struct {
	cues   map[int64]subtitles.Cue
	finals map[int64]bool
}

func NewMerger() *Merger {
	return &Merger{cues: map[int64]subtitles.Cue{}, finals: map[int64]bool{}}
}

func NewMergerFromTrack(track subtitles.Track) *Merger {
	merger := NewMerger()
	for index, cue := range track.Cues {
		merger.cues[int64(index+1)] = cue
		merger.finals[int64(index+1)] = true
	}
	return merger
}

func (m *Merger) Apply(event Event) error {
	if event.Sequence <= 0 {
		return ErrSequence
	}
	text := strings.TrimSpace(event.Text)
	if text == "" {
		return nil
	}
	if math.IsNaN(event.Start) || math.IsInf(event.Start, 0) || math.IsNaN(event.End) || math.IsInf(event.End, 0) || event.Start < 0 || event.End <= event.Start {
		return ErrInvalidCue
	}
	if m.finals[event.Sequence] {
		// A duplicate final is harmless; a later conflicting event is not
		// allowed to rewrite a committed cue.
		if cue := m.cues[event.Sequence]; cue.Start == event.Start && cue.End == event.End && cue.Text == text {
			return nil
		}
		if event.Type == "final" {
			return nil
		}
		return ErrSequence
	}
	m.cues[event.Sequence] = subtitles.Cue{Start: event.Start, End: event.End, Text: text}
	if event.Type == "final" {
		m.finals[event.Sequence] = true
	}
	return nil
}

func (m *Merger) Track(language, label string) subtitles.Track {
	keys := make([]int64, 0, len(m.cues))
	for key := range m.cues {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		left, right := m.cues[keys[i]], m.cues[keys[j]]
		if left.Start == right.Start {
			return keys[i] < keys[j]
		}
		return left.Start < right.Start
	})
	cues := make([]subtitles.Cue, 0, len(keys))
	for _, key := range keys {
		cue := m.cues[key]
		if len(cues) > 0 && cue.Start < cues[len(cues)-1].Start {
			cue.Start = cues[len(cues)-1].Start
		}
		if cue.End <= cue.Start {
			continue
		}
		cues = append(cues, cue)
	}
	return subtitles.Track{Language: language, Label: label, Automatic: true, Source: "automatic", Cues: cues}
}
