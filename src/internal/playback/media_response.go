package playback

import (
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type cancelBody struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (b *cancelBody) Close() error { defer b.cancel(); return b.ReadCloser.Close() }

func (s *Service) fetchCandidate(ctx context.Context, method, raw, byteRange, referer string) (*http.Response, context.CancelFunc, error) {
	attempt, cancel := context.WithCancel(ctx)
	req, err := http.NewRequestWithContext(attempt, method, raw, nil)
	if err != nil {
		return nil, cancel, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	if referer == "" {
		referer = "https://www.bilibili.com/"
	}
	req.Header.Set("Referer", referer)
	req.Header.Set("Accept-Encoding", "identity")
	if byteRange != "" {
		req.Header.Set("Range", byteRange)
	}
	// Bound connection / response-header stalls, without placing a short deadline
	// on a healthy long video body. Four candidates + one refresh stay bounded.
	headerDone := make(chan struct{})
	timer := time.AfterFunc(3*time.Second, func() { cancel(); close(headerDone) })
	response, err := s.http.Do(req)
	if !timer.Stop() {
		<-headerDone
	}
	if err == nil && attempt.Err() != nil {
		response.Body.Close()
		return nil, cancel, attempt.Err()
	}
	return response, cancel, err
}

func validRangeInput(raw string) bool {
	if raw == "" {
		return true
	}
	if len(raw) > 100 || !singleRange.MatchString(raw) {
		return false
	}
	start, end, _ := strings.Cut(strings.TrimPrefix(raw, "bytes="), "-")
	if start == "" {
		value, err := strconv.ParseInt(end, 10, 64)
		return err == nil && value > 0
	}
	first, err := strconv.ParseInt(start, 10, 64)
	if err != nil {
		return false
	}
	if end == "" {
		return true
	}
	last, err := strconv.ParseInt(end, 10, 64)
	return err == nil && last >= first
}

func validMediaResponse(response *http.Response, requested string) bool {
	encoding := response.Header.Get("Content-Encoding")
	if encoding != "" && encoding != "identity" {
		return false
	}
	if requested != "" && response.StatusCode == http.StatusRequestedRangeNotSatisfiable {
		raw := response.Header.Get("Content-Range")
		if !strings.HasPrefix(raw, "bytes */") {
			return false
		}
		total, err := strconv.ParseInt(strings.TrimPrefix(raw, "bytes */"), 10, 64)
		if err != nil || total < 0 {
			return false
		}
		_, _, ok := requestedBounds(requested, total)
		return !ok
	}
	mediaType := strings.ToLower(strings.TrimSpace(strings.Split(response.Header.Get("Content-Type"), ";")[0]))
	if mediaType != "" && mediaType != "video/mp4" && mediaType != "application/octet-stream" && mediaType != "binary/octet-stream" {
		return false
	}
	if requested == "" {
		return response.StatusCode == http.StatusOK && response.Header.Get("Content-Range") == ""
	}
	if response.StatusCode != http.StatusPartialContent {
		return false
	}
	raw := response.Header.Get("Content-Range")
	if !strings.HasPrefix(raw, "bytes ") {
		return false
	}
	bounds, size, ok := strings.Cut(strings.TrimPrefix(raw, "bytes "), "/")
	if !ok {
		return false
	}
	first, last, ok := strings.Cut(bounds, "-")
	if !ok {
		return false
	}
	start, e1 := strconv.ParseInt(first, 10, 64)
	end, e2 := strconv.ParseInt(last, 10, 64)
	total, e3 := strconv.ParseInt(size, 10, 64)
	if e1 != nil || e2 != nil || e3 != nil || total <= 0 || start < 0 || end < start || end >= total {
		return false
	}
	requestedStart, requestedEnd, ok := requestedBounds(requested, total)
	if !ok || start != requestedStart || end != requestedEnd {
		return false
	}
	if rawLength := response.Header.Get("Content-Length"); rawLength != "" {
		length, err := strconv.ParseInt(rawLength, 10, 64)
		if err != nil || length != end-start+1 {
			return false
		}
	}
	return true
}

func mediaSize(response *http.Response) int64 {
	if _, total, ok := strings.Cut(response.Header.Get("Content-Range"), "/"); ok {
		value, _ := strconv.ParseInt(total, 10, 64)
		return value
	}
	value, _ := strconv.ParseInt(response.Header.Get("Content-Length"), 10, 64)
	return value
}

func requestedBounds(raw string, total int64) (int64, int64, bool) {
	if total <= 0 {
		return 0, 0, false
	}
	first, last, _ := strings.Cut(strings.TrimPrefix(raw, "bytes="), "-")
	if first == "" {
		suffix, err := strconv.ParseInt(last, 10, 64)
		if err != nil || suffix <= 0 {
			return 0, 0, false
		}
		if suffix > total {
			suffix = total
		}
		return total - suffix, total - 1, true
	}
	start, err := strconv.ParseInt(first, 10, 64)
	if err != nil || start >= total {
		return 0, 0, false
	}
	end := total - 1
	if last != "" {
		value, err := strconv.ParseInt(last, 10, 64)
		if err != nil || value < start {
			return 0, 0, false
		}
		if value < end {
			end = value
		}
	}
	return start, end, true
}
