package discovery

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"tangentry/internal/content"
)

var ErrInvalidBatch = errors.New("invalid discovery batch")

type BatchSourceStatus struct {
	Status               string `json:"status"`
	Message              string `json:"message,omitempty"`
	Restricted           bool   `json:"restricted,omitempty"`
	RequiresVerification bool   `json:"requiresVerification,omitempty"`
}

// BatchRequest describes a discovery stream request. Cursor is an opaque
// offset into a persisted snapshot; seed makes the exploration order stable.
type BatchRequest struct {
	View              string   `json:"view"`
	Size              int      `json:"size"`
	Topic             string   `json:"topic,omitempty"`
	Source            string   `json:"source,omitempty"`
	SubscriptionRatio int      `json:"subscriptionRatio"`
	ExplorationRatio  int      `json:"explorationRatio"`
	Cursor            string   `json:"cursor,omitempty"`
	Seed              int64    `json:"seed,omitempty"`
	UnreadIDs         []string `json:"unreadIds,omitempty"`
}

type BatchItem struct {
	Item        content.Item `json:"item"`
	Source      string       `json:"source"`
	Reason      string       `json:"reason"`
	CarriedOver bool         `json:"carriedOver"`
}

type BatchResponse struct {
	BatchID      string                       `json:"batchId"`
	NextCursor   string                       `json:"nextCursor,omitempty"`
	Items        []BatchItem                  `json:"items"`
	Reason       string                       `json:"reason"`
	HasNextBatch bool                         `json:"hasNextBatch"`
	EndReason    string                       `json:"endReason,omitempty"`
	SourceStatus map[string]BatchSourceStatus `json:"sourceStatus"`
}

// BatchCandidateProvider supplies one page from a source. Implementations may
// persist it in RecommendationQueue before returning, preserving account
// isolation and browsing marks.
type BatchCandidateProvider interface {
	Fetch(context.Context, string, int) (Feed, error)
}

type BatchService struct {
	provider      BatchCandidateProvider
	subscriptions func(context.Context) (Feed, error)
	sources       []string
	snapshots     BatchSnapshotRepository
	enabled       func(context.Context, string) (bool, error)
	topics        func(context.Context) ([]TopicRule, error)
	clock         func() time.Time
}

type BatchSnapshotRepository interface {
	PutDiscoveryBatch(context.Context, string, string, []byte, int64) error
	GetDiscoveryBatch(context.Context, string, string, int64) ([]byte, error)
}

type batchCursor struct {
	BatchID string `json:"batchId"`
	Offset  int    `json:"offset"`
	Key     string `json:"key"`
}

type batchCandidate struct {
	Item   content.Item `json:"item"`
	Source string       `json:"source"`
	Fresh  bool         `json:"fresh"`
	Key    string       `json:"key"`
}

type batchSnapshot struct {
	Candidates []batchCandidate             `json:"candidates"`
	Status     map[string]BatchSourceStatus `json:"status"`
	// NextPages keeps source pagination independent from the user-facing cursor.
	// A zero value means that the source has not been fetched beyond page one.
	NextPages map[string]int  `json:"nextPages,omitempty"`
	Exhausted map[string]bool `json:"exhausted,omitempty"`
}

func NewBatchService(provider BatchCandidateProvider, subscriptions func(context.Context) (Feed, error), sources []string, repositories ...BatchSnapshotRepository) *BatchService {
	var snapshots BatchSnapshotRepository
	if len(repositories) > 0 {
		snapshots = repositories[0]
	}
	return &BatchService{provider: provider, subscriptions: subscriptions, sources: append([]string(nil), sources...), snapshots: snapshots, clock: time.Now}
}

func (s *BatchService) SetSourceEnabled(enabled func(context.Context, string) (bool, error)) {
	s.enabled = enabled
}

func (s *BatchService) SetTopicRules(topics func(context.Context) ([]TopicRule, error)) {
	s.topics = topics
}

