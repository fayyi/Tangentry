package filesystem

import (
	"context"
	"net/http"
	"path/filepath"
	"reflect"
	"strings"
	"tangentry/internal/documents"
	"testing"
)

func TestPDFImportIsDurableAndBounded(t *testing.T) {
	s, err := OpenDocuments(filepath.Join(t.TempDir(), "docs"), &http.Client{})
	if err != nil {
		t.Fatal(err)
	}
	item, err := s.Import(context.Background(), strings.NewReader("%PDF-1.7\nfixture"), "测试 PDF", "https://example.com/file.pdf")
	if err != nil {
		t.Fatal(err)
	}
	file, err := s.File(item.PlatformID)
	if err != nil {
		t.Fatal(err)
	}
	file.Close()
	got, err := s.Get(item.PlatformID)
	if err != nil || !reflect.DeepEqual(got, item) {
		t.Fatal("metadata durability", err)
	}
	for _, raw := range []string{"<html>fake PDF</html>", "", strings.Repeat("x", documents.MaxSize+1)} {
		if _, err := s.Import(context.Background(), strings.NewReader(raw), "bad", "https://example.com"); err != documents.ErrInvalid {
			t.Fatal("invalid PDF accepted", err)
		}
	}
	if _, err := s.File("../../etc/passwd"); err != documents.ErrInvalid {
		t.Fatal("path accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Import(ctx, strings.NewReader("%PDF-test"), "test", "https://example.com"); err != documents.ErrInvalid {
		t.Fatal("canceled import")
	}
}
