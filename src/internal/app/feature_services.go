package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	articleadapter "tangentry/internal/adapters/articles"
	"tangentry/internal/ai"
	"tangentry/internal/articles"
	"tangentry/internal/config"
	"tangentry/internal/content"
	"tangentry/internal/discovery"
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

// apiBundle contains long-lived domain services, separate from external clients.
type apiBundle struct {
	content           *content.Service
	articles          *articles.Service
	recommendations   *recommender
	discoveryBatch    *discovery.BatchService
	discoveryFeedback *discovery.FeedbackService
	sources           *subscriptions.Service
	downloads         *downloads.Service
	transcription     *transcription.Service
	modelDownloads    *transcription.ModelDownloads
	modelDir          string
	api               apiServices
}

type apiServices struct {
	ai             *ai.Service
	compatibility  *playback.Compatibility
	favorites      *favoritesync.Service
	groupService   *subscriptions.GroupService
	inbox          *subscriptions.InboxService
	savedViews     *subscriptions.SavedViewService
	notes          *notes.Service
	noteFrames     *notes.Frames
	noteExports    *notes.ExportService
	playback       *playback.Service
	progress       *readingprogress.Service
	readState      *readstate.Service
	history        *history.Service
	preferences    *discovery.Preferences
	savedSubtitles *subtitles.SavedService
	subtitles      *subtitles.Service
}

// wireFeatures builds domain services around the runtime capabilities. It does
// not start goroutines; lifecycle startup is kept in startBackgroundServices so
// construction remains deterministic and failures can be cleaned up uniformly.
func wireFeatures(dataDir string, configuration config.Config, option Options, runtime *runtimeResources, started *closers) (*apiBundle, error) {
	store, p, toolkit, processing := runtime.store, runtime.platforms, runtime.toolkit, runtime.downloads
	records := runtime.repositories
	feeds := subscriptionFeeds{platformSubscriptions: p.subscriptions, records: records.subscription}
	sources := subscriptions.NewService(store, feeds.snapshot, feeds.options()...)
	captions := subtitles.NewService(toolkit)
	started.add(captions.Close)
	savedCaptions := subtitles.NewSavedService(store)
	contentService := content.NewService(store)
	inboxService := subscriptions.NewInboxService(store)
	savedViews := subscriptions.NewSavedViewService(store)
	recommendations := &recommender{
		platforms:     p,
		subscriptions: sources.Gallery,
		cache:         newRecommendationCache(),
		queue: discovery.NewRecommendationQueue(store, func(source string) discovery.QueueScope {
			return discovery.QueueScope{Platform: source, Account: discovery.PersonalAccount}
		}),
		override: option.Recommendations,
	}
	discoveryBatch := discovery.NewBatchService(recommendations, sources.Gallery, []string{"subscriptions", "bilibili", "youtube", "xiaohongshu", "linuxdo", "xiaoyuzhou", "douyin", "huggingface"}, store)
	discoveryBatch.SetSourceEnabled(func(ctx context.Context, source string) (bool, error) {
		return discovery.NewPreferences(store).Enabled(ctx, source)
	})
	discoveryBatch.SetTopicRules(discovery.NewTopicRules(store).List)
	discoveryFeedback := discovery.NewFeedbackService(store)
	discoveryBatch.SetFeedback(discoveryFeedback.List)

	resolvePlayback := p.media.bilibili.Playback
	if p.media.douyin != nil {
		resolvePlayback = func(ctx context.Context, id string) (playback.Source, error) {
			if strings.HasPrefix(id, "BV") {
				return p.media.bilibili.Playback(ctx, id)
			}
			return resolveDouyinPlayback(ctx, id,
				func(ctx context.Context, id string) (playback.Source, error) {
					media, err := p.media.douyin.ResolveVideoMedia(ctx, id)
					if err != nil {
						return playback.Source{}, err
					}
					return playback.Source{URL: media.URL, Backups: media.Backups, Platform: "douyin", Referer: "https://www.douyin.com/video/" + id}, nil
				}, p.network.toolkit.DouyinPlayback)
		}
	}
	if option.Playback != nil {
		resolvePlayback = option.Playback
	}
	resolveAudio := newAudioResolver(contentService, inboxService, processing, p.transcriptionSource(option.BilibiliAudio))
	speech, modelDownloads, modelDir, err := newTranscription(dataDir, configuration, p, option, resolveAudio, savedCaptions)
	if err != nil {
		return nil, err
	}
	started.add(speech.Close)
	started.add(modelDownloads.Close)

	var favoriteRemote favoritesync.Remote = p.media.bilibili
	if option.FavoritesRemote != nil {
		favoriteRemote = option.FavoritesRemote
	}
	favorites := favoritesync.New(store, favoriteRemote, processing)
	started.add(favorites.Close)
	noteService := notes.NewService(store)
	languageAI := newLanguageAI(dataDir, store, option)
	if languageAI != nil {
		languageAI.SetSummaryNoteWriter(func(ctx context.Context, result ai.SummaryResult) error {
			body := fmt.Sprintf("# AI 摘要\n\n%s\n\n## 要点\n\n%s", result.Overview, strings.Join(summaryNotePoints(result.Points), "\n"))
			if len(result.Outline) > 0 {
				body += "\n\n## 大纲\n\n" + strings.Join(summaryNoteOutline(result.Outline, 0), "\n")
			}
			createID := summaryNoteID(result.ContentID)
			saved, err := noteService.Create(ctx, notes.Input{CreateID: createID, ContentID: result.ContentID, Body: body, Tags: []string{"AI摘要"}})
			if err == nil || err == notes.ErrCreationConflict {
				if err == nil && saved.Body == body {
					return nil
				}
				items, listErr := noteService.List(ctx, result.ContentID)
				if listErr != nil {
					return listErr
				}
				for _, note := range items {
					if note.ID == createID || containsTag(note.Tags, "AI摘要") {
						_, editErr := noteService.Edit(ctx, note.ID, notes.Edit{Body: body, Tags: &[]string{"AI摘要"}, ExpectedUpdatedAt: &note.UpdatedAt})
						return editErr
					}
				}
			}
			return err
		})
		started.add(languageAI.Close)
	}
	compatibility := newCompatibility(p)
	if compatibility != nil {
		started.add(func() { _ = compatibility.Close() })
	}
	started.add(sources.Close)

	return &apiBundle{
		content: contentService, articles: articles.New(store, &articleadapter.Extractor{HTTP: p.network.public}), sources: sources,
		recommendations: recommendations, discoveryBatch: discoveryBatch, discoveryFeedback: discoveryFeedback, downloads: processing, transcription: speech, modelDownloads: modelDownloads, modelDir: modelDir,
		api: apiServices{
			ai: languageAI, compatibility: compatibility, favorites: favorites,
			groupService: subscriptions.NewGroupService(store), inbox: inboxService, savedViews: savedViews,
			notes: noteService, noteFrames: notes.NewFrames(store, processing.NoteFrameFile, toolkit.CaptureNoteFrame),
			noteExports: notes.NewExporter(store), playback: playback.New(resolvePlayback, p.network.public),
			progress: readingprogress.NewService(store), readState: readstate.New(store), history: history.New(store),
			preferences: discovery.NewPreferences(store), savedSubtitles: savedCaptions, subtitles: captions,
		},
	}, nil
}

