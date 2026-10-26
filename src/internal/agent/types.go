// Package agent defines the restricted, application-neutral surface exposed to
// local agents. User-generated platform text is always marked untrusted.
package agent

import (
	"context"
	"time"

	"tangentry/internal/content"
	"tangentry/internal/discovery"
	"tangentry/internal/notes"
	"tangentry/internal/subscriptions"
	"tangentry/internal/subtitles"
)

type runIDContextKey struct{}

// WithRunID associates an Agent tool call with the durable local run that
// initiated it. The value is only used for audit correlation; it never grants
// additional scopes or permissions.
func WithRunID(ctx context.Context, runID string) context.Context {
	return context.WithValue(ctx, runIDContextKey{}, runID)
}

func RunID(ctx context.Context) string {
	if value, ok := ctx.Value(runIDContextKey{}).(string); ok {
		return value
	}
	return ""
}

const (
	ScopeContentRead    = "content_read"
	ScopeNetworkFetch   = "network_fetch"
	ScopeImageRead      = "image_read"
	ScopePrivateNotes   = "private_notes_read"
	ScopeDerived        = "derived_processing"
	ScopeAgentWrite     = "agent_write"
	RatingWorthWatching = "值得看"
	RatingCanWatch      = "可以看"
	RatingNotWorth      = "不值当"
)

type Capability struct {
	Platform      string            `json:"platform"`
	Features      map[string]string `json:"features"`
	LoginRequired map[string]bool   `json:"loginRequired,omitempty"`
	Reasons       map[string]string `json:"reasons,omitempty"`
}

type CapabilityMatrix struct {
	ProtocolVersion string       `json:"protocolVersion"`
	Platforms       []Capability `json:"platforms"`
}

type SearchResult struct {
	Query     string                            `json:"query"`
	Fetched   bool                              `json:"fetched"`
	Local     content.Page                      `json:"local"`
	Sources   map[string]discovery.SearchSource `json:"sources,omitempty"`
	Link      *discovery.Feed                   `json:"link,omitempty"`
	Untrusted bool                              `json:"untrusted"`
}

type ContentResult struct {
	Item      content.Item `json:"item"`
	Source    string       `json:"source"`
	Fetched   bool         `json:"fetched"`
	Untrusted bool         `json:"untrusted"`
}

type InboxResult struct {
	Page      subscriptions.InboxPage `json:"page"`
	Count     int                     `json:"count"`
	Untrusted bool                    `json:"untrusted"`
}

type SubtitleResult struct {
	Document  subtitles.Document `json:"document"`
	Source    string             `json:"source"`
	Untrusted bool               `json:"untrusted"`
}

type AgentImage struct {
	Data              []byte  `json:"data,omitempty"`
	MIMEType          string  `json:"mimeType"`
	Position          float64 `json:"positionSeconds"`
	Source            string  `json:"source"`
	ResourceURL       string  `json:"resourceUrl,omitempty"`
	ResourceExpiresAt int64   `json:"resourceExpiresAt,omitempty"`
	Width             int     `json:"width,omitempty"`
	Height            int     `json:"height,omitempty"`
	Approximate       bool    `json:"approximate"`
	Untrusted         bool    `json:"untrusted"`
}

type SocialResult struct {
	Status    string       `json:"status"`
	Reason    string       `json:"reason,omitempty"`
	Source    string       `json:"source,omitempty"`
	Count     int          `json:"count,omitempty"`
	Items     []SocialItem `json:"items,omitempty"`
	Untrusted bool         `json:"untrusted"`
}

type SocialItem struct {
	Text            string  `json:"text"`
	Author          string  `json:"author,omitempty"`
	Likes           int     `json:"likes,omitempty"`
	CreatedAt       int64   `json:"createdAt,omitempty"`
	ReplyToNumber   int     `json:"replyToNumber,omitempty"`
	ReplyToUser     string  `json:"replyToUser,omitempty"`
	PositionSeconds float64 `json:"positionSeconds"`
	Mode            string  `json:"mode"`
}

type ReviewEvent struct {
	ID             string `json:"id"`
	IdempotencyKey string `json:"idempotencyKey"`
	RunID          string `json:"runId,omitempty"`
	ContentID      string `json:"contentId"`
	Title          string `json:"title"`
	Author         string `json:"author"`
	Status         string `json:"status"`
	Rating         string `json:"rating,omitempty"`
	Reason         string `json:"reason,omitempty"`
	CreatedAt      int64  `json:"createdAt"`
}

type ReviewInput struct {
	IdempotencyKey string `json:"idempotencyKey"`
	RunID          string `json:"runId,omitempty"`
	ContentID      string `json:"contentId"`
	Title          string `json:"title"`
	Author         string `json:"author,omitempty"`
	Status         string `json:"status"`
	Rating         string `json:"rating,omitempty"`
	Reason         string `json:"reason,omitempty"`
}

type ReportEntry struct {
	ContentID string `json:"contentId"`
	Title     string `json:"title"`
	Author    string `json:"author,omitempty"`
	URL       string `json:"url"`
	ViewCount int64  `json:"viewCount,omitempty"`
	Rating    string `json:"rating"`
	Reason    string `json:"reason"`
	Takeaway  string `json:"takeaway,omitempty"`
	Status    string `json:"status"`
}

type ReportInput struct {
	Date              string        `json:"date"`
	Entries           []ReportEntry `json:"entries"`
	NoSubtitleSkipped int           `json:"noSubtitleSkipped"`
	Summary           string        `json:"summary,omitempty"`
}

