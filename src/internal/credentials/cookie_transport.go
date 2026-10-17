package credentials

import (
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Header returns only cookies matching the actual destination, path, TLS and expiry.
func (s *Store) Header(platform, rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || !allowedDomain(platform, u.Hostname()) {
		return ""
	}
	s.mu.RLock()
	e, ok := s.entries[platform]
	fallback, file := s.fallbackHeader, s.fallbackFile
	s.mu.RUnlock()
	if !ok && platform == "bilibili" {
		if fallback != "" {
			return fallback
		}
		if raw, err := os.ReadFile(file); err == nil {
			e.Jar, _ = normalize(platform, "netscape", string(raw))
		}
	}
	values := []string{}
	for _, line := range strings.Split(e.Jar, "\n") {
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 7 {
			continue
		}
		host := strings.ToLower(u.Hostname())
		domain := strings.TrimPrefix(f[0], ".")
		matches := host == domain || (f[1] == "TRUE" && strings.HasSuffix(host, "."+domain))
		expiry, _ := strconv.ParseInt(f[4], 10, 64)
		requestPath := u.EscapedPath()
		if requestPath == "" {
			requestPath = "/"
		}
		pathMatches := requestPath == f[2] || strings.HasPrefix(requestPath, f[2]) && (strings.HasSuffix(f[2], "/") || len(requestPath) > len(f[2]) && requestPath[len(f[2])] == '/')
		if matches && pathMatches && (f[3] != "TRUE" || u.Scheme == "https") && (expiry == 0 || expiry > time.Now().Unix()) {
			values = append(values, f[5]+"="+f[6])
		}
	}
	return strings.Join(values, "; ")
}

func headerFromJar(jar string) string {
	values := []string{}
	for _, line := range strings.Split(jar, "\n") {
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) == 7 {
			values = append(values, f[5]+"="+f[6])
		}
	}
	return strings.Join(values, "; ")
}

// jarExpiry returns the earliest explicit cookie expiry. Session cookies return
// zero; the value is safe for settings UI and never includes a cookie value.
func jarExpiry(jar string) int64 {
	var earliest int64
	for _, line := range strings.Split(jar, "\n") {
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 7 {
			continue
		}
		if expiry, err := strconv.ParseInt(fields[4], 10, 64); err == nil && expiry > 0 && (earliest == 0 || expiry < earliest) {
			earliest = expiry
		}
	}
	return earliest
}

// Snapshot is a private, disposable jar per CLI invocation. yt-dlp may mutate it.
func (s *Store) Snapshot(platform string) (string, func(), error) {
	s.mu.RLock()
	e, ok := s.entries[platform]
	header, file := s.fallbackHeader, s.fallbackFile
	s.mu.RUnlock()
	if !ok && platform == "bilibili" {
		if file != "" {
			raw, err := os.ReadFile(file)
			if err != nil {
				return "", func() {}, ErrStorage
			}
			e.Jar, err = normalize(platform, "netscape", string(raw))
			if err != nil {
				return "", func() {}, ErrInvalid
			}
		} else if header != "" {
			var err error
			e.Jar, err = normalize(platform, "header", header)
			if err != nil {
				return "", func() {}, ErrInvalid
			}
		}
	}
	if e.Jar == "" {
		return "", func() {}, nil
	}
	f, err := os.CreateTemp(s.directory, "request-*.txt")
	if err != nil {
		return "", func() {}, ErrStorage
	}
	name := f.Name()
	cleanup := func() { _ = os.Remove(name) }
	if _, err = f.WriteString(e.Jar); err != nil {
		f.Close()
		cleanup()
		return "", func() {}, ErrStorage
	}
	if f.Close() != nil {
		cleanup()
		return "", func() {}, ErrStorage
	}
	return name, cleanup, nil
}
