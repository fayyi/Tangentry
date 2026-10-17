package tools

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tangentry/internal/transcription"
)

func TestMAIExplicitSessionFlowAndCleanup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audio.mp3")
	if err := os.WriteFile(path, []byte("synthetic"), 0600); err != nil {
		t.Fatal(err)
	}
	var paths []string
	engine := &Transcriber{CloudHTTP: &http.Client{Transport: speechTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "playground.microsoft.ai" || r.Header.Get("Cookie") != "session=fixture" || r.Header.Get("Authorization") != "" {
			t.Fatal("credentials routed incorrectly")
		}
		paths = append(paths, r.URL.Path)
		body := `[{"result":{"data":{"json":{"conversation":{"id":"conversation","title":"test"}}}}}]`
		switch r.URL.Path {
		case "/api/trpc/conversations.addAudioPromptAndInitResponse":
			body = `[{"result":{"data":{"json":{"response":{"userMessage":{},"modelMessage":{"id":"message"}}}}}}]`
		case "/api/chat/stream":
			body = "event: message\ndata: {\"result\":{\"data\":{\"json\":{\"rich_transcription\":{\"utterances\":[{\"start\":1,\"end\":3,\"text\":\"你好\"}]}}}}}\n\ndata: [DONE]\n\n"
		case "/api/trpc/conversations.delete":
			body = `[{"result":{"data":{}}}]`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}}
	data, err := engine.cloudRequest(context.Background(), transcription.Config{Backend: "mai", MAICookie: "session=fixture"}, path)
	if err != nil {
		t.Fatal(err)
	}
	cues, err := parseCloudSpeech(data, "zh", 60)
	if err != nil || len(cues) != 1 || cues[0].Start != 1 || cues[0].End != 3 || len(paths) != 4 || paths[3] != "/api/trpc/conversations.delete" {
		t.Fatal(cues, err, paths)
	}
}

func TestMAIFailuresNeverRetryOrExposeSession(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audio.mp3")
	os.WriteFile(path, []byte("synthetic"), 0600)
	for _, status := range []int{401, 403, 451, 429, 307} {
		calls := 0
		engine := &Transcriber{CloudHTTP: &http.Client{Transport: speechTransport(func(r *http.Request) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: status, Header: http.Header{"Location": []string{"https://example.org/leak"}}, Body: io.NopCloser(strings.NewReader("session=private"))}, nil
		})}}
		_, err := engine.cloudRequest(context.Background(), transcription.Config{Backend: "mai", MAICookie: "session=private"}, path)
		if err == nil || strings.Contains(err.Error(), "private") || calls != 1 {
			t.Fatal(status, err, calls)
		}
	}
	for _, body := range []string{"data: [DONE]\n", "data: {\"rich_transcription\":{\"utterances\":[]}}\n"} {
		if _, err := parseMAIStream(strings.NewReader(body)); err == nil {
			t.Fatal("missing transcript accepted")
		}
	}
}

func TestMAINestedIdentifiersAndStreamEvents(t *testing.T) {
	conversation := []byte(`[{"result":{"data":{"json":{"conversation":{"id":"conversation-id","title":"fixture"}}}}}]`)
	added := []byte(`[{"result":{"data":{"json":{"response":{"userMessage":{"id":"user-id"},"modelMessage":{"id":"model-id"}}}}}}]`)
	if got := findMAIString(conversation, "id", "title"); got != "conversation-id" {
		t.Fatal("conversation", got)
	}
	if got := findMAIString(added, "id", "modelMessage"); got != "model-id" {
		t.Fatal("model message", got)
	}
	data, err := parseMAIStream(strings.NewReader("event: result\ndata: {\"result\":{\"data\":{\"json\":{\"rich_transcription\":{\"utterances\":[{\"start\":0,\"end\":1,\"text\":\"nested\"}]}}}}}\n"))
	if err != nil || !strings.Contains(string(data), "nested") {
		t.Fatal(string(data), err)
	}
}

func TestMAISpeakerTurnsAreOrderedWithoutChangingTimes(t *testing.T) {
	for _, key := range []string{"rich_transcription", "transcription_segments"} {
		payload := `[{"start":5,"end":8,"text":"second speaker"},{"start":1,"end":6,"text":"first speaker"},{"start":0,"end":0,"text":" "}]`
		if key == "rich_transcription" {
			payload = `{"utterances":` + payload + `}`
		}
		data, err := parseMAIStream(strings.NewReader(`data: {"` + key + `":` + payload + "}\n\n"))
		if err != nil {
			t.Fatal(key, err)
		}
		cues, err := parseCloudSpeech(data, "en", 60)
		if err != nil || len(cues) != 2 || cues[0].Start != 1 || cues[0].End != 6 || cues[1].Start != 5 || cues[1].End != 8 {
			t.Fatal(key, cues, err)
		}
	}
}

func TestQwenRequestUsesSelectedModel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audio.mp3")
	os.WriteFile(path, []byte("fixture"), 0600)
	engine := &Transcriber{CloudHTTP: &http.Client{Transport: speechTransport(func(r *http.Request) (*http.Response, error) {
		if r.ParseMultipartForm(1<<20) != nil {
			t.Fatal("multipart")
		}
		defer r.MultipartForm.RemoveAll()
		if r.FormValue("model") != "Qwen/Qwen3-ASR-1.7B" || r.FormValue("response_format") != "" {
			t.Fatal("wrong Qwen fields")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"text":"你好"}`))}, nil
	})}}
	if _, err := engine.cloudRequest(context.Background(), transcription.Config{Backend: "siliconflow", SiliconflowModel: "Qwen/Qwen3-ASR-1.7B"}, path); err != nil {
		t.Fatal(err)
	}
	chunk := speechChunk{path: path}
	one, _ := speechCacheKey(context.Background(), transcription.Config{Backend: "siliconflow"}, chunk, 30)
	two, _ := speechCacheKey(context.Background(), transcription.Config{Backend: "siliconflow", SiliconflowModel: "Qwen/Qwen3-ASR-1.7B"}, chunk, 30)
	if one == two {
		t.Fatal("models share cached text")
	}
}