type Report struct {
	ID        string `json:"id"`
	Date      string `json:"date"`
	Kind      string `json:"kind"`
	Markdown  string `json:"markdown"`
	CreatedAt int64  `json:"createdAt"`
}

type ReportQuery struct {
	Kind  string `json:"kind,omitempty"`
	Date  string `json:"date,omitempty"`
	Limit int    `json:"limit,omitempty"`
}

type DigestInput struct {
	Date    string        `json:"date"`
	Entries []ReportEntry `json:"entries"`
}

type NoteDraft struct {
	ID                 string                    `json:"id"`
	ContentID          string                    `json:"contentId"`
	Body               string                    `json:"body"`
	Position           *float64                  `json:"positionSeconds,omitempty"`
	SubtitleReferences []notes.SubtitleReference `json:"subtitleReferences,omitempty"`
	Tags               []string                  `json:"tags,omitempty"`
	State              string                    `json:"state"`
	CreatedAt          int64                     `json:"createdAt"`
	UpdatedAt          int64                     `json:"updatedAt"`
}

type NoteDraftInput struct {
	ContentID          string                    `json:"contentId"`
	Body               string                    `json:"body"`
	Position           *float64                  `json:"positionSeconds,omitempty"`
	SubtitleReferences []notes.SubtitleReference `json:"subtitleReferences,omitempty"`
	Tags               []string                  `json:"tags,omitempty"`
}

type AuditEvent struct {
	RunID           string `json:"runId,omitempty"`
	Action          string `json:"action"`
	NetworkFetch    bool   `json:"networkFetch"`
	ExternalProcess bool   `json:"externalProcessing"`
	Outcome         string `json:"outcome"`
	CreatedAt       int64  `json:"createdAt"`
}

type Run struct {
	ID         string `json:"id"`
	Client     string `json:"client"`
	Trigger    string `json:"trigger"`
	State      string `json:"state"`
	StartedAt  int64  `json:"startedAt"`
	FinishedAt int64  `json:"finishedAt,omitempty"`
	Error      string `json:"error,omitempty"`
}

type Activity struct {
	Runs  []Run        `json:"runs"`
	Audit []AuditEvent `json:"audit"`
}

type Provider interface {
	Capabilities(context.Context) CapabilityMatrix
	Search(context.Context, string, string, string, bool) (SearchResult, error)
	Content(context.Context, string, bool) (ContentResult, error)
	Inbox(context.Context, subscriptions.InboxQuery) (InboxResult, error)
	Subtitles(context.Context, string, string, bool) (SubtitleResult, error)
	Notes(context.Context, string, string) ([]notes.Note, error)
	Social(context.Context, string) (SocialResult, error)
	CaptureFrames(context.Context, string, []float64) ([]AgentImage, error)
}

type Repository interface {
	CreateRun(context.Context, Run) error
	FinishRun(context.Context, string, string, string) error
	RecordReview(context.Context, ReviewEvent) (ReviewEvent, bool, error)
	AppendReport(context.Context, Report) (Report, error)
	SaveNoteDraft(context.Context, NoteDraft) (NoteDraft, error)
	RecordAudit(context.Context, AuditEvent) error
	ListReviews(context.Context, string, int) ([]ReviewEvent, error)
}

// OperationRepository is an optional persistence extension for long-running
// derived work. Keeping it separate lets lightweight embedders and tests use
// the core Agent repository without having to persist asynchronous state.
type OperationRepository interface {
	CreateOperation(context.Context, Operation) error
	UpdateOperation(context.Context, Operation) error
	GetOperation(context.Context, string) (Operation, error)
	RecoverOperations(context.Context) error
}

type ActivityRepository interface {
	ListRuns(context.Context, int) ([]Run, error)
	ListAudit(context.Context, int) ([]AuditEvent, error)
}

type ReportRepository interface {
	ListReports(context.Context, ReportQuery) ([]Report, error)
}

type Operation struct {
	ID                 string    `json:"id"`
	RunID              string    `json:"runId,omitempty"`
	State              string    `json:"state"`
	Action             string    `json:"action,omitempty"`
	ContentID          string    `json:"contentId,omitempty"`
	Source             string    `json:"source"`
	CostClass          string    `json:"costClass"`
	EstimatedDuration  string    `json:"estimatedDuration"`
	ExternalProcessing bool      `json:"externalProcessing"`
	StartedAt          time.Time `json:"startedAt"`
	FinishedAt         time.Time `json:"finishedAt,omitempty"`
	Error              string    `json:"error,omitempty"`
}

type DerivedRequest struct {
	ContentID      string `json:"contentId"`
	AllowCloud     bool   `json:"allowCloud,omitempty"`
	Fetch          bool   `json:"fetch,omitempty"`
	TargetLanguage string `json:"targetLanguage,omitempty"`
}

type DerivedPlan struct {
	Source             string
	CostClass          string
	EstimatedDuration  string
	ExternalProcessing bool
	Run                func(context.Context) error
}

type DerivedProvider interface {
	PrepareDerived(context.Context, string, DerivedRequest) (DerivedPlan, error)
}

type IntegrationSettings struct {
	Enabled bool     `json:"enabled"`
	Scopes  []string `json:"scopes"`
}

type IntegrationStatus struct {
	Enabled         bool     `json:"enabled"`
	Running         bool     `json:"running"`
	ProtocolVersion string   `json:"protocolVersion"`
	Endpoint        string   `json:"endpoint,omitempty"`
	ConnectionFile  string   `json:"connectionFile,omitempty"`
	Scopes          []string `json:"scopes"`
}
