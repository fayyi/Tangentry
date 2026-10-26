package app

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"io"
	"math"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"tangentry/internal/agent"
	"tangentry/internal/content"
	"tangentry/internal/discovery"
	"tangentry/internal/downloads"
	"tangentry/internal/notes"
	"tangentry/internal/subscriptions"
	"tangentry/internal/subtitles"
)

type agentUnifiedSearch interface {
	Search(context.Context, string, discovery.SearchOptions) (discovery.SearchResponse, error)
}

type agentProvider struct {
	services *services
	search   agentUnifiedSearch
}

func newAgentProvider(s *services) *agentProvider {
	deps := httpDiscoveryDependencies(s, newHTTPAdapterBundle(s.adapters, s.features.downloads))
	return &agentProvider{services: s, search: deps.UnifiedSearch}
}

func (p *agentProvider) Capabilities(context.Context) agent.CapabilityMatrix {
	derivedStatus := "unsupported"
	if p != nil && p.services != nil && p.services.features != nil && p.services.features.transcription != nil && p.services.features.transcription.Status().Ready {
		derivedStatus = "configured_on_request"
	}
	if p != nil && p.services != nil && p.services.features != nil {
		if aiService := p.services.features.api.ai; aiService != nil {
			status := aiService.Status()
			if status.Ready || status.SummaryReady {
				derivedStatus = "configured_on_request"
			}
		}
	}
	features := map[string]map[string]string{
		"bilibili":      {"search": "supported", "detail": "supported", "subtitles": "supported", "social": "downloaded_local_danmaku", "frames": "completed_local_downloads", "derived": derivedStatus},
		"youtube":       {"search": "supported", "detail": "supported", "subtitles": "supported", "social": "unsupported", "frames": "completed_local_downloads", "derived": derivedStatus},
		"xiaohongshu":   {"search": "supported", "detail": "cached_or_manual_fetch", "subtitles": "unsupported", "social": "unsupported", "frames": "completed_local_downloads", "derived": derivedStatus},
		"douyin":        {"search": "supported", "detail": "supported", "subtitles": "transcription_only", "social": "unsupported", "frames": "completed_local_downloads", "derived": derivedStatus},
		"xiaoyuzhou":    {"search": "supported", "detail": "supported", "subtitles": "podcast_transcript_not_yet_exposed", "social": "unsupported", "frames": "completed_local_downloads", "derived": derivedStatus},
		"linuxdo":       {"search": "supported", "detail": "cached_or_manual_fetch", "subtitles": "unsupported", "social": "cached_discussion", "frames": "unsupported", "derived": derivedStatus},
		"rss":           {"search": "local_library_and_inbox", "detail": "cached_only", "subtitles": "unsupported", "social": "unsupported", "frames": "unsupported", "derived": derivedStatus},
		"subscriptions": {"search": "local_inbox", "detail": "cached_only", "subtitles": "unsupported", "social": "unsupported", "frames": "unsupported", "derived": derivedStatus},
		"huggingface":   {"search": "unsupported", "detail": "unsupported", "subtitles": "unsupported", "social": "unsupported", "frames": "unsupported", "derived": "unsupported"},
	}
	platforms := make([]agent.Capability, 0, len(features))
	platformNames := make([]string, 0, len(features))
	for platform := range features {
		platformNames = append(platformNames, platform)
	}
	sort.Strings(platformNames)
	for _, platform := range platformNames {
		capability := features[platform]
		login := map[string]bool{}
		reasons := map[string]string{}
		for name, status := range capability {
			if status == "unsupported" {
				reasons[name] = "当前 Agent 领域层没有该平台适配器。"
			}
		}
		if platform == "bilibili" || platform == "youtube" {
			login["detail"] = true
		}
		platforms = append(platforms, agent.Capability{Platform: platform, Features: capability, LoginRequired: login, Reasons: reasons})
	}
	return agent.CapabilityMatrix{ProtocolVersion: "1", Platforms: platforms}
}

func (p *agentProvider) Search(ctx context.Context, query, platform, cursor string, fetch bool) (agent.SearchResult, error) {
	if fetch {
		response, err := p.search.Search(ctx, query, discovery.SearchOptions{Platform: platform, Cursor: cursor})
		if err != nil {
			return agent.SearchResult{}, err
		}
		return agent.SearchResult{Query: response.Query, Fetched: true, Local: response.Local, Sources: response.Sources, Link: response.Link, Untrusted: true}, nil
	}
	page, err := p.services.features.content.Page(ctx, content.Query{Q: query, Platform: platform, Limit: 50})
	if err != nil {
		return agent.SearchResult{}, err
	}
	if platform == "" {
		inbox, inboxErr := p.services.features.api.inbox.List(ctx, subscriptions.InboxQuery{Query: query, Limit: 50})
		if inboxErr == nil {
			seen := make(map[string]bool, len(page.Items))
			for _, item := range page.Items {
				seen[item.ID] = true
			}
			for _, entry := range inbox.Entries {
				if !seen[entry.Item.ID] {
					page.Items = append(page.Items, entry.Item)
				}
			}
			page.Total = len(page.Items)
		}
	}
	return agent.SearchResult{Query: query, Local: page, Untrusted: true}, nil
}

