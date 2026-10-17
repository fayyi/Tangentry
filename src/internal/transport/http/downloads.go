package httptransport

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"tangentry/internal/downloads"
)

func (h *mediaHandler) tools(w http.ResponseWriter, r *http.Request) {
	if h.media.ToolStatus == nil {
		fail(w, downloads.ErrTools)
		return
	}
	write(w, http.StatusOK, h.media.ToolStatus())
}

// toolUpdate starts replacing the managed yt-dlp with its latest release;
// the tool report shows progress.
func (h *mediaHandler) toolUpdate(w http.ResponseWriter, r *http.Request) {
	if h.media.UpdateYTDLP == nil || h.media.ToolStatus == nil {
		fail(w, downloads.ErrTools)
		return
	}
	if err := h.media.UpdateYTDLP(); err != nil {
		fail(w, err)
		return
	}
	write(w, http.StatusAccepted, h.media.ToolStatus())
}

func (h *mediaHandler) downloadList(w http.ResponseWriter, r *http.Request) {
	if h.media.Downloads == nil {
		fail(w, downloads.ErrTools)
		return
	}
	jobs, err := h.media.Downloads.List(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	write(w, http.StatusOK, map[string]any{"jobs": jobs})
}

func (h *mediaHandler) downloadStart(w http.ResponseWriter, r *http.Request) {
	if h.media.Downloads == nil {
		fail(w, downloads.ErrTools)
		return
	}
	if r.Header.Get("Content-Type") != "application/json" {
		write(w, http.StatusUnsupportedMediaType, apiError{"invalid_input", "请使用 JSON 格式提交下载。"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
	var request downloads.Request
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(&request) != nil || decoder.Decode(new(any)) != io.EOF {
		fail(w, downloads.ErrInvalid)
		return
	}
	job, err := h.media.Downloads.Start(r.Context(), request)
	if err != nil {
		fail(w, err)
		return
	}
	write(w, http.StatusAccepted, job)
}

func (h *mediaHandler) downloadInfo(w http.ResponseWriter, r *http.Request) {
	if h.media.Downloads == nil {
		fail(w, downloads.ErrTools)
		return
	}
	if r.Header.Get("Content-Type") != "application/json" {
		write(w, http.StatusUnsupportedMediaType, apiError{"invalid_input", "请使用 JSON 格式提交链接。"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
	var request struct {
		URL string `json:"url"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(&request) != nil || decoder.Decode(new(any)) != io.EOF {
		fail(w, downloads.ErrInvalid)
		return
	}
	info, err := h.media.Downloads.Inspect(r.Context(), request.URL)
	if err != nil {
		fail(w, err)
		return
	}
	write(w, http.StatusOK, info)
}

func (h *mediaHandler) downloadArtifact(w http.ResponseWriter, r *http.Request) {
	h.serveDownload(w, r, false)
}

func (h *mediaHandler) downloadCancel(w http.ResponseWriter, r *http.Request) {
	if h.media.Downloads == nil {
		fail(w, downloads.ErrTools)
		return
	}
	if err := h.media.Downloads.Cancel(r.Context(), r.PathValue("id")); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *mediaHandler) downloadDelete(w http.ResponseWriter, r *http.Request) {
	if h.media.Downloads == nil {
		fail(w, downloads.ErrTools)
		return
	}
	removeFiles, ok := decodeDeleteFiles(w, r)
	if !ok {
		return
	}
	if err := h.media.Downloads.Delete(r.Context(), r.PathValue("id"), removeFiles); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *mediaHandler) downloadClear(w http.ResponseWriter, r *http.Request) {
	if h.media.Downloads == nil {
		fail(w, downloads.ErrTools)
		return
	}
	removeFiles, ok := decodeDeleteFiles(w, r)
	if !ok {
		return
	}
	if err := h.media.Downloads.Clear(r.Context(), removeFiles); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func decodeDeleteFiles(w http.ResponseWriter, r *http.Request) (bool, bool) {
	if r.Header.Get("Content-Type") != "application/json" {
		write(w, http.StatusUnsupportedMediaType, apiError{"invalid_input", "请使用 JSON 确认删除范围。"})
		return false, false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	var request struct {
		DeleteFiles bool `json:"deleteFiles"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(&request) != nil || decoder.Decode(new(any)) != io.EOF {
		fail(w, downloads.ErrInvalid)
		return false, false
	}
	return request.DeleteFiles, true
}

func (h *mediaHandler) downloadFile(w http.ResponseWriter, r *http.Request) {
	h.serveDownload(w, r, false)
}

func (h *mediaHandler) downloadMedia(w http.ResponseWriter, r *http.Request) {
	h.serveDownload(w, r, true)
}

// Both routes reuse the completed-job file boundary. ServeContent handles Range
// and HEAD, so seeking does not require reading a whole video into application RAM.
func (h *mediaHandler) serveDownload(w http.ResponseWriter, r *http.Request, inline bool) {
	if h.media.Downloads == nil {
		fail(w, downloads.ErrTools)
		return
	}
	file, err := h.media.Downloads.Artifact(r.Context(), r.PathValue("id"), r.PathValue("name"))
	if err != nil {
		fail(w, err)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		fail(w, err)
		return
	}
	disposition := "attachment"
	if inline {
		mediaTypes := map[string]string{".mp4": "video/mp4", ".webm": "video/webm", ".mkv": "video/x-matroska", ".mov": "video/quicktime", ".mp3": "audio/mpeg", ".m4a": "audio/mp4", ".flac": "audio/flac", ".wav": "audio/wav", ".opus": "audio/ogg", ".ogg": "audio/ogg"}
		mediaType, supported := mediaTypes[strings.ToLower(filepath.Ext(info.Name()))]
		if !supported {
			fail(w, downloads.ErrNotFound)
			return
		}
		w.Header().Set("Content-Type", mediaType)
		disposition = "inline"
	}
	w.Header().Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": info.Name()}))
	// Media/files can take longer than the server's API response deadline. Keep
	// this exception scoped to an already opened completed file, not API work.
	if err := http.NewResponseController(w).SetWriteDeadline(time.Time{}); err != nil && !errors.Is(err, http.ErrNotSupported) {
		fail(w, err)
		return
	}
	http.ServeContent(w, r, info.Name(), info.ModTime(), file)
}
