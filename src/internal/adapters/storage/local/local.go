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
	info, err := os.Lstat(root)
	if os.IsNotExist(err) {
		if err := os.MkdirAll(root, 0700); err != nil {
			return nil, err
		}
		info, err = os.Lstat(root)
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		if err == nil {
			err = backup.ErrInvalidPackage
		}
		return nil, err
	}
	return &Store{root: root}, nil
}

func (s *Store) Upload(ctx context.Context, name string, r io.Reader) error {
	path, err := s.object(name)
	if err != nil {
		return err
	}
	if err = s.safeParents(filepath.Dir(path)); err != nil {
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
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		if err == nil {
			err = os.ErrInvalid
		}
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
	rootInfo, err := os.Lstat(root)
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return nil, err
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 {
		return nil, backup.ErrInvalidPackage
	}
	err = filepath.Walk(root, func(path string, info os.FileInfo, e error) error {
		if e != nil || info == nil || info.IsDir() {
			return e
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return filepath.SkipDir
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
	info, err := os.Lstat(s.root)
	if err == nil && !info.IsDir() {
		return os.ErrInvalid
	}
	return err
}

func (s *Store) safeParents(dir string) error {
	root, _ := filepath.Abs(s.root)
	current := root
	rel, err := filepath.Rel(root, dir)
	if err != nil {
		return err
	}
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if part == "." || part == "" {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			if err := os.Mkdir(current, 0700); err != nil {
				return err
			}
			continue
		}
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return backup.ErrInvalidPackage
		}
	}
	return nil
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
	if err := s.checkParents(filepath.Dir(result)); err != nil {
		return "", err
	}
	return result, nil
}

func (s *Store) checkParents(dir string) error {
	root, _ := filepath.Abs(s.root)
	current := root
	rel, err := filepath.Rel(root, dir)
	if err != nil {
		return err
	}
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if part == "" || part == "." {
			continue
		}
		current = filepath.Join(current, part)
		info, statErr := os.Lstat(current)
		if os.IsNotExist(statErr) {
			continue
		}
		if statErr != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return backup.ErrInvalidPackage
		}
	}
	return nil
}
