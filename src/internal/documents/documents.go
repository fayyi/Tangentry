// Package documents defines the PDF import and retrieval contract. File I/O is
// implemented by an adapter; library membership is managed separately.
package documents

import (
	"context"
	"errors"
	"io"
	"os"

	"tangentry/internal/content"
)

const MaxSize = 50 << 20

var ErrInvalid = errors.New("invalid PDF")
var ErrUnavailable = errors.New("PDF unavailable")

// Service is the document capability exposed to the library transport.
type Service interface {
	Import(context.Context, io.Reader, string, string) (content.Item, error)
	Get(string) (content.Item, error)
	File(string) (*os.File, error)
}
