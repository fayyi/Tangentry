package webdav

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"unicode"

	"tangentry/internal/backup"
)

const maxFolders = 500

type folderMultistatus struct {
	Responses []struct {
		Href     string           `xml:"href"`
		Propstat []folderPropstat `xml:"propstat"`
	} `xml:"response"`
}

type folderPropstat struct {
	Status string `xml:"status"`
	Prop   struct {
		ResourceType struct {
			Collection *struct{} `xml:"collection"`
		} `xml:"resourcetype"`
	} `xml:"prop"`
}

func (s *Store) Browse(ctx context.Context, folderPath string) ([]backup.RemoteFolder, error) {
	if err := validateFolderPath(folderPath); err != nil {
		return nil, err
	}
	u, err := s.folderURL(folderPath)
	if err != nil {
		return nil, err
	}
	items, err := s.propfind(ctx, u, "1")
	if err != nil {
		return nil, err
	}
	basePath := strings.TrimSuffix(u.Path, "/")
	folders := make([]backup.RemoteFolder, 0)
	seen := make(map[string]struct{})
	for _, item := range items.Responses {
		href, ok := canonicalHref(item.Href, s.base)
		if !ok || !withinEndpointRoot(href.Path, s.base.Path) {
			return nil, errors.New("invalid WebDAV folder href")
		}
		if !responseIsCollection(item.Propstat) {
			continue
		}
		name, ok := directChildName(item.Href, s.base, basePath)
		if !ok {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		if len(folders) == maxFolders {
			return nil, backup.ErrTooManyFolders
		}
		childPath := name
		if folderPath != "" {
			childPath = folderPath + "/" + name
		}
		folders = append(folders, backup.RemoteFolder{Name: name, Path: childPath})
	}
	sort.Slice(folders, func(i, j int) bool { return folders[i].Name < folders[j].Name })
	return folders, nil
}

func (s *Store) CreateFolder(ctx context.Context, parentPath, name string) (bool, error) {
	if err := validateFolderPath(parentPath); err != nil {
		return false, err
	}
	if err := validateFolderName(name); err != nil {
		return false, err
	}
	parentURL, err := s.folderURL(parentPath)
	if err != nil {
		return false, err
	}
	target := *parentURL
	target.Path = strings.TrimSuffix(parentURL.Path, "/") + "/" + name
	target.RawPath = escapeURLPath(target.Path)
	resp, err := s.request(ctx, "MKCOL", &target, nil, -1, nil)
	if err != nil {
		return false, folderStatusError(err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusCreated:
		return false, nil
	case http.StatusUnauthorized:
		return false, backup.ErrFolderUnauthorized
	case http.StatusForbidden:
		return false, backup.ErrFolderPermissionDenied
	case http.StatusMethodNotAllowed, http.StatusConflict:
		// DAV servers vary between 405 and 409 for both existing collections
		// and invalid parents. Confirm the resource types before classifying.
		exists, probeErr := s.confirmCollection(ctx, &target)
		if probeErr != nil {
			return false, probeErr
		}
		if exists {
			return true, nil
		}
		parentExists, parentErr := s.confirmCollection(ctx, parentURL)
		if parentErr != nil {
			return false, parentErr
		}
		if !parentExists {
			return false, backup.ErrFolderParentNotFound
		}
		return false, backup.ErrFolderPermissionDenied
	case http.StatusNotFound:
		return false, backup.ErrFolderParentNotFound
	default:
		return false, folderStatusError(statusError{status: resp.StatusCode})
	}
}

func (s *Store) confirmCollection(ctx context.Context, u *url.URL) (bool, error) {
	items, err := s.propfind(ctx, u, "0")
	if err != nil {
		if errors.Is(err, backup.ErrFolderParentNotFound) {
			return false, nil
		}
		return false, err
	}
	for _, item := range items.Responses {
		if !responseIsCollection(item.Propstat) {
			continue
		}
		href, valid := canonicalHref(item.Href, s.base)
		if valid && strings.TrimSuffix(href.Path, "/") == strings.TrimSuffix(u.Path, "/") {
			return true, nil
		}
	}
	return false, nil
}

func (s *Store) propfind(ctx context.Context, u *url.URL, depth string) (folderMultistatus, error) {
	body := strings.NewReader(`<?xml version="1.0" encoding="utf-8"?><d:propfind xmlns:d="DAV:"><d:prop><d:resourcetype/></d:prop></d:propfind>`)
	resp, err := s.request(ctx, "PROPFIND", u, body, int64(body.Len()), http.Header{"Depth": []string{depth}, "Content-Type": []string{"application/xml"}})
	if err != nil {
		return folderMultistatus{}, folderStatusError(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return folderMultistatus{}, backup.ErrFolderUnauthorized
	}
	if resp.StatusCode == http.StatusForbidden {
		return folderMultistatus{}, backup.ErrFolderPermissionDenied
	}
	if resp.StatusCode == http.StatusNotFound {
		return folderMultistatus{}, backup.ErrFolderParentNotFound
	}
	if err := checkStatus(resp, http.StatusMultiStatus); err != nil {
		return folderMultistatus{}, folderStatusError(err)
	}
	if resp.ContentLength > maxPropfind {
		return folderMultistatus{}, backup.ErrFolderResponseTooLarge
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxPropfind+1))
	if err != nil {
		return folderMultistatus{}, err
	}
	if len(data) > maxPropfind {
		return folderMultistatus{}, backup.ErrFolderResponseTooLarge
	}
	var result folderMultistatus
	if err := xml.Unmarshal(data, &result); err != nil {
		return folderMultistatus{}, errors.New("invalid WebDAV PROPFIND response")
	}
	return result, nil
}

func responseIsCollection(propstats []folderPropstat) bool {
	for _, propstat := range propstats {
		if status, ok := propstatSuccessful(propstat.Status); ok && status && propstat.Prop.ResourceType.Collection != nil {
			return true
		}
	}
	return false
}

func propstatSuccessful(statusLine string) (bool, bool) {
	var protocol string
	var status int
	if _, err := fmt.Sscanf(strings.TrimSpace(statusLine), "%s %d", &protocol, &status); err != nil || status == 0 {
		return false, false
	}
	return status >= 200 && status < 300, true
}

func directChildName(rawHref string, base *url.URL, requestedPath string) (string, bool) {
	href, ok := canonicalHref(rawHref, base)
	if !ok {
		return "", false
	}
	parent := strings.TrimSuffix(requestedPath, "/")
	child := strings.TrimSuffix(href.Path, "/")
	if child == parent || !strings.HasPrefix(child, parent+"/") {
		return "", false
	}
	remainder := strings.TrimPrefix(child, parent+"/")
	if remainder == "" || strings.Contains(remainder, "/") || hasControlCharacter(remainder) || remainder == "." || remainder == ".." {
		return "", false
	}
	return remainder, true
}

func canonicalHref(rawHref string, base *url.URL) (*url.URL, bool) {
	href, err := url.Parse(rawHref)
	if err != nil || href.RawQuery != "" || href.Fragment != "" || href.User != nil {
		return nil, false
	}
	if href.Host != "" && (!strings.EqualFold(href.Host, base.Host) || href.Scheme != "" && href.Scheme != base.Scheme) {
		return nil, false
	}
	if href.IsAbs() && (href.Scheme != base.Scheme || !strings.EqualFold(href.Host, base.Host)) {
		return nil, false
	}
	if href.Opaque != "" || !strings.HasPrefix(href.Path, "/") || strings.Contains(href.Path, "\\") || strings.Contains(href.Path, "//") || hasControlCharacter(href.Path) {
		return nil, false
	}
	for _, segment := range nonEmptyPathSegments(href.EscapedPath()) {
		if segment == "" || segment == "." || segment == ".." {
			return nil, false
		}
		decoded := segment
		for depth := 0; depth < 8; depth++ {
			next, err := url.PathUnescape(decoded)
			if err != nil || strings.ContainsAny(next, "/\\\x00") || hasControlCharacter(next) {
				return nil, false
			}
			if next == decoded {
				break
			}
			decoded = next
			if depth == 7 || decoded == "." || decoded == ".." {
				return nil, false
			}
		}
	}
	return href, true
}

func (s *Store) folderURL(folderPath string) (*url.URL, error) {
	if err := validateFolderPath(folderPath); err != nil {
		return nil, err
	}
	u := *s.base
	root := strings.TrimSuffix(u.Path, "/")
	fullPath := s.prefix
	if folderPath != "" {
		if fullPath != "" {
			fullPath += "/"
		}
		fullPath += folderPath
	}
	if fullPath == "" {
		u.Path = root + "/"
	} else {
		u.Path = root + "/" + fullPath
	}
	u.RawPath = escapeURLPath(u.Path)
	return &u, nil
}

func escapeURLPath(value string) string {
	segments := nonEmptyPathSegments(value)
	if len(segments) == 0 {
		if strings.HasSuffix(value, "/") {
			return "/"
		}
		return ""
	}
	escaped := make([]string, len(segments))
	for i, segment := range segments {
		escaped[i] = url.PathEscape(segment)
	}
	result := "/" + strings.Join(escaped, "/")
	if strings.HasSuffix(value, "/") {
		result += "/"
	}
	return result
}

func nonEmptyPathSegments(value string) []string {
	trimmed := strings.Trim(value, "/")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "/")
}

func withinEndpointRoot(value, root string) bool {
	root = strings.TrimSuffix(root, "/")
	if root == "" {
		return strings.HasPrefix(value, "/")
	}
	return value == root || strings.HasPrefix(value, root+"/")
}

func validateFolderPath(value string) error {
	if value == "" {
		return nil
	}
	if strings.HasPrefix(value, "/") || strings.Contains(value, "\\") || strings.ContainsRune(value, '\x00') || hasControlCharacter(value) {
		return backup.ErrInvalidFolderPath
	}
	for _, part := range strings.Split(value, "/") {
		if part == "" || part == "." || part == ".." {
			return backup.ErrInvalidFolderPath
		}
		decoded := part
		for depth := 0; depth < 8; depth++ {
			next, err := url.PathUnescape(decoded)
			if err != nil || strings.ContainsAny(next, "/\\\x00") || hasControlCharacter(next) {
				return backup.ErrInvalidFolderPath
			}
			if next == decoded {
				break
			}
			decoded = next
			if depth == 7 || decoded == "." || decoded == ".." {
				return backup.ErrInvalidFolderPath
			}
		}
	}
	return nil
}

func validateFolderName(value string) error {
	if strings.TrimSpace(value) == "" || value == "." || value == ".." || len([]rune(value)) > 120 || strings.ContainsAny(value, "/\\") || strings.ContainsRune(value, '\x00') || hasControlCharacter(value) {
		return backup.ErrInvalidFolderName
	}
	decoded := value
	for depth := 0; depth < 8; depth++ {
		next, err := url.PathUnescape(decoded)
		if err != nil || strings.ContainsAny(next, "/\\\x00") || hasControlCharacter(next) {
			return backup.ErrInvalidFolderName
		}
		if next == decoded {
			break
		}
		decoded = next
		if depth == 7 || decoded == "." || decoded == ".." {
			return backup.ErrInvalidFolderName
		}
	}
	return nil
}

func hasControlCharacter(value string) bool {
	for _, r := range value {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}

func folderStatusError(err error) error {
	var status statusError
	if !errors.As(err, &status) {
		return err
	}
	switch status.status {
	case http.StatusUnauthorized:
		return backup.ErrFolderUnauthorized
	case http.StatusForbidden:
		return backup.ErrFolderPermissionDenied
	case http.StatusNotFound:
		return backup.ErrFolderParentNotFound
	default:
		return err
	}
}
