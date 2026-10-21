// Package webdav implements the small WebDAV subset used by encrypted backups.
// It deliberately uses the Go standard library instead of adding a third-party
// client (the evaluated gowebdav v0.13.0 is BSD-3-Clause) so redirect, body
// replay and response limits remain visible and auditable here.
package webdav

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	"tangentry/internal/backup"
)

const (
	defaultTimeout = 30 * time.Second
	maxRetries     = 2
	maxPropfind    = 4 << 20
)

type Config struct {
	Endpoint         string
	Username         string
	Password         string
	RemotePath       string
	AllowInsecureTLS bool
	Timeout          time.Duration
	TempDir          string
}

type Store struct {
	base      *url.URL
	prefix    string
	user      string
	pass      string
	client    *http.Client
	timeout   time.Duration
	tempDir   string
	readLimit int64
}

type statusError struct{ status int }

func (e statusError) Error() string        { return fmt.Sprintf("WebDAV request failed: HTTP %d", e.status) }
func (e statusError) ErrorSummary() string { return fmt.Sprintf("WebDAV 返回 HTTP %d。", e.status) }

func New(cfg Config) (*Store, error) {
	u, err := url.Parse(strings.TrimSpace(cfg.Endpoint))
	if err != nil || u.Scheme != "http" && u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || hasTraversal(u.Path) {
		return nil, errors.New("invalid WebDAV endpoint")
	}
	u.Path = strings.TrimRight(path.Clean("/"+u.Path), "/")
	u.RawPath = ""
	prefix, err := cleanPath(cfg.RemotePath)
	if err != nil {
		return nil, err
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: cfg.AllowInsecureTLS} // #nosec G402 -- explicit user option
	originScheme, originHost := u.Scheme, u.Host
	client := &http.Client{Transport: transport, Timeout: timeout}
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != originScheme || req.URL.Host != originHost {
			req.Header.Del("Authorization")
			return http.ErrUseLastResponse
		}
		return nil
	}
	return &Store{base: u, prefix: prefix, user: cfg.Username, pass: cfg.Password, client: client, timeout: timeout, tempDir: cfg.TempDir, readLimit: backup.MaxPackageSize}, nil
}

func cleanPath(value string) (string, error) {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	if value == "" || value == "." || value == "/" {
		return "", nil
	}
	if strings.ContainsRune(value, '\x00') || strings.HasPrefix(value, "/") {
		return "", errors.New("invalid WebDAV path")
	}
	parts := strings.Split(value, "/")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			continue
		}
		decoded := part
		for depth := 0; depth < 8; depth++ {
			var err error
			next, err := url.PathUnescape(decoded)
			if err != nil || strings.ContainsAny(next, "/\\\x00") {
				return "", errors.New("invalid WebDAV path")
			}
			if next == decoded {
				break
			}
			decoded = next
			if depth == 7 {
				return "", errors.New("invalid WebDAV path")
			}
		}
		if decoded == "." {
			continue
		}
		if decoded == ".." {
			return "", errors.New("WebDAV path traversal rejected")
		}
		out = append(out, decoded)
	}
	return strings.Join(out, "/"), nil
}

func hasTraversal(value string) bool {
	decoded := value
	for depth := 0; depth < 8; depth++ {
		for _, part := range strings.Split(strings.ReplaceAll(decoded, "\\", "/"), "/") {
			if part == ".." {
				return true
			}
		}
		unescaped, err := url.PathUnescape(decoded)
		if err != nil {
			return true
		}
		if unescaped == decoded {
			return false
		}
		decoded = unescaped
	}
	return true
}

func (s *Store) objectURL(name string) (*url.URL, error) {
	clean, err := cleanPath(name)
	if err != nil {
		return nil, errors.New("invalid WebDAV object path")
	}
	full := clean
	if clean == "" && s.prefix == "" {
		u := *s.base
		return &u, nil
	}
	if s.prefix != "" && clean != "" {
		full = s.prefix + "/" + clean
	} else if s.prefix != "" {
		full = s.prefix
	}
	u := *s.base
	base := strings.TrimSuffix(u.Path, "/")
	if full == "" {
		u.Path = base + "/"
	} else {
		u.Path = base + "/" + full
	}
	u.Path = path.Clean(u.Path)
	u.RawPath = escapeURLPath(u.Path)
	return &u, nil
}

