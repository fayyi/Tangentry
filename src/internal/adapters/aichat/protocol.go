package aichat

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"tangentry/internal/ai"
)

// Client owns only the provider transport. Prompt construction and response
// decoding live in the task-specific files next to it.
type Client struct{ HTTP *http.Client }

// rejectedRequest classifies only known validation messages. Provider bodies
// may contain credentials or private source text and are never surfaced.
func rejectedRequest(body io.Reader) error {
	var envelope struct {
		Error struct {
			Message string `json:"message"`
			Param   string `json:"param"`
			Code    string `json:"code"`
		} `json:"error"`
	}
	if json.NewDecoder(io.LimitReader(body, 4096)).Decode(&envelope) == nil {
		message := strings.ToLower(envelope.Error.Message)
		if strings.Contains(message, "unable to determine target translation language") {
			return ai.ErrTranslationLanguage
		}
		if envelope.Error.Param == "response_format" && (envelope.Error.Code == "unsupported_parameter" || envelope.Error.Code == "unsupported_value") {
			return ai.ErrJSONModeUnsupported
		}
	}
	return ai.ErrRequestRejected
}

func (c *Client) Chat(ctx context.Context, config ai.Config, source string, messages []ai.ChatMessage) (string, error) {
	if !ai.ValidEndpoint(config.BaseURL) || strings.TrimSpace(config.Model) == "" {
		return "", ai.ErrInvalid
	}
	chat := make([]map[string]string, 0, len(messages)+1)
	chat = append(chat, map[string]string{"role": "system", "content": "你是 Tangentry 阅读助手。根据给定内容回答问题，无法确定时明确说明。" + source})
	for _, message := range messages {
		chat = append(chat, map[string]string{"role": message.Role, "content": message.Content})
	}
	payload := map[string]any{"model": config.Model, "stream": false, "messages": chat}
	return c.complete(ctx, config, payload)
}

// ChatStream consumes an OpenAI-compatible text/event-stream. A provider that
// closes the stream without any content is treated as a format error so the
// HTTP layer can fall back to the regular completion endpoint.
func (c *Client) ChatStream(ctx context.Context, config ai.Config, source string, messages []ai.ChatMessage, emit func(string) error) error {
	if !ai.ValidEndpoint(config.BaseURL) || strings.TrimSpace(config.Model) == "" {
		return ai.ErrInvalid
	}
	chat := make([]map[string]string, 0, len(messages)+1)
	chat = append(chat, map[string]string{"role": "system", "content": "你是 Tangentry 阅读助手。根据给定内容回答问题，无法确定时明确说明。" + source})
	for _, message := range messages {
		chat = append(chat, map[string]string{"role": message.Role, "content": message.Content})
	}
	payload := map[string]any{"model": config.Model, "stream": true, "messages": chat}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(config.BaseURL, "/")+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return ai.ErrProvider
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream, application/json")
	if config.Key != "" {
		req.Header.Set("Authorization", "Bearer "+config.Key)
	}
	client := c.HTTP
	if client == nil {
		client = New().HTTP
	}
	safe := *client
	safe.Jar = nil
	safe.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := safe.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return ai.ErrProvider
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		if response.StatusCode == http.StatusBadRequest || response.StatusCode == http.StatusUnprocessableEntity {
			return rejectedRequest(response.Body)
		}
		if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
			return ai.ErrAuth
		}
		if response.StatusCode == http.StatusTooManyRequests {
			return ai.ErrRate
		}
		return ai.ErrProvider
	}
	return parseStream(response.Body, emit)
}

// parseStream is deliberately small and line-oriented: OpenAI-compatible
// servers send one JSON object per data line and terminate with [DONE].
func parseStream(body io.Reader, emit func(string) error) error {
	reader := bufio.NewScanner(io.LimitReader(body, (256<<10)+1))
	reader.Buffer(make([]byte, 4096), 64<<10)
	emitted := false
	for reader.Scan() {
		line := strings.TrimSpace(reader.Text())
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if strings.HasPrefix(line, "data:") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		}
		if line == "[DONE]" {
			if !emitted {
				return ai.ErrFormat
			}
			return nil
		}
		var event struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
				FinishReason string `json:"finish_reason"`
			} `json:"choices"`
		}
		if json.Unmarshal([]byte(line), &event) != nil || len(event.Choices) != 1 {
			return ai.ErrFormat
		}
		if value := event.Choices[0].Delta.Content; value != "" {
			emitted = true
			if err := emit(value); err != nil {
				return err
			}
		}
	}
	if err := reader.Err(); err != nil {
		return err
	}
	if !emitted {
		return ai.ErrFormat
	}
	return nil
}

func New() *Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = 30 * time.Second
	return &Client{HTTP: &http.Client{Transport: transport, Timeout: 90 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

func (c *Client) complete(ctx context.Context, config ai.Config, payload map[string]any) (string, error) {
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(config.BaseURL, "/")+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", ai.ErrProvider
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if config.Key != "" {
		req.Header.Set("Authorization", "Bearer "+config.Key)
	}
	// Copy the configured client so a supplied cookie jar or redirect policy
	// cannot send unrelated sessions or forward keys to another destination.
	client := c.HTTP
	if client == nil {
		client = New().HTTP
	}
	safe := *client
	safe.Jar = nil
	safe.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := safe.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return "", context.DeadlineExceeded
		}
		return "", ai.ErrProvider
	}
	defer response.Body.Close()
	switch response.StatusCode {
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		return "", rejectedRequest(response.Body)
	case http.StatusUnauthorized, http.StatusForbidden:
		return "", ai.ErrAuth
	case http.StatusTooManyRequests:
		return "", ai.ErrRate
	case http.StatusGatewayTimeout:
		return "", ai.ErrGatewayTimeout
	}
	if response.StatusCode != http.StatusOK {
		return "", ai.ErrProvider
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, (256<<10)+1))
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return "", context.DeadlineExceeded
		}
		return "", ai.ErrProvider
	}
	if len(data) > 256<<10 {
		return "", ai.ErrFormat
	}
	var envelope struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
				Refusal string `json:"refusal"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}
	if json.Unmarshal(data, &envelope) != nil || len(envelope.Choices) != 1 {
		return "", ai.ErrFormat
	}
	choice := envelope.Choices[0]
	if choice.Message.Refusal != "" || choice.FinishReason == "content_filter" {
		return "", ai.ErrRefused
	}
	if choice.FinishReason == "length" {
		return "", ai.ErrTruncated
	}
	if choice.FinishReason != "stop" {
		return "", ai.ErrFormat
	}
	return choice.Message.Content, nil
}

func structuredJSON(content string) string {
	text := strings.TrimSpace(content)
	for _, prefix := range []string{"```json\n", "```\n", "```json\r\n", "```\r\n"} {
		if strings.HasPrefix(text, prefix) && strings.HasSuffix(text, "\n```") {
			return strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(text, prefix), "\n```"))
		}
	}
	return text
}
