package app

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// Model file operations are deliberately separate from directory migration:
// the migration code owns locking, symlink switching and rollback, while this
// module only copies a validated regular-file tree with cancellation support.
func copyModelTree(ctx context.Context, source, target string) error {
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		dest := filepath.Join(target, relative)
		if entry.IsDir() {
			return os.MkdirAll(dest, 0700)
		}
		if !entry.Type().IsRegular() {
			return errors.New("model storage contains unsupported files")
		}
		return copyModelFile(ctx, path, dest)
	})
}

type modelReader struct {
	ctx context.Context
	io.Reader
}

func (r modelReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.Reader.Read(p)
}

func copyModelFile(ctx context.Context, source, target string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, err = io.Copy(output, modelReader{ctx, input})
	if err == nil {
		err = output.Sync()
	}
	return errors.Join(err, output.Close())
}
