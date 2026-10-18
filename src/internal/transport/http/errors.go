package httptransport

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"tangentry/internal/articles"
	"tangentry/internal/content"
	"tangentry/internal/credentials"
	"tangentry/internal/discovery"
	"tangentry/internal/documents"
	"tangentry/internal/downloads"
	"tangentry/internal/favoritesync"
	"tangentry/internal/history"
	"tangentry/internal/notes"
	"tangentry/internal/playback"
	"tangentry/internal/readingprogress"
	"tangentry/internal/readstate"
	"tangentry/internal/subscriptions"
	"tangentry/internal/subtitles"
	"tangentry/internal/transcription"
)

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func write(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

// errorResponse maps a domain error to the user-facing API error. respond
// replaces the fixed response when the error carries its own details.
type errorResponse struct {
	err     error
	status  int
	code    string
	message string
	respond func(http.ResponseWriter, error)
}

// errorResponses is checked in order; the first matching error wins.
var errorResponses = []errorResponse{
	{err: notes.ErrCreationConflict, status: http.StatusConflict, code: "note_creation_conflict", message: "上一次保存已完成，但这份草稿有变化。请核对已存笔记，或另存当前草稿。"},
	{err: notes.ErrCreationDeleted, status: http.StatusConflict, code: "note_creation_deleted", message: "上一次保存的笔记已被删除。需要保留当前草稿时，请另存为新笔记。"},
	{err: notes.ErrFrameBusy, status: http.StatusTooManyRequests, code: "note_frame_busy", message: "正在截取画面，请稍后重试。"},
	{err: notes.ErrFrameSource, status: http.StatusNotFound, code: "note_frame_source", message: "这段本机视频已不可用，请检查下载。"},
	{err: notes.ErrFrameImage, status: http.StatusUnprocessableEntity, code: "note_frame_image", message: "截图无法保存：图片需为 JPEG 且不超过 1 MB，请重新截屏。"},
	{err: notes.ErrFrameUnavailable, status: http.StatusUnprocessableEntity, code: "note_frame_unavailable", message: "未能截取画面，请检查视频时间和 FFmpeg。"},
	{err: notes.ErrExportBusy, status: http.StatusTooManyRequests, code: "note_export_busy", message: "正在准备另一份笔记导出，请稍后重试。"},
	{err: notes.ErrExportEmpty, status: http.StatusNotFound, code: "note_export_empty", message: "当前范围没有已保存笔记可导出，请先保存笔记或调整筛选。"},
	{err: notes.ErrExportLimit, status: http.StatusRequestEntityTooLarge, code: "note_export_limit", message: "笔记导出超过大小或 10,000 条限制，请按日期、标签或来源缩小范围。"},
	{err: notes.ErrExportUnavailable, status: http.StatusInternalServerError, code: "note_export_unavailable", message: "笔记导出未能完成，原笔记仍保留，请重试。"},
	{err: articles.ErrInvalid, status: http.StatusBadRequest, code: "invalid_article", message: "请选择有效的 RSS 文章，原站地址来自已保存的内容。"},
	{err: articles.ErrNotFound, status: http.StatusNotFound, code: "article_not_found", message: "这篇文章还没有本机全文，或已不在保存的内容中。"},
	{err: articles.ErrConflict, status: http.StatusConflict, code: "article_conflict", message: "全文已在其他页面更新，请先读取本机版本。"},
	{err: articles.ErrBusy, status: http.StatusTooManyRequests, code: "article_busy", message: "正在获取其他正文，请稍后再试。"},
	{err: articles.ErrUnavailable, respond: articleUnavailable},
	{err: history.ErrInvalid, status: http.StatusBadRequest, code: "invalid_history", message: "历史记录内容无效。"},
	{err: readstate.ErrInvalid, status: http.StatusBadRequest, code: "invalid_read_state", message: "请选择有效内容并明确设置已读或未读，一次最多 200 条。"},
	{err: transcription.ErrUploadConsent, status: http.StatusBadRequest, code: "transcription_upload_consent", message: "请重新读取转录设置，并确认允许上传音频至当前服务。"},
	{err: transcription.ErrInvalid, status: http.StatusBadRequest, code: "invalid_transcription", message: "请检查内容标识、模型和识别语言。音视频最长支持 6 小时。"},
	{err: transcription.ErrNotReady, status: http.StatusServiceUnavailable, code: "transcription_not_ready", message: "请先在工具设置配置转录引擎、对应模型和 FFmpeg。"},
	{err: transcription.ErrBusy, status: http.StatusConflict, code: "transcription_busy", message: "已有转写正在进行，请等待完成或取消后再试。"},
	{err: transcription.ErrNotFound, status: http.StatusNotFound, code: "transcription_not_found", message: "转写任务已不存在；已保存字幕仍可读取。"},
	{err: transcription.ErrUnavailable, status: http.StatusBadGateway, code: "transcription_unavailable", message: "转写暂不可用，请检查配置与数据目录。"},
	{err: playback.ErrInvalid, status: http.StatusBadRequest, code: "invalid_playback", message: "视频标识或播放范围无效。"},
	{err: playback.ErrUnavailable, status: http.StatusBadGateway, code: "playback_unavailable", message: "未能取得可播放的视频流，请检查登录配置与网络，或打开原视频。"},
	{err: playback.ErrExpired, status: http.StatusGone, code: "playback_expired", message: "播放连接已失效，请重新连接视频。"},
	{err: playback.ErrBusy, status: http.StatusTooManyRequests, code: "playback_busy", message: "正在处理其他播放请求，请稍后重试。"},
	{err: documents.ErrInvalid, status: http.StatusBadRequest, code: "invalid_pdf", message: "请选择有效的 PDF 文件（最大 50 MB），或检查文档地址。"},
	{err: documents.ErrUnavailable, status: http.StatusBadGateway, code: "pdf_unavailable", message: "PDF 暂时无法读取，请检查地址、网络与文件权限后重试。"},
	{err: credentials.ErrInvalid, status: http.StatusBadRequest, code: "invalid_cookie", message: "Cookie 格式不正确或包含其他平台域名，请检查所选平台与格式。"},
	{err: credentials.ErrStorage, status: http.StatusServiceUnavailable, code: "cookie_storage", message: "登录配置未能保存，请检查数据目录权限后重试。"},
	{err: subtitles.ErrSavedInvalid, status: http.StatusBadRequest, code: "invalid_saved_subtitle", message: "字幕内容、时间或版本不完整，请重新读取后保存。"},
	{err: subtitles.ErrSavedConflict, status: http.StatusConflict, code: "saved_subtitle_conflict", message: "本机字幕已被其他页面修改，请重新读取后再操作。"},
	{err: subtitles.ErrSavedLimit, status: http.StatusConflict, code: "saved_subtitle_limit", message: "每段视频最多保存 32 种字幕语言，请先移除不需要的版本。"},
	{err: readingprogress.ErrInvalid, status: http.StatusBadRequest, code: "invalid_progress", message: "阅读进度不完整或超出视频时长，请重新加载视频。"},
	{err: readingprogress.ErrConflict, status: http.StatusConflict, code: "progress_conflict", message: "另一个页面已更新进度。自动保存已暂停，可重新读取或明确保存当前位置。"},
	{err: subscriptions.ErrInboxInvalid, status: http.StatusBadRequest, code: "invalid_inbox_query", message: "请检查收件箱筛选或重新加载第一页。"},
	{err: subscriptions.ErrSavedViewInvalid, status: http.StatusBadRequest, code: "invalid_saved_view", message: "请检查视图名称、筛选条件、图标和颜色。"},
	{err: subscriptions.ErrSavedViewNotFound, status: http.StatusNotFound, code: "saved_view_not_found", message: "这条保存视图已不存在，请重新加载列表。"},
	{err: subscriptions.ErrInboxNotFound, status: http.StatusNotFound, code: "inbox_not_found", message: "本地历史中没有这份内容，请从来源再次获取。"},
	{err: subscriptions.ErrGroupInvalid, status: http.StatusBadRequest, code: "invalid_subscription_group", message: "请选择有效分组和当前版本，分组名称为 1–60 字，不能包含换行或控制字符。"},
	{err: subscriptions.ErrGroupNotFound, status: http.StatusNotFound, code: "subscription_group_not_found", message: "这个分组已经不存在，请重新加载分组。"},
	{err: subscriptions.ErrGroupConflict, status: http.StatusConflict, code: "subscription_group_conflict", message: "分组已被其他页面修改，输入已保留；请重新加载并确认当前分组后再保存。"},
	{err: subscriptions.ErrGroupName, status: http.StatusConflict, code: "subscription_group_name", message: "这个分组名称已存在，请使用其他名称。"},
	{err: subscriptions.ErrGroupLimit, status: http.StatusConflict, code: "subscription_group_limit", message: "最多保存 100 个分组，请先整理现有分组。"},
	{err: subscriptions.ErrChannelInput, status: http.StatusBadRequest, code: "invalid_youtube_channel", message: "请输入 YouTube 频道 ID（UC…）、频道网址或 @handle；单视频和播放列表请在搜索页导入。"},
	{err: subscriptions.ErrAddressConflict, status: http.StatusConflict, code: "subscription_address_conflict", message: "订阅地址已在其他页面修改，输入已保留；请重新读取当前地址后再保存。"},
	{err: subscriptions.ErrAddressExists, status: http.StatusConflict, code: "subscription_address_exists", message: "这个订阅地址已属于另一个来源，请打开已有来源。"},
	{err: subscriptions.ErrInvalid, status: http.StatusBadRequest, code: "invalid_subscription", message: "请输入所选来源的 UID、主页或 RSS / Atom 地址，来源名称最多 100 字。"},
	{err: subscriptions.ErrNotFound, status: http.StatusNotFound, code: "subscription_not_found", message: "这个来源已经不存在，请重新加载来源。"},
	{err: subscriptions.ErrBusy, status: http.StatusConflict, code: "subscription_busy", message: "来源正在更新或修改，请等待当前操作完成。"},
	{err: subscriptions.ErrSnapshot, status: http.StatusBadGateway, code: "subscription_incomplete", message: "平台返回的投稿不完整，上次成功内容已保留。"},
	{err: favoritesync.ErrInvalid, status: http.StatusBadRequest, code: "invalid_favorite_sync", message: "请选择有效的收藏夹，间隔至少 15 分钟，每轮下载 1–5 个视频，画质为 720p 或 1080p。"},
	{err: favoritesync.ErrNotFound, status: http.StatusNotFound, code: "favorite_sync_not_found", message: "没有找到这份收藏同步配置。"},
	{err: favoritesync.ErrBusy, status: http.StatusConflict, code: "favorite_sync_busy", message: "正在读取或同步收藏，请等待完成后再操作。"},
	{err: favoritesync.ErrIncomplete, status: http.StatusBadGateway, code: "favorite_sync_incomplete", message: "平台返回的收藏夹不完整，或超过 4000 条。原有内容已保留，请稍后重试或选择较小的收藏夹。"},
	{err: notes.ErrInvalid, status: http.StatusBadRequest, code: "invalid_note", message: "请填写 1–8000 字的笔记，并检查内容、关联位置和标签。标签最多 12 个，每个 1–32 字，可用文字、数字、下划线或连字符。"},
	{err: notes.ErrNotFound, status: http.StatusNotFound, code: "note_not_found", message: "这条笔记已不存在，请重新加载笔记。"},
	{err: notes.ErrConflict, status: http.StatusConflict, code: "note_conflict", message: "这条笔记已在其他窗口修改，本次操作未改变最新内容。请重新查看笔记后再操作；编辑草稿可另存为新笔记。"},
	// The requesting client has gone away; never log it as an application failure.
	{err: context.Canceled, respond: func(http.ResponseWriter, error) {}},
	{err: context.DeadlineExceeded, status: http.StatusGatewayTimeout, code: "request_timeout", message: "获取超时，请稍后重试。"},
	{err: subtitles.ErrInvalid, status: http.StatusBadRequest, code: "invalid_input", message: "请输入 B站单视频 BV 号 / 地址或 YouTube 单视频地址，并检查字幕语言。"},
	{err: subtitles.ErrTools, status: http.StatusServiceUnavailable, code: "tools_unavailable", message: "字幕读取需要 yt-dlp，请检查工具设置。"},
	{err: subtitles.ErrBusy, status: http.StatusTooManyRequests, code: "reader_busy", message: "已有字幕请求正在处理，请稍后重试。"},
	{err: subtitles.ErrLogin, status: http.StatusUnauthorized, code: "subtitles_login_required", message: "平台拒绝了字幕访问，需要有效登录或账号权限。请在原站确认视频可观看，并在工具设置重新导入 Cookie。已保存字幕仍可使用。"},
	{err: subtitles.ErrRestricted, status: http.StatusTooManyRequests, code: "subtitles_restricted", message: "平台限制了字幕请求或要求访问验证。请暂停重试，在原站完成验证后再试；已保存字幕仍可使用。"},
	{err: subtitles.ErrPublicUnavailable, status: http.StatusBadGateway, code: "public_subtitles_unavailable", message: "公开字幕暂时读取失败，请稍后重试。"},
	{err: subtitles.ErrUnavailable, status: http.StatusBadGateway, code: "subtitles_unavailable", message: "字幕未能完整获取，请检查视频访问权限、登录配置与网络后重试。"},
	{err: subtitles.ErrFormat, status: http.StatusBadGateway, code: "subtitles_invalid", message: "平台返回的字幕暂时无法读取，请稍后重试。"},
	{err: downloads.ErrInvalid, status: http.StatusBadRequest, code: "invalid_input", message: "请输入有效的 HTTP / HTTPS 视频或音频链接，并检查下载格式、画质和字幕选择。"},
	{err: downloads.ErrNotFound, status: http.StatusNotFound, code: "not_found", message: "任务或下载文件不存在，请刷新任务列表。"},
	{err: downloads.ErrActive, status: http.StatusConflict, code: "download_active", message: "进行中的下载不能删除，请先取消任务。"},
	{err: downloads.ErrBusy, status: http.StatusConflict, code: "queue_busy", message: "下载队列已满，请等待已有任务完成。"},
	{err: downloads.ErrToolBusy, status: http.StatusConflict, code: "tool_downloading", message: "正在下载所需工具，请稍候重试；工具设置中可查看进度。"},
	{err: downloads.ErrTools, status: http.StatusServiceUnavailable, code: "tools_unavailable", message: "下载工具尚未准备好，请在工具设置中检查。"},
	{err: content.ErrInvalid, status: http.StatusBadRequest, code: "invalid_input", message: "请输入正确的 UP 主 UID、B站主页地址或视频 BV 号 / 地址。"},
	{err: discovery.ErrInvalidQuery, status: http.StatusBadRequest, code: "invalid_input", message: "请输入正确的 UP 主 UID、B站主页地址或视频 BV 号 / 地址。"},
	{err: discovery.ErrInvalidFeedback, status: http.StatusBadRequest, code: "invalid_discovery_feedback", message: "请选择有效的发现反馈和目标。"},
	{err: content.ErrNotFound, status: http.StatusNotFound, code: "not_found", message: "这条内容已经不在资料库中。"},
	{err: discovery.ErrUnavailable, respond: platformUnavailable},
}

func fail(w http.ResponseWriter, err error) {
	var platform *discovery.PlatformError
	if errors.As(err, &platform) {
		platformUnavailable(w, err)
		return
	}
	for _, response := range errorResponses {
		if !errors.Is(err, response.err) {
			continue
		}
		if response.respond != nil {
			response.respond(w, err)
		} else {
			write(w, response.status, apiError{response.code, response.message})
		}
		return
	}
	slog.Error("request failed", "error", err)
	write(w, http.StatusInternalServerError, apiError{"internal_error", "操作未能完成，请重试。"})
}

func articleUnavailable(w http.ResponseWriter, err error) {
	message := "未能获取原站全文，可继续阅读订阅文字或打开原内容。"
	var detail *articles.FetchError
	if errors.As(err, &detail) {
		message = detail.Message
	}
	write(w, http.StatusBadGateway, apiError{"article_unavailable", message})
}

func platformUnavailable(w http.ResponseWriter, err error) {
	var platform *discovery.PlatformError
	if !errors.As(err, &platform) {
		write(w, http.StatusBadGateway, apiError{"platform_unavailable", "暂时无法获取平台内容。"})
		return
	}
	status, code := http.StatusBadGateway, "platform_unavailable"
	if platform.Restricted {
		status, code = http.StatusTooManyRequests, "platform_restricted"
		w.Header().Set("Retry-After", "60")
	}
	write(w, status, apiError{code, platform.Message})
}