func (p *agentProvider) Content(ctx context.Context, id string, fetch bool) (agent.ContentResult, error) {
	item, err := p.services.features.content.Get(ctx, id)
	if err == nil {
		return agent.ContentResult{Item: item, Source: "cache", Untrusted: true}, nil
	}
	if !errors.Is(err, content.ErrNotFound) {
		return agent.ContentResult{}, err
	}
	if !fetch {
		item, inboxErr := p.services.features.api.inbox.Item(ctx, id)
		if inboxErr != nil {
			return agent.ContentResult{}, content.ErrNotFound
		}
		return agent.ContentResult{Item: item, Source: "subscription_inbox", Untrusted: true}, nil
	}
	item, err = p.fetchContent(ctx, id)
	if err != nil {
		return agent.ContentResult{}, err
	}
	return agent.ContentResult{Item: item, Source: "platform", Fetched: true, Untrusted: true}, nil
}

func (p *agentProvider) fetchContent(ctx context.Context, id string) (content.Item, error) {
	platform, platformID, _ := strings.Cut(id, ":")
	switch platform {
	case "bilibili":
		return p.services.adapters.discovery.bilibili.Video(ctx, platformID)
	case "youtube":
		return p.services.adapters.network.toolkit.YouTube(ctx, "https://www.youtube.com/watch?v="+platformID)
	case "douyin":
		return p.services.adapters.network.toolkit.Douyin(ctx, "https://www.douyin.com/video/"+platformID)
	case "xiaoyuzhou":
		return p.services.adapters.discovery.podcasts.Episode(ctx, "https://www.xiaoyuzhoufm.com/episode/"+platformID)
	case "xiaohongshu":
		item, found, err := p.services.runtime.repositories.xiaohongshu.XiaohongshuDetail(ctx, id)
		if err != nil {
			return content.Item{}, err
		}
		if found {
			return item, nil
		}
		return content.Item{}, agent.ErrUnsupported
	default:
		return content.Item{}, agent.ErrUnsupported
	}
}

func (p *agentProvider) Inbox(ctx context.Context, query subscriptions.InboxQuery) (agent.InboxResult, error) {
	if query.Limit == 0 {
		query.Limit = 50
	}
	page, err := p.services.features.api.inbox.List(ctx, query)
	if err != nil {
		return agent.InboxResult{}, err
	}
	count, err := p.services.features.api.inbox.Count(ctx, query)
	if err != nil {
		return agent.InboxResult{}, err
	}
	return agent.InboxResult{Page: page, Count: count, Untrusted: true}, nil
}

func (p *agentProvider) Subtitles(ctx context.Context, id, language string, fetch bool) (agent.SubtitleResult, error) {
	if !fetch {
		tracks, err := p.services.features.api.savedSubtitles.List(ctx, id)
		if err != nil {
			return agent.SubtitleResult{}, err
		}
		if language != "" {
			filtered := tracks[:0]
			for _, track := range tracks {
				if track.Track.Language == language {
					filtered = append(filtered, track)
				}
			}
			tracks = filtered
		}
		if len(tracks) == 0 {
			return agent.SubtitleResult{Source: "cache", Untrusted: true}, agent.ErrUnsupported
		}
		doc := subtitles.Document{VideoID: id, Part: 1, Status: "available"}
		for _, saved := range tracks {
			doc.Tracks = append(doc.Tracks, saved.Track)
			doc.Languages = append(doc.Languages, subtitles.Language{Language: saved.Track.Language, Label: saved.Track.Label, Automatic: saved.Track.Automatic, Source: "cache", HasURL: false})
		}
		return agent.SubtitleResult{Document: doc, Source: "cache", Untrusted: true}, nil
	}
	platform, platformID, _ := strings.Cut(id, ":")
	switch platform {
	case "bilibili":
		return p.readBilibiliSubtitle(ctx, platformID, language)
	case "youtube":
		doc, err := p.services.features.api.subtitles.ReadYouTube(ctx, "https://www.youtube.com/watch?v="+platformID, language)
		if err != nil {
			return agent.SubtitleResult{}, err
		}
		return agent.SubtitleResult{Document: doc, Source: subtitleSource(doc.Status), Untrusted: true}, nil
	default:
		return agent.SubtitleResult{Source: "unsupported", Untrusted: true}, agent.ErrUnsupported
	}
}

