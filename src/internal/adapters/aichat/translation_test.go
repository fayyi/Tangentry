package aichat

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"tangentry/internal/ai"
)

func TestTranslationPromptsUseConfiguredTargetLanguage(t *testing.T) {
	marker := regexp.MustCompile(`\[\[TANGENTRY_([0-9]+)\]\]`)
	for _, tc := range []struct {
		name        string
		model       string
		target      string
		targetName  string
		translation string
		segments    []ai.Segment
	}{
		{name: "structured single", model: "fixture", target: "ja", targetName: "日语", translation: "日本語の訳文", segments: []ai.Segment{{Index: 7, Text: "source"}}},
		{name: "structured batch", model: "fixture", target: "ja", targetName: "日语", translation: "日本語の訳文", segments: []ai.Segment{{Index: 7, Text: "first"}, {Index: 19, Text: "second"}}},
		{name: "dedicated single", model: "bilibili/Index-Translate-35B-A3B", target: "ja", targetName: "日语", translation: "日本語の訳文", segments: []ai.Segment{{Index: 7, Text: "source"}}},
		{name: "dedicated batch", model: "bilibili/Index-Translate-35B-A3B", target: "ja", targetName: "日语", translation: "日本語の訳文", segments: []ai.Segment{{Index: 7, Text: "first"}, {Index: 19, Text: "second"}}},
		{name: "default target", model: "fixture", targetName: "简体中文", translation: "简体中文译文", segments: []ai.Segment{{Index: 7, Text: "source"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			transport := chatRoundTripper(func(r *http.Request) (*http.Response, error) {
				var request struct {
					Messages []struct {
						Role    string `json:"role"`
						Content string `json:"content"`
					} `json:"messages"`
				}
				if json.NewDecoder(r.Body).Decode(&request) != nil || len(request.Messages) == 0 {
					t.Error("invalid translation request")
					return &http.Response{StatusCode: http.StatusBadRequest, Body: io.NopCloser(strings.NewReader("invalid request")), Header: make(http.Header), Request: r}, nil
				}
				var prompt strings.Builder
				for _, message := range request.Messages {
					prompt.WriteString(message.Content)
					prompt.WriteByte('\n')
				}
				if !strings.Contains(prompt.String(), "SYSTEM -> "+tc.targetName) || !strings.Contains(prompt.String(), "USER -> "+tc.targetName) {
					t.Errorf("request did not consistently use requested target language: %s", prompt.String())
				}

				if textTranslationModel(tc.model) {
					if len(tc.segments) == 1 {
						return translationResponse(r, completion(tc.translation, "stop")), nil
					}
					matches := marker.FindAllStringSubmatch(prompt.String(), -1)
					var output strings.Builder
					for _, match := range matches {
						fmt.Fprintf(&output, "[[TANGENTRY_%s]]\n%s%s\n", match[1], tc.translation, match[1])
					}
					return translationResponse(r, completion(output.String(), "stop")), nil
				}
				translations := make([]map[string]any, len(tc.segments))
				for i, segment := range tc.segments {
					translations[i] = map[string]any{"index": segment.Index, "text": fmt.Sprintf("%s%d", tc.translation, segment.Index)}
				}
				body, _ := json.Marshal(map[string]any{"translations": translations})
				return translationResponse(r, completion(string(body), "stop")), nil
			})

			config := ai.Config{
				BaseURL:           "https://ai-fixture.example/v1",
				Model:             tc.model,
				TranslationTarget: tc.target,
				TranslationTemplate: ai.PromptTemplate{
					System: "SYSTEM -> {{target_language}}",
					User:   "USER -> {{target_language}} {{content}}",
				},
			}
			client := &Client{HTTP: &http.Client{Transport: transport}}
			result, err := client.Translate(context.Background(), config, tc.segments)
			if err != nil || len(result) != len(tc.segments) {
				t.Fatalf("translation with requested target failed: result=%v err=%v", result, err)
			}
			for i, segment := range tc.segments {
				if result[i].Index != segment.Index || !strings.Contains(result[i].Text, tc.translation) {
					t.Fatalf("translation target or index changed: got=%v want index %d", result[i], segment.Index)
				}
			}
		})
	}
}

func translationResponse(request *http.Request, body string) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: request}
}
