package httptransport

import (
	"context"

	"tangentry/internal/content"
)

// The handler depends only on the platform operations it exposes, so the
// composition root can supply any implementation and tests can supply fakes.

// LoginFlow is a QR or device login the user completes on another screen.
type LoginFlow[State any] interface {
	Start(context.Context) (State, error)
	Poll(ctx context.Context, id string) (State, error)
}

// BrowserLoginFlow also supports logins completed in the system browser.
type BrowserLoginFlow[State any] interface {
	LoginFlow[State]
	StartBrowser(context.Context) (State, error)
	CompleteBrowser(ctx context.Context, id, callback string) (State, error)
	Cancel(id string)
}

type BilibiliBrowser interface {
	Browse(ctx context.Context, kind string, page int, cursor string, area int) (BrowsePage, error)
	Profile(context.Context) (Profile, error)
}

type ImageCache interface {
	Read(ctx context.Context, raw string) (ImageData, error)
}

// These are transport contracts, not platform adapter DTOs. Adapters are
// translated into them at the composition root so HTTP stays platform-neutral.
type BrowsePage struct {
	Items       []content.Item `json:"items"`
	Rooms       []LiveRoom     `json:"rooms,omitempty"`
	Next        string         `json:"next,omitempty"`
	HasMore     bool           `json:"hasMore"`
	Page        int            `json:"page"`
	SourceTitle string         `json:"sourceTitle"`
}
type LiveRoom struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Author    string `json:"author"`
	Thumbnail string `json:"thumbnailUrl"`
	URL       string `json:"url"`
}
type Profile struct {
	LoggedIn bool   `json:"loggedIn"`
	ID       string `json:"id,omitempty"`
	Name     string `json:"name,omitempty"`
	Avatar   string `json:"avatar,omitempty"`
	Level    int    `json:"level,omitempty"`
}
type ImageData struct {
	Bytes []byte
	Type  string
}

type QRLoginState struct {
	ID        string `json:"id"`
	Image     string `json:"image,omitempty"`
	Status    string `json:"status"`
	ExpiresAt int64  `json:"expiresAt"`
}
type LinuxdoState struct {
	ID        string `json:"id"`
	URL       string `json:"url,omitempty"`
	Code      string `json:"code,omitempty"`
	Status    string `json:"status"`
	ExpiresAt int64  `json:"expiresAt"`
	Interval  int    `json:"interval"`
}
type LinuxdoSession struct {
	LoggedIn bool   `json:"loggedIn"`
	Username string `json:"username,omitempty"`
}
