package ai

import (
	"context"
	"sync"

	"tangentry/internal/subtitles"
)

const MaxInputBytes = 3 << 20

const (
	batchBytes        = 8000
	batchCount        = 32
	articleBatchBytes = 2000
)

type Segment struct {
	Index int    `json:"index"`
	Text  string `json:"text"`
}

type Translator interface {
	Translate(context.Context, Config, []Segment) ([]Segment, error)
}

type Input struct {
	Source    string          `json:"source,omitempty"`
	ContentID string          `json:"contentId"`
	Track     subtitles.Track `json:"track"`
	Revision  int64           `json:"revision"`
	// Regenerate discards the stored translation of this text first.
	Regenerate bool `json:"regenerate,omitempty"`
}

type Result struct {
	Key       string   `json:"key"`
	ContentID string   `json:"contentId"`
	Language  string   `json:"language"`
	BaseURL   string   `json:"baseUrl"`
	Model     string   `json:"model"`
	Total     int      `json:"total"`
	Texts     []string `json:"texts"`
	UpdatedAt int64    `json:"updatedAt"`
}

type Inspection struct {
	Settings Status `json:"settings"`
	Result   Result `json:"result"`
	Active   bool   `json:"active"`
}

type Service struct {
	mu           sync.Mutex
	config       Config
	settingsPath string
	results      ResultStore
	summaries    *summaryStore
	provider     Translator
	summarizer   Summarizer
	active       string
	cancel       context.CancelFunc
	closed       bool
	wait         sync.WaitGroup
}
