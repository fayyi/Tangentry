package tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
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

func TestCloudRequestsUseProviderFieldsAndNeverFollowRedirects(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audio.mp3")
	os.WriteFile(path, []byte("explicit synthetic audio fixture"), 0600)
	for _, backend := range []string{"groq", "cloudflare", "siliconflow"} {
		t.Run(backend, func(t *testing.T) {
			calls := 0
			engine := &Transcriber{CloudHTTP: &http.Client{Transport: speechTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Method != "POST" || r.Header.Get("Authorization") != "Bearer fixture-secret" || r.Header.Get("Cookie") != "" || r.ContentLength == 0 {
					t.Fatal("unsafe or incomplete request")
				}
				if backend == "cloudflare" {
					if r.URL.Host != "api.cloudflare.com" || !strings.Contains(r.URL.Path, "/accounts/"+strings.Repeat("a", 32)+"/ai/run/") {
						t.Fatal(r.URL)
					}
					var payload map[string]any
					if json.NewDecoder(r.Body).Decode(&payload) != nil {
						t.Fatal("invalid json")
					}
					data, err := base64.StdEncoding.DecodeString(payload["audio"].(string))
					if err != nil || string(data) != "explicit synthetic audio fixture" || payload["language"] != "zh" || payload["vad_filter"] != false {
						t.Fatal("incorrect audio payload")
					}
				} else {
					host := "api.groq.com"
					model := "whisper-large-v3"
					if backend == "siliconflow" {
						host, model = "api.siliconflow.cn", "FunAudioLLM/SenseVoiceSmall"
					}
					if r.URL.Host != host || r.ParseMultipartForm(1<<20) != nil {
						t.Fatal("wrong multipart endpoint")
					}
					defer r.MultipartForm.RemoveAll()
					if r.FormValue("model") != model {
						t.Fatal("wrong model")
					}
					if backend == "groq" && (r.FormValue("response_format") != "verbose_json" || r.FormValue("timestamp_granularities[]") != "segment" || r.FormValue("language") != "zh") {
						t.Fatal("missing timing fields")
					}
					if backend == "siliconflow" && r.FormValue("response_format") != "" {
						t.Fatal("unsupported timing format requested")
					}
					file, _, err := r.FormFile("file")
					if err != nil {
						t.Fatal(err)
					}
					data, _ := io.ReadAll(file)
					file.Close()
					if string(data) != "explicit synthetic audio fixture" {
						t.Fatal("wrong audio")
					}
				}
				return &http.Response{StatusCode: 307, Header: http.Header{"Location": []string{"https://untrusted.example/leak"}}, Body: io.NopCloser(strings.NewReader("fixture-secret"))}, nil
			})}}
			_, err := engine.cloudRequest(context.Background(), transcription.Config{Backend: backend, GroqModel: "whisper-large-v3", Language: "zh", GroqAPIKey: "fixture-secret", CloudflareAPIToken: "fixture-secret", SiliconflowAPIKey: "fixture-secret", CloudflareAccountID: strings.Repeat("a", 32)}, path)
			if err == nil || strings.Contains(err.Error(), "fixture-secret") || calls != 1 {
				t.Fatal("redirect followed or secret exposed", err, calls)
			}
		})
	}
}

func TestCloudFailureIsBoundedSanitizedAndNotRetried(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audio.mp3")
	os.WriteFile(path, []byte("fixture"), 0600)
	for _, code := range []int{401, 403, 429, 413, 500} {
		calls := 0
		engine := &Transcriber{CloudHTTP: &http.Client{Transport: speechTransport(func(r *http.Request) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader("secret-from-provider"))}, nil
		})}}
		_, err := engine.cloudRequest(context.Background(), transcription.Config{Backend: "groq"}, path)
		var failure *transcription.Failure
		if !errors.As(err, &failure) || strings.Contains(err.Error(), "secret-from-provider") || calls != 1 {
			t.Fatal(code, err, calls)
		}
	}
	if err := os.Truncate(path, cloudUploadLimit+1); err != nil {
		t.Fatal(err)
	}
	engine := &Transcriber{CloudHTTP: &http.Client{Transport: speechTransport(func(*http.Request) (*http.Response, error) { t.Fatal("oversized file uploaded"); return nil, nil })}}
	if _, err := engine.cloudRequest(context.Background(), transcription.Config{Backend: "groq"}, path); err == nil {
		t.Fatal("size bound lost")
	}
}