func (s *BatchService) Create(ctx context.Context, request BatchRequest) (BatchResponse, error) {
	if err := validateBatchRequest(&request); err != nil {
		return BatchResponse{}, err
	}
	if request.Seed == 0 {
		request.Seed = 1
	}
	sort.Strings(request.UnreadIDs)
	cursor, err := parseBatchCursor(request.Cursor)
	if err != nil {
		return BatchResponse{}, err
	}
	requestKey := batchRequestKey(request)
	if cursor.BatchID != "" {
		if cursor.Key != requestKey {
			return BatchResponse{}, ErrInvalidBatch
		}
		requestKey = cursor.Key
	}

	var snapshot batchSnapshot
	if cursor.BatchID != "" {
		if s.snapshots == nil {
			return BatchResponse{}, ErrUnavailable
		}
		payload, err := s.snapshots.GetDiscoveryBatch(ctx, cursor.BatchID, requestKey, s.clock().UnixMilli())
		if err != nil {
			return BatchResponse{}, err
		}
		if unmarshalBatchSnapshot(payload, &snapshot) != nil || cursor.Offset > len(snapshot.Candidates) {
			return BatchResponse{}, ErrInvalidBatch
		}
		if snapshot.NextPages == nil {
			snapshot.NextPages = map[string]int{}
		}
		if snapshot.Exhausted == nil {
			snapshot.Exhausted = map[string]bool{}
		}
		if cursor.Offset == len(snapshot.Candidates) && !allSourcesExhausted(snapshot) {
			if err := s.extendSnapshot(ctx, request, &snapshot); err != nil {
				return BatchResponse{}, err
			}
			if err := s.saveSnapshot(ctx, cursor.BatchID, requestKey, snapshot); err != nil {
				return BatchResponse{}, err
			}
		}
		return s.page(request, requestKey, cursor, snapshot), nil
	}

	all := make([]batchCandidate, 0, 128)
	status := make(map[string]BatchSourceStatus, len(s.sources)+1)
	seen := make(map[string]struct{})
	add := func(source, reason string, feed Feed, fresh bool) {
		for _, item := range feed.Items {
			if content.Validate(item) != nil {
				continue
			}
			if _, ok := seen[item.ID]; ok {
				continue
			}
			seen[item.ID] = struct{}{}
			all = append(all, batchCandidate{Item: item, Source: source, Fresh: fresh, Key: stableKey(request.Seed, item.ID) + "\x00" + reason})
		}
	}

	nextPages := make(map[string]int, len(s.sources))
	exhausted := make(map[string]bool, len(s.sources)+1)
	if s.subscriptions != nil && request.Source == "" {
		feed, fetchErr := s.subscriptions(ctx)
		if fetchErr != nil {
			status["subscriptions"] = sourceError(fetchErr)
			exhausted["subscriptions"] = true
		} else {
			status["subscriptions"] = BatchSourceStatus{Status: "ok"}
			add("subscriptions", "订阅优先", feed, false)
			// Subscription feeds are already complete snapshots, not page APIs.
			exhausted["subscriptions"] = true
		}
	}
	for _, source := range s.sources {
		if source == "subscriptions" {
			continue
		}
		if request.Source != "" && source != request.Source {
			continue
		}
		if s.enabled != nil {
			on, enabledErr := s.enabled(ctx, source)
			if enabledErr != nil {
				status[source] = sourceError(enabledErr)
				exhausted[source] = true
				continue
			}
			if !on {
				status[source] = BatchSourceStatus{Status: "disabled"}
				exhausted[source] = true
				continue
			}
		}
		feed, fetchErr := s.provider.Fetch(ctx, source, 1)
		if fetchErr != nil {
			status[source] = sourceError(fetchErr)
			exhausted[source] = true
			continue
		}
		status[source] = BatchSourceStatus{Status: "ok"}
		add(source, "来源更新", feed, true)
		if len(feed.Items) == 0 || !feed.HasMore {
			exhausted[source] = true
		} else {
			nextPages[source] = 2
		}
	}

	all = s.filterTopicCandidates(ctx, request, all)
	latest := append([]batchCandidate(nil), all...)
	sort.SliceStable(latest, func(i, j int) bool {
		if latest[i].Item.PublishedAt != latest[j].Item.PublishedAt {
			return latest[i].Item.PublishedAt > latest[j].Item.PublishedAt
		}
		return latest[i].Item.ID < latest[j].Item.ID
	})
	if request.View == "random" {
		sort.SliceStable(all, func(i, j int) bool { return all[i].Key < all[j].Key })
	}
	if request.View == "latest" {
		all = latest
	}
	if request.View == "topic" {
		all = latest
	}

	carried := make(map[string]struct{}, len(request.UnreadIDs))
	for _, id := range request.UnreadIDs {
		if content.ValidID(id) {
			carried[id] = struct{}{}
		}
	}
	all = selectBatchOrder(all, request, carried)
	snapshot = batchSnapshot{Candidates: all, Status: status, NextPages: nextPages, Exhausted: exhausted}
	// If every source starts empty but advertises a continuation, fetch one
	// continuation before exposing an empty stream.
	if len(snapshot.Candidates) == 0 && !allSourcesExhausted(snapshot) {
		if err := s.extendSnapshot(ctx, request, &snapshot); err != nil {
			return BatchResponse{}, err
		}
	}
	payload, err := json.Marshal(snapshot)
	if err != nil {
		return BatchResponse{}, err
	}
	batchID := stableKey(request.Seed, requestKey+digestCandidates(snapshot.Candidates))[:24]
	if s.snapshots != nil {
		if err := s.snapshots.PutDiscoveryBatch(ctx, batchID, requestKey, payload, s.clock().Add(24*time.Hour).UnixMilli()); err != nil {
			return BatchResponse{}, err
		}
	}
	return s.page(request, requestKey, batchCursor{BatchID: batchID}, snapshot), nil
}

