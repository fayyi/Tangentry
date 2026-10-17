package xiaohongshu

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const maxImageBytes = 12 << 20

var ErrImageUnavailable = errors.New("xiaohongshu image unavailable")

type ImageData struct {
	Bytes []byte
	Type  string
}

type imageFlight struct {
	done chan struct{}
	data ImageData
	err  error
}

// Images keeps only pictures requested by the reader. Its caller resolves the
// source from saved note metadata; this is not an arbitrary URL proxy.
type Images struct {
	root    string
	http    *http.Client
	mu      sync.Mutex
	flights map[string]*imageFlight
	slots   chan struct{}
}

func OpenImages(root string, client *http.Client) (*Images, error) {
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	return &Images{root: root, http: client, flights: map[string]*imageFlight{}, slots: make(chan struct{}, 4)}, nil
}

func ImageVersion(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func (s *Images) Read(ctx context.Context, raw string) (ImageData, error) {
	if imageURL(raw) != raw {
		return ImageData{}, ErrImageUnavailable
	}
	key := ImageVersion(raw)
	s.mu.Lock()
	if active := s.flights[key]; active != nil {
		s.mu.Unlock()
		select {
		case <-ctx.Done():
			return ImageData{}, ctx.Err()
		case <-active.done:
			return active.data, active.err
		}
	}
	active := &imageFlight{done: make(chan struct{})}
	s.flights[key] = active
	s.mu.Unlock()
	active.data, active.err = s.read(ctx, raw, key)
	s.mu.Lock()
	delete(s.flights, key)
	close(active.done)
	s.mu.Unlock()
	return active.data, active.err
}

func (s *Images) read(ctx context.Context, raw, key string) (ImageData, error) {
	path := filepath.Join(s.root, key+".img")
	if info, err := os.Lstat(path); err == nil && !info.Mode().IsRegular() {
		return ImageData{}, ErrImageUnavailable
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return ImageData{}, ErrImageUnavailable
	}
	if file, err := os.Open(path); err == nil {
		info, statErr := file.Stat()
		if statErr != nil || !info.Mode().IsRegular() || info.Size() > maxImageBytes {
			file.Close()
			return ImageData{}, ErrImageUnavailable
		}
		data, readErr := io.ReadAll(io.LimitReader(file, maxImageBytes+1))
		closeErr := file.Close()
		if readErr != nil || closeErr != nil {
			return ImageData{}, ErrImageUnavailable
		}
		if kind := rasterType(data); kind != "" {
			return ImageData{Bytes: data, Type: kind}, nil
		}
		// A damaged cache may be replaced; a filesystem failure must not
		// silently trigger another platform request.
	} else if !errors.Is(err, os.ErrNotExist) {
		return ImageData{}, ErrImageUnavailable
	}
	if s.http == nil {
		return ImageData{}, ErrImageUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	case <-ctx.Done():
		return ImageData{}, ctx.Err()
	}
	client := *s.http
	client.Jar = nil
	// Image redirects and account credentials are unnecessary for public CDN
	// images. Never follow a CDN error to an unrelated destination.
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return ImageData{}, ErrImageUnavailable
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Referer", "https://www.xiaohongshu.com/")
	req.Header.Set("Accept", "image/avif,image/webp,image/png,image/jpeg,image/gif")
	res, err := client.Do(req)
	if err != nil {
		return ImageData{}, ErrImageUnavailable
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK || res.ContentLength > maxImageBytes {
		return ImageData{}, ErrImageUnavailable
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, maxImageBytes+1))
	kind := rasterType(data)
	if err != nil || len(data) > maxImageBytes || kind == "" {
		return ImageData{}, ErrImageUnavailable
	}
	if err := ctx.Err(); err != nil {
		return ImageData{}, err
	}
	file, err := os.CreateTemp(s.root, ".image-*")
	if err != nil {
		return ImageData{}, ErrImageUnavailable
	}
	defer os.Remove(file.Name())
	_, writeErr := file.Write(data)
	syncErr := file.Sync()
	closeErr := file.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil || os.Rename(file.Name(), path) != nil {
		return ImageData{}, ErrImageUnavailable
	}
	return ImageData{Bytes: data, Type: kind}, nil
}

func rasterType(data []byte) string {
	switch http.DetectContentType(data) {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		return http.DetectContentType(data)
	}
	// AVIF uses an ISO media header rather than the signatures recognized by
	// net/http. Accept its image brands only, never SVG or HTML.
	if len(data) >= 24 && bytes.Equal(data[4:8], []byte("ftyp")) &&
		(bytes.Equal(data[8:12], []byte("avif")) || bytes.Equal(data[8:12], []byte("avis"))) {
		return "image/avif"
	}
	return ""
}
