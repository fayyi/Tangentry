// Package filesystem stores local user files such as imported PDFs.
package filesystem

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"tangentry/internal/content"
	"tangentry/internal/documents"
	"tangentry/internal/netpolicy"
)

var idPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

type Documents struct {
	Directory string
	HTTP      *http.Client
}

func OpenDocuments(directory string, client *http.Client) (*Documents, error) {
	if os.MkdirAll(directory, 0700) != nil {
		return nil, documents.ErrUnavailable
	}
	return &Documents{directory, client}, nil
}
func (s *Documents) Import(ctx context.Context, reader io.Reader, title, url string) (content.Item, error) {
	file, err := os.CreateTemp(s.Directory, ".pdf-*")
	if err != nil {
		return content.Item{}, documents.ErrUnavailable
	}
	name := file.Name()
	defer os.Remove(name)
	hash := sha256.New()
	size, err := io.Copy(io.MultiWriter(file, hash), io.LimitReader(reader, documents.MaxSize+1))
	if err != nil || size > documents.MaxSize || ctx.Err() != nil {
		file.Close()
		return content.Item{}, documents.ErrInvalid
	}
	prefix := make([]byte, min(size, 1024))
	if _, err = file.ReadAt(prefix, 0); err != nil && err != io.EOF {
		file.Close()
		return content.Item{}, documents.ErrInvalid
	}
	if !bytes.Contains(prefix, []byte("%PDF-")) {
		file.Close()
		return content.Item{}, documents.ErrInvalid
	}
	if file.Close() != nil {
		return content.Item{}, documents.ErrUnavailable
	}
	id := hex.EncodeToString(hash.Sum(nil))
	title = strings.TrimSpace(title)
	if title == "" {
		title = "PDF 文档"
	}
	if len(title) > 2000 {
		return content.Item{}, documents.ErrInvalid
	}
	item := content.Item{ID: "pdf:" + id, Platform: "pdf", PlatformID: id, Kind: "pdf", Title: title, URL: url, Description: "已保存的 PDF 原文件，可在本机阅读。"}
	if content.Validate(item) != nil {
		return content.Item{}, documents.ErrInvalid
	}
	if os.Rename(name, filepath.Join(s.Directory, id+".pdf")) != nil {
		return content.Item{}, documents.ErrUnavailable
	}
	payload, _ := json.Marshal(item)
	meta, err := os.CreateTemp(s.Directory, ".meta-*")
	if err != nil {
		return content.Item{}, documents.ErrUnavailable
	}
	defer os.Remove(meta.Name())
	if _, err = meta.Write(payload); err != nil {
		meta.Close()
		return content.Item{}, documents.ErrUnavailable
	}
	if meta.Close() != nil || os.Rename(meta.Name(), filepath.Join(s.Directory, id+".json")) != nil {
		return content.Item{}, documents.ErrUnavailable
	}
	return item, nil
}
func (s *Documents) Fetch(ctx context.Context, raw string) (content.Item, error) {
	canonical, err := netpolicy.URL(raw)
	if err != nil {
		return content.Item{}, documents.ErrInvalid
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", canonical, nil)
	req.Header.Set("Accept", "application/pdf")
	res, err := s.HTTP.Do(req)
	if err != nil {
		return content.Item{}, documents.ErrUnavailable
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return content.Item{}, documents.ErrUnavailable
	}
	title := filepath.Base(res.Request.URL.Path)
	return s.Import(ctx, res.Body, title, canonical)
}
func (s *Documents) File(id string) (*os.File, error) {
	if !idPattern.MatchString(id) {
		return nil, documents.ErrInvalid
	}
	p := filepath.Join(s.Directory, id+".pdf")
	info, err := os.Lstat(p)
	if err != nil || !info.Mode().IsRegular() {
		return nil, documents.ErrUnavailable
	}
	return os.Open(p)
}
func (s *Documents) Get(id string) (content.Item, error) {
	if !idPattern.MatchString(id) {
		return content.Item{}, documents.ErrInvalid
	}
	raw, err := os.ReadFile(filepath.Join(s.Directory, id+".json"))
	if err != nil {
		return content.Item{}, documents.ErrUnavailable
	}
	var item content.Item
	if json.Unmarshal(raw, &item) != nil {
		return content.Item{}, documents.ErrUnavailable
	}
	return item, nil
}

var _ documents.Service = (*Documents)(nil)
