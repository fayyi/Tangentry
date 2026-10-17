package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"tangentry/internal/subtitles"
	"tangentry/internal/transcription"
)

func TestSpeechCacheUnreadableEntryIsNotAnUploadCacheMiss(t *testing.T) {
	cache, err := newSpeechCache(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	key := strings.Repeat("a", 64)
	path := filepath.Join(cache.root, key+".json")
	if err := os.WriteFile(path, []byte("private cached text"), 0000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(path, 0600)
	if file, err := os.Open(path); err == nil {
		file.Close()
		t.Skip("filesystem permits reading mode-000 files")
	}
	if _, found, err := cache.load(key, 30); found || err == nil {
		t.Fatal("unreadable cache treated as a recoverable miss", found, err)
	}
}

func TestSpeechCacheIdentityAndPrivatePersistence(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0755); err != nil {
		t.Fatal(err)
	}
	cache, err := newSpeechCache(root)
	if err != nil {
		t.Fatal(err)
	}
	rootInfo, err := os.Stat(root)
	if err != nil || rootInfo.Mode().Perm() != 0700 {
		t.Fatal("existing cache directory permissions not private")
	}
	audio := filepath.Join(t.TempDir(), "audio.mp3")
	os.WriteFile(audio, []byte("explicit compressed audio fixture"), 0600)
	chunk := speechChunk{path: audio, start: 0, end: 30}
	cfg := transcription.Config{Backend: "groq", Language: "zh", GroqAPIKey: "private-fixture-key"}
	key, err := speechCacheKey(context.Background(), cfg, chunk, 30)
	if err != nil {
		t.Fatal(err)
	}
	rotated := cfg
	rotated.GroqAPIKey = "another-private-fixture-key"
	rotated.GroqModel = "whisper-large-v3-turbo"
	rotatedKey, _ := speechCacheKey(context.Background(), rotated, chunk, 30)
	if key != rotatedKey {
		t.Fatal("credential replacement invalidated the same recognition input")
	}
	for _, changed := range []transcription.Config{
		{Backend: "groq", Language: "en"}, {Backend: "groq", Language: "zh", GroqModel: "whisper-large-v3"},
		{Backend: "siliconflow", Language: "zh"}, {Backend: "cloudflare", Language: "zh"},
	} {
		other, _ := speechCacheKey(context.Background(), changed, chunk, 30)
		if other == key {
			t.Fatal("recognition settings mixed in cache")
		}
	}
	shorter, _ := speechCacheKey(context.Background(), cfg, chunk, 29)
	if shorter == key {
		t.Fatal("clipped timing reused")
	}
	cues := []subtitles.Cue{{Start: 1, End: 4, Text: "缓存识别文字"}}
	if err := cache.save(key, cues, 30); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, key+".json")
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal("cache permissions not private")
	}
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "private-fixture-key") || strings.Contains(string(data), audio) {
		t.Fatal("credentials or audio path persisted")
	}
	reopened, err := newSpeechCache(root)
	if err != nil {
		t.Fatal(err)
	}
	got, found, loadErr := reopened.load(key, 30)
	if loadErr != nil || !found || len(got) != 1 || got[0] != cues[0] {
		t.Fatal("restarted service did not recover cached recognition")
	}
	os.WriteFile(audio, []byte("different recording"), 0600)
	different, _ := speechCacheKey(context.Background(), cfg, chunk, 30)
	if different == key {
		t.Fatal("changed audio reused stale captions")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := speechCacheKey(ctx, cfg, chunk, 30); !errors.Is(err, context.Canceled) {
		t.Fatal("hashing ignored cancellation")
	}
}

