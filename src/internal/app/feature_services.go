package app

import (
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
	content         *content.Service
	articles        *articles.Service
	recommendations *recommender
	sources         *subscriptions.Service
	downloads       *downloads.Service
	transcription   *transcription.Service
	modelDownloads  *transcription.ModelDownloads
	modelDir        string
	api             apiServices
}

type apiServices struct {
	ai             *ai.Service
	compatibility  *playback.Compatibility
	favorites      *favoritesync.Service
	groupService   *subscriptions.GroupService
	inbox          *subscriptions.InboxService
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
	recommendations := &recommender{
		platforms:     p,
		subscriptions: sources.Gallery,
		queue: discovery.NewRecommendationQueue(store, func(source string) discovery.QueueScope {
			return discovery.QueueScope{Platform: source, Account: discovery.PersonalAccount}
		}),
		override: option.Recommendations,
	}

	resolvePlayback := p.media.bilibili.Playback
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
	languageAI := newLanguageAI(dataDir, store, option)
	if languageAI != nil {
		started.add(languageAI.Close)
	}
	compatibility := newCompatibility(p)
	if compatibility != nil {
		started.add(func() { _ = compatibility.Close() })
	}
	started.add(sources.Close)

	return &apiBundle{
		content: contentService, articles: articles.New(store, &articleadapter.Extractor{HTTP: p.network.public}), sources: sources,
		recommendations: recommendations, downloads: processing, transcription: speech, modelDownloads: modelDownloads, modelDir: modelDir,
		api: apiServices{
			ai: languageAI, compatibility: compatibility, favorites: favorites,
			groupService: subscriptions.NewGroupService(store), inbox: inboxService,
			notes: notes.NewService(store), noteFrames: notes.NewFrames(store, processing.NoteFrameFile, toolkit.CaptureNoteFrame),
			noteExports: notes.NewExporter(store), playback: playback.New(resolvePlayback, p.network.public),
			progress: readingprogress.NewService(store), readState: readstate.New(store), history: history.New(store),
			preferences: discovery.NewPreferences(store), savedSubtitles: savedCaptions, subtitles: captions,
		},
	}, nil
}

// startBackgroundServices starts only work that must outlive the constructor.
// Keep this after all feature construction succeeds so a failed wiring path
// never leaves a refresh goroutine behind.
func startBackgroundServices(features *apiBundle) {
	features.sources.StartRefresh()
}
