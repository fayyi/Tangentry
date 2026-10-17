package tools

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
)

func sha256File(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	sum := sha256.New()
	if _, err := io.Copy(sum, io.LimitReader(file, managedDownloadLimit+1)); err != nil {
		return "", err
	}
	return hex.EncodeToString(sum.Sum(nil)), nil
}

func (m *managedTool) download(ctx context.Context, release managedRelease, destination string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, release.URL, nil)
	if err != nil {
		return err
	}
	response, err := m.client.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("%s download: HTTP %d", m.name, response.StatusCode)
	}
	file, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	hash := sha256.New()
	counter := &countingWriter{total: response.ContentLength, report: m.progress}
	size, copyErr := io.Copy(io.MultiWriter(file, hash, counter), io.LimitReader(response.Body, managedDownloadLimit+1))
	if copyErr != nil {
		_ = file.Close()
		return copyErr
	}
	if size > managedDownloadLimit {
		_ = file.Close()
		return fmt.Errorf("%s download exceeds %d bytes", m.name, managedDownloadLimit)
	}
	if hex.EncodeToString(hash.Sum(nil)) != release.SHA256 {
		_ = file.Close()
		return fmt.Errorf("%s checksum mismatch", m.name)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func extractMember(archive, member, destination string) error {
	reader, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer reader.Close()
	for _, file := range reader.File {
		if file.Name != member || file.FileInfo().IsDir() {
			continue
		}
		source, err := file.Open()
		if err != nil {
			return err
		}
		target, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
		if err != nil {
			_ = source.Close()
			return err
		}
		size, copyErr := io.Copy(target, io.LimitReader(source, 4*managedDownloadLimit+1))
		closeSourceErr := source.Close()
		closeTargetErr := target.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeSourceErr != nil {
			return closeSourceErr
		}
		if closeTargetErr != nil {
			return closeTargetErr
		}
		if size > 4*managedDownloadLimit {
			return errors.New("archive member is too large")
		}
		return nil
	}
	return fmt.Errorf("%s is missing from the archive", member)
}

// countingWriter reports download progress on the first chunk, then at most
// every 256 KiB.
type countingWriter struct {
	total    int64
	received int64
	reported int64
	report   func(received, total int64)
}

func (c *countingWriter) Write(data []byte) (int, error) {
	c.received += int64(len(data))
	if c.reported == 0 || c.received-c.reported >= 256<<10 || c.received == c.total {
		c.reported = c.received
		c.report(c.received, max(c.total, 0))
	}
	return len(data), nil
}