func TestSpeechCacheRejectsCorruptTimingsAndUnsafePaths(t *testing.T) {
	root := t.TempDir()
	cache, err := newSpeechCache(root)
	if err != nil {
		t.Fatal(err)
	}
	key := strings.Repeat("a", 64)
	path := filepath.Join(root, key+".json")
	for _, cues := range [][]subtitles.Cue{
		{{Start: -1, End: 4, Text: "invalid"}}, {{Start: 1, End: 31, Text: "outside chunk"}},
		{{Start: 2, End: 4, Text: "first"}, {Start: 1, End: 2, Text: "reversed"}},
		{{Start: 1, End: 4, Text: ""}}, {{Start: math.NaN(), End: 4, Text: "not finite"}},
	} {
		if cache.save(key, cues, 30) == nil {
			t.Fatal("invalid checkpoint accepted")
		}
	}
	for _, body := range []string{
		`{"version":99,"key":"` + key + `","cues":[]}`,
		`{"version":1,"key":"other","cues":[]}`,
		`{"version":1,"key":"` + key + `","cues":[{"start":1,"end":31,"text":"bad"}]}`,
		`{"version":1,"key":"` + key + `","cues":null}`,
		`not json`,
	} {
		os.WriteFile(path, []byte(body), 0600)
		if _, found, err := cache.load(key, 30); found || err != nil {
			t.Fatal("corrupt checkpoint reused")
		}
	}
	os.Remove(path)
	target := filepath.Join(t.TempDir(), "target")
	os.WriteFile(target, []byte("outside fixture"), 0600)
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if _, found, err := cache.load(key, 30); found || err != nil {
		t.Fatal("cache followed symlink")
	}
	if err := cache.save(key, []subtitles.Cue{}, 30); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(target); string(data) != "outside fixture" {
		t.Fatal("cache overwrote symlink target")
	}
	if cues, found, err := cache.load(key, 30); err != nil || !found || len(cues) != 0 {
		t.Fatal("silent checkpoint was lost")
	}
	linkedRoot := filepath.Join(t.TempDir(), "cache")
	os.Symlink(root, linkedRoot)
	if _, err := newSpeechCache(linkedRoot); err == nil {
		t.Fatal("symlink cache root accepted")
	}
}

func TestSpeechCacheEvictsOldestEntriesWithinCountAndSizeBudgets(t *testing.T) {
	root := t.TempDir()
	old := time.Now().Add(-time.Hour)
	for i := 0; i < speechCacheFiles+1; i++ {
		path := filepath.Join(root, fmt.Sprintf("%064x.json", i))
		os.WriteFile(path, []byte("{}"), 0600)
		os.Chtimes(path, old.Add(time.Duration(i)*time.Second), old.Add(time.Duration(i)*time.Second))
	}
	unrelated := filepath.Join(root, "keep.txt")
	os.WriteFile(unrelated, []byte("not a managed cache entry"), 0600)
	cache, err := newSpeechCache(root)
	if err != nil || len(cache.entries) != speechCacheFiles {
		t.Fatal("file budget not enforced", err)
	}
	if _, err := os.Stat(filepath.Join(root, fmt.Sprintf("%064x.json", 0))); !os.IsNotExist(err) {
		t.Fatal("oldest entry not removed")
	}
	if _, err := os.Stat(unrelated); err != nil {
		t.Fatal("unrelated file removed")
	}
	large := filepath.Join(root, strings.Repeat("f", 64)+".json")
	file, err := os.Create(large)
	if err != nil {
		t.Fatal(err)
	}
	if err = file.Truncate(speechCacheBytes + 1); err != nil {
		t.Fatal(err)
	}
	file.Close()
	os.Chtimes(large, old.Add(-time.Hour), old.Add(-time.Hour))
	cache, err = newSpeechCache(root)
	if err != nil || cache.bytes > speechCacheBytes {
		t.Fatal("byte budget not enforced", err)
	}
	if _, err := os.Stat(large); !os.IsNotExist(err) {
		t.Fatal("old large file not evicted")
	}
}