func unmarshalBatchSnapshot(payload []byte, snapshot *batchSnapshot) error {
	if err := json.Unmarshal(payload, snapshot); err == nil {
		return nil
	}
	var legacy struct {
		Candidates []batchCandidate  `json:"candidates"`
		Status     map[string]string `json:"status"`
		NextPages  map[string]int    `json:"nextPages,omitempty"`
		Exhausted  map[string]bool   `json:"exhausted,omitempty"`
	}
	if err := json.Unmarshal(payload, &legacy); err != nil {
		return err
	}
	status := make(map[string]BatchSourceStatus, len(legacy.Status))
	for source, value := range legacy.Status {
		status[source] = BatchSourceStatus{Status: value}
	}
	*snapshot = batchSnapshot{Candidates: legacy.Candidates, Status: status, NextPages: legacy.NextPages, Exhausted: legacy.Exhausted}
	return nil
}

func (s *BatchService) extendSnapshot(ctx context.Context, request BatchRequest, snapshot *batchSnapshot) error {
	seen := make(map[string]struct{}, len(snapshot.Candidates))
	for _, candidate := range snapshot.Candidates {
		seen[candidate.Item.ID] = struct{}{}
	}
	add := func(source, reason string, feed Feed) {
		for _, item := range feed.Items {
			if content.Validate(item) != nil {
				continue
			}
			if _, ok := seen[item.ID]; ok {
				continue
			}
			seen[item.ID] = struct{}{}
			snapshot.Candidates = append(snapshot.Candidates, batchCandidate{
				Item: item, Source: source, Fresh: true,
				Key: stableKey(request.Seed, item.ID) + "\x00" + reason,
			})
		}
	}
	for _, source := range s.sources {
		if source == "subscriptions" || request.Source != "" && source != request.Source || snapshot.Exhausted[source] {
			continue
		}
		page := snapshot.NextPages[source]
		if page < 2 {
			page = 2
		}
		for attempts := 0; attempts < 100 && !snapshot.Exhausted[source]; attempts++ {
			feed, err := s.provider.Fetch(ctx, source, page)
			if err != nil {
				snapshot.Status[source] = sourceError(err)
				snapshot.Exhausted[source] = true
				break
			}
			snapshot.Status[source] = BatchSourceStatus{Status: "ok"}
			before := len(snapshot.Candidates)
			add(source, "来源更新", feed)
			if request.Topic != "" {
				newCandidates := s.filterTopicCandidates(ctx, request, snapshot.Candidates[before:])
				snapshot.Candidates = append(snapshot.Candidates[:before], newCandidates...)
			}
			snapshot.NextPages[source] = page + 1
			if len(feed.Items) == 0 || !feed.HasMore {
				snapshot.Exhausted[source] = true
			}
			if len(snapshot.Candidates) > before || snapshot.Exhausted[source] {
				break
			}
			page++
		}
	}
	return nil
}

func (s *BatchService) saveSnapshot(ctx context.Context, batchID, requestKey string, snapshot batchSnapshot) error {
	if s.snapshots == nil {
		return ErrUnavailable
	}
	payload, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	return s.snapshots.PutDiscoveryBatch(ctx, batchID, requestKey, payload, s.clock().Add(24*time.Hour).UnixMilli())
}

