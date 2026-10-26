package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"tangentry/internal/content"
	"tangentry/internal/notes"
	"tangentry/internal/subscriptions"
)

var ErrInvalid = errors.New("invalid agent request")
var ErrUnsupported = errors.New("agent capability unsupported")
var ErrConflict = errors.New("agent review already recorded")

var idempotencyPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)
var reportDatePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

type Service struct {
	provider   Provider
	repo       Repository
	opMu       sync.Mutex
	operations map[string]Operation
	opCancel   map[string]context.CancelFunc
	opWG       sync.WaitGroup
	closed     bool
}

func New(provider Provider, repository Repository) *Service {
	service := &Service{provider: provider, repo: repository, operations: make(map[string]Operation), opCancel: make(map[string]context.CancelFunc)}
	if persisted, ok := repository.(OperationRepository); ok {
		// An operation cannot safely resume across process boundaries because its
		// provider context and cancellation handle are process-local. Mark it as
		// interrupted before accepting new requests; the durable record remains
		// queryable for audit and user-facing status.
		_ = persisted.RecoverOperations(context.Background())
	}
	return service
}

func NewID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}

func (s *Service) Capabilities(ctx context.Context) CapabilityMatrix {
	return s.provider.Capabilities(ctx)
}

func (s *Service) Search(ctx context.Context, query, platform, cursor string, fetch bool) (SearchResult, error) {
	if strings.TrimSpace(query) == "" || utf8.RuneCountInString(query) > 200 || len(platform) > 40 || len(cursor) > 512 {
		return SearchResult{}, ErrInvalid
	}
	result, err := s.provider.Search(ctx, query, platform, cursor, fetch)
	s.RecordAudit(ctx, "search_content", fetch, false, outcome(err))
	return result, err
}

func (s *Service) Content(ctx context.Context, id string, fetch bool) (ContentResult, error) {
	if !content.ValidID(id) {
		return ContentResult{}, ErrInvalid
	}
	result, err := s.provider.Content(ctx, id, fetch)
	s.RecordAudit(ctx, "get_content", fetch, false, outcome(err))
	return result, err
}

func (s *Service) Inbox(ctx context.Context, query subscriptions.InboxQuery) (InboxResult, error) {
	result, err := s.provider.Inbox(ctx, query)
	s.RecordAudit(ctx, "list_inbox", false, false, outcome(err))
	return result, err
}

func (s *Service) Subtitles(ctx context.Context, id, language string, fetch bool) (SubtitleResult, error) {
	if !content.ValidID(id) || len(language) > 40 {
		return SubtitleResult{}, ErrInvalid
	}
	result, err := s.provider.Subtitles(ctx, id, language, fetch)
	s.RecordAudit(ctx, "get_subtitles", fetch, false, outcome(err))
	return result, err
}

func (s *Service) Notes(ctx context.Context, query, contentID string) ([]notes.Note, error) {
	if len(query) > 200 || contentID != "" && !content.ValidID(contentID) {
		return nil, ErrInvalid
	}
	result, err := s.provider.Notes(ctx, query, contentID)
	s.RecordAudit(ctx, "search_notes", false, false, outcome(err))
	return result, err
}

func (s *Service) Social(ctx context.Context, id string) (SocialResult, error) {
	if !content.ValidID(id) {
		return SocialResult{}, ErrInvalid
	}
	provider := s.provider
	result, err := provider.Social(ctx, id)
	s.RecordAudit(ctx, "get_social_context", false, false, outcome(err))
	return result, err
}

