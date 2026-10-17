package notes

import (
	"archive/zip"
	"bytes"
	"context"
	"time"
)

// exportArchive owns ZIP mechanics and the uncompressed payload limit. The
// note exporter can therefore focus on selecting and validating entries.
type exportArchive struct {
	output       bytes.Buffer
	writer       *zip.Writer
	bytesWritten int
}

func newExportArchive() *exportArchive {
	archive := &exportArchive{}
	archive.writer = zip.NewWriter(&archive.output)
	return archive
}

func (a *exportArchive) Add(ctx context.Context, name string, body []byte, modified time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	a.bytesWritten += len(body)
	if a.bytesWritten > ExportMaxArchiveBytes {
		return ErrExportLimit
	}
	header := &zip.FileHeader{Name: name, Method: zip.Deflate, Modified: modified.UTC()}
	header.SetMode(0600)
	file, err := a.writer.CreateHeader(header)
	if err != nil {
		return err
	}
	_, err = file.Write(body)
	return err
}

func (a *exportArchive) Close(ctx context.Context) ([]byte, error) {
	if err := a.writer.Close(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return a.output.Bytes(), nil
}
