package tools

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"time"

	"tangentry/internal/transcription"
)

// Playground is an experimental web protocol, not the paid Azure MAI API.
// This file owns uploads, tRPC envelopes, request headers and cleanup.
func (t *Transcriber) maiRequest(ctx context.Context, cfg transcription.Config, path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, storageFailure(err)
	}
	if info.Size() > cloudUploadLimit {
		return nil, transcription.Failed("upload_too_large", "转录片段超过 25 MB。")
	}
	audio, err := os.ReadFile(path)
	if err != nil {
		return nil, storageFailure(err)
	}
	client := &http.Client{Timeout: 12 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	if t.CloudHTTP != nil {
		client.Transport = t.CloudHTTP.Transport
	}
	post := func(work context.Context, endpoint string, payload any) (*http.Response, error) {
		body, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		r, err := http.NewRequestWithContext(work, "POST", "https://playground.microsoft.ai"+endpoint, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Cookie", cfg.MAICookie)
		r.Header.Set("Origin", "https://playground.microsoft.ai")
		r.Header.Set("Referer", "https://playground.microsoft.ai/chat")
		var id [16]byte
		if _, err := rand.Read(id[:]); err != nil {
			return nil, err
		}
		r.Header.Set("x-request-id", hex.EncodeToString(id[:]))
		response, err := client.Do(r)
		if err != nil {
			return nil, transcription.Failed("cloud_network", "Microsoft Playground 连接失败，请手动重试。")
		}
		if response.StatusCode != http.StatusOK {
			response.Body.Close()
			stage := "建立会话"
			switch endpoint {
			case "/api/trpc/conversations.addAudioPromptAndInitResponse?batch=1":
				stage = "上传片段"
			case "/api/chat/stream":
				stage = "读取转录结果"
			case "/api/trpc/conversations.delete?batch=1":
				stage = "清理会话"
			}
			switch response.StatusCode {
			case http.StatusUnauthorized, http.StatusForbidden:
				return nil, transcription.Failed("cloud_credentials", "Microsoft Playground 登录已失效或拒绝访问，请在原站检查后更新登录会话。")
			case http.StatusUnavailableForLegalReasons:
				return nil, transcription.Failed("mai_consent", "Microsoft Playground 返回 HTTP 451，可能尚未接受音频生物特征通知或受地区政策限制。请用同一账号在原站手动上传一小段音频，阅读并处理通知；原站可转录后更新 Cookie 再重试。原站仍拒绝时请改用本机转录。")
			case http.StatusTooManyRequests:
				return nil, transcription.Failed("cloud_quota", "Microsoft Playground 在"+stage+"时限制请求。"+speechRetryHint(response.Header.Get("Retry-After"), time.Now())+"已完成片段保留，请稍后手动重试；也可切换本机转录。")
			default:
				return nil, transcription.Failed("cloud_response", "Microsoft Playground 未完成请求；实验接口可能已变更。")
			}
		}
		return response, nil
	}
	batch := func(work context.Context, proc string, payload any) (json.RawMessage, error) {
		r, err := post(work, "/api/trpc/"+proc+"?batch=1", map[string]any{"0": payload})
		if err != nil {
			return nil, err
		}
		defer r.Body.Close()
		var result []struct {
			Result struct {
				Data json.RawMessage `json:"data"`
			} `json:"result"`
		}
		if json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&result) != nil || len(result) != 1 || len(result[0].Result.Data) == 0 {
			return nil, transcription.Failed("cloud_response", "Microsoft Playground 返回了无法识别的会话结果。")
		}
		var wrapped struct {
			JSON json.RawMessage `json:"json"`
		}
		if json.Unmarshal(result[0].Result.Data, &wrapped) == nil && len(wrapped.JSON) > 0 {
			return wrapped.JSON, nil
		}
		return result[0].Result.Data, nil
	}
	created, err := batch(ctx, "conversations.create", map[string]any{"title": "Tangentry transcription", "modelId": "mai-transcribe-2"})
	if err != nil {
		return nil, err
	}
	conversationID := findMAIString(created, "id", "title")
	if conversationID == "" {
		return nil, transcription.Failed("cloud_response", "Microsoft Playground 未返回会话标识。")
	}
	// Cleanup is attempted even after cancellation, with a separate bounded context.
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		r, err := post(cleanup, "/api/trpc/conversations.delete?batch=1", map[string]any{"0": map[string]string{"id": conversationID}})
		if err == nil {
			r.Body.Close()
		}
	}()
	added, err := batch(ctx, "conversations.addAudioPromptAndInitResponse", map[string]any{
		"conversationId": conversationID, "modelId": "mai-transcribe-2", "audioBase64": base64.StdEncoding.EncodeToString(audio),
		"audioMimeType": "audio/mpeg", "audioFileName": "audio.mp3", "lastMessageId": nil,
	})
	if err != nil {
		return nil, err
	}
	modelMessageID := findMAIString(added, "id", "modelMessage")
	if modelMessageID == "" {
		return nil, transcription.Failed("cloud_response", "Microsoft Playground 未返回转录任务标识。")
	}
	r, err := post(ctx, "/api/chat/stream", map[string]any{"conversationId": conversationID, "modelId": "mai-transcribe-2", "pendingModelMessageId": modelMessageID, "isFirstMessage": true, "transcribeOptions": map[string]bool{"diarize": true}})
	if err != nil {
		return nil, err
	}
	defer r.Body.Close()
	return parseMAIStream(io.LimitReader(r.Body, 16<<20))
}