// Derived is the explicit boundary for long-running AI work. The first
// gateway release advertises the stable operation shape even when a platform
// or configured backend cannot service a particular request; callers receive
// unsupported rather than an accidental synchronous fallback.
func (s *Service) StartDerived(ctx context.Context, action string, input DerivedRequest) (Operation, error) {
	if action != "summarize_content" && action != "translate_content" && action != "transcribe_content" {
		return Operation{}, ErrInvalid
	}
	if !content.ValidID(input.ContentID) || action == "translate_content" && (input.TargetLanguage == "" || utf8.RuneCountInString(input.TargetLanguage) > 80) {
		return Operation{}, ErrInvalid
	}
	s.opMu.Lock()
	if s.closed {
		s.opMu.Unlock()
		return Operation{}, context.Canceled
	}
	s.opMu.Unlock()
	provider, ok := s.provider.(DerivedProvider)
	if !ok {
		return Operation{Action: action, ContentID: input.ContentID, State: "unsupported", Source: "none", CostClass: "none", EstimatedDuration: "unknown", Error: "派生处理适配器不可用。"}, nil
	}
	plan, err := provider.PrepareDerived(ctx, action, input)
	if errors.Is(err, ErrUnsupported) {
		return Operation{Action: action, ContentID: input.ContentID, State: "unsupported", Source: "none", CostClass: "none", EstimatedDuration: "unknown", ExternalProcessing: false, Error: "当前来源、字幕或处理后端不支持该操作，或云端处理尚未明确授权。"}, nil
	}
	if err != nil {
		return Operation{}, err
	}
	if plan.Run == nil {
		return Operation{}, ErrUnsupported
	}
	id, err := NewID()
	if err != nil {
		return Operation{}, err
	}
	now := time.Now()
	runID := RunID(ctx)
	operation := Operation{ID: id, RunID: runID, Action: action, ContentID: input.ContentID, State: "queued", Source: plan.Source, CostClass: plan.CostClass, EstimatedDuration: plan.EstimatedDuration, ExternalProcessing: plan.ExternalProcessing, StartedAt: now}
	workContext := WithRunID(context.Background(), runID)
	work, cancel := context.WithCancel(workContext)
	s.opMu.Lock()
	if s.closed {
		s.opMu.Unlock()
		cancel()
		return Operation{}, context.Canceled
	}
	s.operations[id] = operation
	s.opCancel[id] = cancel
	s.opWG.Add(1)
	s.opMu.Unlock()
	if persisted, ok := s.repo.(OperationRepository); ok {
		if err := persisted.CreateOperation(ctx, operation); err != nil {
			cancel()
			s.opMu.Lock()
			delete(s.operations, id)
			delete(s.opCancel, id)
			s.opMu.Unlock()
			s.opWG.Done()
			return Operation{}, err
		}
	}
	s.RecordAudit(ctx, action, input.Fetch, plan.ExternalProcessing, "queued")
	go s.runDerived(work, id, plan.Run)
	return operation, nil
}

func (s *Service) runDerived(ctx context.Context, id string, run func(context.Context) error) {
	defer s.opWG.Done()
	s.opMu.Lock()
	operation, ok := s.operations[id]
	if !ok || operation.State == "cancelled" {
		s.opMu.Unlock()
		return
	}
	operation.State = "running"
	s.operations[id] = operation
	s.opMu.Unlock()
	s.persistOperation(operation)
	var err error
	if ctx.Err() != nil {
		err = ctx.Err()
	} else {
		err = run(ctx)
	}
	s.opMu.Lock()
	operation = s.operations[id]
	if ctx.Err() != nil {
		operation.State = "cancelled"
		operation.Error = "操作已取消。"
	} else if errors.Is(err, ErrUnsupported) {
		operation.State = "unsupported"
		operation.Error = "当前能力不可用。"
	} else if err != nil {
		operation.State = "failed"
		operation.Error = "操作失败；详细错误未保存。"
	} else {
		operation.State = "completed"
	}
	operation.FinishedAt = time.Now()
	s.operations[id] = operation
	delete(s.opCancel, id)
	s.opMu.Unlock()
	s.persistOperation(operation)
	s.RecordAudit(WithRunID(context.Background(), operation.RunID), operation.Action, false, operation.ExternalProcessing, operation.State)
}

func (s *Service) Operation(ctx context.Context, id string) (Operation, error) {
	s.opMu.Lock()
	operation, ok := s.operations[id]
	s.opMu.Unlock()
	if ok {
		return operation, nil
	}
	if persisted, persistedOK := s.repo.(OperationRepository); persistedOK {
		return persisted.GetOperation(ctx, id)
	}
	return Operation{}, ErrInvalid
}

func (s *Service) CancelOperation(_ context.Context, id string) error {
	s.opMu.Lock()
	operation, ok := s.operations[id]
	if !ok {
		s.opMu.Unlock()
		return ErrInvalid
	}
	if operation.State == "running" || operation.State == "queued" {
		if cancel := s.opCancel[id]; cancel != nil {
			cancel()
		}
		if operation.State == "queued" {
			operation.State = "cancelled"
			operation.FinishedAt = time.Now()
			operation.Error = "操作已取消。"
			s.operations[id] = operation
			delete(s.opCancel, id)
			s.opMu.Unlock()
			s.persistOperation(operation)
			s.RecordAudit(WithRunID(context.Background(), operation.RunID), operation.Action, false, operation.ExternalProcessing, operation.State)
			return nil
		}
	}
	s.opMu.Unlock()
	return nil
}

