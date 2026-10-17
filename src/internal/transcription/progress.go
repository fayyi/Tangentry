package transcription

import "context"

type progressKey struct{}

// WithProgress lets an engine or installer report the bytes of a first-use
// download without widening the Engine interface.
func WithProgress(ctx context.Context, report func(received, total int64)) context.Context {
	return context.WithValue(ctx, progressKey{}, report)
}

// ReportProgress forwards download progress to the job that owns ctx, if any.
func ReportProgress(ctx context.Context, received, total int64) {
	if report, ok := ctx.Value(progressKey{}).(func(int64, int64)); ok {
		report(received, total)
	}
}
