package transcription

import "context"

// SourceResolver is the only application capability needed to locate media
// for a transcription job. Platform and persistence policy lives outside the
// transcription domain.
type SourceResolver func(context.Context, string) (Audio, error)

func (r SourceResolver) Resolve(ctx context.Context, id string) (Audio, error) {
	return r(ctx, id)
}