func (s *Service) persistOperation(operation Operation) {
	if persisted, ok := s.repo.(OperationRepository); ok {
		_ = persisted.UpdateOperation(context.Background(), operation)
	}
}

// Close cancels in-flight derived work and waits for final durable updates
// before the application's SQLite store is closed.
func (s *Service) Close() {
	if s == nil {
		return
	}
	s.opMu.Lock()
	if s.closed {
		s.opMu.Unlock()
		return
	}
	s.closed = true
	for _, cancel := range s.opCancel {
		cancel()
	}
	s.opMu.Unlock()
	s.opWG.Wait()
}

func (s *Service) CaptureFrames(ctx context.Context, id string, positions []float64) ([]AgentImage, error) {
	if !content.ValidID(id) || len(positions) == 0 || len(positions) > 6 {
		return nil, ErrInvalid
	}
	for _, position := range positions {
		if math.IsNaN(position) || math.IsInf(position, 0) || position < 0 || position > 604800 {
			return nil, ErrInvalid
		}
	}
	images, err := s.provider.CaptureFrames(ctx, id, positions)
	s.RecordAudit(ctx, "capture_frames", false, false, outcome(err))
	return images, err
}

func (s *Service) RecordReview(ctx context.Context, input ReviewInput) (ReviewEvent, bool, error) {
	input.ContentID = strings.TrimSpace(input.ContentID)
	input.Title = strings.TrimSpace(input.Title)
	input.Author = strings.TrimSpace(input.Author)
	input.Status = strings.TrimSpace(input.Status)
	input.Rating = strings.TrimSpace(input.Rating)
	input.Reason = strings.TrimSpace(input.Reason)
	if input.RunID == "" {
		input.RunID = RunID(ctx)
	}
	if !idempotencyPattern.MatchString(input.IdempotencyKey) || !content.ValidID(input.ContentID) || input.Title == "" || utf8.RuneCountInString(input.Title) > 500 || utf8.RuneCountInString(input.Author) > 200 || utf8.RuneCountInString(input.Reason) > 2000 || input.RunID != "" && !idempotencyPattern.MatchString(input.RunID) {
		return ReviewEvent{}, false, ErrInvalid
	}
	switch input.Status {
	case "selected", "skipped", "commented":
	default:
		return ReviewEvent{}, false, ErrInvalid
	}
	if input.Rating != "" && input.Rating != RatingWorthWatching && input.Rating != RatingCanWatch && input.Rating != RatingNotWorth {
		return ReviewEvent{}, false, ErrInvalid
	}
	id, err := NewID()
	if err != nil {
		return ReviewEvent{}, false, err
	}
	event := ReviewEvent{ID: id, IdempotencyKey: input.IdempotencyKey, RunID: input.RunID, ContentID: input.ContentID, Title: input.Title, Author: input.Author, Status: input.Status, Rating: input.Rating, Reason: input.Reason, CreatedAt: time.Now().Unix()}
	result, duplicate, err := s.repo.RecordReview(ctx, event)
	s.RecordAudit(ctx, "record_review", false, false, outcome(err))
	return result, duplicate, err
}

func (s *Service) Reviews(ctx context.Context, contentID string, limit int) ([]ReviewEvent, error) {
	if contentID != "" && !content.ValidID(contentID) || limit < 1 || limit > 100 {
		return nil, ErrInvalid
	}
	return s.repo.ListReviews(ctx, contentID, limit)
}

func (s *Service) SaveReport(ctx context.Context, input ReportInput) (Report, error) {
	if !validDate(input.Date) || len(input.Entries) > 100 || input.NoSubtitleSkipped < 0 || input.NoSubtitleSkipped > 10000 || utf8.RuneCountInString(input.Summary) > 2000 {
		return Report{}, ErrInvalid
	}
	for _, entry := range input.Entries {
		if err := validateReportEntry(entry); err != nil {
			return Report{}, err
		}
	}
	markdown := renderReport(input)
	return s.appendReport(ctx, input.Date, "daily", markdown)
}

func (s *Service) SaveDigest(ctx context.Context, input DigestInput) (Report, error) {
	if !validDate(input.Date) || len(input.Entries) > 1000 {
		return Report{}, ErrInvalid
	}
	for _, entry := range input.Entries {
		if err := validateDigestEntry(entry); err != nil {
			return Report{}, err
		}
	}
	markdown := renderDigest(input)
	return s.appendReport(ctx, input.Date, "digest", markdown)
}