func (p *agentProvider) readBilibiliSubtitle(ctx context.Context, id, language string) (agent.SubtitleResult, error) {
	query := id
	if language != "" {
		doc, err := p.services.features.api.subtitles.ReadLanguage(ctx, query, language)
		if err != nil {
			return agent.SubtitleResult{}, err
		}
		return agent.SubtitleResult{Document: doc, Source: subtitleSource(doc.Status), Untrusted: true}, nil
	}
	doc, err := p.services.features.api.subtitles.Read(ctx, query)
	if err != nil {
		return agent.SubtitleResult{}, err
	}
	return agent.SubtitleResult{Document: doc, Source: subtitleSource(doc.Status), Untrusted: true}, nil
}

func subtitleSource(status string) string {
	switch status {
	case "cc", "ai", "available":
		return "platform"
	default:
		return status
	}
}

func (p *agentProvider) Notes(ctx context.Context, query, contentID string) ([]notes.Note, error) {
	if contentID != "" {
		return p.services.features.api.notes.List(ctx, contentID)
	}
	page, err := p.services.features.api.notes.Search(ctx, query, "")
	if err != nil {
		return nil, err
	}
	return page.Notes, nil
}

func (p *agentProvider) Social(ctx context.Context, id string) (agent.SocialResult, error) {
	if !content.ValidID(id) {
		return agent.SocialResult{}, agent.ErrInvalid
	}
	platform, platformID, _ := strings.Cut(id, ":")
	if platform == "bilibili" {
		result, err := p.localBilibiliSocial(ctx, platformID)
		if err != nil || result.Status == "available" {
			return result, err
		}
	}
	if platform == "linuxdo" {
		result, err := p.cachedDiscussionSocial(ctx, id)
		if err != nil || result.Status == "available" {
			return result, err
		}
	}
	return agent.SocialResult{Status: "unsupported", Reason: "当前来源没有可读取的本地社交素材；此工具不会主动下载或联网抓取。", Untrusted: true}, nil
}

func (p *agentProvider) localBilibiliSocial(ctx context.Context, platformID string) (agent.SocialResult, error) {
	jobs, err := p.services.features.downloads.List(ctx)
	if err != nil {
		return agent.SocialResult{}, err
	}
	for _, job := range jobs {
		jobPlatform, jobID, _, parseErr := discovery.VideoURL(job.URL)
		if parseErr != nil || jobPlatform != "bilibili" || jobID != platformID || job.State != "completed" || job.Mode == "audio" {
			continue
		}
		for _, name := range job.Files {
			if !strings.HasSuffix(strings.ToLower(filepath.Base(name)), ".danmaku.xml") {
				continue
			}
			file, openErr := p.services.features.downloads.Artifact(ctx, job.ID, name)
			if errors.Is(openErr, downloads.ErrNotFound) {
				continue
			}
			if openErr != nil {
				return agent.SocialResult{}, openErr
			}
			info, statErr := file.Stat()
			if statErr != nil {
				_ = file.Close()
				return agent.SocialResult{}, statErr
			}
			if info.Size() <= 0 || info.Size() > maxAgentDanmakuXML {
				_ = file.Close()
				continue
			}
			data, readErr := io.ReadAll(io.LimitReader(file, maxAgentDanmakuXML+1))
			_ = file.Close()
			if readErr != nil {
				return agent.SocialResult{}, readErr
			}
			items, parseErr := parseAgentDanmaku(data)
			if parseErr != nil {
				return agent.SocialResult{}, parseErr
			}
			return agent.SocialResult{Status: "available", Source: "completed_local_download", Count: len(items), Items: items, Untrusted: true}, nil
		}
	}
	return agent.SocialResult{Status: "unsupported"}, nil
}

func (p *agentProvider) cachedDiscussionSocial(ctx context.Context, id string) (agent.SocialResult, error) {
	item, err := p.services.features.content.Get(ctx, id)
	if errors.Is(err, content.ErrNotFound) {
		item, err = p.services.features.api.inbox.Item(ctx, id)
	}
	if err != nil {
		if errors.Is(err, content.ErrNotFound) {
			return agent.SocialResult{Status: "unsupported", Reason: "缓存中没有该讨论内容。", Untrusted: true}, nil
		}
		return agent.SocialResult{}, err
	}
	if item.Discussion == nil {
		return agent.SocialResult{Status: "unsupported", Reason: "缓存内容没有讨论数据。", Untrusted: true}, nil
	}
	return cachedDiscussionSocialResult(item), nil
}

