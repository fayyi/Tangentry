package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"tangentry/internal/agent/gateway"
)

func TestInitializeAndToolsRespectGrantedScopes(t *testing.T) {
	server := New(gateway.Connection{Scopes: []string{"content_read", "agent_write"}})
	var output bytes.Buffer
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`,
	}, "\n")
	if err := server.Serve(context.Background(), strings.NewReader(input), &output); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("responses = %d: %s", len(lines), output.String())
	}
	var listed struct {
		Result struct {
			Tools []tool `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[1]), &listed); err != nil {
		t.Fatal(err)
	}
	for _, item := range listed.Result.Tools {
		if item.Name == "search_notes" || item.Name == "capture_frames" || item.Name == "summarize_content" {
			t.Fatalf("tool escaped scope filter: %s", item.Name)
		}
	}
	for _, item := range listed.Result.Tools {
		if item.Name != "list_inbox" {
			continue
		}
		if _, exists := item.InputSchema["required"]; exists {
			t.Fatalf("optional-only tool emitted null required: %+v", item.InputSchema)
		}
	}
	if len(listed.Result.Tools) < 4 {
		t.Fatalf("expected read and ledger tools: %v", listed.Result.Tools)
	}
}

func TestMCPNotificationsAndInvalidRequests(t *testing.T) {
	server := New(gateway.Connection{Scopes: []string{"content_read"}})
	var output bytes.Buffer
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"no_such_tool","arguments":{}}}`,
		`{"jsonrpc":"1.0","id":2,"method":"ping","params":{}}`,
		`{"jsonrpc":"2.0","id":{"bad":true},"method":"ping","params":{}}`,
	}, "\n") + "\n"
	if err := server.Serve(context.Background(), strings.NewReader(input), &output); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("notification produced a response or invalid request was dropped: %q", output.String())
	}
	var unknown struct {
		Error *rpcError `json:"error"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &unknown); err != nil || unknown.Error == nil || unknown.Error.Code != -32602 {
		t.Fatalf("unknown tool error = %s", lines[0])
	}
	var invalid struct {
		ID    json.RawMessage `json:"id"`
		Error *rpcError       `json:"error"`
	}
	if err := json.Unmarshal([]byte(lines[1]), &invalid); err != nil || string(invalid.ID) != "2" || invalid.Error == nil || invalid.Error.Code != -32600 {
		t.Fatalf("invalid request response = %s", lines[1])
	}
	var invalidID struct {
		ID    json.RawMessage `json:"id"`
		Error *rpcError       `json:"error"`
	}
	if err := json.Unmarshal([]byte(lines[2]), &invalidID); err != nil || string(invalidID.ID) != "null" || invalidID.Error == nil || invalidID.Error.Code != -32600 {
		t.Fatalf("invalid id response = %s", lines[2])
	}
}

func TestMCPGatewayErrorsAreStableAndDoNotEchoBodies(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"code":"invalid_input","message":"/private/path/database.sqlite"}`))
	}))
	defer server.Close()
	bridge := New(gateway.Connection{Endpoint: server.URL, Token: "token", Scopes: []string{"content_read"}})
	var output bytes.Buffer
	input := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_capabilities","arguments":{}}}` + "\n"
	if err := bridge.Serve(context.Background(), strings.NewReader(input), &output); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "private/path") || !strings.Contains(output.String(), "Agent 请求参数无效") {
		t.Fatalf("unstable gateway error leaked: %s", output.String())
	}
}

func TestCaptureFramesReturnsMCPImageContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/agent/v1/frame/0123456789abcdef0123456789abcdef" {
			if r.Header.Get("Authorization") != "Bearer token" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write([]byte("jpeg-frame"))
			return
		}
		if r.URL.Path != "/agent/v1/frames" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"resourceUrl":"` + serverURLForFrames(r) + `/agent/v1/frame/0123456789abcdef0123456789abcdef","mimeType":"image/jpeg","positionSeconds":3.5,"source":"completed_local_download"}]`))
	}))
	defer server.Close()

	bridge := New(gateway.Connection{Endpoint: server.URL, Token: "token", Scopes: []string{"image_read"}})
	// The test server uses a dynamic loopback port; the bridge only follows
	// frame URLs on this exact configured endpoint.
	var output bytes.Buffer
	input := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"capture_frames","arguments":{"id":"youtube:abcdefghijk","positions":[3.5]}}}` + "\n"
	if err := bridge.Serve(context.Background(), strings.NewReader(input), &output); err != nil {
		t.Fatal(err)
	}
	var response struct {
		Result struct {
			Content []struct {
				Type string `json:"type"`
				Data string `json:"data"`
			} `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal(output.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Result.Content) != 2 || response.Result.Content[1].Type != "image" || response.Result.Content[1].Data != "anBlZy1mcmFtZQ==" {
		t.Fatalf("image content = %+v", response.Result.Content)
	}
}

func serverURLForFrames(r *http.Request) string {
	return "http://" + r.Host
}

func TestFetchFrameRejectsOtherHosts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("unexpected request to frame resource")
	}))
	defer server.Close()

	bridge := New(gateway.Connection{Endpoint: "http://127.0.0.1:1234", Token: "token"})
	if _, err := bridge.fetchFrame(context.Background(), server.URL+"/agent/v1/frame/0123456789abcdef0123456789abcdef", "image/jpeg"); err == nil {
		t.Fatal("frame URL outside current gateway was accepted")
	}
}

func TestMCPRunLifecycleCorrelatesGatewayRequests(t *testing.T) {
	var started, correlated, finished bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/agent/v1/run":
			started = true
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"run-test-1"}`))
		case "/agent/v1/capabilities":
			correlated = r.Header.Get("X-Tangentry-Agent-Run") == "run-test-1"
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"protocolVersion":"1","platforms":[]}`))
		case "/agent/v1/run/finish":
			var input struct {
				ID    string `json:"id"`
				State string `json:"state"`
			}
			if json.NewDecoder(r.Body).Decode(&input) == nil && input.ID == "run-test-1" && input.State == "completed" {
				finished = true
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"finished":true}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	bridge := New(gateway.Connection{Endpoint: server.URL, Token: "token", Scopes: []string{"content_read", "agent_write"}})
	var output bytes.Buffer
	input := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_capabilities","arguments":{}}}` + "\n"
	if err := bridge.Serve(context.Background(), strings.NewReader(input), &output); err != nil {
		t.Fatal(err)
	}
	if !started || !correlated || !finished {
		t.Fatalf("run lifecycle started=%v correlated=%v finished=%v; output=%s", started, correlated, finished, output.String())
	}
}
