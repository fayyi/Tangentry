package tools

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"tangentry/internal/subtitles"
	"tangentry/internal/transcription"
)

func (t *Transcriber) cloudRequest(ctx context.Context, cfg transcription.Config, path string) ([]byte, error) {
	if cfg.Backend == "mai" {
		return t.maiRequest(ctx, cfg, path)
	}
	requestCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	endpoint, key := "https://api.groq.com/openai/v1/audio/transcriptions", cfg.GroqAPIKey
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
		return nil, transcription.ErrInvalid
	}
	if info.Size() > cloudUploadLimit {
		return nil, transcription.Failed("upload_too_large", "转录片段超过 25 MB，请使用本机转录。")
	}
	var body io.Reader
	var contentLength int64
	contentType := "application/json"
	if cfg.Backend == "groq" || cfg.Backend == "siliconflow" {
		body, contentType, contentLength, endpoint, key, err = t.multipartSpeechBody(path, cfg)
		if err != nil {
			return nil, err
		}
	} else {
		body, contentLength, endpoint, key, err = cloudflareSpeechBody(path, cfg)
		if err != nil {
			return nil, err
		}
	}
	req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, endpoint, body)
	if err != nil {
		if closer, ok := body.(io.Closer); ok {
			_ = closer.Close()
		}
		return nil, transcription.ErrInvalid
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", contentType)
	req.ContentLength = contentLength
	client := http.Client{Timeout: 10 * time.Minute}
	if t.CloudHTTP != nil {
		client = *t.CloudHTTP
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, transcription.Failed("cloud_network", "云端转录连接失败或超时，可稍后手动重试；已保存字幕不受影响。")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		switch response.StatusCode {
		case 401, 403:
			return nil, transcription.Failed("cloud_credentials", "云端转录凭证无效或权限不足，请检查工具设置。")
		case 429:
			return nil, transcription.Failed("cloud_quota", "云端转录额度或请求频率受限。"+speechRetryHint(response.Header.Get("Retry-After"), time.Now())+"请稍后手动重试或切换本机转录。")
		case 413:
			return nil, transcription.Failed("upload_too_large", "云端拒绝了音频大小，请切换本机转录。")
		default:
			return nil, transcription.Failed("cloud_response", "云端转录服务未完成请求，请稍后手动重试。")
		}
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, (4<<20)+1))
	if err != nil || len(data) > 4<<20 {
		return nil, subtitles.ErrFormat
	}
	if cfg.Backend == "cloudflare" {
		var envelope struct {
			Success bool            `json:"success"`
			Result  json.RawMessage `json:"result"`
		}
		if json.Unmarshal(data, &envelope) != nil || !envelope.Success || len(envelope.Result) == 0 {
			return nil, transcription.Failed("cloud_response", "Cloudflare 未返回可用转录结果，请检查权限与额度后手动重试。")
		}
		data = envelope.Result
	}
	return data, nil
}

func (t *Transcriber) multipartSpeechBody(path string, cfg transcription.Config) (io.Reader, string, int64, string, string, error) {
	file, err := os.CreateTemp(filepath.Dir(path), "upload-*")
	if err != nil {
		return nil, "", 0, "", "", storageFailure(err)
	}
	keepFile := false
	defer func() {
		if !keepFile {
			_ = file.Close()
			_ = os.Remove(file.Name())
		}
	}()
	writer := multipart.NewWriter(file)
	model := cfg.GroqModel
	if model == "" {
		model = "whisper-large-v3-turbo"
	}
	fields := map[string]string{"model": model, "response_format": "verbose_json", "timestamp_granularities[]": "segment", "temperature": "0"}
	endpoint, key := "https://api.groq.com/openai/v1/audio/transcriptions", cfg.GroqAPIKey
	if cfg.Language != "auto" {
		fields["language"] = cfg.Language
	}
	if cfg.Backend == "siliconflow" {
		endpoint, key = "https://api.siliconflow.cn/v1/audio/transcriptions", cfg.SiliconflowAPIKey
		fields = map[string]string{"model": "FunAudioLLM/SenseVoiceSmall"}
		if cfg.SiliconflowModel != "" {
			fields["model"] = cfg.SiliconflowModel
		}
	}
	for field, value := range fields {
		if err := writer.WriteField(field, value); err != nil {
			return nil, "", 0, "", "", storageFailure(err)
		}
	}
	part, err := writer.CreateFormFile("file", "audio.mp3")
	if err != nil {
		return nil, "", 0, "", "", storageFailure(err)
	}
	audio, err := os.Open(path)
	if err != nil {
		return nil, "", 0, "", "", storageFailure(err)
	}
	_, copyErr := io.Copy(part, io.LimitReader(audio, cloudUploadLimit+1))
	audio.Close()
	if copyErr != nil {
		return nil, "", 0, "", "", storageFailure(copyErr)
	}
	if err := writer.Close(); err != nil {
		return nil, "", 0, "", "", storageFailure(err)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, "", 0, "", "", storageFailure(err)
	}
	stat, err := file.Stat()
	if err != nil {
		return nil, "", 0, "", "", storageFailure(err)
	}
	// Keep the staged request file alive through the request by returning a
	// reader that owns it; the caller's HTTP client closes the body only after
	// the request, so use a read-closer wrapper for cleanup.
	keepFile = true
	return &temporaryReader{File: file, Path: file.Name()}, writer.FormDataContentType(), stat.Size(), endpoint, key, nil
}

type temporaryReader struct {
	*os.File
	Path string
}

func (r *temporaryReader) Close() error {
	err := r.File.Close()
	removeErr := os.Remove(r.Path)
	if err != nil {
		return err
	}
	return removeErr
}

func cloudflareSpeechBody(path string, cfg transcription.Config) (io.Reader, int64, string, string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, 0, "", "", storageFailure(err)
	}
	data, readErr := io.ReadAll(io.LimitReader(file, cloudUploadLimit+1))
	file.Close()
	if readErr != nil {
		return nil, 0, "", "", storageFailure(readErr)
	}
	if len(data) > cloudUploadLimit {
		return nil, 0, "", "", transcription.Failed("upload_too_large", "转录片段超过 25 MB，请使用本机转录。")
	}
	payload := map[string]any{"audio": base64.StdEncoding.EncodeToString(data), "task": "transcribe", "vad_filter": false}
	if cfg.Language != "auto" {
		payload["language"] = cfg.Language
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, 0, "", "", err
	}
	return bytes.NewReader(raw), int64(len(raw)), "https://api.cloudflare.com/client/v4/accounts/" + cfg.CloudflareAccountID + "/ai/run/@cf/openai/whisper-large-v3-turbo", cfg.CloudflareAPIToken, nil
}
