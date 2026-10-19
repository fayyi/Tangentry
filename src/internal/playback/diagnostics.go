package playback

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"mime"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"tangentry/internal/discovery"
)

type DiagnosticOutcome string

const (
	OutcomeOK         DiagnosticOutcome = "ok"
	OutcomeRestricted DiagnosticOutcome = "restricted"
	OutcomeTimeout    DiagnosticOutcome = "timeout"
	OutcomeTooLarge   DiagnosticOutcome = "too_large"
	OutcomeNoURL      DiagnosticOutcome = "no_url"
	OutcomeHTTPError  DiagnosticOutcome = "http_error"
	OutcomeException  DiagnosticOutcome = "exception"
)

// DiagnosticStage is a credential-free projection of one Douyin playback phase.
// Optional values remain explicit nulls in JSON to make missing evidence clear.
type DiagnosticStage struct {
	Stage                         string            `json:"stage"`
	Outcome                       DiagnosticOutcome `json:"outcome"`
	HTTPStatus                    *int              `json:"http_status"`
	ContentType                   *string           `json:"content_type"`
	PageURL                       *string           `json:"page_url"`
	BlockedNavigationURL          *string           `json:"blocked_navigation_url"`
	PageReadyState                *string           `json:"page_ready_state"`
	IsDouyinContext               *bool             `json:"is_douyin_context"`
	YTDLPVersion                  *string           `json:"ytdlp_version"`
	YTDLPExitCode                 *int              `json:"ytdlp_exit_code"`
	YTDLPStderrFirstErrorRedacted *string           `json:"ytdlp_stderr_first_error_redacted"`
	Reason                        *string           `json:"reason"`
	APICode                       *string           `json:"api_code"`
	BusinessStatus                *int              `json:"business_status"`
	CandidateCount                *int              `json:"candidate_count"`
	AcceptedCount                 *int              `json:"accepted_count"`
	VideoElementCount             *int              `json:"video_element_count"`
	Attempted                     *int              `json:"attempted"`
	Refreshed                     *bool             `json:"refreshed"`
	CookieConfigured              *bool             `json:"cookie_configured"`
	FrontendMediaCode             *int              `json:"frontend_media_code"`
	FrontendMessage               *string           `json:"frontend_message"`
	BytesRead                     *int64            `json:"bytes_read"`
	ResponseBytes                 *int64            `json:"response_bytes"`
	DetailURLCount                *int              `json:"detail_url_count"`
	VideoSourceCount              *int              `json:"video_source_count"`
	Protocol                      *string           `json:"protocol"`
	Extension                     *string           `json:"extension"`
	VideoCodec                    *string           `json:"video_codec"`
	AudioCodec                    *string           `json:"audio_codec"`
	DurationSeconds               *int              `json:"duration_seconds"`
	CDNAllowed                    *bool             `json:"cdn_allowed"`
	SignatureDeadlineUnix         *int64            `json:"signature_deadline_unix"`
}

type AttemptDiagnostics struct {
	Stages       []DiagnosticStage `json:"details"`
	FinalOutcome DiagnosticOutcome `json:"final_outcome"`
}

type DiagnosticTrace struct {
	mu           sync.Mutex
	stages       []DiagnosticStage
	finalOutcome DiagnosticOutcome
	logged       bool
}

func NewDiagnosticTrace() *DiagnosticTrace {
	return &DiagnosticTrace{stages: make([]DiagnosticStage, 0, 12)}
}

func ContextWithDiagnostics(ctx context.Context, trace *DiagnosticTrace) context.Context {
	return context.WithValue(ctx, diagnosticContextKey{}, trace)
}

func DiagnosticsFromContext(ctx context.Context) *DiagnosticTrace {
	trace, _ := ctx.Value(diagnosticContextKey{}).(*DiagnosticTrace)
	return trace
}

type diagnosticContextKey struct{}

func Reason(value string) *string { return &value }

func RecordDiagnostic(ctx context.Context, stage DiagnosticStage) {
	if trace := DiagnosticsFromContext(ctx); trace != nil {
		trace.Add(stage)
	}
}