func cachedDiscussionSocialResult(item content.Item) agent.SocialResult {
	if item.Discussion == nil {
		return agent.SocialResult{Status: "unsupported", Reason: "缓存内容没有讨论数据。", Untrusted: true}
	}
	items := make([]agent.SocialItem, 0, min(len(item.Discussion.Posts), maxAgentSocialItems))
	for _, post := range item.Discussion.Posts {
		if len(items) >= maxAgentSocialItems {
			break
		}
		var textParts []string
		for _, block := range post.Blocks {
			if text := strings.TrimSpace(block.Text); text != "" {
				textParts = append(textParts, text)
			}
			if summary := strings.TrimSpace(block.Summary); summary != "" && summary != block.Text {
				textParts = append(textParts, summary)
			}
		}
		text := strings.TrimSpace(strings.Join(textParts, "\n"))
		if text == "" {
			continue
		}
		if utf8.RuneCountInString(text) > maxAgentDiscussionTextRunes {
			text = string([]rune(text)[:maxAgentDiscussionTextRunes])
		}
		items = append(items, agent.SocialItem{Text: text, Author: safeAgentSocialLabel(post.Name, post.Username), Likes: post.LikeCount, CreatedAt: post.CreatedAt, ReplyToNumber: post.ReplyToNumber, ReplyToUser: post.ReplyToUser, Mode: "discussion"})
	}
	if len(items) == 0 {
		return agent.SocialResult{Status: "unsupported", Reason: "缓存讨论没有可读取的正文。", Untrusted: true}
	}
	return agent.SocialResult{Status: "available", Source: "cached_discussion", Count: len(items), Items: items, Untrusted: true}
}

const maxAgentDiscussionTextRunes = 4000

func safeAgentSocialLabel(primary, fallback string) string {
	if value := strings.TrimSpace(primary); value != "" {
		return value
	}
	return strings.TrimSpace(fallback)
}

const (
	maxAgentDanmakuXML     = 16 << 20
	maxAgentDanmakuEntries = 100000
	maxAgentSocialItems    = 500
)

type agentDanmakuXML struct {
	Entries []struct {
		Params string `xml:"p,attr"`
		Text   string `xml:",chardata"`
	} `xml:"d"`
}

func parseAgentDanmaku(data []byte) ([]agent.SocialItem, error) {
	if len(data) == 0 || len(data) > maxAgentDanmakuXML {
		return nil, agent.ErrInvalid
	}
	var document agentDanmakuXML
	decoder := xml.NewDecoder(bytes.NewReader(data))
	decoder.Strict = true
	if err := decoder.Decode(&document); err != nil || len(document.Entries) > maxAgentDanmakuEntries {
		return nil, agent.ErrInvalid
	}
	items := make([]agent.SocialItem, 0, min(len(document.Entries), maxAgentSocialItems))
	for _, entry := range document.Entries {
		if len(items) >= maxAgentSocialItems {
			break
		}
		fields := strings.Split(entry.Params, ",")
		if len(fields) < 2 || !utf8.ValidString(entry.Text) {
			continue
		}
		position, err := strconv.ParseFloat(fields[0], 64)
		if err != nil || math.IsNaN(position) || math.IsInf(position, 0) || position < 0 || position > 604800 {
			continue
		}
		text := strings.TrimSpace(strings.NewReplacer("\r", " ", "\n", " ", "\t", " ").Replace(entry.Text))
		if text == "" || utf8.RuneCountInString(text) > 500 {
			continue
		}
		mode := "scroll"
		if modeID, err := strconv.Atoi(fields[1]); err == nil {
			switch modeID {
			case 4:
				mode = "bottom"
			case 5:
				mode = "top"
			case 7:
				mode = "positioned"
			}
		}
		items = append(items, agent.SocialItem{Text: text, PositionSeconds: position, Mode: mode})
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].PositionSeconds < items[j].PositionSeconds })
	return items, nil
}

func (p *agentProvider) CaptureFrames(ctx context.Context, id string, positions []float64) ([]agent.AgentImage, error) {
	if !content.ValidID(id) || len(positions) == 0 || len(positions) > 6 {
		return nil, agent.ErrInvalid
	}
	item, err := p.services.features.content.Get(ctx, id)
	if err != nil {
		item, err = p.services.features.api.inbox.Item(ctx, id)
	}
	if err != nil {
		return nil, agent.ErrUnsupported
	}
	path, err := p.services.features.downloads.CompletedPath(ctx, item.URL)
	if err != nil {
		return nil, agent.ErrUnsupported
	}
	result := make([]agent.AgentImage, 0, len(positions))
	for _, position := range positions {
		if math.IsNaN(position) || math.IsInf(position, 0) || position < 0 || position > 604800 {
			return nil, agent.ErrInvalid
		}
		data, err := p.services.adapters.network.toolkit.CaptureNoteFrame(ctx, path, position)
		if err != nil {
			return nil, err
		}
		result = append(result, agent.AgentImage{Data: data, MIMEType: "image/jpeg", Position: position, Source: "completed_local_download", Approximate: true, Untrusted: true})
	}
	return result, nil
}