func (s *Store) request(ctx context.Context, method string, u *url.URL, body io.ReadSeeker, size int64, headers http.Header) (*http.Response, error) {
	var last error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var reader io.Reader
		if body != nil {
			if _, err := body.Seek(0, io.SeekStart); err != nil {
				return nil, err
			}
			reader = body
		}
		req, err := http.NewRequestWithContext(ctx, method, u.String(), reader)
		if err != nil {
			return nil, err
		}
		if size >= 0 {
			req.ContentLength = size
		}
		for key, values := range headers {
			for _, value := range values {
				req.Header.Add(key, value)
			}
		}
		if s.user != "" {
			req.SetBasicAuth(s.user, s.pass)
		}
		resp, err := s.client.Do(req)
		if err == nil && !retryable(resp.StatusCode) {
			return resp, nil
		}
		if resp != nil {
			_ = resp.Body.Close()
			last = statusError{status: resp.StatusCode}
		} else {
			last = err
		}
		if !retryableError(resp, err) || attempt == maxRetries {
			return nil, last
		}
		timer := time.NewTimer(time.Duration(1<<attempt) * 100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	return nil, last
}

func retryable(status int) bool {
	switch status {
	case http.StatusRequestTimeout, http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}
func retryableError(resp *http.Response, err error) bool {
	if err != nil {
		if resp != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return false
		}
		var networkErr net.Error
		return errors.As(err, &networkErr) && (networkErr.Timeout() || networkErr.Temporary())
	}
	return retryable(resp.StatusCode)
}

func checkStatus(resp *http.Response, ok ...int) error {
	for _, status := range ok {
		if resp.StatusCode == status {
			return nil
		}
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	return statusError{status: resp.StatusCode}
}

func (s *Store) mkdirParents(ctx context.Context, u *url.URL) error {
	relative := strings.TrimPrefix(u.Path, strings.TrimSuffix(s.base.Path, "/")+"/")
	parts := strings.Split(strings.Trim(relative, "/"), "/")
	if len(parts) > 0 {
		parts = parts[:len(parts)-1]
	}
	current := strings.TrimSuffix(s.base.Path, "/")
	for _, part := range parts {
		if part == "" {
			continue
		}
		current += "/" + part
		dir := *s.base
		dir.Path = current
		resp, err := s.request(ctx, "MKCOL", &dir, nil, -1, nil)
		if err != nil {
			// Existing directories commonly answer 405 or 409; continue so a
			// server with stricter MKCOL semantics can still accept the upload.
			if strings.Contains(err.Error(), "HTTP 405") || strings.Contains(err.Error(), "HTTP 409") {
				continue
			}
			return err
		}
		if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusMethodNotAllowed && resp.StatusCode != http.StatusConflict {
			status := resp.StatusCode
			_ = resp.Body.Close()
			return statusError{status: status}
		}
		_ = resp.Body.Close()
	}
	return nil
}

func (s *Store) Upload(ctx context.Context, name string, input io.Reader) error {
	final, err := s.objectURL(name)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// A temporary file makes every retry replayable without retaining the
	// encrypted snapshot in memory.
	tmp, err := os.CreateTemp(s.tempDir, ".tangentry-webdav-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	defer tmp.Close()
	if err := tmp.Chmod(0600); err != nil {
		return err
	}
	limited := io.LimitReader(input, backup.MaxPackageSize+1)
	size, err := io.Copy(tmp, contextReader{ctx: ctx, reader: limited})
	if err != nil {
		return err
	}
	if size > backup.MaxPackageSize {
		return backup.ErrInvalidPackage
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := s.mkdirParents(ctx, final); err != nil {
		return err
	}
	randomBytes := make([]byte, 12)
	if _, err := rand.Read(randomBytes); err != nil {
		return err
	}
	temporary := *final
	temporary.Path += ".upload-" + hex.EncodeToString(randomBytes)
	cleanup := func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = s.deleteURL(cleanupCtx, &temporary)
	}
	defer cleanup()
	putResponse, err := s.request(ctx, http.MethodPut, &temporary, tmp, size, nil)
	if err != nil {
		return err
	}
	putErr := checkStatus(putResponse)
	_ = putResponse.Body.Close()
	if putErr != nil {
		return putErr
	}
	resp, err := s.request(ctx, "MOVE", &temporary, nil, -1, http.Header{"Destination": []string{final.String()}, "Overwrite": []string{"T"}})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return checkStatus(resp)
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

func (s *Store) deleteURL(ctx context.Context, u *url.URL) error {
	resp, err := s.request(ctx, http.MethodDelete, u, nil, -1, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil
	}
	return checkStatus(resp)
}

type limitedBody struct {
	body  io.ReadCloser
	read  int64
	limit int64
}

func (r *limitedBody) Read(p []byte) (int, error) {
	if r.read >= r.limit {
		var one [1]byte
		n, err := r.body.Read(one[:])
		if n > 0 {
			return 0, backup.ErrInvalidPackage
		}
		return n, err
	}
	max := r.limit - r.read
	if int64(len(p)) > max {
		p = p[:max]
	}
	n, err := r.body.Read(p)
	r.read += int64(n)
	if r.read > r.limit {
		return n, backup.ErrInvalidPackage
	}
	return n, err
}
func (r *limitedBody) Close() error { return r.body.Close() }

func (s *Store) Read(ctx context.Context, name string) (io.ReadCloser, error) {
	u, err := s.objectURL(name)
	if err != nil {
		return nil, err
	}
	resp, err := s.request(ctx, http.MethodGet, u, nil, -1, nil)
	if err != nil {
		return nil, err
	}
	if err := checkStatus(resp); err != nil {
		resp.Body.Close()
		return nil, err
	}
	if resp.ContentLength > backup.MaxPackageSize {
		resp.Body.Close()
		return nil, backup.ErrInvalidPackage
	}
	return &limitedBody{body: resp.Body, limit: s.readLimit}, nil
}

type multistatus struct {
	Responses []struct {
		Href     string `xml:"href"`
		Propstat []struct {
			Prop struct {
				ContentLength int64 `xml:"getcontentlength"`
			} `xml:"prop"`
		} `xml:"propstat"`
	} `xml:"response"`
}

func (s *Store) List(ctx context.Context, prefix string) ([]string, error) {
	u, err := s.objectURL(prefix)
	if err != nil {
		return nil, err
	}
	body := strings.NewReader(`<?xml version="1.0" encoding="utf-8"?><d:propfind xmlns:d="DAV:"><d:allprop/></d:propfind>`)
	resp, err := s.request(ctx, "PROPFIND", u, body, int64(body.Len()), http.Header{"Depth": []string{"infinity"}, "Content-Type": []string{"application/xml"}})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if err := checkStatus(resp, http.StatusMultiStatus); err != nil {
		return nil, err
	}
	if resp.ContentLength > maxPropfind {
		return nil, backup.ErrInvalidPackage
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxPropfind+1))
	if err != nil || len(data) > maxPropfind {
		return nil, backup.ErrInvalidPackage
	}
	var result multistatus
	if err := xml.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("invalid WebDAV PROPFIND response")
	}
	basePath := strings.TrimSuffix(s.base.Path, "/")
	if s.prefix != "" {
		basePath += "/" + s.prefix
	}
	out := make([]string, 0, len(result.Responses))
	for _, item := range result.Responses {
		for _, propstat := range item.Propstat {
			if propstat.Prop.ContentLength > backup.MaxPackageSize {
				return nil, backup.ErrInvalidPackage
			}
		}
		hrefURL, err := url.Parse(item.Href)
		if err != nil || hrefURL.RawQuery != "" || hrefURL.Fragment != "" {
			continue
		}
		if hrefURL.IsAbs() {
			if hrefURL.Scheme != s.base.Scheme || hrefURL.Host != s.base.Host {
				continue
			}
		}
		href := hrefURL.Path
		if href != basePath && !strings.HasPrefix(href, basePath+"/") {
			continue
		}
		rel := strings.TrimPrefix(strings.TrimPrefix(href, basePath), "/")
		if rel == "" || strings.HasSuffix(rel, "/") {
			continue
		}
		clean, err := cleanPath(rel)
		if err == nil && clean != "" {
			out = append(out, clean)
		}
	}
	return out, nil
}

func (s *Store) Delete(ctx context.Context, name string) error {
	u, err := s.objectURL(name)
	if err != nil {
		return err
	}
	return s.deleteURL(ctx, u)
}

func (s *Store) TestConnection(ctx context.Context) error {
	_, err := s.Browse(ctx, "")
	return err
}

func (s *Store) Close() error {
	if transport, ok := s.client.Transport.(*http.Transport); ok {
		transport.CloseIdleConnections()
	}
	return nil
}
