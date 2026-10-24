package httptransport

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"tangentry/internal/ai"
	"tangentry/internal/subtitles"
	"testing"
	"time"
)

type aiTranslatorFunc func(context.Context, ai.Config, []ai.Segment) ([]ai.Segment, error)

func TestAIOutputFailuresAreActionableAndDoNotExposeProviderText(t *testing.T) {
	for _, fail := range []func(http.ResponseWriter, error){aiFail, summaryFail} {
		for _, tc := range []struct {
			err  error
			code string
		}{{ai.ErrTruncated, "ai_output_truncated"}, {ai.ErrRefused, "ai_output_refused"}, {ai.ErrFormat, "ai_invalid_output"}} {
			w := httptest.NewRecorder()
			fail(w, fmt.Errorf("synthetic-private-output: %w", tc.err))
			var output apiError
			if json.Unmarshal(w.Body.Bytes(), &output) != nil || w.Code != 502 || output.Code != tc.code || strings.Contains(w.Body.String(), "synthetic-private") || strings.Contains(w.Body.String(), "JSON 模式") || !strings.Contains(w.Body.String(), "仍保留") {
				t.Fatal("misleading or private error", w.Code, w.Body)
			}
		}
	}
	w := httptest.NewRecorder()
	aiFail(w, ai.ErrIncomplete)
	if w.Code != 502 || !strings.Contains(w.Body.String(), "ai_incomplete_output") || !strings.Contains(w.Body.String(), "漏了部分字幕") {
		t.Fatal(w.Code, w.Body)
	}
}

func TestSummaryErrorsExplainNextStep(t *testing.T) {
	for _, tc := range []struct {
		err     error
		status  int
		code    string
		message string
	}{
		{ai.ErrInvalid, 400, "invalid_ai_summary", "摘要来源和正文内容"},
		{ai.ErrNotReady, 503, "ai_not_ready", "工具设置填写支持指令与 JSON 输出的摘要模型"},
		{ai.ErrBusy, 409, "ai_busy", "已有 AI 操作正在进行"},
		{ai.ErrConflict, 409, "ai_config_changed", "AI 配置已更新"},
		{ai.ErrStorage, 503, "ai_storage_failed", "摘要缓存无法读写"},
	} {
		w := httptest.NewRecorder()
		summaryFail(w, tc.err)
		var output apiError
		if err := json.Unmarshal(w.Body.Bytes(), &output); err != nil || w.Code != tc.status || output.Code != tc.code || !strings.Contains(output.Message, tc.message) {
			t.Fatalf("summary error mapping changed: %#v status=%d body=%s err=%v", tc, w.Code, w.Body.String(), err)
		}
	}
}

func TestAIParameterRejectionIsDistinctFromNetworkFailure(t *testing.T) {
	for _, operation := range []struct {
		name string
		fail func(http.ResponseWriter, error)
	}{{"translation", aiFail}, {"summary", summaryFail}} {
		t.Run(operation.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			operation.fail(w, fmt.Errorf("private-upstream-body: %w", ai.ErrRequestRejected))
			var result apiError
			if json.Unmarshal(w.Body.Bytes(), &result) != nil || w.Code != 502 || !strings.Contains(w.Body.String(), "ai_request_rejected") || !strings.Contains(w.Body.String(), "400/422") || strings.Contains(w.Body.String(), "private-upstream") {
				t.Fatal(w.Code, w.Body)
			}
			w = httptest.NewRecorder()
			operation.fail(w, fmt.Errorf("private-provider-detail: %w", ai.ErrJSONModeUnsupported))
			if w.Code != 502 || !strings.Contains(w.Body.String(), "ai_json_mode_unsupported") || !strings.Contains(w.Body.String(), "response_format") || strings.Contains(w.Body.String(), "private-provider") {
				t.Fatal(w.Code, w.Body)
			}
			w = httptest.NewRecorder()
			operation.fail(w, fmt.Errorf("private-gateway-html: %w", ai.ErrGatewayTimeout))
			if w.Code != 504 || !strings.Contains(w.Body.String(), "ai_gateway_timeout") || strings.Contains(w.Body.String(), "private-gateway") {
				t.Fatal(w.Code, w.Body)
			}
			w = httptest.NewRecorder()
			operation.fail(w, fmt.Errorf("private-model-response: %w", ai.ErrTranslationLanguage))
			if w.Code != 502 || !strings.Contains(w.Body.String(), "ai_translation_language") || !strings.Contains(w.Body.String(), "400/422") || strings.Contains(w.Body.String(), "private-model") {
				t.Fatal(w.Code, w.Body)
			}
		})
	}
}

