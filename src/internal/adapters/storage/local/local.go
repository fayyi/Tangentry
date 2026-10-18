package local

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"

	"tangentry/internal/backup"
)

type Store struct{ root string }

func New(root string) (*Store, error) {
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	return &Store{root: root}, nil
}

func (s *Store) Upload(ctx context.Context, name string, r io.Reader) error {
	path, err := s.object(name)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".upload-")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err = ctx.Err(); err != nil {
		tmp.Close()
		return err
	}
	if _, err = io.Copy(tmp, r); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
func (s *Store) Read(ctx context.Context, name string) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path, err := s.object(name)
	if err != nil {
		return nil, err
	}
	return os.Open(path)
}
func (s *Store) List(ctx context.Context, prefix string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var out []string
	root, err := s.object(prefix)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(root); err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return nil, err
	}
	err = filepath.Walk(root, func(path string, info os.FileInfo, e error) error {
		if e != nil || info == nil || info.IsDir() {
			return e
		}
		rel, e := filepath.Rel(s.root, path)
		if e == nil {
			out = append(out, filepath.ToSlash(rel))
		}
		return e
	})
	return out, err
}
func (s *Store) Delete(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path, err := s.object(name)
	if err != nil {
		return err
	}
	return os.Remove(path)
}
func (s *Store) TestConnection(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := os.Stat(s.root)
	return err
}
func (s *Store) object(name string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(name))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || filepath.IsAbs(clean) {
		return "", backup.ErrInvalidPackage
	}
	root, _ := filepath.Abs(s.root)
	result, _ := filepath.Abs(filepath.Join(root, clean))
	if result != root && !strings.HasPrefix(result, root+string(filepath.Separator)) {
		return "", backup.ErrInvalidPackage
	}
	return result, nil
}
