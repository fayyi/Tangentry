package httptransport

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"tangentry/internal/transcription"
)

func decodeTranscription(w http.ResponseWriter, r *http.Request, value any) bool {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		write(w, 415, apiError{"invalid_transcription", "请使用 JSON 提交。"})
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if d.Decode(value) != nil || d.Decode(new(any)) != io.EOF {
		fail(w, transcription.ErrInvalid)
		return false
	}
	return true
}