func (f aiTranslatorFunc) Translate(ctx context.Context, c ai.Config, s []ai.Segment) ([]ai.Segment, error) {
	return f(ctx, c, s)
}

type aiSummaryFunc func(context.Context, ai.Config, ai.SummaryRequest) (ai.SummaryDraft, error)

func (f aiSummaryFunc) Summarize(ctx context.Context, c ai.Config, r ai.SummaryRequest) (ai.SummaryDraft, error) {
	return f(ctx, c, r)
}

type aiCombinedProvider struct{ aiSummaryFunc }

func (aiCombinedProvider) Translate(context.Context, ai.Config, []ai.Segment) ([]ai.Segment, error) {
	return nil, ai.ErrProvider
}
func aiHTTPSource(revision int64) ai.Input {
	return ai.Input{ContentID: "youtube:jNQXAC9IVRw", Revision: revision, Track: subtitles.Track{Language: "en", Label: "English", Cues: []subtitles.Cue{{Start: 1.25, End: 4.75, Text: "Original"}}}}
}
func TestAIHTTPStrictPrivateConfigurationAndExplicitInference(t *testing.T) {
	var calls atomic.Int64
	service, err := ai.New(filepath.Join(t.TempDir(), "ai"), ai.NewMemoryResults(), aiTranslatorFunc(func(_ context.Context, _ ai.Config, s []ai.Segment) ([]ai.Segment, error) {
		calls.Add(1)
		return []ai.Segment{{Index: s[0].Index, Text: "譯文"}}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	h := New(nil, nil, nil, Options{AI: service})
	config := `{"baseUrl":"https://ai-fixture.example/v1","model":"fixture","key":"synthetic-http-private-key","revision":1}`
	for _, tc := range []struct {
		body, media string
		status      int
	}{{config + ` {}`, "application/json", 400}, {strings.Replace(config, `"revision":1`, `"revision":1,"extra":true`, 1), "application/json", 400}, {config, "text/plain", 415}} {
		w := storageRequest(h, "PUT", "/api/ai/settings", tc.body, tc.media)
		if w.Code != tc.status {
			t.Fatal(w.Code, w.Body)
		}
	}
	w := storageRequest(h, "PUT", "/api/ai/settings", config, "application/json")
	if w.Code != 200 || strings.Contains(w.Body.String(), "synthetic-http-private-key") {
		t.Fatal(w.Code, w.Body)
	}
	w = storageRequest(h, "GET", "/api/ai/settings", "", "")
	if w.Code != 200 || strings.Contains(w.Body.String(), "synthetic-http-private-key") || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("private settings", w.Body)
	}
	payload, _ := json.Marshal(aiHTTPSource(2))
	w = storageRequest(h, "POST", "/api/ai/subtitles/inspect", string(payload), "application/json")
	if w.Code != 200 || calls.Load() != 0 {
		t.Fatal("inspection made inference")
	}
	var inspected ai.Inspection
	json.Unmarshal(w.Body.Bytes(), &inspected)
	for _, path := range []string{"/api/ai/subtitles/inspect?q=unexpected", "/api/ai/subtitles/translate?q=unexpected"} {
		if storageRequest(h, "POST", path, string(payload), "application/json").Code != 400 {
			t.Fatal("unexpected query accepted")
		}
	}
	r := httptest.NewRequest("POST", "http://127.0.0.1/api/ai/subtitles/translate", bytes.NewReader(payload))
	r.Header.Set("Origin", "https://other.example")
	r.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 || calls.Load() != 0 {
		t.Fatal("unauthorized inference")
	}
	w = storageRequest(h, "POST", "/api/ai/subtitles/translate", string(payload), "application/json")
	if w.Code != 200 || calls.Load() != 1 || !strings.Contains(w.Body.String(), "译文") {
		t.Fatal(w.Code, w.Body)
	}
	w = storageRequest(h, "GET", "/api/ai/subtitles/"+inspected.Result.Key, "", "")
	if w.Code != 200 || calls.Load() != 1 {
		t.Fatal("cache read inferred")
	}
	w = storageRequest(h, "POST", "/api/ai/subtitles/translate", string(payload), "application/json")
	if w.Code != 200 || calls.Load() != 1 {
		t.Fatal("repeat translation charged")
	}
	tooBig := strings.Repeat(" ", ai.MaxInputBytes) + string(payload)
	if storageRequest(h, "POST", "/api/ai/subtitles/inspect", tooBig, "application/json").Code != http.StatusRequestEntityTooLarge {
		t.Fatal("unbounded request")
	}
}

func TestAITitleTranslationPreservesTitleFieldsAndUsesTitleCache(t *testing.T) {
	var calls atomic.Int64
	service, err := ai.New(filepath.Join(t.TempDir(), "ai"), ai.NewMemoryResults(), aiTranslatorFunc(func(_ context.Context, _ ai.Config, segments []ai.Segment) ([]ai.Segment, error) {
		calls.Add(1)
		return []ai.Segment{{Index: segments[0].Index, Text: "Translated title"}}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	key := "title-translation-key"
	status, err := service.Configure(ai.ConfigureInput{BaseURL: "https://ai-fixture.example/v1", Model: "fixture", Key: &key, Revision: 1})
	if err != nil {
		t.Fatal(err)
	}
	h := New(nil, nil, nil, Options{AI: service})
	payload, err := json.Marshal(ai.Input{
		ContentID:      "youtube:jNQXAC9IVRw",
		Source:         "title",
		TargetLanguage: "en",
		Revision:       status.Revision,
		Track:          subtitles.Track{Language: "text-title", Label: "title", Cues: []subtitles.Cue{{Start: 0, End: 1, Text: "Original title"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	response := storageRequest(h, "POST", "/api/ai/subtitles/inspect", string(payload), "application/json")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"targetLanguage":"en"`) {
		t.Fatalf("title inspection rejected or lost target: %d %s", response.Code, response.Body.String())
	}
	response = storageRequest(h, "POST", "/api/ai/subtitles/translate", string(payload), "application/json")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "Translated title") || calls.Load() != 1 {
		t.Fatalf("title translation failed: %d %s calls=%d", response.Code, response.Body.String(), calls.Load())
	}
}

func TestAIRequestErrorsExplainWhatNeedsFixing(t *testing.T) {
	service, err := ai.New(filepath.Join(t.TempDir(), "ai"), ai.NewMemoryResults(), aiTranslatorFunc(func(context.Context, ai.Config, []ai.Segment) ([]ai.Segment, error) {
		return nil, ai.ErrProvider
	}))
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	h := New(nil, nil, nil, Options{AI: service})
	valid := `{"contentId":"youtube:jNQXAC9IVRw","revision":1,"track":{"language":"en","cues":[{"start":0,"end":1,"text":"hello"}]}}`
	tests := []struct {
		name, body, media, code, message string
		status                           int
	}{
		{name: "media", body: valid, media: "text/plain", code: "ai_json_required", message: "格式", status: http.StatusUnsupportedMediaType},
		{name: "trailing", body: valid + ` {}`, media: "application/json", code: "ai_invalid_json", message: "额外内容", status: http.StatusBadRequest},
		{name: "unknown", body: strings.Replace(valid, `"track"`, `"extra":true,"track"`, 1), media: "application/json", code: "ai_unknown_field", message: "不支持的字段", status: http.StatusBadRequest},
		{name: "missing", body: `{"contentId":"youtube:jNQXAC9IVRw","revision":1,"track":{"language":"en","cues":[{"start":0,"end":1}]}}`, media: "application/json", code: "ai_incomplete_content", message: "缺少必需字段", status: http.StatusBadRequest},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := storageRequest(h, "POST", "/api/ai/subtitles/inspect", test.body, test.media)
			var output apiError
			if json.Unmarshal(response.Body.Bytes(), &output) != nil || response.Code != test.status || output.Code != test.code || !strings.Contains(output.Message, test.message) {
				t.Fatalf("unexpected error: %d %s", response.Code, response.Body.String())
			}
		})
	}
}
func TestAIHTTPOverwritesOnlyThisOperationsWriteDeadline(t *testing.T) {
	service, err := ai.New(filepath.Join(t.TempDir(), "ai"), ai.NewMemoryResults(), aiTranslatorFunc(func(ctx context.Context, _ ai.Config, s []ai.Segment) ([]ai.Segment, error) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(100 * time.Millisecond):
			return []ai.Segment{{Index: 0, Text: "译文"}}, nil
		}
	}))
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	_, err = service.Configure(ai.ConfigureInput{BaseURL: "https://ai-fixture.example/v1", Model: "fixture", Revision: 1})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(New(nil, nil, nil, Options{AI: service}))
	server.Config.WriteTimeout = 20 * time.Millisecond
	server.Start()
	defer server.Close()
	body, _ := json.Marshal(aiHTTPSource(2))
	response, err := http.Post(server.URL+"/api/ai/subtitles/translate", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal("long translation inherited short deadline", err)
	}
	defer response.Body.Close()
	data, _ := io.ReadAll(response.Body)
	if response.StatusCode != 200 || !strings.Contains(string(data), "译文") {
		t.Fatal(response.StatusCode, string(data))
	}
}

func TestAISummaryHTTPIsExplicitAndReturnsValidatedCitations(t *testing.T) {
	var calls atomic.Int64
	provider := aiCombinedProvider{aiSummaryFunc(func(_ context.Context, _ ai.Config, request ai.SummaryRequest) (ai.SummaryDraft, error) {
		calls.Add(1)
		if request.Mode != "leaf" || len(request.Segments) != 1 {
			return ai.SummaryDraft{}, ai.ErrFormat
		}
		return ai.SummaryDraft{Overview: "摘要概述", Points: []ai.SummaryPoint{{Text: "关键点", Indices: []int{request.Segments[0].Index}}}}, nil
	})}
	service, err := ai.New(filepath.Join(t.TempDir(), "ai"), ai.NewMemoryResults(), provider, provider)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	key := "summary-http-key"
	status, err := service.Configure(ai.ConfigureInput{BaseURL: "https://ai-fixture.example/v1", Model: "fixture", Key: &key, Revision: 1})
	if err != nil {
		t.Fatal(err)
	}
	h := New(nil, nil, nil, Options{AI: service})
	payload := `{"contentId":"youtube:jNQXAC9IVRw","source":"subtitle","revision":` + fmt.Sprint(status.Revision) + `,"segments":[{"index":0,"start":2,"end":4,"text":"原文"}]}`
	if response := storageRequest(h, "POST", "/api/ai/summaries/inspect", payload, "application/json"); response.Code != 200 || calls.Load() != 0 {
		t.Fatalf("inspect inferred: %d %s", response.Code, response.Body.String())
	}
	response := storageRequest(h, "POST", "/api/ai/summaries/generate", payload, "application/json")
	if response.Code != 200 || calls.Load() != 1 || !strings.Contains(response.Body.String(), `"start":2`) {
		t.Fatalf("generate: %d %s", response.Code, response.Body.String())
	}
	if response := storageRequest(h, "POST", "/api/ai/summaries/generate?q=x", payload, "application/json"); response.Code != 400 || calls.Load() != 1 {
		t.Fatal("summary accepted query")
	}
}

func TestAISummaryTitleOverridesHTTPRoundTrip(t *testing.T) {
	service, err := ai.New(filepath.Join(t.TempDir(), "ai"), ai.NewMemoryResults(), aiTranslatorFunc(func(context.Context, ai.Config, []ai.Segment) ([]ai.Segment, error) {
		return nil, ai.ErrProvider
	}))
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	secret := "title-override-test-key"
	status, err := service.Configure(ai.ConfigureInput{BaseURL: "https://ai-fixture.example/v1", Model: "fixture", Key: &secret, Revision: 1})
	if err != nil {
		t.Fatal(err)
	}
	h := New(nil, nil, nil, Options{AI: service})
	key := strings.Repeat("a", 64)
	path := "/api/ai/summaries/" + key + "/titles"
	response := storageRequest(h, "PUT", path, `{"nodeId":"section-1","title":"自定义章节"}`, "application/json")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "自定义章节") {
		t.Fatalf("put title override: %d %s", response.Code, response.Body.String())
	}
	response = storageRequest(h, "GET", path, "", "")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"nodeId":"section-1"`) {
		t.Fatalf("get title override: %d %s", response.Code, response.Body.String())
	}
	response = storageRequest(h, "DELETE", path+"/section-1", "", "")
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), `"nodeId":"section-1"`) {
		t.Fatalf("delete title override: %d %s", response.Code, response.Body.String())
	}
	_ = status
}

func TestAIConnectionCheckIsExplicitPrivateRevisionBoundAndUncached(t *testing.T) {
	var calls atomic.Int64
	root := filepath.Join(t.TempDir(), "ai")
	service, err := ai.New(root, ai.NewMemoryResults(), aiTranslatorFunc(func(_ context.Context, _ ai.Config, input []ai.Segment) ([]ai.Segment, error) {
		calls.Add(1)
		if len(input) != 1 || input[0].Text != "The reader keeps your notes with the original article." {
			t.Error("diagnostic used caller source text")
		}
		return []ai.Segment{{Index: 0, Text: "synthetic-private-generated-text"}}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	key := "synthetic-check-key"
	status, err := service.Configure(ai.ConfigureInput{BaseURL: "https://ai-fixture.example/v1", Model: "fixture", Key: &key, Revision: 1})
	if err != nil {
		t.Fatal(err)
	}
	h := New(nil, nil, nil, Options{AI: service})
	payload := fmt.Sprintf(`{"task":"translation","revision":%d}`, status.Revision)
	for _, tc := range []struct {
		method, path, body, media string
		status                    int
	}{
		{"GET", "/api/ai/check", "", "", 404},
		{"POST", "/api/ai/check", payload, "text/plain", 415},
		{"POST", "/api/ai/check?q=extra", payload, "application/json", 400},
		{"POST", "/api/ai/check", payload + " {}", "application/json", 400},
		{"POST", "/api/ai/check", `{"task":"translation","revision":1}`, "application/json", 409},
		{"POST", "/api/ai/check", `{"task":"other","revision":2}`, "application/json", 400},
		{"POST", "/api/ai/check", `{"task":"translation","revision":2,"text":"private-article"}`, "application/json", 400},
		{"POST", "/api/ai/check", `{"task":"summary","revision":2}`, "application/json", 503},
	} {
		response := storageRequest(h, tc.method, tc.path, tc.body, tc.media)
		if response.Code != tc.status {
			t.Fatal(tc.path, response.Code, response.Body)
		}
	}
	request := httptest.NewRequest("POST", "/api/ai/check", strings.NewReader(payload))
	request.Header.Set("Origin", "https://other.example")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	h.ServeHTTP(response, request)
	if response.Code != 403 || calls.Load() != 0 {
		t.Fatal("unauthorized or invalid check invoked provider")
	}
	for index := 0; index < 2; index++ {
		response = storageRequest(h, "POST", "/api/ai/check", payload, "application/json")
		var result ai.CheckResult
		if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &result) != nil || result.Model != "fixture" || result.Task != "translation" || result.Revision != status.Revision || response.Header().Get("Cache-Control") != "no-store" || strings.Contains(response.Body.String(), "synthetic-check-key") || strings.Contains(response.Body.String(), "synthetic-private-generated-text") {
			t.Fatal("unsafe or incorrect check result", response.Code, response.Body)
		}
	}
	if calls.Load() != 2 {
		t.Fatal("connection check was unexpectedly cached or retried")
	}
}