func (trace *DiagnosticTrace) Add(stage DiagnosticStage) {
	if trace == nil {
		return
	}
	stage = SanitizeDiagnosticStage(stage)
	trace.mu.Lock()
	if len(trace.stages) >= 128 {
		remove := -1
		for index, existing := range trace.stages {
			if existing.Stage == "session_media_fetch" {
				remove = index
				break
			}
		}
		if remove < 0 {
			remove = len(trace.stages) - 1
		}
		trace.stages = append(trace.stages[:remove], trace.stages[remove+1:]...)
	}
	trace.stages = append(trace.stages, stage)
	trace.mu.Unlock()
}

func (trace *DiagnosticTrace) SetFinalOutcome(outcome DiagnosticOutcome) {
	if trace == nil || !validOutcome(outcome) {
		return
	}
	trace.mu.Lock()
	if !trace.logged {
		trace.finalOutcome = outcome
	}
	trace.mu.Unlock()
}

func (trace *DiagnosticTrace) Snapshot() AttemptDiagnostics {
	if trace == nil {
		return AttemptDiagnostics{Stages: []DiagnosticStage{}}
	}
	trace.mu.Lock()
	defer trace.mu.Unlock()
	stages := make([]DiagnosticStage, len(trace.stages))
	copy(stages, trace.stages)
	for index := range stages {
		stages[index] = SanitizeDiagnosticStage(stages[index])
	}
	stages = orderDiagnosticStages(stages)
	return AttemptDiagnostics{Stages: stages, FinalOutcome: trace.finalOutcome}
}

func orderDiagnosticStages(stages []DiagnosticStage) []DiagnosticStage {
	order := map[string]int{"frontend_request": 0, "playback_open": 1, "webview_session": 2, "webview_page_load": 3, "webview_detail_fetch": 4, "webview_media_extract": 5, "resolver_fallback": 6, "ytdlp": 7, "source_validate": 8, "session_media_fetch": 9, "frontend_media": 10}
	for i := 1; i < len(stages); i++ {
		for j := i; j > 0 && order[stages[j].Stage] < order[stages[j-1].Stage]; j-- {
			stages[j], stages[j-1] = stages[j-1], stages[j]
		}
	}
	return stages
}

func (trace *DiagnosticTrace) RecordFrontendMedia(code int, message string) AttemptDiagnostics {
	if trace == nil {
		return AttemptDiagnostics{Stages: []DiagnosticStage{}}
	}
	message = strings.TrimSpace(message)
	stage := DiagnosticStage{Stage: "frontend_media", Outcome: OutcomeException, FrontendMediaCode: &code, FrontendMessage: &message, Reason: stringPointer(frontendMediaReason(code))}
	trace.Add(stage)
	outcome := OutcomeException
	if code == 1 {
		outcome = OutcomeHTTPError
	} else if code == 0 {
		outcome = OutcomeOK
	}
	trace.SetFinalOutcome(outcome)
	return trace.Snapshot()
}

func (trace *DiagnosticTrace) Finish(outcome DiagnosticOutcome) {
	if trace == nil {
		return
	}
	if !validOutcome(outcome) {
		outcome = OutcomeException
	}
	trace.mu.Lock()
	trace.finalOutcome = outcome
	trace.mu.Unlock()
}

func (trace *DiagnosticTrace) IsLogged() bool {
	if trace == nil {
		return false
	}
	trace.mu.Lock()
	defer trace.mu.Unlock()
	return trace.logged
}

func (trace *DiagnosticTrace) Log(outcome DiagnosticOutcome) {
	if trace == nil {
		return
	}
	trace.mu.Lock()
	if trace.logged {
		trace.mu.Unlock()
		return
	}
	trace.logged = true
	trace.finalOutcome = outcome
	if !validOutcome(trace.finalOutcome) {
		trace.finalOutcome = OutcomeException
	}
	trace.mu.Unlock()
	snapshot := trace.Snapshot()
	encoded, err := json.Marshal(snapshot.Stages)
	if err != nil {
		encoded = []byte("[]")
	}
	slog.Info("douyin playback diagnostic", "final_outcome", snapshot.FinalOutcome, "diagnostics", string(encoded))
}

type DiagnosticError struct {
	cause   error
	trace   *DiagnosticTrace
	message string
}