func (s *BatchService) filterTopicCandidates(ctx context.Context, request BatchRequest, candidates []batchCandidate) []batchCandidate {
	if request.Topic == "" {
		return candidates
	}
	terms := []string{strings.TrimSpace(request.Topic)}
	if s.topics != nil {
		if rules, topicErr := s.topics(ctx); topicErr == nil {
			for _, rule := range rules {
				if rule.ID == request.Topic || strings.EqualFold(rule.Name, request.Topic) {
					terms = rule.Keywords
					break
				}
			}
		}
	}
	lowerTerms := make([]string, 0, len(terms))
	for _, term := range terms {
		if term = strings.TrimSpace(term); term != "" {
			lowerTerms = append(lowerTerms, strings.ToLower(term))
		}
	}
	return filterCandidates(candidates, func(value batchCandidate) bool {
		text := strings.ToLower(strings.Join([]string{value.Item.Title, value.Item.Author, value.Item.Description, value.Item.PreviewText}, " "))
		for _, term := range lowerTerms {
			if strings.Contains(text, term) {
				return true
			}
		}
		return false
	})
}

func allSourcesExhausted(snapshot batchSnapshot) bool {
	if len(snapshot.Exhausted) == 0 {
		return true
	}
	for _, exhausted := range snapshot.Exhausted {
		if !exhausted {
			return false
		}
	}
	return true
}

func (s *BatchService) page(request BatchRequest, requestKey string, cursor batchCursor, snapshot batchSnapshot) BatchResponse {
	start := cursor.Offset
	end := min(start+request.Size, len(snapshot.Candidates))
	items := make([]BatchItem, 0, end-start)
	for _, value := range snapshot.Candidates[start:end] {
		reason := "最近更新"
		if value.Source == "subscriptions" {
			reason = "订阅优先"
		} else if request.View == "random" {
			reason = "少量随机探索"
		} else if request.View == "topic" {
			reason = "主题命中：" + request.Topic
		}
		carriedOver := contains(request.UnreadIDs, value.Item.ID)
		items = append(items, BatchItem{Item: value.Item, Source: value.Source, Reason: reason, CarriedOver: carriedOver})
	}
	response := BatchResponse{BatchID: cursor.BatchID, Items: items, Reason: batchReason(request), HasNextBatch: end < len(snapshot.Candidates) || !allSourcesExhausted(snapshot), SourceStatus: snapshot.Status}
	if response.HasNextBatch {
		response.NextCursor = encodeBatchCursor(batchCursor{BatchID: cursor.BatchID, Offset: end, Key: requestKey})
	} else {
		response.EndReason = "暂时没有更多了"
	}
	return response
}

func selectBatchOrder(candidates []batchCandidate, request BatchRequest, carried map[string]struct{}) []batchCandidate {
	newCandidates := make([]batchCandidate, 0, len(candidates))
	carryCandidates := make([]batchCandidate, 0, len(carried))
	for _, candidate := range candidates {
		if _, ok := carried[candidate.Item.ID]; ok {
			carryCandidates = append(carryCandidates, candidate)
		} else {
			newCandidates = append(newCandidates, candidate)
		}
	}
	carryLimit := request.Size * 30 / 100
	if len(carryCandidates) > carryLimit {
		carryCandidates = carryCandidates[:carryLimit]
	}
	result := append([]batchCandidate(nil), carryCandidates...)
	for start := 0; start < len(newCandidates); start += request.Size {
		end := min(start+request.Size, len(newCandidates))
		result = append(result, selectBatch(newCandidates[start:end], request)...)
	}
	return result
}

func selectBatch(candidates []batchCandidate, request BatchRequest) []batchCandidate {
	selected := make([]batchCandidate, 0, len(candidates))
	used := make(map[string]struct{}, len(candidates))
	subscriptionTarget := len(candidates) * request.SubscriptionRatio / 100
	explorationTarget := len(candidates) * request.ExplorationRatio / 100
	if request.View == "random" {
		explorationTarget = min(explorationTarget, len(candidates)/3)
	}
	lastAuthor, lastSource, authorStreak, sourceStreak := "", "", 0, 0
	appendCandidate := func(candidate batchCandidate) bool {
		if _, ok := used[candidate.Item.ID]; ok {
			return false
		}
		author := candidate.Item.AuthorID
		if author == "" {
			author = candidate.Item.Author
		}
		if author != "" && author == lastAuthor && authorStreak >= 2 || candidate.Source == lastSource && sourceStreak >= 2 {
			return false
		}
		selected = append(selected, candidate)
		used[candidate.Item.ID] = struct{}{}
		if author == lastAuthor {
			authorStreak++
		} else {
			lastAuthor, authorStreak = author, 1
		}
		if candidate.Source == lastSource {
			sourceStreak++
		} else {
			lastSource, sourceStreak = candidate.Source, 1
		}
		return true
	}
	for _, candidate := range candidates {
		if candidate.Source == "subscriptions" && countSource(selected, "subscriptions") < subscriptionTarget {
			appendCandidate(candidate)
		}
	}
	exploration := 0
	for _, candidate := range candidates {
		if candidate.Source != "subscriptions" && candidate.Fresh && exploration < explorationTarget && appendCandidate(candidate) {
			exploration++
		}
	}
	for _, candidate := range candidates {
		appendCandidate(candidate)
	}
	// If diversity constraints leave a short page, use the remaining stable
	// order rather than pretending the candidate pool is empty.
	for _, candidate := range candidates {
		if _, ok := used[candidate.Item.ID]; ok {
			continue
		}
		selected = append(selected, candidate)
		used[candidate.Item.ID] = struct{}{}
	}
	return selected
}

