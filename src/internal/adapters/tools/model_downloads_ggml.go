package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"tangentry/internal/transcription"
)

type ggmlSpec struct {
	size   int64
	sha256 string
}

// Official whisper.cpp LFS hashes; publish only complete verified files.
var ggmlModels = map[string]ggmlSpec{
	"whisper-tiny":           {77691713, "be07e048e1e599ad46341c8d2a135645097a538221678b7acdd1b1919c6e1b21"},
	"whisper-base":           {147951465, "60ed5bc3dd14eea856493d334349b405782ddcaf0028d4b5df4088345fba2efe"},
	"whisper-small":          {487601967, "1be3a9b2063867b937e64e2ec7483364a79917e157fa98c5d94b5c1fffea987b"},
	"whisper-medium":         {1533763059, "6c14d5adee5f86394037b4e4e8b59f1673b6cee10e3cf0b11bbdbee79c156208"},
	"whisper-large-v3-turbo": {1624555275, "1fc70f774d38eb169993ac391eea357ef47c88757ef72ee5943879b7e8e2bc69"},
	"whisper-large-v3":       {3095033483, "64d182b440b98d5203c4f9bd541544d84c605196c4f7b845dfa11fb23594d1e2"},
}

func (m *ModelInstaller) installGGML(ctx context.Context, option transcription.ModelOption, stage func(string)) error {
	spec, ok := ggmlModels[option.ID]
	if !ok {
		return transcription.ErrInvalid
	}
	// Settings and a first transcription may request the same model at once;
	// the second waits and then finds it installed.
	m.ggmlMu.Lock()
	defer m.ggmlMu.Unlock()
	if installedModel(filepath.Join(m.Root, modelDirectory(option)), option) {
		return nil
	}
	source := m.ggmlSource
	if source == "" {
		source = "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source+modelDirectory(option), nil)
	if err != nil {
		return err
	}
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("model download: HTTP %d", response.StatusCode)
	}
	return m.receiveGGML(ctx, response.Body, option, spec, stage)
}

func (m *ModelInstaller) receiveGGML(ctx context.Context, body io.Reader, option transcription.ModelOption, spec ggmlSpec, stage func(string)) error {
	if err := os.MkdirAll(m.Root, 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(m.Root, ".ggml-download-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	hash := sha256.New()
	progress := &countingWriter{total: spec.size, report: func(received, total int64) { transcription.ReportProgress(ctx, received, total) }}
	size, err := io.Copy(io.MultiWriter(file, hash, progress), io.LimitReader(body, spec.size+1))
	if err != nil {
		return err
	}
	stage("verifying")
	if err := ctx.Err(); err != nil {
		return err
	}
	if size != spec.size || hex.EncodeToString(hash.Sum(nil)) != spec.sha256 {
		return fmt.Errorf("model checksum mismatch")
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return m.publish(ctx, file.Name(), option)
}

// catalogGGML returns the catalog whisper.cpp model a managed model path names.
func (m *ModelInstaller) catalogGGML(path string) (transcription.ModelOption, bool) {
	if m == nil || m.Root == "" || path == "" {
		return transcription.ModelOption{}, false
	}
	same := func(a, b string) bool {
		if filepath.Clean(a) == filepath.Clean(b) {
			return true
		}
		left, errLeft := filepath.EvalSymlinks(a)
		right, errRight := filepath.EvalSymlinks(b)
		return errLeft == nil && errRight == nil && left == right
	}
	if !same(filepath.Dir(path), m.Root) {
		return transcription.ModelOption{}, false
	}
	for _, option := range transcription.ModelCatalog() {
		if option.Backend == "whisper.cpp" && modelDirectory(option) == filepath.Base(path) {
			return option, true
		}
	}
	return transcription.ModelOption{}, false
}
