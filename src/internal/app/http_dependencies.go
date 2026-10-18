package app

import (
	"context"
	"tangentry/internal/content"
	"tangentry/internal/discovery"
	httptransport "tangentry/internal/transport/http"
)

// httpDependencies exposes only the feature-specific ports prepared by the
// composition root. Transport code never traverses the application registry.
func httpDependencies(w httpWiring) httptransport.Dependencies { return w.dependencies }

func httpAccountDependencies(adapters httpAdapterBundle) httptransport.AccountDependencies {
	return adapters.accounts
}

func httpDiscoveryDependencies(s *services, adapters httpAdapterBundle) httptransport.DiscoveryDependencies {
	p := s.adapters.discovery
	return httptransport.DiscoveryDependencies{
		Search: discovery.NewService(p.creators), BilibiliBrowser: adapters.bilibiliBrowser,
		RecommendationConfig: s.features.api.preferences, RecommendationQueue: s.features.recommendations.queue,
		RecommendationPage: s.features.recommendations.Page, Imports: s.imports.importSource,
		LinuxdoPosts: func(ctx context.Context, id string, postIDs []int64) (content.Discussion, error) {
			return s.adapters.discovery.forum.TopicPosts(ctx, id, postIDs)
		},
		DiscoveryBatch:        s.features.discoveryBatch,
		DiscoveryTopics:       discovery.NewTopicRules(s.runtime.store),
		XiaohongshuDetail:     s.runtime.repositories.xiaohongshu.XiaohongshuDetail,
		XiaohongshuImageItems: s.runtime.repositories.xiaohongshu.XiaohongshuImageCandidates,
		XiaohongshuImages:     adapters.imageCache, XiaohongshuImageVersion: adapters.imageVersion,
		XiaohongshuRefresh: s.imports.refreshXiaohongshu,
		PaperImport:        s.imports.importSource,
	}
}

func httpLibraryDependencies(s *services) httptransport.LibraryDependencies {
	return httptransport.LibraryDependencies{
		Content: s.features.content, Documents: s.runtime.documents, Articles: s.features.articles, ArticleImageHTTP: s.adapters.network.public,
	}
}

func httpAIDependencies(s *services) httptransport.AIDependencies {
	var languageAI httptransport.AIService
	if s.features.api.ai != nil {
		languageAI = s.features.api.ai
	}
	return httptransport.AIDependencies{AI: languageAI, Transcription: s.features.transcription, ModelDownloads: s.features.modelDownloads}
}

func httpPodcastDependencies(s *services, adapters httpAdapterBundle, episodeID func(context.Context, string) string) httptransport.PodcastDependencies {
	return httptransport.PodcastDependencies{
		Captions:       adapters.podcastCaptions,
		EpisodeID:      episodeID,
		PublicCaptions: adapters.publicPodcastCaptions,
		SavedSubtitles: s.features.api.savedSubtitles, Inbox: s.features.api.inbox, Content: s.features.content,
	}
}

func httpMediaDependencies(s *services, adapters httpAdapterBundle) httptransport.MediaDependencies {
	var compatibility httptransport.CompatibilityService
	if s.features.api.compatibility != nil {
		compatibility = s.features.api.compatibility
	}
	return httptransport.MediaDependencies{
		Subtitles: s.features.api.subtitles, SavedSubtitles: s.features.api.savedSubtitles, Downloads: s.features.downloads,
		ToolStatus: adapters.toolStatus, UpdateYTDLP: adapters.updateYTDLP,
		Playback: s.features.api.playback, Compatibility: compatibility,
	}
}

func httpNoteDependencies(s *services) httptransport.NoteDependencies {
	return httptransport.NoteDependencies{
		Notes: s.features.api.notes, NoteFrames: s.features.api.noteFrames, NoteExports: s.features.api.noteExports,
		ReadState: s.features.api.readState, History: s.features.api.history, Progress: s.features.api.progress,
	}
}

func httpSubscriptionDependencies(s *services) httptransport.SubscriptionDependencies {
	return httptransport.SubscriptionDependencies{
		Subscriptions: s.features.sources, SubscriptionGroups: s.features.api.groupService,
		SubscriptionInbox: s.features.api.inbox, Favorites: s.features.api.favorites,
	}
}
