package credentials

import (
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"
)

func allowedDomain(platform, domain string) bool {
	root, ok := domains[platform]
	domain = strings.ToLower(strings.TrimPrefix(domain, "."))
	return ok && (domain == root || strings.HasSuffix(domain, "."+root))
}

func allowedStorageDomain(platform, domain string) bool {
	if allowedDomain(platform, domain) {
		return true
	}
	domain = strings.ToLower(strings.TrimPrefix(domain, "."))
	return platform == "youtube" && (domain == "google.com" || strings.HasSuffix(domain, ".google.com"))
}

func normalize(platform, format, raw string) (string, error) {
	root, ok := domains[platform]
	if !ok || len(raw) > 256<<10 || !utf8.ValidString(raw) {
		return "", ErrInvalid
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ErrInvalid
	}
	lines := []string{"# Netscape HTTP Cookie File"}
	switch format {
	case "header":
		if strings.ContainsAny(raw, "\r\n\t") {
			return "", ErrInvalid
		}
		raw = strings.TrimPrefix(raw, "Cookie:")
		for _, pair := range strings.Split(raw, ";") {
			if strings.TrimSpace(pair) == "" {
				continue
			}
			name, value, ok := strings.Cut(strings.TrimSpace(pair), "=")
			if !ok || !validName(name) || !validValue(value) {
				return "", ErrInvalid
			}
			lines = append(lines, strings.Join([]string{"." + root, "TRUE", "/", "TRUE", "0", name, value}, "\t"))
		}
	case "netscape":
		for _, line := range strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n") {
			line = strings.TrimSuffix(line, "\r")
			if strings.HasPrefix(line, "#HttpOnly_") {
				line = strings.TrimPrefix(line, "#HttpOnly_")
			} else if strings.HasPrefix(line, "#") || strings.TrimSpace(line) == "" {
				continue
			}
			f := strings.Split(line, "\t")
			if len(f) != 7 || !allowedStorageDomain(platform, f[0]) || (f[1] != "TRUE" && f[1] != "FALSE") || !strings.HasPrefix(f[2], "/") || strings.ContainsAny(f[2], "\r\n") || (f[3] != "TRUE" && f[3] != "FALSE") || !validName(f[5]) || !validValue(f[6]) {
				return "", ErrInvalid
			}
			expiry, err := strconv.ParseInt(f[4], 10, 64)
			if err != nil || expiry < 0 {
				return "", ErrInvalid
			}
			f[0] = strings.ToLower(f[0])
			domain := strings.TrimPrefix(f[0], ".")
			if f[1] == "TRUE" {
				f[0] = "." + domain
			} else {
				f[0] = domain
			}
			lines = append(lines, strings.Join(f, "\t"))
		}
	default:
		return "", ErrInvalid
	}
	if len(lines) < 2 || len(lines) > 501 {
		return "", ErrInvalid
	}
	return strings.Join(lines, "\n") + "\n", nil
}

func validName(s string) bool {
	if s == "" || len(s) > 256 {
		return false
	}
	for _, r := range s {
		if r <= 32 || r >= 127 || strings.ContainsRune("()<>@,;:\\\"/[]?={}", r) {
			return false
		}
	}
	return true
}

func validValue(s string) bool {
	if len(s) > 16384 {
		return false
	}
	for _, r := range s {
		if r < 32 || r >= 127 || r == ';' {
			return false
		}
	}
	return true
}

func PlatformForURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	for _, p := range platforms {
		if allowedDomain(p, u.Hostname()) {
			return p
		}
	}
	if u.Hostname() == "youtu.be" {
		return "youtube"
	}
	return ""
}
