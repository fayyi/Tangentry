package httptransport

import (
	"net/http"

	"tangentry/internal/credentials"
)

// CredentialStore exposes only the account operations used by HTTP. The
// transport should not require the concrete on-disk credential store.
type CredentialStore interface {
	States() []credentials.State
	Save(platform, format, raw string) error
	Clear(platform string) error
}

// HTTPClient is the only capability needed by the article image proxy. Keep
// the transport independent from a concrete *http.Client so composition roots
// can provide a policy-bound client or a focused test double.
type HTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}
