package httptransport

import (
	"encoding/json"
	"io"
	"mime"
	"net/http"
)

func (h *aiHandler) aiReady(w http.ResponseWriter) bool {
	if h.ai.AI == nil {
		write(w, 503, apiError{"ai_unavailable", "AI 功能暂不可用。"})
		return false
	}
	return true
}

// decodeAIRequest keeps JSON media-type, size and trailing-data checks in one
// transport boundary. Feature handlers only deal with already decoded inputs.
func decodeAIRequest(w http.ResponseWriter, r *http.Request, value any, limit int64) bool {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		write(w, 415, apiError{"invalid_ai_input", "请使用 JSON 提交 AI 配置或字幕。"})
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if len(r.URL.Query()) != 0 || decoder.Decode(value) != nil || decoder.Decode(new(any)) != io.EOF {
		write(w, 400, apiError{"invalid_ai_input", "AI 配置或字幕内容不完整，请重新读取后操作。"})
		return false
	}
	return true
}

func setAILongWriteDeadline(w http.ResponseWriter) error {
	return http.NewResponseController(w).SetWriteDeadline(aiLongWriteDeadline())
}