func resolveDouyinPlayback(ctx context.Context, id string, pageResolve, ytdlpResolve func(context.Context, string) (playback.Source, error)) (playback.Source, error) {
	trace := playback.DiagnosticsFromContext(ctx)
	source, err := pageResolve(ctx, id)
	if err == nil && source.URL != "" {
		source.Platform, source.Referer = "douyin", "https://www.douyin.com/video/"+id
		playback.RecordDiagnostic(ctx, playback.DiagnosticStage{Stage: "resolver_fallback", Outcome: playback.OutcomeOK, Reason: appDiagnosticReason("webview_success")})
		return source, nil
	}
	if err == nil && source.URL == "" {
		playback.RecordDiagnostic(ctx, playback.DiagnosticStage{Stage: "webview_media_extract", Outcome: playback.OutcomeNoURL, Reason: appDiagnosticReason("media_not_found")})
	}
	var restricted *discovery.PlatformError
	if err != nil && errors.As(err, &restricted) && restricted.Restricted {
		playback.RecordDiagnostic(ctx, playback.DiagnosticStage{Stage: "resolver_fallback", Outcome: playback.OutcomeRestricted, Reason: appDiagnosticReason("fallback_blocked_restricted")})
		return playback.Source{}, playback.WrapDiagnostic(err, trace, playback.DiagnosticSummary(trace.Snapshot().Stages))
	}
	if err != nil && (errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil) {
		outcome, reason := playback.OutcomeTimeout, "fallback_blocked_timeout"
		if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
			outcome, reason = playback.OutcomeException, "fallback_blocked_cancelled"
		}
		playback.RecordDiagnostic(ctx, playback.DiagnosticStage{Stage: "resolver_fallback", Outcome: outcome, Reason: appDiagnosticReason(reason)})
		return playback.Source{}, playback.WrapDiagnostic(err, trace, playback.DiagnosticSummary(trace.Snapshot().Stages))
	}
	playback.RecordDiagnostic(ctx, playback.DiagnosticStage{Stage: "resolver_fallback", Outcome: playback.OutcomeOK, Reason: appDiagnosticReason("fallback_allowed")})
	source, err = ytdlpResolve(ctx, "https://www.douyin.com/video/"+id)
	if err != nil {
		return playback.Source{}, playback.WrapDiagnostic(err, trace, playback.DiagnosticSummary(trace.Snapshot().Stages))
	}
	source.Platform, source.Referer = "douyin", "https://www.douyin.com/video/"+id
	return source, nil
}

func appDiagnosticReason(value string) *string { return &value }

func summaryNoteID(contentID string) string {
	sum := sha256.Sum256([]byte("ai-summary-note:" + contentID))
	return hex.EncodeToString(sum[:16])
}
func containsTag(tags []string, target string) bool {
	for _, tag := range tags {
		if tag == target {
			return true
		}
	}
	return false
}

func summaryNotePoints(points []ai.SummaryPoint) []string {
	result := make([]string, 0, len(points))
	for index, point := range points {
		result = append(result, fmt.Sprintf("%d. %s", index+1, point.Text))
	}
	return result
}

func summaryNoteOutline(nodes []ai.SummaryOutline, depth int) []string {
	result := make([]string, 0)
	for _, node := range nodes {
		result = append(result, strings.Repeat("  ", depth)+"- "+node.Title)
		result = append(result, summaryNoteOutline(node.Children, depth+1)...)
	}
	return result
}

// startBackgroundServices starts only work that must outlive the constructor.
// Keep this after all feature construction succeeds so a failed wiring path
// never leaves a refresh goroutine behind.
func startBackgroundServices(features *apiBundle) {
	features.sources.StartRefresh()
}
