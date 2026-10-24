package automaticsubtitles

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"tangentry/internal/automaticsubtitles/worker"
	"tangentry/internal/subtitles"
	"tangentry/internal/transcription"
)

type transcriptionFixture struct {
	job   transcription.Job
	track subtitles.Track
	calls int
}

type recognizerFixture struct {
	track subtitles.Track
	calls chan AudioChunk
}

type unavailableRecognizer struct{ recognizerFixture }

func (unavailableRecognizer) StreamingReady() bool { return false }

type blockingRecognizer struct {
	started chan struct{}
	release chan struct{}
}

func (f *blockingRecognizer) RecognizePCM(ctx context.Context, _ string, _ []byte, _ float64) (subtitles.Track, error) {
	select {
	case f.started <- struct{}{}:
	default:
	}
	select {
	case <-f.release:
		return subtitles.Track{Cues: []subtitles.Cue{{Start: 0, End: 1, Text: "迟到字幕"}}}, nil
	case <-ctx.Done():
		return subtitles.Track{}, ctx.Err()
	}
}

func (f *recognizerFixture) RecognizePCM(_ context.Context, _ string, pcm []byte, offset float64) (subtitles.Track, error) {
	chunk := AudioChunk{Data: append([]byte(nil), pcm...), StartSeconds: offset}
	if f.calls != nil {
		f.calls <- chunk
	}
	track := f.track
	for index := range track.Cues {
		track.Cues[index].Start += offset
		track.Cues[index].End += offset
	}
	return track, nil
}

func (f *transcriptionFixture) StartAutomatic(context.Context, string, string) (transcription.Job, error) {
	f.calls++
	return f.job, nil
}
func (f *transcriptionFixture) Get(string) (transcription.Job, error) { return f.job, nil }
func (f *transcriptionFixture) Result(string) (subtitles.Track, bool) { return f.track, true }
func (f *transcriptionFixture) Cancel(string) error                   { return nil }

type savedFixture struct {
	tracks []subtitles.SavedTrack
	saved  []subtitles.SavedTrack
}

func (f *savedFixture) List(context.Context, string) ([]subtitles.SavedTrack, error) {
	return append([]subtitles.SavedTrack(nil), f.tracks...), nil
}
func (f *savedFixture) Save(_ context.Context, id string, track subtitles.Track, revision int64) (subtitles.SavedTrack, error) {
	if revision != 0 {
		return subtitles.SavedTrack{}, errors.New("unexpected revision")
	}
	value := subtitles.SavedTrack{ContentID: id, Track: track, Revision: 1}
	f.saved = append(f.saved, value)
	return value, nil
}

type sessionStoreFixture struct{ values map[string]StoredSession }

func (f *sessionStoreFixture) ListAutomaticSessions(_ context.Context, contentID string) ([]StoredSession, error) {
	values := make([]StoredSession, 0)
	for _, value := range f.values {
		if value.Session.ContentID == contentID {
			values = append(values, value)
		}
	}
	return values, nil
}
func (f *sessionStoreFixture) GetAutomaticSession(_ context.Context, id string) (StoredSession, error) {
	value, ok := f.values[id]
	if !ok {
		return StoredSession{}, ErrNotFound
	}
	return value, nil
}
func (f *sessionStoreFixture) SaveAutomaticSession(_ context.Context, value StoredSession) error {
	if f.values == nil {
		f.values = map[string]StoredSession{}
	}
	f.values[value.Session.ID] = value
	return nil
}

