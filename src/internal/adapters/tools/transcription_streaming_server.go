package tools

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"mime/multipart"
	"net"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/longbridgeapp/opencc"
	"tangentry/internal/subtitles"
	"tangentry/internal/transcription"
)

// residentStreamingServer keeps whisper.cpp's model loaded between chunks.
// Requests are serialized because the browser already sends audio in order and
// a single local model should not compete with itself for the GPU.
type residentStreamingServer struct {
	cmd       *exec.Cmd
	cancel    context.CancelFunc
	done      chan struct{}
	waitMu    sync.Mutex
	waitErr   error
	endpoint  string
	client    *http.Client
	requestMu sync.Mutex
}

func (t *Transcriber) residentServer(ctx context.Context, cfg transcription.Config) (*residentStreamingServer, error) {
	if cfg.Cloud() || cfg.Backend != "whisper.cpp" || cfg.Model == "" {
		return nil, transcription.ErrInvalid
	}
	binary := executable(t.StreamingBinary, "whisper-server")
	if binary == "" {
		return nil, transcription.ErrUnavailable
	}
	key := binary + "\x00" + cfg.Model
	t.streamingMu.Lock()
	defer t.streamingMu.Unlock()
	if t.streaming != nil && t.streamingKey == key {
		select {
		case <-t.streaming.done:
			// The model worker exited between chunks. Drop it and start a
			// fresh process below; the caller still owns the current PCM frame.
			t.streaming = nil
			t.streamingKey = ""
		default:
			return t.streaming, nil
		}
	}
	if t.streaming != nil {
		_ = t.streaming.close()
		t.streaming = nil
		t.streamingKey = ""
	}
	server, err := startResidentStreamingServer(ctx, binary, cfg.Model)
	if err != nil {
		return nil, err
	}
	t.streaming, t.streamingKey = server, key
	return server, nil
}

func startResidentStreamingServer(ctx context.Context, binary, model string) (*residentStreamingServer, error) {
	port, err := freeLoopbackPort()
	if err != nil {
		return nil, transcription.ErrUnavailable
	}
	processCtx, cancel := context.WithCancel(context.Background())
	cmd := command(processCtx, binary, "-m", model, "--host", "127.0.0.1", "--port", strconv.Itoa(port))
	cmd.Env = toolEnvironment()
	cmd.Stdout = io.Discard
	warnings := &boundedBuffer{}
	cmd.Stderr = warnings
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, transcription.ErrUnavailable
	}
	server := &residentStreamingServer{
		cmd: cmd, cancel: cancel, done: make(chan struct{}),
		endpoint: fmt.Sprintf("http://127.0.0.1:%d/inference", port),
		client:   &http.Client{Timeout: 90 * time.Second},
	}
	go func() {
		err := cmd.Wait()
		server.waitMu.Lock()
		server.waitErr = err
		server.waitMu.Unlock()
		close(server.done)
	}()
	ready := make(chan struct{})
	go func() {
		defer close(ready)
		for {
			requestCtx, requestCancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
			request, _ := http.NewRequestWithContext(requestCtx, http.MethodGet, server.endpoint, nil)
			response, err := server.client.Do(request)
			requestCancel()
			if response != nil {
				response.Body.Close()
			}
			if err == nil {
				return
			}
			select {
			case <-server.done:
				return
			case <-time.After(40 * time.Millisecond):
			}
		}
	}()
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	select {
	case <-ready:
		select {
		case <-server.done:
			if err := server.err(); err != nil {
				return nil, toolFailure("streaming_start", warnings.String())
			}
			return nil, transcription.ErrUnavailable
		default:
			return server, nil
		}
	case <-server.done:
		return nil, toolFailure("streaming_start", warnings.String())
	case <-timer.C:
		_ = server.close()
		return nil, transcription.Failed("streaming_start", "本机实时识别服务启动超时，请检查 Whisper 模型与工具配置。")
	case <-ctx.Done():
		_ = server.close()
		return nil, ctx.Err()
	}
}

func freeLoopbackPort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	address, ok := listener.Addr().(*net.TCPAddr)
	if !ok || address.Port == 0 {
		return 0, errors.New("loopback port unavailable")
	}
	return address.Port, nil
}

func (s *residentStreamingServer) recognize(ctx context.Context, pcm []byte, language string) (subtitles.Track, error) {
	s.requestMu.Lock()
	defer s.requestMu.Unlock()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile("file", "chunk.wav")
	if err != nil {
		return subtitles.Track{}, transcription.ErrUnavailable
	}
	if err := writePCM16(part, pcm, 16000, 1); err != nil {
		return subtitles.Track{}, transcription.ErrUnavailable
	}
	if err := form.WriteField("response_format", "verbose_json"); err != nil {
		return subtitles.Track{}, transcription.ErrUnavailable
	}
	if err := form.WriteField("language", language); err != nil {
		return subtitles.Track{}, transcription.ErrUnavailable
	}
	if err := form.WriteField("temperature", "0"); err != nil {
		return subtitles.Track{}, transcription.ErrUnavailable
	}
	if err := form.Close(); err != nil {
		return subtitles.Track{}, transcription.ErrUnavailable
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, &body)
	if err != nil {
		return subtitles.Track{}, transcription.ErrUnavailable
	}
	request.Header.Set("Content-Type", form.FormDataContentType())
	response, err := s.client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return subtitles.Track{}, ctx.Err()
		}
		return subtitles.Track{}, transcription.Failed("streaming_recognizing", "本机实时识别服务暂时不可用，请稍后重试。")
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil || len(data) > 1<<20 {
		return subtitles.Track{}, subtitles.ErrFormat
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return subtitles.Track{}, toolFailure("streaming_recognizing", string(data))
	}
	return parseWhisperServer(data, language)
}

