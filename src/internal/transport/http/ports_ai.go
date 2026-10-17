package httptransport

import (
	"context"

	"tangentry/internal/ai"
	"tangentry/internal/transcription"
)

type AIService interface {
	Status() ai.Status
	Configure(ai.ConfigureInput) (ai.Status, error)
	ListModels(context.Context, ai.ConfigureInput) (ai.ModelsResult, error)
	Check(context.Context, ai.CheckInput) (ai.CheckResult, error)
	Inspect(context.Context, ai.Input) (ai.Inspection, error)
	Translate(context.Context, ai.Input) (ai.Result, error)
	Cached(context.Context, string) (ai.Result, error)
	SummaryInspect(context.Context, ai.SummaryInput) (ai.SummaryInspection, error)
	SummaryGenerate(context.Context, ai.SummaryInput) (ai.SummaryResult, error)
	SummaryCached(context.Context, string) (ai.SummaryResult, error)
	Chat(context.Context, ai.ChatRequest) (ai.ChatResult, error)
	ChatStream(context.Context, ai.ChatRequest, func(string) error) (ai.ChatResult, error)
	ChatHistory(context.Context, string) (ai.ChatResult, error)
}

type TranscriptionService interface {
	Status() transcription.Status
	ConfigureSettings(transcription.SettingsInput) (transcription.Status, error)
	StartWithUploadConsent(context.Context, string, bool, bool, string) (transcription.Job, error)
	Current(string) *transcription.Job
	Get(string) (transcription.Job, error)
	Cancel(string) error
}

type ModelDownloadService interface {
	Status() ([]transcription.ModelOption, *transcription.ModelDownload, bool)
	Start(string) (transcription.ModelDownload, error)
	Cancel(string) error
}