func TestSessionFinalizesDraftWithoutStartingTwice(t *testing.T) {
	transcriber := &transcriptionFixture{
		job:   transcription.Job{ID: "job-1", State: "completed"},
		track: subtitles.Track{Language: "zh", Label: "自动字幕", Automatic: true, Source: "automatic", Cues: []subtitles.Cue{{Start: 0, End: 1, Text: "第一句"}}},
	}
	saved := &savedFixture{}
	service := New(transcriber, saved)
	first, err := service.Start(context.Background(), "youtube:dQw4w9WgXcQ", "zh", "batch-only", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Start(context.Background(), "youtube:dQw4w9WgXcQ", "zh", "batch-only", 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID || transcriber.calls != 1 {
		t.Fatalf("session was duplicated: first=%#v second=%#v calls=%d", first, second, transcriber.calls)
	}
	draft, err := service.Draft(first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(draft.Track.Cues) != 1 || service.Current(first.ContentID).State != Finalizing {
		t.Fatalf("draft was not surfaced: %#v", draft)
	}
	if _, err := service.Finalize(context.Background(), first.ID); err != nil {
		t.Fatal(err)
	}
	if len(saved.saved) != 1 || saved.saved[0].Track.Source != "automatic" {
		t.Fatalf("formal subtitle was not saved: %#v", saved.saved)
	}
}

func TestUnavailableStreamingRecognizerFallsBackToBatch(t *testing.T) {
	recognizer := unavailableRecognizer{}
	service := New(&transcriptionFixture{job: transcription.Job{ID: "job-1", State: "running"}}, &savedFixture{})
	service.SetStreamingRecognizer(&recognizer)
	created, err := service.Start(context.Background(), "youtube:dQw4w9WgXcQ", "zh", "streamable", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if created.Capability != "batch-only" || created.State != Streaming {
		t.Fatalf("unsupported streaming should use batch mode: %#v", created)
	}
}

func TestPersistedStreamingSessionBecomesPausedAfterReload(t *testing.T) {
	store := &sessionStoreFixture{}
	old := &transcriptionFixture{job: transcription.Job{ID: "job-old", State: "running"}}
	saved := &savedFixture{}
	first := New(old, saved, store)
	created, err := first.Start(context.Background(), "youtube:dQw4w9WgXcQ", "zh", "batch-only", 5, 20)
	if err != nil {
		t.Fatal(err)
	}
	if created.State != Streaming {
		t.Fatalf("expected streaming state, got %s", created.State)
	}
	reloaded := New(&transcriptionFixture{}, saved, store)
	value := reloaded.Current(created.ContentID)
	if value == nil || value.State != Paused || value.JobID != "" {
		t.Fatalf("expected paused persisted session, got %#v", value)
	}
}

func TestPlaybackPauseAndResumeKeepsOneSession(t *testing.T) {
	transcriber := &transcriptionFixture{job: transcription.Job{ID: "job-1", State: "running"}}
	service := New(transcriber, &savedFixture{})
	created, err := service.Start(context.Background(), "youtube:dQw4w9WgXcQ", "zh", "batch-only", 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	paused, err := service.Playback(created.ID, Playback{PositionSeconds: 4, DurationSeconds: 20, Playing: false})
	if err != nil || paused.State != Paused || paused.ProcessedSeconds != 4 {
		t.Fatalf("pause = %#v, err=%v", paused, err)
	}
	resumed, err := service.Playback(created.ID, Playback{PositionSeconds: 5, DurationSeconds: 20, Playing: true})
	if err != nil || resumed.State != Streaming || resumed.FurthestPosition != 5 {
		t.Fatalf("resume = %#v, err=%v", resumed, err)
	}
	reused, err := service.Start(context.Background(), created.ContentID, "zh", "batch-only", 5, 20)
	if err != nil || reused.ID != created.ID || transcriber.calls != 1 {
		t.Fatalf("session duplicated after resume: %#v, err=%v, calls=%d", reused, err, transcriber.calls)
	}
}

func TestPlaybackRejectsNonFinitePosition(t *testing.T) {
	transcriber := &transcriptionFixture{job: transcription.Job{ID: "job-1", State: "running"}}
	service := New(transcriber, &savedFixture{})
	created, err := service.Start(context.Background(), "youtube:dQw4w9WgXcQ", "zh", "batch-only", 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Playback(created.ID, Playback{PositionSeconds: math.NaN()}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("NaN position error = %v", err)
	}
}

func TestWorkerEventsBecomePersistedDraft(t *testing.T) {
	transcriber := &transcriptionFixture{job: transcription.Job{ID: "job-1", State: "running"}}
	store := &sessionStoreFixture{}
	service := New(transcriber, &savedFixture{}, store)
	created, err := service.Start(context.Background(), "youtube:dQw4w9WgXcQ", "zh", "streamable", 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ApplyWorkerEvent(created.ID, worker.Event{Type: "ready", SessionID: created.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ApplyWorkerEvent(created.ID, worker.Event{Type: "partial", SessionID: created.ID, Sequence: 1, Start: 0, End: 1, Text: "你好"}); err != nil {
		t.Fatal(err)
	}
	draft, err := service.ApplyWorkerEvent(created.ID, worker.Event{Type: "final", SessionID: created.ID, Sequence: 1, Start: 0, End: 1.2, Text: "你好世界"})
	if err != nil {
		t.Fatal(err)
	}
	if len(draft.Track.Cues) != 1 || draft.Track.Cues[0].Text != "你好世界" {
		t.Fatalf("draft = %#v", draft)
	}
	persisted := store.values[created.ID]
	if len(persisted.Draft.Cues) != 1 || persisted.Draft.Cues[0].End != 1.2 {
		t.Fatalf("persisted = %#v", persisted)
	}
}

func TestStreamingFinalFrameDrainsBeforeFinalize(t *testing.T) {
	recognizer := &recognizerFixture{
		track: subtitles.Track{Language: "zh", Label: "实时本机字幕", Automatic: true, Source: "automatic", Cues: []subtitles.Cue{{Start: 0, End: 1, Text: "最后一句"}}},
		calls: make(chan AudioChunk, 1),
	}
	saved := &savedFixture{}
	service := New(&transcriptionFixture{}, saved)
	service.SetStreamingRecognizer(recognizer)
	created, err := service.Start(context.Background(), "youtube:dQw4w9WgXcQ", "zh", "streamable", 0, 10)
	if err != nil || created.Capability != "streamable" {
		t.Fatalf("streaming session = %#v, err=%v", created, err)
	}
	if _, err := service.AppendAudio(created.ID, AudioChunk{Data: []byte{1, 0, 2, 0}, StartSeconds: 9, SampleRate: PCMRate, Channels: PCMChannels, Final: true}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-recognizer.calls:
	case <-time.After(time.Second):
		t.Fatal("recognizer did not receive final audio")
	}
	if _, err := service.Playback(created.ID, Playback{PositionSeconds: 10, DurationSeconds: 10, Ended: true}); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(time.Second)
	for len(saved.saved) == 0 {
		select {
		case <-deadline:
			t.Fatalf("final draft was not saved: %#v", saved.saved)
		case <-time.After(5 * time.Millisecond):
		}
	}
	if saved.saved[0].Track.Cues[0].Text != "最后一句" || saved.saved[0].Track.Cues[0].Start != 9 {
		t.Fatalf("saved final cue = %#v", saved.saved[0].Track.Cues)
	}
}

func TestPausedStreamDrainsUploadedAudioWithoutResuming(t *testing.T) {
	recognizer := &blockingRecognizer{started: make(chan struct{}, 1), release: make(chan struct{})}
	service := New(&transcriptionFixture{}, &savedFixture{})
	service.SetStreamingRecognizer(recognizer)
	created, err := service.Start(context.Background(), "youtube:dQw4w9WgXcQ", "zh", "streamable", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.AppendAudio(created.ID, AudioChunk{Data: []byte{1, 0}, StartSeconds: 0, SampleRate: PCMRate, Channels: PCMChannels}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-recognizer.started:
	case <-time.After(time.Second):
		t.Fatal("recognizer did not start")
	}
	paused, err := service.Playback(created.ID, Playback{PositionSeconds: 1, DurationSeconds: 10, Playing: false})
	if err != nil || paused.State != Paused {
		t.Fatalf("pause = %#v, err=%v", paused, err)
	}
	close(recognizer.release)
	deadline := time.After(time.Second)
	for {
		value, getErr := service.Get(created.ID)
		if getErr != nil {
			t.Fatal(getErr)
		}
		if value.State != Paused {
			t.Fatalf("drained stream event revived session: %#v", value)
		}
		draft, draftErr := service.Draft(created.ID)
		if draftErr != nil {
			t.Fatal(draftErr)
		}
		if len(draft.Track.Cues) == 1 && draft.Track.Cues[0].Text == "迟到字幕" {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("paused stream did not publish its already-uploaded cue: %#v", draft.Track.Cues)
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func TestSeekDiscardsOldStreamBeforeStartingNewPlayback(t *testing.T) {
	recognizer := &recognizerFixture{calls: make(chan AudioChunk, 4), track: subtitles.Track{Cues: []subtitles.Cue{{Start: 0, End: 1, Text: "片段"}}}}
	service := New(&transcriptionFixture{}, &savedFixture{})
	service.SetStreamingRecognizer(recognizer)
	created, err := service.Start(context.Background(), "youtube:dQw4w9WgXcQ", "zh", "streamable", 0, 30)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.AppendAudio(created.ID, AudioChunk{Data: []byte{1, 0}, StartSeconds: 0, SampleRate: PCMRate, Channels: PCMChannels}); err != nil {
		t.Fatal(err)
	}
	select {
	case first := <-recognizer.calls:
		if first.StartSeconds != 0 {
			t.Fatalf("first chunk start = %v", first.StartSeconds)
		}
	case <-time.After(time.Second):
		t.Fatal("first chunk was not recognized")
	}
	paused, err := service.Playback(created.ID, Playback{PositionSeconds: 12, DurationSeconds: 30, Playing: true, Seeked: true})
	if err != nil || paused.State != Paused {
		t.Fatalf("seek transition = %#v, err=%v", paused, err)
	}
	// Starting playback again reuses the persisted draft but creates a fresh
	// stream generation, so audio from the old timeline cannot leak through.
	resumed, err := service.Start(context.Background(), created.ContentID, "zh", "streamable", 12, 30)
	if err != nil || resumed.ID != created.ID || resumed.State != Streaming {
		t.Fatalf("resumed session = %#v, err=%v", resumed, err)
	}
	if _, err := service.AppendAudio(created.ID, AudioChunk{Data: []byte{2, 0}, StartSeconds: 12, SampleRate: PCMRate, Channels: PCMChannels}); err != nil {
		t.Fatal(err)
	}
	select {
	case second := <-recognizer.calls:
		if second.StartSeconds != 12 {
			t.Fatalf("new chunk start = %v, want 12", second.StartSeconds)
		}
	case <-time.After(time.Second):
		t.Fatal("new stream did not receive audio")
	}
}
