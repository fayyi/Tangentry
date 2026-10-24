package transcription

import (
	"context"

	"tangentry/internal/subtitles"
)

// ConfigStore is the persistence port for transcription settings. The domain
// service only knows how to validate and apply a configuration; filesystem,
// keychain, or test implementations can live outside this package.
type ConfigStore interface {
	Load(Config) (Config, error)
	Save(Config) error
}

type Config struct {
	Backend                   string `json:"backend"`
	Python                    string `json:"python"`
	Binary                    string `json:"binary"`
	Model                     string `json:"model"`
	Language                  string `json:"language"`
	QwenAligner               string `json:"qwenAligner,omitempty"`
	GroqModel                 string `json:"groqModel,omitempty"`
	CloudflareAccountID       string `json:"cloudflareAccountId,omitempty"`
	GroqAPIKey                string `json:"-"`
	CloudflareAPIToken        string `json:"-"`
	SiliconflowAPIKey         string `json:"-"`
	SiliconflowModel          string `json:"siliconflowModel,omitempty"`
	MAICookie                 string `json:"-"`
	SiliconflowSegmentSeconds int    `json:"siliconflowSegmentSeconds,omitempty"`
}

// Secrets are write-only HTTP fields; nil preserves a key and an empty value clears it.
type SettingsInput struct {
	Config
	GroqAPIKey         *string `json:"groqApiKey,omitempty"`
	CloudflareAPIToken *string `json:"cloudflareApiToken,omitempty"`
	SiliconflowAPIKey  *string `json:"siliconflowApiKey,omitempty"`
	MAICookie          *string `json:"maiCookie,omitempty"`
}

type storedConfig struct {
	Config
	GroqAPIKey         string `json:"groqApiKey,omitempty"`
	CloudflareAPIToken string `json:"cloudflareApiToken,omitempty"`
	SiliconflowAPIKey  string `json:"siliconflowApiKey,omitempty"`
	MAICookie          string `json:"maiCookie,omitempty"`
}

func (c Config) Cloud() bool {
	return c.Backend == "groq" || c.Backend == "cloudflare" || c.Backend == "siliconflow" || c.Backend == "mai"
}

type Model struct {
	Backend string `json:"backend"`
	Label   string `json:"label"`
	Path    string `json:"path"`
}

type Status struct {
	Models                    []Model  `json:"models"`
	Config                    Config   `json:"config"`
	Ready                     bool     `json:"ready"`
	Missing                   []string `json:"missing"`
	GroqKeyConfigured         bool     `json:"groqKeyConfigured"`
	CloudflareTokenConfigured bool     `json:"cloudflareTokenConfigured"`
	SiliconflowKeyConfigured  bool     `json:"siliconflowKeyConfigured"`
	MAICookieConfigured       bool     `json:"maiCookieConfigured"`
	// ModelDownload is the size of a catalog model fetched on first use.
	ModelDownload string `json:"modelDownload,omitempty"`
}

type Audio struct {
	LocalPath string // Completed download selected by app; never accepted from HTTP input.
	URL       string
	Duration  int
	Referer   string
	Backups   []string
	Platform  string // Trusted adapter metadata; never accepted from HTTP input.
}

type Engine interface {
	Status(Config) Status
	Transcribe(context.Context, Config, Audio, int, func(string)) (subtitles.Track, error)
}

// StreamingEngine is an optional local-only extension used by automatic
// subtitles. PCM chunks are short and already decoded; implementations must
// never send them to a remote provider.
type StreamingEngine interface {
	RecognizePCM(context.Context, Config, []byte, float64) (subtitles.Track, error)
}

// SavedSubtitles is the persistence capability needed by a transcription job.
// Keeping this port here prevents the job coordinator from depending on the
// concrete subtitle service implementation.
type SavedSubtitles interface {
	List(context.Context, string) ([]subtitles.SavedTrack, error)
	Save(context.Context, string, subtitles.Track, int64) (subtitles.SavedTrack, error)
}

type Job struct {
	ID          string `json:"id"`
	ContentID   string `json:"contentId"`
	State       string `json:"state"`
	Preview     bool   `json:"preview"`
	Error       string `json:"error,omitempty"`
	FailedStage string `json:"failedStage,omitempty"`
	ErrorCode   string `json:"errorCode,omitempty"`
	Backend     string `json:"backend,omitempty"`
	Automatic   bool   `json:"automatic,omitempty"`
	// Received and Total report a first-use model download.
	Received int64 `json:"received,omitempty"`
	Total    int64 `json:"total,omitempty"`
}