func (err *DiagnosticError) Error() string {
	if err == nil || err.message == "" {
		return "抖音视频解析失败，请在详情中查看脱敏阶段信息。"
	}
	var platform *discovery.PlatformError
	if err.cause != nil && errors.As(err.cause, &platform) && platform.Restricted {
		return "抖音页面要求登录或完成验证，请在应用内完成验证后重试。"
	}
	if errors.Is(err.cause, context.DeadlineExceeded) {
		return "抖音视频解析超时，请检查网络后重试。"
	}
	return err.message
}

func (err *DiagnosticError) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.cause
}

func (err *DiagnosticError) Diagnostics() AttemptDiagnostics {
	if err == nil || err.trace == nil {
		return AttemptDiagnostics{Stages: []DiagnosticStage{}}
	}
	return err.trace.Snapshot()
}

func (err *DiagnosticError) Trace() *DiagnosticTrace {
	if err == nil {
		return nil
	}
	return err.trace
}

func WrapDiagnostic(cause error, trace *DiagnosticTrace, message string) error {
	if cause == nil {
		return nil
	}
	return &DiagnosticError{cause: cause, trace: trace, message: message}
}

func SanitizeDiagnosticStage(stage DiagnosticStage) DiagnosticStage {
	if !validStage(stage.Stage) {
		stage.Stage = "playback_open"
	}
	if !validOutcome(stage.Outcome) {
		stage.Outcome = OutcomeException
	}
	stage.ContentType = SanitizeContentType(stage.ContentType)
	stage.PageURL = SanitizeURL(stage.PageURL)
	stage.BlockedNavigationURL = SanitizeURL(stage.BlockedNavigationURL)
	stage.PageReadyState = safeEnum(stage.PageReadyState, "loading", "interactive", "complete", "unknown")
	stage.YTDLPVersion = safeValue(stage.YTDLPVersion, 80)
	stage.YTDLPStderrFirstErrorRedacted = SanitizeStderr(stage.YTDLPStderrFirstErrorRedacted)
	stage.Reason = safeReason(stage.Reason)
	stage.APICode = safeAPIValue(stage.APICode)
	stage.FrontendMessage = safeDiagnosticText(stage.FrontendMessage, 240)
	stage.Protocol = safeEnum(stage.Protocol, "https", "http")
	stage.Extension = safeEnum(stage.Extension, "mp4", "m4a", "webm", "unknown")
	stage.VideoCodec = safeCodec(stage.VideoCodec)
	stage.AudioCodec = safeCodec(stage.AudioCodec)
	return stage
}

func SanitizeURL(value *string) *string {
	if value == nil || len(*value) > 2048 {
		return nil
	}
	raw := strings.TrimSpace(*value)
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Opaque != "" || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" {
		return nil
	}
	if port := u.Port(); port != "" && !((u.Scheme == "https" && port == "443") || (u.Scheme == "http" && port == "80")) {
		return nil
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if strings.ContainsAny(host, " /\\%\r\n\t") || len(host) > 253 {
		return nil
	}
	path := sanitizeDiagnosticPath(host, u.EscapedPath())
	if path == "" {
		return nil
	}
	if len(u.EscapedPath()) > 1024 {
		return nil
	}
	if len(path) > 1024 {
		return nil
	}
	result := host + path
	return &result
}

func sanitizeDiagnosticPath(host, escapedPath string) string {
	if escapedPath == "" {
		return "/"
	}
	leading := strings.HasPrefix(escapedPath, "/")
	trailing := strings.HasSuffix(escapedPath, "/")
	parts := strings.Split(strings.Trim(escapedPath, "/"), "/")
	if len(parts) == 1 && parts[0] == "" {
		return "/"
	}
	if isDouyinCDNHost(host) && len(parts) > 2 {
		parts = parts[:2]
		trailing = false
	}
	for index, part := range parts {
		decoded, err := url.PathUnescape(part)
		if err != nil {
			return ""
		}
		if decoded == "<redacted>" {
			parts[index] = decoded
			continue
		}
		if sensitivePathSegment(decoded) {
			parts[index] = "<redacted>"
		}
	}
	path := strings.Join(parts, "/")
	if leading {
		path = "/" + path
	}
	if trailing && path != "/" {
		path += "/"
	}
	if path == "" {
		return "/"
	}
	return path
}

func isDouyinCDNHost(host string) bool {
	return host == "douyinvod.com" || strings.HasSuffix(host, ".douyinvod.com")
}