func (s *Service) Reports(ctx context.Context, query ReportQuery) ([]Report, error) {
	if query.Kind != "" && query.Kind != "daily" && query.Kind != "digest" || query.Date != "" && !validDate(query.Date) || query.Limit < 0 || query.Limit > 100 {
		return nil, ErrInvalid
	}
	if query.Limit == 0 {
		query.Limit = 20
	}
	repository, ok := s.repo.(ReportRepository)
	if !ok {
		return []Report{}, nil
	}
	return repository.ListReports(ctx, query)
}

func (s *Service) appendReport(ctx context.Context, date, kind, markdown string) (Report, error) {
	id, err := NewID()
	if err != nil {
		return Report{}, err
	}
	result, err := s.repo.AppendReport(ctx, Report{ID: id, Date: date, Kind: kind, Markdown: markdown, CreatedAt: time.Now().Unix()})
	s.RecordAudit(ctx, "save_"+kind, false, false, outcome(err))
	return result, err
}

func (s *Service) SaveNoteDraft(ctx context.Context, input NoteDraftInput) (NoteDraft, error) {
	input.ContentID = strings.TrimSpace(input.ContentID)
	input.Body = strings.TrimSpace(input.Body)
	if !content.ValidID(input.ContentID) || input.Body == "" || utf8.RuneCountInString(input.Body) > 8000 || input.Position != nil && (*input.Position < 0 || *input.Position > 604800) || len(input.Tags) > 32 {
		return NoteDraft{}, ErrInvalid
	}
	if err := notes.ValidateSubtitleReferences(input.SubtitleReferences); err != nil {
		return NoteDraft{}, ErrInvalid
	}
	for _, tag := range input.Tags {
		if tag == "" || utf8.RuneCountInString(tag) > 80 {
			return NoteDraft{}, ErrInvalid
		}
	}
	now := time.Now().Unix()
	id, err := NewID()
	if err != nil {
		return NoteDraft{}, err
	}
	draft := NoteDraft{ID: id, ContentID: input.ContentID, Body: input.Body, Position: input.Position, SubtitleReferences: input.SubtitleReferences, Tags: input.Tags, State: "draft", CreatedAt: now, UpdatedAt: now}
	result, err := s.repo.SaveNoteDraft(ctx, draft)
	s.RecordAudit(ctx, "save_note_draft", false, false, outcome(err))
	return result, err
}

func (s *Service) RecordAudit(ctx context.Context, action string, networkFetch, externalProcessing bool, outcome string) {
	_ = s.repo.RecordAudit(ctx, AuditEvent{RunID: RunID(ctx), Action: action, NetworkFetch: networkFetch, ExternalProcess: externalProcessing, Outcome: outcome, CreatedAt: time.Now().Unix()})
}

func (s *Service) CreateRun(ctx context.Context, client, trigger string) (Run, error) {
	id, err := NewID()
	if err != nil {
		return Run{}, err
	}
	run := Run{ID: id, Client: safeLabel(client, 120), Trigger: safeLabel(trigger, 80), State: "running", StartedAt: time.Now().Unix()}
	return run, s.repo.CreateRun(ctx, run)
}

func (s *Service) FinishRun(ctx context.Context, id, state, errText string) error {
	if !idempotencyPattern.MatchString(id) || state != "completed" && state != "failed" && state != "cancelled" || utf8.RuneCountInString(errText) > 1000 {
		return ErrInvalid
	}
	if state == "failed" && errText != "" {
		errText = "运行失败；出于隐私保护，详细错误未保存。"
	} else {
		errText = ""
	}
	return s.repo.FinishRun(ctx, id, state, errText)
}

func (s *Service) Activity(ctx context.Context, limit int) (Activity, error) {
	if limit < 1 || limit > 100 {
		return Activity{}, ErrInvalid
	}
	repository, ok := s.repo.(ActivityRepository)
	if !ok {
		return Activity{Runs: []Run{}, Audit: []AuditEvent{}}, nil
	}
	runs, err := repository.ListRuns(ctx, limit)
	if err != nil {
		return Activity{}, err
	}
	audit, err := repository.ListAudit(ctx, limit)
	if err != nil {
		return Activity{}, err
	}
	return Activity{Runs: runs, Audit: audit}, nil
}

func validDate(value string) bool {
	if !reportDatePattern.MatchString(value) {
		return false
	}
	_, err := time.Parse("2006-01-02", value)
	return err == nil
}

