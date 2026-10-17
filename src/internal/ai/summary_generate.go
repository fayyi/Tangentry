package ai

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/longbridgeapp/opencc"
)

// SummaryGenerate coordinates one resumable summary operation. Cache encoding
// and migration stay behind summaryStore; this file only owns provider calls,
// checkpoints and reduction order.
func (s *Service) SummaryGenerate(parent context.Context, in SummaryInput) (SummaryResult, error) {
	segments, err := normalizeSummaryInput(in)
	if err != nil {
		return SummaryResult{}, err
	}
	if err = parent.Err(); err != nil {
		return SummaryResult{}, err
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return SummaryResult{}, ErrStorage
	}
	c, provider := s.config, s.summarizer
	if in.Revision != c.Revision {
		s.mu.Unlock()
		return SummaryResult{}, ErrConflict
	}
	if !s.publicStatus(c).SummaryReady || provider == nil {
		s.mu.Unlock()
		return SummaryResult{}, ErrNotReady
	}
	if s.active != "" {
		s.mu.Unlock()
		return SummaryResult{}, ErrBusy
	}
	c = summaryConfig(c)
	key := summaryKey(in, segments, c.SummaryPrompt, summaryBudgetKey(c.SummaryContextBudget))
	groups, totalCalls := summaryPlan(segments, summaryContextBudget(c.SummaryContextBudget))
	ctx, cancel := context.WithTimeout(parent, 45*time.Minute)
	s.active = "summary:" + key
	s.cancel = cancel
	s.wait.Add(1)
	s.mu.Unlock()
	defer func() {
		cancel()
		s.mu.Lock()
		s.active = ""
		s.cancel = nil
		s.mu.Unlock()
		s.wait.Done()
	}()
	fresh := summaryCache{Result: SummaryResult{Key: key, ContentID: in.ContentID, Source: in.Source, BaseURL: c.BaseURL, Model: c.Model, TotalCalls: totalCalls}, Current: []SummaryDraft{}}
	cache := fresh
	if in.Regenerate {
		if err := s.results.DeleteAIResult(ctx, kindSummary, key, targetChinese); err != nil {
			return SummaryResult{}, ErrStorage
		}
	} else if cache, err = s.summaryLoad(ctx, key, in, segments, c); errors.Is(err, ErrNotFound) {
		cache = fresh
	} else if err != nil {
		return SummaryResult{}, err
	}
	if cache.Result.TotalCalls != totalCalls || !validCachedSummary(cache.Result, segments) {
		return SummaryResult{}, ErrStorage
	}
	if cache.Result.Complete {
		return cache.Result, nil
	}
	cache.Result.BaseURL, cache.Result.Model = c.BaseURL, c.Model
	save := func(value summaryCache) error { return s.summaryWrite(ctx, value) }
	converter, err := opencc.New("t2s")
	if err != nil {
		return SummaryResult{}, ErrFormat
	}
	allowedOriginal := make(map[int]bool, len(segments))
	for _, segment := range segments {
		allowedOriginal[segment.Index] = true
	}
	if err := validateSummaryCheckpoint(cache, groups, allowedOriginal); err != nil {
		return SummaryResult{}, err
	}
	if err := s.generateSummaryLeaves(ctx, provider, c, converter, &cache, groups, save); err != nil {
		return SummaryResult{}, err
	}
	if err := s.reduceSummaryDrafts(ctx, provider, c, converter, &cache, segments, save); err != nil {
		return SummaryResult{}, err
	}
	if len(cache.Current) != 1 {
		return SummaryResult{}, ErrFormat
	}
	final := cache.Current[0]
	cache.Result.Complete = true
	cache.Result.Overview = final.Overview
	cache.Result.Points = final.Points
	cache.Result.Citations = buildCitations(final, segments)
	cache.Result.Outline = normalizeOutline(final.Outline, segments)
	if err := save(cache); err != nil {
		return SummaryResult{}, ErrStorage
	}
	s.mu.Lock()
	writer := s.saveSummaryNote
	s.mu.Unlock()
	if writer != nil {
		_ = writer(ctx, cache.Result)
	}
	return cache.Result, nil
}

func validateSummaryCheckpoint(cache summaryCache, groups [][]SummarySegment, allowedOriginal map[int]bool) error {
	for _, draft := range append(append([]SummaryDraft{}, cache.Current...), cache.Next...) {
		if !validateDraft(draft, allowedOriginal) {
			return ErrStorage
		}
	}
	if cache.Result.CompletedCalls <= len(groups) {
		if len(cache.Current) != cache.Result.CompletedCalls || len(cache.Next) != 0 {
			return ErrStorage
		}
		for i, draft := range cache.Current {
			allowed := make(map[int]bool)
			for _, segment := range groups[i] {
				allowed[segment.Index] = true
			}
			if !validateDraft(draft, allowed) {
				return ErrStorage
			}
		}
	}
	for i, draft := range cache.Next {
		start := i * summaryReduceCount
		end := start + summaryReduceCount
		if start >= len(cache.Current) {
			return ErrStorage
		}
		if end > len(cache.Current) {
			end = len(cache.Current)
		}
		allowed := make(map[int]bool)
		for _, previous := range cache.Current[start:end] {
			for _, point := range previous.Points {
				for _, index := range point.Indices {
					allowed[index] = true
				}
			}
			collectOutlineIndices(previous.Outline, allowed)
		}
		if !validateDraft(draft, allowed) {
			return ErrStorage
		}
	}
	if cache.Result.CompletedCalls >= len(groups) {
		count, completed := len(groups), len(groups)
		for count > len(cache.Current) && count > 1 {
			count = (count + summaryReduceCount - 1) / summaryReduceCount
			completed += count
		}
		if count != len(cache.Current) || cache.Result.CompletedCalls != completed+len(cache.Next) {
			return ErrStorage
		}
	}
	return nil
}