func sensitivePathSegment(value string) bool {
	lower := strings.ToLower(strings.TrimSpace(value))
	if lower == "" {
		return false
	}
	if len(value) >= 24 {
		return true
	}
	return strings.Contains(lower, "token") || strings.Contains(lower, "signature") || strings.Contains(lower, "sign=") || strings.HasPrefix(lower, "sig") || strings.HasPrefix(lower, "sign") || strings.HasPrefix(lower, "token") || strings.Contains(lower, "a_bogus") || strings.Contains(lower, "ms_token") || strings.Contains(lower, "x-bogus")
}

func IsDouyinContext(value *string) *bool {
	if value == nil || len(*value) > 2048 {
		return nil
	}
	raw := strings.TrimSpace(*value)
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Hostname() == "" {
		return nil
	}
	if port := u.Port(); port != "" && port != "443" {
		return nil
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	result := host == "douyin.com" || strings.HasSuffix(host, ".douyin.com")
	return &result
}

func SanitizeContentType(value *string) *string {
	if value == nil || len(*value) > 1024 {
		return nil
	}
	mediaType, _, err := mime.ParseMediaType(strings.TrimSpace(*value))
	if err != nil || mediaType == "" || len(mediaType) > 128 {
		return nil
	}
	mediaType = strings.ToLower(mediaType)
	return &mediaType
}

var (
	diagnosticURLPattern = regexp.MustCompile(`(?i)https?://[^\s"'<>]+`)
	authorizationHeader  = regexp.MustCompile(`(?i)\b(authorization|proxy-authorization)\b\s*[:=]\s*[^\s,;&]+(?:\s+[^\s,;&]+)?`)
	cookieHeader         = regexp.MustCompile(`(?i)\b(cookie|set-cookie)\b\s*[:=]\s*[^\r\n]+`)
	secretAssignment     = regexp.MustCompile(`(?i)\b(cookie|set-cookie|authorization|proxy-authorization|token|msToken|a_bogus|signature|sign)\b\s*[:=]\s*("[^"]*"|'[^']*'|[^\s,;&]+)`)
	longRandomValue      = regexp.MustCompile(`\b[A-Za-z0-9_-]{28,}\b`)
	longNumberValue      = regexp.MustCompile(`\b[0-9]{12,}\b`)
)

func SanitizeStderr(value *string) *string {
	if value == nil || len(*value) > 1<<20 {
		return nil
	}
	for _, line := range strings.Split(*value, "\n") {
		if !strings.Contains(strings.ToUpper(line), "ERROR") {
			continue
		}
		line = safeRedactedText(line)
		if len(line) > 320 {
			line = line[:320]
		}
		return &line
	}
	return nil
}

func safeDiagnosticText(value *string, limit int) *string {
	if value == nil || len(*value) > 1<<16 {
		return nil
	}
	text := safeRedactedText(*value)
	if len(text) > limit {
		text = text[:limit]
	}
	return &text
}

func safeRedactedText(value string) string {
	value = strings.ToValidUTF8(value, "�")
	value = strings.Map(func(r rune) rune {
		if r < 0x20 && r != '\t' {
			return -1
		}
		return r
	}, value)
	value = cookieHeader.ReplaceAllString(value, "$1=[已脱敏]")
	value = authorizationHeader.ReplaceAllString(value, "$1=[已脱敏]")
	value = secretAssignment.ReplaceAllString(value, "$1=[已脱敏]")
	value = diagnosticURLPattern.ReplaceAllStringFunc(value, func(raw string) string {
		trimmed := strings.TrimRight(raw, ".,;:!?)]}")
		suffix := raw[len(trimmed):]
		safe := SanitizeURL(&trimmed)
		if safe == nil {
			return "[URL]" + suffix
		}
		return *safe + suffix
	})
	value = longRandomValue.ReplaceAllString(value, "[长字符串已脱敏]")
	value = longNumberValue.ReplaceAllString(value, "[长数字已脱敏]")
	return strings.TrimSpace(value)
}

func safeValue(value *string, limit int) *string {
	if value == nil || len(*value) > limit {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" || strings.ContainsAny(trimmed, "\r\n\x00") {
		return nil
	}
	for _, r := range trimmed {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._+-", r)) {
			return nil
		}
	}
	return &trimmed
}

