package ai

import (
	"context"
	"errors"
)

const summaryPromptVersion = 1
const maxSummaryTextBytes = 1 << 20
const summaryLeafBytes = 7000
const summaryLeafCount = 24
const summaryReduceCount = 6
const summaryPointIndexLimit = 64

const defaultSummaryPrompt = "请用简体中文总结提供的内容，同时输出一句话总结、关键要点和层级大纲。大纲保留原文段落锚点；有时间戳时请关联时间戳。只引用输入中的索引，不要编造事实。"

// These small proxies keep package-level tests focused on summary behavior
// while the storage boundary remains isolated from the generation workflow.
func (s *Service) summaryRead(ctx context.Context, key string) (summaryCache, error) {
	return s.summaries.read(ctx, key)
}

func (s *Service) summaryWrite(ctx context.Context, cache summaryCache) error {
	return s.summaries.write(ctx, cache)
}

func (s *Service) SummaryInspect(ctx context.Context, in SummaryInput) (SummaryInspection, error) {
	if err := ctx.Err(); err != nil {
		return SummaryInspection{}, err
	}
	segments, err := normalizeSummaryInput(in)
	if err != nil {
		return SummaryInspection{}, err
	}
	s.mu.Lock()
	c, active := s.config, s.active
	status := s.publicStatus(c)
	c = summaryConfig(c)
	s.mu.Unlock()
	key := summaryKey(in, segments, c.SummaryPrompt, summaryBudgetKey(c.SummaryContextBudget))
	_, calls := summaryPlan(segments, summaryContextBudget(c.SummaryContextBudget))
	cache, err := s.summaryLoad(ctx, key, in, segments, c)
	if errors.Is(err, ErrNotFound) {
		cache.Result = SummaryResult{Key: key, ContentID: in.ContentID, Source: in.Source, BaseURL: c.BaseURL, Model: c.Model, TotalCalls: calls}
	} else if err != nil {
		return SummaryInspection{}, err
	}
	if cache.Result.TotalCalls != calls || !validCachedSummary(cache.Result, segments) {
		return SummaryInspection{}, ErrStorage
	}
	status.Translating = active != ""
	return SummaryInspection{Settings: status, Result: cache.Result, Active: active == "summary:"+key}, nil
}

// summaryLoad reads the stored summary of this text, adopting a matching
// legacy cache entry from any earlier model.
func (s *Service) summaryLoad(ctx context.Context, key string, in SummaryInput, segments []SummarySegment, c Config) (summaryCache, error) {
	return s.summaries.load(ctx, key, in, segments, c)
}

func (s *Service) SummaryCached(ctx context.Context, key string) (SummaryResult, error) {
	if !keyPattern.MatchString(key) {
		return SummaryResult{}, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return SummaryResult{}, err
	}
	cache, err := s.summaryRead(ctx, key)
	if err != nil {
		return SummaryResult{}, err
	}
	return cache.Result, nil
}