func validateReportEntry(entry ReportEntry) error {
	if !content.ValidID(entry.ContentID) || strings.TrimSpace(entry.Title) == "" || utf8.RuneCountInString(entry.Title) > 500 || utf8.RuneCountInString(entry.Author) > 200 || utf8.RuneCountInString(entry.Reason) > 2000 || utf8.RuneCountInString(entry.Takeaway) > 1000 {
		return ErrInvalid
	}
	if entry.Status != "selected" && entry.Status != "skipped" && entry.Status != "commented" || entry.ViewCount < 0 {
		return ErrInvalid
	}
	if entry.Status == "selected" && entry.Rating != RatingWorthWatching && entry.Rating != RatingCanWatch && entry.Rating != RatingNotWorth || entry.Status != "selected" && entry.Rating != "" {
		return ErrInvalid
	}
	if entry.URL != "" {
		u, err := url.Parse(entry.URL)
		if err != nil || u.Scheme != "https" || u.Host == "" {
			return ErrInvalid
		}
	}
	return nil
}

func validateDigestEntry(entry ReportEntry) error {
	if !content.ValidID(entry.ContentID) || strings.TrimSpace(entry.Title) == "" || utf8.RuneCountInString(entry.Title) > 500 || utf8.RuneCountInString(entry.Author) > 200 || utf8.RuneCountInString(entry.Reason) > 2000 || utf8.RuneCountInString(entry.Takeaway) > 1000 || entry.ViewCount < 0 {
		return ErrInvalid
	}
	if entry.Status != "" && entry.Status != "selected" && entry.Status != "skipped" && entry.Status != "commented" {
		return ErrInvalid
	}
	if entry.Rating != "" && entry.Rating != RatingWorthWatching && entry.Rating != RatingCanWatch && entry.Rating != RatingNotWorth {
		return ErrInvalid
	}
	if entry.URL != "" {
		u, err := url.Parse(entry.URL)
		if err != nil || u.Scheme != "https" || u.Host == "" {
			return ErrInvalid
		}
	}
	return nil
}

func renderReport(input ReportInput) string {
	var out strings.Builder
	fmt.Fprintf(&out, "# Tangentry Agent 日报 · %s\n\n", input.Date)
	for _, entry := range input.Entries {
		if entry.Status != "selected" {
			continue
		}
		fmt.Fprintf(&out, "### %s\n\n", markdownInline(entry.ContentID))
		fmt.Fprintf(&out, "**标题**：%s\n\n**UP / 作者**：%s\n\n**链接**：%s\n\n**播放量**：%d\n\n**档位**：%s\n\n**为什么值得**：%s\n\n**一句话**：%s\n\n", markdownInline(entry.Title), markdownInline(entry.Author), markdownInline(entry.URL), entry.ViewCount, markdownInline(entry.Rating), markdownInline(entry.Reason), markdownInline(entry.Takeaway))
	}
	for _, entry := range input.Entries {
		if entry.Status == "skipped" {
			fmt.Fprintf(&out, "- 跳过：%s（%s）\n", markdownInline(entry.Title), markdownInline(entry.Reason))
		}
	}
	fmt.Fprintf(&out, "\n因无字幕跳过 %d 条\n", input.NoSubtitleSkipped)
	if input.Summary != "" {
		fmt.Fprintf(&out, "\n%s\n", markdownParagraph(input.Summary))
	}
	return out.String()
}

func renderDigest(input DigestInput) string {
	var out strings.Builder
	for _, entry := range input.Entries {
		fmt.Fprintf(&out, "%s · %s · %s · %s · %s · %s", input.Date, markdownInline(entry.ContentID), markdownInline(entry.Title), markdownInline(entry.Author), markdownInline(entry.Reason), markdownInline(entry.URL))
		if entry.Status == "skipped" {
			fmt.Fprintf(&out, " · 跳过")
		}
		out.WriteByte('\n')
	}
	return out.String()
}

func markdownInline(value string) string {
	value = strings.NewReplacer("\\", "\\\\", "`", "\\`", "*", "\\*", "_", "\\_", "[", "\\[", "]", "\\]", "<", "&lt;", ">", "&gt;", "|", "\\|", "\r", " ", "\n", " ").Replace(value)
	return strings.TrimSpace(value)
}

func markdownParagraph(value string) string {
	lines := strings.Fields(strings.ReplaceAll(strings.ReplaceAll(value, "\r", " "), "\n", " "))
	return markdownInline(strings.Join(lines, " "))
}

func safeLabel(value string, limit int) string {
	value = strings.TrimSpace(value)
	if utf8.RuneCountInString(value) > limit {
		return string([]rune(value)[:limit])
	}
	return value
}

func outcome(err error) string {
	if err != nil {
		return "failed"
	}
	return "ok"
}

func ValidateProvider(provider Provider) error {
	if provider == nil {
		return fmt.Errorf("agent provider unavailable")
	}
	return nil
}