func safeReason(value *string) *string {
	if value == nil {
		return nil
	}
	if !validReason(*value) {
		unknown := "unknown"
		return &unknown
	}
	return value
}

func safeAPIValue(value *string) *string {
	if value == nil || len(*value) > 64 {
		return nil
	}
	for _, r := range *value {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return nil
		}
	}
	return value
}

func safeEnum(value *string, allowed ...string) *string {
	if value == nil {
		return nil
	}
	for _, option := range allowed {
		if *value == option {
			return value
		}
	}
	return nil
}

func safeCodec(value *string) *string {
	if value == nil || len(*value) > 32 {
		return nil
	}
	text := strings.ToLower(strings.TrimSpace(*value))
	if text == "" || text == "none" {
		return nil
	}
	for _, r := range text {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-') {
			return nil
		}
	}
	return &text
}

func validOutcome(value DiagnosticOutcome) bool {
	switch value {
	case OutcomeOK, OutcomeRestricted, OutcomeTimeout, OutcomeTooLarge, OutcomeNoURL, OutcomeHTTPError, OutcomeException:
		return true
	default:
		return false
	}
}

func validStage(value string) bool {
	switch value {
	case "frontend_request", "playback_open", "webview_session", "webview_page_load", "webview_detail_fetch", "webview_media_extract", "resolver_fallback", "ytdlp", "source_validate", "session_media_fetch", "frontend_media":
		return true
	default:
		return false
	}
}

func validReason(value string) bool {
	switch value {
	case "id_validated", "invalid_id", "concurrency_limit", "resolver_started", "resolver_timeout", "resolver_error", "resolver_success", "fallback_allowed", "fallback_blocked_restricted", "fallback_blocked_timeout", "fallback_blocked_cancelled", "webview_success", "webview_no_url", "session_created", "session_reused", "session_destroyed", "dom_ready", "navigation_blocked", "page_load_error", "javascript_error", "network_error", "detail_success", "http_status", "business_verification", "non_json", "invalid_json", "response_too_large", "media_found", "media_not_found", "candidate_filtered", "candidate_expired", "cdn_rejected", "source_expired", "source_valid", "tool_unavailable", "cookie_read_error", "cookie_configured", "command_error", "command_failed", "cancelled", "stdout_too_large", "format_invalid", "json_invalid", "timeout", "signature_refresh", "candidate_http_error", "invalid_media_response", "stream_error", "media_aborted", "media_decode_error", "media_network_error", "media_src_not_supported", "api_error", "request_error", "request_sent", "unknown":
		return true
	default:
		return false
	}
}

func frontendMediaReason(code int) string {
	switch code {
	case 1:
		return "media_aborted"
	case 2:
		return "media_src_not_supported"
	case 3:
		return "media_decode_error"
	case 4:
		return "media_src_not_supported"
	default:
		return "unknown"
	}
}

func OutcomeForError(err error) DiagnosticOutcome {
	if err == nil {
		return OutcomeOK
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return OutcomeTimeout
	}
	var platform *discovery.PlatformError
	if errors.As(err, &platform) && platform.Restricted {
		return OutcomeRestricted
	}
	if errors.Is(err, ErrBusy) {
		return OutcomeException
	}
	return OutcomeException
}

func FinalOutcome(err error, stages []DiagnosticStage) DiagnosticOutcome {
	if errors.Is(err, context.DeadlineExceeded) {
		return OutcomeTimeout
	}
	var restricted *discovery.PlatformError
	if errors.As(err, &restricted) && restricted.Restricted {
		return OutcomeRestricted
	}
	if errors.Is(err, ErrBusy) || errors.Is(err, ErrInvalid) || errors.Is(err, ErrExpired) {
		return OutcomeException
	}
	// A generic playback_open error is appended after the resolver returns and
	// must not hide a more specific resolver/source outcome such as no_url or a
	// yt-dlp HTTP failure. Keep it as the final fallback only.
	for _, stageName := range []string{"frontend_media", "session_media_fetch", "ytdlp", "source_validate", "webview_media_extract", "webview_detail_fetch", "webview_page_load", "webview_session", "playback_open"} {
		for index := len(stages) - 1; index >= 0; index-- {
			if stages[index].Stage == stageName {
				return stages[index].Outcome
			}
		}
	}
	return OutcomeForError(err)
}