func (s *Service) generateSummaryLeaves(ctx context.Context, provider Summarizer, config Config, converter *opencc.OpenCC, cache *summaryCache, groups [][]SummarySegment, save func(summaryCache) error) error {
	for cache.Result.CompletedCalls < len(groups) {
		if len(cache.Current) != cache.Result.CompletedCalls || len(cache.Next) != 0 {
			return ErrStorage
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		group := groups[len(cache.Current)]
		allowed := map[int]bool{}
		for _, segment := range group {
			allowed[segment.Index] = true
		}
		batchCtx, batchCancel := context.WithTimeout(ctx, 90*time.Second)
		draft, callErr := provider.Summarize(batchCtx, config, SummaryRequest{Mode: "leaf", Prompt: summaryPrompt(config.SummaryPrompt), Segments: group})
		batchCancel()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if callErr != nil {
			return callErr
		}
		if !validateDraft(draft, allowed) {
			return ErrFormat
		}
		if err := convertSummaryDraft(converter, &draft); err != nil {
			return err
		}
		cache.Current = append(cache.Current, draft)
		cache.Result.CompletedCalls++
		if err := save(*cache); err != nil {
			return ErrStorage
		}
	}
	return nil
}

func (s *Service) reduceSummaryDrafts(ctx context.Context, provider Summarizer, config Config, converter *opencc.OpenCC, cache *summaryCache, segments []SummarySegment, save func(summaryCache) error) error {
	for len(cache.Current) > 1 {
		next := cache.Next
		if len(next) > (len(cache.Current)+summaryReduceCount-1)/summaryReduceCount {
			return ErrStorage
		}
		for start := len(next) * summaryReduceCount; start < len(cache.Current); start += summaryReduceCount {
			end := start + summaryReduceCount
			if end > len(cache.Current) {
				end = len(cache.Current)
			}
			drafts := cache.Current[start:end]
			allowed := map[int]bool{}
			for _, d := range drafts {
				for _, p := range d.Points {
					for _, index := range p.Indices {
						allowed[index] = true
					}
				}
				collectOutlineIndices(d.Outline, allowed)
			}
			batchCtx, batchCancel := context.WithTimeout(ctx, 90*time.Second)
			draft, callErr := provider.Summarize(batchCtx, config, SummaryRequest{Mode: "reduce", Prompt: summaryPrompt(config.SummaryPrompt), Drafts: drafts})
			batchCancel()
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if callErr != nil {
				return callErr
			}
			if !validateDraft(draft, allowed) {
				return ErrFormat
			}
			if err := convertSummaryDraft(converter, &draft); err != nil {
				return err
			}
			next = append(next, draft)
			cache.Result.CompletedCalls++
			cache.Next = next
			if err := save(*cache); err != nil {
				return ErrStorage
			}
		}
		cache.Current = next
		cache.Next = nil
		if err := save(*cache); err != nil {
			return ErrStorage
		}
	}
	return nil
}

func collectOutlineIndices(nodes []SummaryOutline, allowed map[int]bool) {
	for _, node := range nodes {
		for _, index := range node.Indices {
			allowed[index] = true
		}
		collectOutlineIndices(node.Children, allowed)
	}
}

func convertSummaryDraft(converter *opencc.OpenCC, draft *SummaryDraft) error {
	var err error
	draft.Overview, err = converter.Convert(strings.TrimSpace(draft.Overview))
	if err != nil {
		return ErrFormat
	}
	for i := range draft.Points {
		draft.Points[i].Text, _ = converter.Convert(strings.TrimSpace(draft.Points[i].Text))
	}
	return nil
}

func normalizeOutline(outline []SummaryOutline, segments []SummarySegment) []SummaryOutline {
	if len(outline) == 0 {
		return nil
	}
	byIndex := make(map[int]SummarySegment, len(segments))
	for _, segment := range segments {
		byIndex[segment.Index] = segment
	}
	var walk func([]SummaryOutline, int) []SummaryOutline
	walk = func(nodes []SummaryOutline, parent int) []SummaryOutline {
		result := make([]SummaryOutline, 0, len(nodes))
		for _, node := range nodes {
			node.Title = strings.TrimSpace(node.Title)
			if node.Title == "" || len(node.Title) > 400 || node.Level < 0 || node.Level > 8 || len(node.Indices) > summaryPointIndexLimit {
				continue
			}
			valid := true
			for _, index := range node.Indices {
				if _, ok := byIndex[index]; !ok {
					valid = false
					break
				}
			}
			if !valid {
				continue
			}
			if len(node.Indices) > 0 {
				first, last := byIndex[node.Indices[0]], byIndex[node.Indices[len(node.Indices)-1]]
				node.Start, node.End = first.Start, last.End
				node.Page = first.Page
			}
			node.Children = walk(node.Children, node.Level)
			result = append(result, node)
		}
		return result
	}
	return walk(outline, -1)
}