func (s *residentStreamingServer) close() error {
	if s == nil {
		return nil
	}
	s.cancel()
	select {
	case <-s.done:
		return s.err()
	case <-time.After(3 * time.Second):
		return errors.New("streaming worker did not stop")
	}
}

func (s *residentStreamingServer) err() error {
	s.waitMu.Lock()
	defer s.waitMu.Unlock()
	return s.waitErr
}

func writePCM16(w io.Writer, pcm []byte, sampleRate, channels int) error {
	if len(pcm) == 0 || sampleRate <= 0 || channels <= 0 {
		return subtitles.ErrFormat
	}
	dataSize := uint32(len(pcm))
	byteRate := uint32(sampleRate * channels * 2)
	blockAlign := uint16(channels * 2)
	write := func(value any) error { return binary.Write(w, binary.LittleEndian, value) }
	if _, err := io.WriteString(w, "RIFF"); err != nil {
		return err
	}
	if err := write(uint32(36) + dataSize); err != nil {
		return err
	}
	if _, err := io.WriteString(w, "WAVEfmt "); err != nil {
		return err
	}
	if err := write(uint32(16)); err != nil {
		return err
	}
	if err := write(uint16(1)); err != nil {
		return err
	}
	if err := write(uint16(channels)); err != nil {
		return err
	}
	if err := write(uint32(sampleRate)); err != nil {
		return err
	}
	if err := write(byteRate); err != nil {
		return err
	}
	if err := write(blockAlign); err != nil {
		return err
	}
	if err := write(uint16(16)); err != nil {
		return err
	}
	if _, err := io.WriteString(w, "data"); err != nil {
		return err
	}
	if err := write(dataSize); err != nil {
		return err
	}
	_, err := w.Write(pcm)
	return err
}

// parseWhisperServer converts whisper.cpp's OpenAI-compatible verbose JSON
// into Tangentry cues while keeping the same validation and Chinese cleanup as
// the existing CLI parser.
func parseWhisperServer(data []byte, language string) (subtitles.Track, error) {
	var value struct {
		Language string `json:"language"`
		Segments []struct {
			Start float64 `json:"start"`
			End   float64 `json:"end"`
			Text  string  `json:"text"`
		} `json:"segments"`
	}
	if err := json.Unmarshal(data, &value); err != nil || len(value.Segments) > 10000 {
		return subtitles.Track{}, subtitles.ErrFormat
	}
	track := subtitles.Track{Language: "whisper", Label: "实时本机字幕", Automatic: true, Source: "automatic", Cues: make([]subtitles.Cue, 0, len(value.Segments))}
	previousStart := -1.0
	for _, segment := range value.Segments {
		if !finite(segment.Start) || !finite(segment.End) || segment.Start < 0 || segment.Start < previousStart || segment.End <= segment.Start || segment.End > 6*3600 || strings.TrimSpace(segment.Text) == "" {
			return subtitles.Track{}, subtitles.ErrFormat
		}
		track.Cues = append(track.Cues, subtitles.Cue{Start: segment.Start, End: segment.End, Text: strings.TrimSpace(segment.Text)})
		previousStart = segment.Start
	}
	if language == "zh" || language == "auto" && (strings.HasPrefix(strings.ToLower(value.Language), "zh") || strings.Contains(strings.ToLower(value.Language), "chinese")) {
		return normalizeStreamingChinese(track)
	}
	return track, nil
}

func finite(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }

func normalizeStreamingChinese(track subtitles.Track) (subtitles.Track, error) {
	converter, err := opencc.New("t2s")
	if err != nil {
		return subtitles.Track{}, transcription.ErrUnavailable
	}
	for index := range track.Cues {
		text, err := converter.Convert(track.Cues[index].Text)
		if err != nil {
			return subtitles.Track{}, transcription.ErrUnavailable
		}
		track.Cues[index].Text = text
	}
	track.Label = "简体中文 · 实时本机字幕"
	return track, nil
}

func (t *Transcriber) Close() error {
	t.streamingMu.Lock()
	defer t.streamingMu.Unlock()
	if t.streaming == nil {
		return nil
	}
	err := t.streaming.close()
	t.streaming = nil
	t.streamingKey = ""
	return err
}

// ResidentStreamingReady reports whether the optional local worker can be
// started for the current configuration without actually loading a model.
// The UI can use this as a capability hint; recognition still validates the
// model and reports a precise runtime error when a session begins.
func (t *Transcriber) ResidentStreamingReady(cfg transcription.Config) bool {
	return !cfg.Cloud() && cfg.Backend == "whisper.cpp" && executable(t.StreamingBinary, "whisper-server") != ""
}
