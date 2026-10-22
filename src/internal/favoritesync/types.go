// Package favoritesync synchronizes explicitly selected Bilibili folders.
// Platform requests and persistent downloads remain separate adapters.
package favoritesync

import (
	"context"
	"errors"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"unicode"
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
	Enabled           bool              `json:"enabled"`
	AutoDownload      bool              `json:"autoDownload"`
	IntervalMinutes   int               `json:"intervalMinutes"`
	MaxPerRun         int               `json:"maxPerRun"`
	MaxHeight         int               `json:"maxHeight"`
	Download          downloads.Options `json:"download"`
	DownloadSubtitles bool              `json:"downloadSubtitles"`
	DirectoryName     string            `json:"directoryName"`
	TitleInclude      string            `json:"titleInclude,omitempty"`
	TitleExclude      string            `json:"titleExclude,omitempty"`
	AuthorInclude     string            `json:"authorInclude,omitempty"`
}
type Transfer struct {
	JobID    string `json:"jobId"`
	Attempts int    `json:"attempts"`
	Key      string `json:"key,omitempty"`
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
func DefaultPolicy() Policy {
	return Policy{IntervalMinutes: 60, MaxPerRun: 3, MaxHeight: 1080, Download: downloads.Options{Container: "mp4"}}
}

func NormalizeSource(source Source) Source {
	source.Policy = normalizedPolicy(source.Policy)
	if source.Policy.DirectoryName == "" && source.Title != "" && source.Title != "收藏夹 "+source.FolderID {
		source.Policy.DirectoryName = folderDirectoryName(source.Title, source.FolderID)
	}
	return source
}

func normalizeDownloadOptions(options downloads.Options) downloads.Options {
	_ = options.Normalize()
	return options
}

func emptyDownloadOptions(options downloads.Options) bool {
	return options.Mode == "" && options.Container == "" && !options.VideoOnly && options.AudioFormat == "" && options.VideoFormatID == "" && options.AudioFormatID == "" && len(options.SubtitleLanguages) == 0 && !options.AutomaticSubtitles && !options.DownloadDanmaku && !options.DownloadThumbnail && !options.DownloadInfoJSON && !options.Compress
}

func validPolicy(p Policy) bool {
	if !emptyDownloadOptions(p.Download) {
		options := p.Download
		if options.Normalize() != nil {
			return false
		}
	}
	p = normalizedPolicy(p)
	options := p.Download
	if options.Mode != "video" || options.VideoFormatID != "" || options.AudioFormatID != "" || p.DownloadSubtitles != (len(options.SubtitleLanguages) > 0) {
		return false
	}
	if p.DirectoryName != "" {
		if validateDirectoryName(p.DirectoryName) != nil {
			return false
		}
	}
	return p.IntervalMinutes >= 15 && p.IntervalMinutes <= 10080 && p.MaxPerRun >= 1 && p.MaxPerRun <= 5 &&
		(p.MaxHeight == 0 || p.MaxHeight == 480 || p.MaxHeight == 720 || p.MaxHeight == 1080 || p.MaxHeight == 1440 || p.MaxHeight == 2160) &&
		validFilter(p.TitleInclude) && validFilter(p.TitleExclude) && validFilter(p.AuthorInclude)
}

func folderDirectoryName(title, id string) string {
	if strings.HasPrefix(title, "收藏夹 "+id) {
		title = "收藏夹"
	}
	name := cleanDirectoryLabel(title, 48)
	if name == "" {
		name = "收藏夹"
	}
	if id != "" {
		name += " (" + id + ")"
	}
	if validateDirectoryName(name) == nil {
		return name
	}
	return "收藏夹 (" + id + ")"
}

func videoDirectoryName(item content.Item) string {
	name := cleanDirectoryLabel(item.Title, 48)
	if name == "" {
		name = "B站视频"
	}
	name += " [" + item.PlatformID + "]"
	if validateDirectoryName(name) == nil {
		return name
	}
	return "B站视频 [" + item.PlatformID + "]"
}

func cleanDirectoryLabel(value string, limit int) string {
	var builder strings.Builder
	space := false
	separator := false
	runes := 0
	for _, char := range strings.TrimSpace(value) {
		if unicode.IsControl(char) || strings.ContainsRune(`/\\:<>"|?*`, char) {
			if builder.Len() > 0 {
				separator = true
			}
			continue
		}
		if unicode.IsSpace(char) {
			space = builder.Len() > 0
			continue
		}
		if separator {
			builder.WriteByte('_')
			separator = false
		}
		if space {
			builder.WriteByte(' ')
			space = false
		}
		if runes >= limit {
			break
		}
		builder.WriteRune(char)
		runes++
	}
	name := strings.TrimSpace(builder.String())
	if strings.HasPrefix(name, ".") {
		name = "_" + strings.TrimLeft(name, ".")
	}
	if strings.HasSuffix(name, ".") {
		name = strings.TrimRight(name, ".") + "_"
	}
	reserved := strings.ToUpper(name)
	if slices.Contains([]string{"CON", "PRN", "AUX", "NUL", "COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9", "LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9"}, reserved) {
		name += "_"
	}
	return name
}

func validateDirectoryName(name string) error {
	if strings.Contains(name, "/") || utf8.RuneCountInString(name) > 80 {
		return downloads.ErrInvalid
	}
	_, err := downloads.NormalizeSubdirectory("B站收藏夹/" + name)
	return err
}

func validFolder(f Folder) bool {
	return folderID.MatchString(f.ID) && f.Title != "" && utf8.ValidString(f.Title) && utf8.RuneCountInString(f.Title) <= 200 && f.Count >= 0 && f.Count <= MaxItems
}