func TestCloudTranscriptionDoesNotUploadWhenCachedAudioIsUnreadable(t *testing.T) {
	ffmpeg := executable("", "ffmpeg")
	if ffmpeg == "" {
		t.Skip("FFmpeg required for cache/upload boundary verification")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	input := filepath.Join(t.TempDir(), "source.wav")
	cmd := command(ctx, ffmpeg, "-nostdin", "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=16000:duration=2", "-y", input)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatal(err, string(output))
	}
	calls := 0
	client := &http.Client{Transport: speechTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"text":"明确缓存权限测试"}`))}, nil
	})}
	root := t.TempDir()
	engine := &Transcriber{FFmpeg: ffmpeg, CacheDir: root, CloudHTTP: client}
	cfg := transcription.Config{Backend: "siliconflow", Language: "zh", SiliconflowSegmentSeconds: 30}
	if track, err := engine.transcribeCloud(ctx, cfg, input, t.TempDir(), 0, func(string) {}); err != nil || len(track.Cues) != 1 || calls != 1 {
		t.Fatal("initial controlled upload failed", track, err, calls)
	}
	files, err := os.ReadDir(root)
	if err != nil || len(files) != 1 {
		t.Fatal("expected one successful cache entry", files, err)
	}
	path := filepath.Join(root, files[0].Name())
	if err := os.Chmod(path, 0000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(path, 0600)
	if file, err := os.Open(path); err == nil {
		file.Close()
		t.Skip("filesystem permits reading mode-000 files")
	}
	stages := []string{}
	track, err := engine.transcribeCloud(ctx, cfg, input, t.TempDir(), 0, func(stage string) { stages = append(stages, stage) })
	if err == nil || len(track.Cues) != 0 || calls != 1 || len(stages) != 0 {
		t.Fatal("unreadable cache triggered upload or returned captions", track, err, calls, stages)
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	track, err = engine.transcribeCloud(ctx, cfg, input, t.TempDir(), 0, func(stage string) { stages = append(stages, stage) })
	if err != nil || len(track.Cues) != 1 || calls != 1 || len(stages) != 1 || stages[0] != "cached" {
		t.Fatal("restored permissions did not recover without upload", track, err, calls, stages)
	}
}

func TestRealCloudTranscriptionResumesAfterFailureAndCancellation(t *testing.T) {
	ffmpeg := executable("", "ffmpeg")
	if ffmpeg == "" {
		t.Skip("FFmpeg required for independent chunk resume verification")
	}
	input := filepath.Join(t.TempDir(), "source.wav")
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := command(ctx, ffmpeg, "-nostdin", "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=16000:duration=65", "-y", input)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatal(err, string(output))
	}
	for _, mode := range []string{"failure", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			cfg := transcription.Config{Backend: "siliconflow", Language: "zh", SiliconflowSegmentSeconds: 30}
			calls := 0
			work, stop := context.WithCancel(ctx)
			defer stop()
			client := &http.Client{Transport: speechTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if calls == 2 {
					if mode == "cancel" {
						stop()
						<-r.Context().Done()
						return nil, r.Context().Err()
					}
					return &http.Response{StatusCode: 429, Body: io.NopCloser(strings.NewReader("private quota fixture"))}, nil
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"text":"保存这一片段"}`))}, nil
			})}
			engine := &Transcriber{FFmpeg: ffmpeg, CacheDir: root, CloudHTTP: client}
			track, err := engine.transcribeCloud(work, cfg, input, t.TempDir(), 0, func(string) {})
			if err == nil || len(track.Cues) != 0 || calls != 2 {
				t.Fatal("incomplete transcript returned", track, err, calls)
			}
			files, _ := os.ReadDir(root)
			if len(files) != 1 {
				t.Fatal("only successful chunk should be checkpointed", len(files))
			}
			engine = &Transcriber{FFmpeg: ffmpeg, CacheDir: root, CloudHTTP: client}
			stages := []string{}
			track, err = engine.transcribeCloud(ctx, cfg, input, t.TempDir(), 0, func(stage string) { stages = append(stages, stage) })
			if err != nil || len(track.Cues) != 3 || calls != 4 {
				t.Fatal("completed chunk uploaded again", track, err, calls, stages)
			}
			if len(stages) != 3 || stages[0] != "cached" || stages[1] != "uploading" {
				t.Fatal("resume not visible", stages)
			}
			if math.Abs(track.Cues[1].Start-30) > .1 || math.Abs(track.Cues[2].Start-60) > .1 {
				t.Fatal("resumed global timeline lost", track)
			}
			engine = &Transcriber{FFmpeg: ffmpeg, CacheDir: root, CloudHTTP: client}
			track, err = engine.transcribeCloud(ctx, cfg, input, t.TempDir(), 0, func(stage string) {
				if stage != "cached" {
					t.Fatal("fully cached job uploaded")
				}
			})
			if err != nil || calls != 4 || len(track.Cues) != 3 {
				t.Fatal("restart failed to reuse full result", err, calls)
			}
			for _, file := range files {
				var record cachedSpeech
				data, _ := os.ReadFile(filepath.Join(root, file.Name()))
				if json.Unmarshal(data, &record) != nil || len(record.Cues) != 1 {
					t.Fatal("invalid checkpoint")
				}
			}
		})
	}
}
