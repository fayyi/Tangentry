package local

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"tangentry/internal/backup"
)

type Store struct {
	rootPath string
	root     *os.Root
}

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
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	opened, err := os.OpenRoot(absolute)
	if err != nil {
		return nil, err
	}
	return &Store{rootPath: absolute, root: opened}, nil
}

func (s *Store) Close() error { return s.root.Close() }

func (s *Store) Upload(ctx context.Context, name string, r io.Reader) error {
	clean, err := cleanObject(name)
	if err != nil {
		return err
	}
	parent := filepath.ToSlash(filepath.Dir(clean))
	if parent == "." {
		parent = ""
	}
	if err := s.mkdirParents(parent); err != nil {
		return err
	}
	var random [12]byte
	if _, err := rand.Read(random[:]); err != nil {
		return err
	}
	temporary := filepath.ToSlash(filepath.Join(parent, ".upload-"+hex.EncodeToString(random[:])))
	file, err := s.root.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer s.root.Remove(temporary)
	if err := ctx.Err(); err != nil {
		file.Close()
		return err
	}
	if _, err := io.Copy(file, r); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return s.root.Rename(temporary, filepath.ToSlash(clean))
}

func (s *Store) Read(ctx context.Context, name string) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	clean, err := cleanObject(name)
	if err != nil {
		return nil, err
	}
	if err := s.checkParents(filepath.ToSlash(filepath.Dir(clean))); err != nil {
		return nil, err
	}
	info, err := s.root.Lstat(filepath.ToSlash(clean))
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		if err == nil {
			err = backup.ErrInvalidPackage
		}
		return nil, err
	}
	file, err := s.root.Open(filepath.ToSlash(clean))
	if err != nil {
		return nil, err
	}
	openedInfo, err := file.Stat()
	if err != nil || !openedInfo.Mode().IsRegular() || !os.SameFile(info, openedInfo) {
		file.Close()
		return nil, backup.ErrInvalidPackage
	}
	return file, nil
}

func (s *Store) List(ctx context.Context, prefix string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	clean, err := cleanObject(prefix)
	if err != nil {
		return nil, err
	}
	fsys := s.root.FS()
	if _, err := fs.Stat(fsys, clean); os.IsNotExist(err) {
		return []string{}, nil
	} else if err != nil {
		return nil, err
	}
	var out []string
	err = fs.WalkDir(fsys, clean, func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		info, err := s.root.Lstat(filepath.ToSlash(name))
		if err != nil || !info.Mode().IsRegular() {
			return nil
		}
		out = append(out, filepath.ToSlash(name))
		return nil
	})
	return out, err
}

func (s *Store) Delete(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	clean, err := cleanObject(name)
	if err != nil {
		return err
	}
	if err := s.checkParents(filepath.ToSlash(filepath.Dir(clean))); err != nil {
		return err
	}
	return s.root.Remove(filepath.ToSlash(clean))
}

func (s *Store) TestConnection(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	info, err := s.root.Stat(".")
	if err == nil && !info.IsDir() {
		return os.ErrInvalid
	}
	return err
}

func (s *Store) mkdirParents(name string) error {
	if name == "" || name == "." {
		return nil
	}
	current := ""
	for _, part := range strings.Split(name, "/") {
		if part == "" || part == "." || part == ".." {
			return backup.ErrInvalidPackage
		}
		current = filepath.ToSlash(filepath.Join(current, part))
		info, err := s.root.Lstat(current)
		if os.IsNotExist(err) {
			if err := s.root.Mkdir(current, 0700); err != nil {
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

func (s *Store) checkParents(name string) error {
	if name == "" || name == "." {
		return nil
	}
	current := ""
	for _, part := range strings.Split(filepath.ToSlash(name), "/") {
		if part == "" || part == "." || part == ".." {
			return backup.ErrInvalidPackage
		}
		current = filepath.ToSlash(filepath.Join(current, part))
		info, err := s.root.Lstat(current)
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return backup.ErrInvalidPackage
		}
	}
	return nil
}

func cleanObject(name string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(name))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || filepath.IsAbs(clean) {
		return "", backup.ErrInvalidPackage
	}
	return clean, nil
}

func mustRelative(root, name string) string {
	rel, _ := filepath.Rel(root, name)
	return rel
}
