package httptransport

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"

	"tangentry/internal/ai"
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
		write(w, http.StatusUnsupportedMediaType, apiError{"ai_json_required", "AI 请求格式不正确，请刷新页面后重试。"})
		return false
	}
	if len(r.URL.Query()) != 0 {
		write(w, http.StatusBadRequest, apiError{"ai_unexpected_query", "AI 请求包含不支持的查询参数，请刷新页面后重试。"})
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		writeAIRequestDecodeError(w, err)
		return false
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		writeAIRequestDecodeError(w, err)
		return false
	}
	return true
}

func writeAIRequestDecodeError(w http.ResponseWriter, err error) {
	var tooLarge *http.MaxBytesError
	switch {
	case err == nil:
		write(w, http.StatusBadRequest, apiError{"ai_invalid_json", "AI 请求后面还有额外内容，请刷新页面后重试。"})
	case errors.As(err, &tooLarge):
		write(w, http.StatusRequestEntityTooLarge, apiError{"ai_input_too_large", "字幕或摘要内容超过单次请求上限，请缩短内容后重试。"})
	case errors.Is(err, ai.ErrInvalid):
		write(w, http.StatusBadRequest, apiError{"ai_incomplete_content", "字幕或摘要缺少必需字段，请重新读取内容后重试。"})
	case strings.Contains(err.Error(), "unknown field"):
		write(w, http.StatusBadRequest, apiError{"ai_unknown_field", "AI 请求包含当前版本不支持的字段，请刷新应用后重试。"})
	default:
		var typeError *json.UnmarshalTypeError
		if errors.As(err, &typeError) {
			write(w, http.StatusBadRequest, apiError{"ai_invalid_field_type", "AI 请求中的字段类型不正确，请重新读取设置或字幕后重试。"})
			return
		}
		write(w, http.StatusBadRequest, apiError{"ai_invalid_json", "AI 请求不是有效的 JSON，请刷新页面后重试。"})
	}
}

func setAILongWriteDeadline(w http.ResponseWriter) error {
	return http.NewResponseController(w).SetWriteDeadline(aiLongWriteDeadline())
}