func TestCloudTimingVTTAndCoarseRecognition(t *testing.T) {
	for _, raw := range []string{
		`{"language":"chinese","segments":[{"start":1.25,"end":4,"text":"聽說這個視頻。"}]}`,
		`{"transcription_info":{"language":"zh"},"vtt":"WEBVTT\n\n00:01.250 --> 00:04.000\n聽說這個視頻。\n"}`,
	} {
		cues, err := parseCloudSpeech([]byte(raw), "auto", 60)
		if err != nil || len(cues) != 1 || cues[0].Start != 1.25 || cues[0].End != 4 || cues[0].Text != "听说这个视频。" {
			t.Fatal(cues, err)
		}
	}
	if cues, err := parseCloudSpeech([]byte(`{"text":"","segments":[]}`), "auto", 60); err != nil || len(cues) != 0 {
		t.Fatal("silent cloud chunk invalidated full recording", err)
	}
	for _, raw := range []string{
		`{"text":"no timing"}`, `{"segments":[{"end":4,"text":"missing start"}]}`,
		`{"segments":[{"start":-1,"end":4,"text":"negative"}]}`,
		`{"segments":[{"start":1,"end":70,"text":"beyond chunk"}]}`,
		`{"segments":[{"start":2,"end":4,"text":"first"},{"start":1,"end":2,"text":"reversed"}]}`,
	} {
		if _, err := parseCloudSpeech([]byte(raw), "auto", 60); err == nil {
			t.Fatal("bad timing accepted", raw)
		}
	}
	cues, err := parseSiliconflowSpeech([]byte(`{"text":"<|zh|><|NEUTRAL|><|Speech|><|withitn|>聽說這個視頻。"}`), "auto", 30)
	if err != nil || len(cues) != 1 || cues[0].Start != 0 || cues[0].End != 30 || cues[0].Text != "听说这个视频。" {
		t.Fatal(cues, err)
	}
	cues, err = parseSiliconflowSpeech([]byte(`{"text":"<|nospeech|>"}`), "auto", 30)
	if err != nil || len(cues) != 0 {
		t.Fatal("silent chunk invented captions")
	}
	if _, err := parseSiliconflowSpeech([]byte(`{}`), "zh", 30); err == nil {
		t.Fatal("malformed text accepted")
	}
}

func TestCloudSpeechValidationDoesNotDependOnCache(t *testing.T) {
	for _, duration := range []float64{0, -1, 21601, math.NaN(), math.Inf(1)} {
		if _, err := parseCloudSpeech([]byte(`{"text":"","segments":[]}`), "auto", duration); err != subtitles.ErrFormat {
			t.Fatal("invalid duration accepted", duration, err)
		}
	}
	raw, err := json.Marshal(map[string]any{"segments": []map[string]any{{"start": 0, "end": 1, "text": strings.Repeat("x", 8001)}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parseCloudSpeech(raw, "auto", 30); err != subtitles.ErrFormat {
		t.Fatal("oversized cue accepted without cache", err)
	}
	raw, err = json.Marshal(map[string]any{"segments": []map[string]any{{"start": 0, "end": 1, "text": strings.Repeat("x", 8000)}}})
	if err != nil {
		t.Fatal(err)
	}
	cues, err := parseCloudSpeech(raw, "auto", 30)
	if err != nil || len(cues) != 1 || len(cues[0].Text) != 8000 {
		t.Fatal("valid boundary rejected", err)
	}
}

func TestRealCloudAudioCuttingAndTimeOffsets(t *testing.T) {
	ffmpeg := executable("", "ffmpeg")
	if ffmpeg == "" {
		t.Skip("FFmpeg required for media splitting verification")
	}
	root := t.TempDir()
	input := filepath.Join(root, "source.wav")
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := command(ctx, ffmpeg, "-nostdin", "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=16000:duration=605", "-y", input)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatal(err, string(output))
	}
	for _, backend := range []string{"groq", "cloudflare", "siliconflow"} {
		t.Run(backend, func(t *testing.T) {
			calls := 0
			engine := &Transcriber{FFmpeg: ffmpeg, CloudHTTP: &http.Client{Transport: speechTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				payload := `{"language":"zh","segments":[{"start":0.5,"end":2,"text":"測試片段"}]}`
				if backend == "cloudflare" {
					payload = `{"success":true,"result":` + payload + `}`
				}
				if backend == "siliconflow" {
					payload = `{"text":"測試片段"}`
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(payload))}, nil
			})}}
			dir := t.TempDir()
			limit := 0
			if backend == "siliconflow" {
				limit = 60
			}
			cfg := transcription.Config{Backend: backend, Language: "zh", SiliconflowSegmentSeconds: 30}
			track, err := engine.transcribeCloud(ctx, cfg, input, dir, limit, func(string) {})
			wantCalls, lastStart := 2, 600.5
			if backend == "cloudflare" {
				wantCalls = 3
			}
			if backend == "siliconflow" {
				lastStart = 30
			}
			if err != nil || calls != wantCalls || len(track.Cues) != wantCalls {
				t.Fatal(track, err, calls)
			}
			if math.Abs(track.Cues[len(track.Cues)-1].Start-lastStart) > .1 {
				t.Fatal("chunk offset lost", track.Cues)
			}
			if track.Cues[0].Text != "测试片段" {
				t.Fatal("Chinese normalization lost")
			}
			if backend == "siliconflow" && (!strings.Contains(track.Label, "片段字幕") || track.Cues[len(track.Cues)-1].End != 60) {
				t.Fatal("coarse timing not bounded or labeled", track)
			}
		})
	}
}

