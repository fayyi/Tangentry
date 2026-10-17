package filesystem

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"tangentry/internal/transcription"
)

// TranscriptionSettings stores the private transcription configuration as a
// mode-0600 JSON file and replaces it atomically. Secrets are deliberately
// kept outside transcription.Config's public JSON representation.
type TranscriptionSettings struct {
	Path string
}

type transcriptionRecord struct {
	transcription.Config
	GroqAPIKey         string `json:"groqApiKey,omitempty"`
	CloudflareAPIToken string `json:"cloudflareApiToken,omitempty"`
	SiliconflowAPIKey  string `json:"siliconflowApiKey,omitempty"`
	MAICookie          string `json:"maiCookie,omitempty"`
}

func NewTranscriptionSettings(path string) *TranscriptionSettings {
	return &TranscriptionSettings{Path: path}
}

func (store *TranscriptionSettings) Load(initial transcription.Config) (transcription.Config, error) {
	data, err := os.ReadFile(store.Path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return initial, nil
		}
		return transcription.Config{}, err
	}
	var record transcriptionRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return transcription.Config{}, transcription.ErrInvalid
	}
	record.Config.GroqAPIKey = record.GroqAPIKey
	record.Config.CloudflareAPIToken = record.CloudflareAPIToken
	record.Config.SiliconflowAPIKey = record.SiliconflowAPIKey
	record.Config.MAICookie = record.MAICookie
	if !transcription.ValidateConfig(record.Config) {
		return transcription.Config{}, transcription.ErrInvalid
	}
	return record.Config, nil
}

func (store *TranscriptionSettings) Save(config transcription.Config) error {
	data, err := json.Marshal(transcriptionRecord{
		Config:             config,
		GroqAPIKey:         config.GroqAPIKey,
		CloudflareAPIToken: config.CloudflareAPIToken,
		SiliconflowAPIKey:  config.SiliconflowAPIKey,
		MAICookie:          config.MAICookie,
	})
	if err != nil {
		return transcription.ErrUnavailable
	}
	file, err := os.CreateTemp(filepath.Dir(store.Path), ".transcription-*")
	if err != nil {
		return transcription.ErrUnavailable
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if err := file.Chmod(0600); err != nil {
		_ = file.Close()
		return transcription.ErrUnavailable
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return transcription.ErrUnavailable
	}
	if err := file.Close(); err != nil {
		return transcription.ErrUnavailable
	}
	if err := os.Rename(temporary, store.Path); err != nil {
		return transcription.ErrUnavailable
	}
	return nil
}