func validateBatchRequest(request *BatchRequest) error {
	if request.View == "" {
		request.View = "wander"
	}
	if request.View != "wander" && request.View != "latest" && request.View != "topic" && request.View != "random" {
		return ErrInvalidBatch
	}
	if request.Size == 0 {
		request.Size = 15
	}
	if request.Size != 10 && request.Size != 15 && request.Size != 25 {
		return ErrInvalidBatch
	}
	if request.SubscriptionRatio == 0 {
		request.SubscriptionRatio = 60
	}
	if request.ExplorationRatio == 0 {
		request.ExplorationRatio = 15
	}
	if request.SubscriptionRatio < 0 || request.SubscriptionRatio > 100 || request.ExplorationRatio < 0 || request.ExplorationRatio > 30 || request.SubscriptionRatio+request.ExplorationRatio > 100 {
		return ErrInvalidBatch
	}
	if request.View == "topic" && strings.TrimSpace(request.Topic) == "" {
		return ErrInvalidBatch
	}
	if request.Source != "" && !RecommendationPlatform(request.Source) {
		return ErrInvalidBatch
	}
	if len(request.UnreadIDs) > 100 {
		return ErrInvalidBatch
	}
	return nil
}

func parseBatchCursor(raw string) (batchCursor, error) {
	if raw == "" {
		return batchCursor{}, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	var cursor batchCursor
	if err != nil || json.Unmarshal(decoded, &cursor) != nil || cursor.BatchID == "" || cursor.Offset < 0 || cursor.Key == "" {
		return batchCursor{}, ErrInvalidBatch
	}
	return cursor, nil
}

func filterCandidates[T any](values []T, keep func(T) bool) []T {
	result := make([]T, 0, len(values))
	for _, value := range values {
		if keep(value) {
			result = append(result, value)
		}
	}
	return result
}

func stableKey(seed int64, id string) string {
	sum := sha256.Sum256([]byte(strconv.FormatInt(seed, 10) + ":" + id))
	return hex.EncodeToString(sum[:])
}

func digestCandidates(items []batchCandidate) string {
	parts := make([]string, len(items))
	for index, item := range items {
		parts[index] = item.Item.ID
	}
	return stableKey(0, strings.Join(parts, ","))[:16]
}

func encodeBatchCursor(cursor batchCursor) string {
	raw, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func batchRequestKey(request BatchRequest) string {
	request.Cursor = ""
	request.UnreadIDs = nil
	raw, _ := json.Marshal(request)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func countSource(values []batchCandidate, source string) int {
	count := 0
	for _, value := range values {
		if value.Source == source {
			count++
		}
	}
	return count
}

func sourceError(err error) BatchSourceStatus {
	status := BatchSourceStatus{Status: "error", Message: "来源获取失败，请稍后重试。"}
	var platform *PlatformError
	if errors.As(err, &platform) {
		status.Status = "unavailable"
		status.Message = platform.Message
		status.Restricted = platform.Restricted
		status.RequiresVerification = platform.Restricted || requiresVerification(platform.Message)
		return status
	}
	if errors.Is(err, ErrUnavailable) {
		status.Status = "unavailable"
	}
	return status
}

func requiresVerification(message string) bool {
	return strings.Contains(strings.ToLower(message), "cookie") || strings.Contains(message, "登录") || strings.Contains(message, "验证") || strings.Contains(message, "cloudflare")
}

func batchReason(request BatchRequest) string {
	switch request.View {
	case "latest":
		return "按最近更新时间整理"
	case "topic":
		return "围绕主题“" + request.Topic + "”"
	case "random":
		return "保留订阅主线，加入少量随机探索"
	default:
		return "订阅优先，兼顾来源与创作者多样性"
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