func TestCloudCancellationAtSuccessfulResponseDoesNotCacheOrUploadNextChunk(t *testing.T) {
	ffmpeg := executable("", "ffmpeg")
	if ffmpeg == "" {
		t.Skip("FFmpeg required for media splitting verification")
	}
	root := t.TempDir()
	input := filepath.Join(root, "source.wav")
	cmd := command(context.Background(), ffmpeg, "-nostdin", "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=16000:duration=32", "-y", input)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatal(err, string(output))
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	cacheRoot := filepath.Join(root, "cache")
	engine := &Transcriber{FFmpeg: ffmpeg, CacheDir: cacheRoot, CloudHTTP: &http.Client{Transport: speechTransport(func(*http.Request) (*http.Response, error) {
		calls++
		cancel()
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"text":"cancelled response"}`))}, nil
	})}}
	track, err := engine.transcribeCloud(ctx, transcription.Config{Backend: "siliconflow", Language: "zh", SiliconflowSegmentSeconds: 15}, input, t.TempDir(), 0, func(string) {})
	if !errors.Is(err, context.Canceled) || len(track.Cues) != 0 || calls != 1 {
		t.Fatal("cancelled response completed or uploaded another chunk", track, err, calls)
	}
	entries, err := os.ReadDir(cacheRoot)
	if err != nil || len(entries) != 0 {
		t.Fatal("cancelled response was cached", entries, err)
	}
}

func TestCloudCancellationStopsRequest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audio.mp3")
	os.WriteFile(path, []byte("fixture"), 0600)
	ctx, cancel := context.WithCancel(context.Background())
	engine := &Transcriber{CloudHTTP: &http.Client{Transport: speechTransport(func(r *http.Request) (*http.Response, error) {
		cancel()
		<-r.Context().Done()
		return nil, r.Context().Err()
	})}}
	_, err := engine.cloudRequest(ctx, transcription.Config{Backend: "groq"}, path)
	if !errors.Is(err, context.Canceled) {
		t.Fatal("cancel lost", err)
	}
}

func TestCloudResumeAfterCancellationReusesOnlyCompletedChunks(t *testing.T) {
	ffmpeg := executable("", "ffmpeg")
	if ffmpeg == "" {
		t.Skip("FFmpeg required for media splitting verification")
	}
	root := t.TempDir()
	input := filepath.Join(root, "source.wav")
	cmd := command(context.Background(), ffmpeg, "-nostdin", "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=16000:duration=32", "-y", input)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatal(err, string(output))
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	engine := &Transcriber{FFmpeg: ffmpeg, CacheDir: filepath.Join(root, "cache"), CloudHTTP: &http.Client{Transport: speechTransport(func(*http.Request) (*http.Response, error) {
		calls++
		if calls == 2 {
			cancel()
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"text":"completed chunk"}`))}, nil
	})}}
	cfg := transcription.Config{Backend: "siliconflow", Language: "zh", SiliconflowSegmentSeconds: 15}
	track, err := engine.transcribeCloud(ctx, cfg, input, t.TempDir(), 0, func(string) {})
	if !errors.Is(err, context.Canceled) || len(track.Cues) != 0 || calls != 2 {
		t.Fatal("cancellation returned partial track", track, err, calls)
	}
	entries, err := os.ReadDir(engine.CacheDir)
	if err != nil || len(entries) != 1 {
		t.Fatal("completed chunk cache not preserved", entries, err)
	}
	cached := 0
	track, err = engine.transcribeCloud(context.Background(), cfg, input, t.TempDir(), 0, func(stage string) {
		if stage == "cached" {
			cached++
		}
	})
	if err != nil || len(track.Cues) != 3 || calls != 4 || cached != 1 {
		t.Fatal("resume repeated completed upload or reused cancelled response", track, err, calls, cached)
	}
	if track.Cues[0].Start != 0 || math.Abs(track.Cues[1].Start-15) > .1 || math.Abs(track.Cues[2].Start-30) > .1 {
		t.Fatal("resume changed original time offsets", track.Cues)
	}
}
