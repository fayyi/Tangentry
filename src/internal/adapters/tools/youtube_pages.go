package tools

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"time"

	"tangentry/internal/discovery"
)

type youtubePageState struct {
	account [32]byte
	expires time.Time
	pages   map[int]discovery.Feed
	next    json.RawMessage
}

// Continuations stay private to the account and process, never in browser URLs.
func (t *Toolkit) YouTubeRecommendationPage(ctx context.Context, page int) (discovery.Feed, error) {
	if err := ctx.Err(); err != nil {
		return discovery.Feed{}, err
	}
	if page < 1 || page > 64 {
		return discovery.Feed{}, discovery.ErrInvalidQuery
	}
	if !t.youtubeMu.TryLock() {
		return discovery.Feed{}, &discovery.PlatformError{Message: "YouTube 推荐正在获取，请稍后重试。"}
	}
	defer t.youtubeMu.Unlock()
	cookie := ""
	if t.credentials != nil {
		cookie = t.credentials.Header("youtube", "https://www.youtube.com/youtubei/v1/browse")
	}
	account := sha256.Sum256([]byte(cookie))
	for key, cached := range t.youtubeSessions {
		if cached.account != account || time.Now().After(cached.expires) {
			delete(t.youtubeSessions, key)
		}
	}
	state := &t.youtubePages
	if session := discovery.RecommendationSession(ctx); session != "" {
		if t.youtubeSessions == nil {
			t.youtubeSessions = map[string]*youtubePageState{}
		}
		state = t.youtubeSessions[session]
		if state == nil {
			if len(t.youtubeSessions) >= 8 {
				return discovery.Feed{}, &discovery.PlatformError{Message: "推荐浏览会话较多，请稍后刷新重试。"}
			}
			state = &youtubePageState{}
			t.youtubeSessions[session] = state
		}
	}
	if state.account != account || time.Now().After(state.expires) {
		*state = youtubePageState{account: account, pages: map[int]discovery.Feed{}}
	}
	// A failed refresh must leave the currently displayed feed continuable.
	working := state
	if page == 1 {
		working = &youtubePageState{account: account, pages: map[int]discovery.Feed{}}
	}
	if page > 1 {
		if cached, ok := state.pages[page]; ok {
			return cached, nil
		}
		if page != len(state.pages)+1 || len(state.next) == 0 {
			return discovery.Feed{}, &discovery.PlatformError{Message: "YouTube 推荐会话已过期或没有下一页，请刷新推荐。"}
		}
	}
	var feed discovery.Feed
	var next json.RawMessage
	var err error
	feed, next, err = t.youTubeHomeBatch(ctx, working.next)
	if err != nil {
		return discovery.Feed{}, err
	}
	if err := ctx.Err(); err != nil {
		return discovery.Feed{}, err
	}
	currentCookie := ""
	if t.credentials != nil {
		currentCookie = t.credentials.Header("youtube", "https://www.youtube.com/youtubei/v1/browse")
	}
	currentAccount := sha256.Sum256([]byte(currentCookie))
	if currentAccount != account {
		*state = youtubePageState{account: currentAccount, pages: map[int]discovery.Feed{}}
		return discovery.Feed{}, &discovery.PlatformError{Message: "YouTube 登录信息已变化，请刷新推荐。"}
	}
	feed.Page = page
	if page == 64 {
		next = nil
		feed.HasMore = false
	}
	working.pages[page] = feed
	working.next = next
	if page == 1 {
		working.expires = time.Now().Add(19 * time.Minute)
		*state = *working
	}
	return feed, nil
}
