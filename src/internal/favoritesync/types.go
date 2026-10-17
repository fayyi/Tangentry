// Package favoritesync synchronizes explicitly selected Bilibili folders.
// Platform requests and persistent downloads remain separate adapters.
package favoritesync

import (
	"context"
	"errors"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"

	"tangentry/internal/content"
	"tangentry/internal/downloads"
)

var (
	ErrInvalid    = errors.New("invalid favorite sync policy")
	ErrNotFound   = errors.New("favorite sync source not found")
	ErrBusy       = errors.New("favorite synchronization busy")
	ErrIncomplete = errors.New("incomplete favorite folder")
)

const MaxItems = 4000

type Folder struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Count int    `json:"count"`
}
type Page struct {
	Folder   Folder
	Items    []content.Item
	HasMore  bool
	Excluded int
	Entries  int
}
type Remote interface {
	Folders(context.Context) ([]Folder, error)
	Page(context.Context, string, int) (Page, error)
}
type Downloader interface {
	Start(context.Context, downloads.Request) (downloads.Job, error)
	List(context.Context) ([]downloads.Job, error)
	CompletedPath(context.Context, string) (string, error)
}
type Policy struct {
	Enabled         bool   `json:"enabled"`
	AutoDownload    bool   `json:"autoDownload"`
	IntervalMinutes int    `json:"intervalMinutes"`
	MaxPerRun       int    `json:"maxPerRun"`
	MaxHeight       int    `json:"maxHeight"`
	TitleInclude    string `json:"titleInclude,omitempty"`
	TitleExclude    string `json:"titleExclude,omitempty"`
	AuthorInclude   string `json:"authorInclude,omitempty"`
}
type Transfer struct {
	JobID    string `json:"jobId"`
	Attempts int    `json:"attempts"`
}
type Source struct {
	FolderID        string              `json:"folderId"`
	Title           string              `json:"title"`
	Policy          Policy              `json:"policy"`
	Items           []content.Item      `json:"items"`
	Pending         []string            `json:"pending"`
	Transfers       map[string]Transfer `json:"transfers"`
	CreatedAt       int64               `json:"createdAt"`
	LastAttemptAt   int64               `json:"lastAttemptAt"`
	LastSuccessAt   int64               `json:"lastSuccessAt"`
	RestrictedUntil int64               `json:"restrictedUntil,omitempty"`
	Excluded        int                 `json:"excluded"`
	Message         string              `json:"message"`
	Active          bool                `json:"active"`
}
type Input struct {
	Query  string `json:"query"`
	Policy Policy `json:"policy"`
}
type Repository interface {
	FavoriteRestriction(context.Context) (int64, error)
	ExtendFavoriteRestriction(context.Context, int64) error
	ListFavoriteSources(context.Context) ([]Source, error)
	GetFavoriteSource(context.Context, string) (Source, error)
	PutFavoriteSource(context.Context, Source) error
	DeleteFavoriteSource(context.Context, string) error
}

var folderID = regexp.MustCompile(`^[1-9][0-9]{0,17}$`)

func ParseFolder(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if folderID.MatchString(raw) {
		return raw, nil
	}
	if len(raw) > 2048 {
		return "", ErrInvalid
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || !strings.EqualFold(u.Host, "space.bilibili.com") || u.User != nil || u.Fragment != "" {
		return "", ErrInvalid
	}
	id := u.Query().Get("fid")
	if !folderID.MatchString(id) {
		return "", ErrInvalid
	}
	return id, nil
}
func DefaultPolicy() Policy { return Policy{IntervalMinutes: 60, MaxPerRun: 3, MaxHeight: 1080} }
func validPolicy(p Policy) bool {
	return p.IntervalMinutes >= 15 && p.IntervalMinutes <= 10080 && p.MaxPerRun >= 1 && p.MaxPerRun <= 5 && (p.MaxHeight == 720 || p.MaxHeight == 1080) &&
		validFilter(p.TitleInclude) && validFilter(p.TitleExclude) && validFilter(p.AuthorInclude)
}
func validFolder(f Folder) bool {
	return folderID.MatchString(f.ID) && f.Title != "" && utf8.ValidString(f.Title) && utf8.RuneCountInString(f.Title) <= 200 && f.Count >= 0 && f.Count <= MaxItems
}