func DiagnosticSummary(stages []DiagnosticStage) string {
	parts := make([]string, 0, 2)
	for _, stage := range stages {
		switch stage.Stage {
		case "webview_detail_fetch":
			if stage.HTTPStatus != nil && *stage.HTTPStatus >= 400 {
				parts = append(parts, "应用内页面详情接口返回 HTTP "+strconv.Itoa(*stage.HTTPStatus))
			} else if stage.Outcome == OutcomeTimeout {
				parts = append(parts, "应用内页面详情接口等待超时")
			} else if stage.Outcome == OutcomeTooLarge {
				parts = append(parts, "应用内页面详情接口响应超过 4 MiB 限制")
			} else if stage.Outcome == OutcomeRestricted {
				parts = append(parts, "应用内页面要求登录或完成验证")
			} else if stage.Outcome == OutcomeNoURL {
				parts = append(parts, "详情接口没有返回媒体地址")
			} else if stage.Outcome == OutcomeException {
				if stage.Reason != nil && *stage.Reason == "non_json" {
					parts = append(parts, "应用内页面详情接口返回非 JSON")
				} else {
					parts = append(parts, "应用内页面详情接口解析异常")
				}
			}
		case "webview_page_load":
			if stage.Outcome == OutcomeRestricted {
				if stage.Reason != nil && *stage.Reason == "navigation_blocked" {
					parts = append(parts, "应用内页面导航被安全策略阻止")
				} else {
					parts = append(parts, "应用内页面停留在登录或验证页面")
				}
			} else if stage.Outcome == OutcomeTimeout {
				parts = append(parts, "应用内抖音页面加载超时")
			}
		case "webview_media_extract":
			if stage.Outcome == OutcomeNoURL {
				parts = append(parts, "应用内页面没有找到可用的媒体地址")
			}
		case "resolver_fallback":
			if stage.Reason != nil {
				switch *stage.Reason {
				case "fallback_allowed":
					parts = append(parts, "应用内页面失败后允许 yt-dlp 回退")
				case "fallback_blocked_restricted":
					parts = append(parts, "检测到页面验证，已阻止 yt-dlp 回退")
				case "fallback_blocked_timeout":
					parts = append(parts, "页面超时，已阻止 yt-dlp 回退")
				}
			}
		case "ytdlp":
			if stage.YTDLPExitCode != nil {
				text := "yt-dlp"
				if stage.YTDLPVersion != nil {
					text += " " + *stage.YTDLPVersion
				}
				text += " 退出码 " + strconv.Itoa(*stage.YTDLPExitCode)
				if stage.YTDLPStderrFirstErrorRedacted != nil {
					text += "，stderr: " + *stage.YTDLPStderrFirstErrorRedacted
				}
				parts = append(parts, text)
			} else if stage.Outcome == OutcomeTimeout {
				parts = append(parts, "yt-dlp 解析超时")
			} else if stage.Outcome == OutcomeNoURL {
				parts = append(parts, "yt-dlp 未返回兼容的媒体格式")
			} else if stage.Outcome == OutcomeException && stage.Reason != nil {
				switch *stage.Reason {
				case "tool_unavailable":
					parts = append(parts, "yt-dlp 尚未准备好")
				case "cookie_read_error":
					parts = append(parts, "抖音凭据配置无法读取")
				case "command_error":
					parts = append(parts, "yt-dlp 命令不可用")
				case "command_failed":
					parts = append(parts, "yt-dlp 执行失败")
				}
			}
		case "source_validate":
			if stage.Outcome != OutcomeOK {
				parts = append(parts, "解析结果未通过播放地址安全校验")
			}
		case "session_media_fetch":
			if stage.HTTPStatus != nil && *stage.HTTPStatus >= 400 {
				parts = append(parts, "媒体地址返回 HTTP "+strconv.Itoa(*stage.HTTPStatus))
			} else if stage.Outcome == OutcomeTimeout {
				parts = append(parts, "媒体地址请求超时")
			} else if stage.Outcome == OutcomeException && stage.Reason != nil && *stage.Reason == "invalid_media_response" {
				parts = append(parts, "媒体地址返回格式不符合播放要求")
			}
		}
	}
	if len(parts) == 0 {
		return "解析未能取得可播放的视频地址。"
	}
	return strings.Join(parts, "；") + "。"
}
