package ai

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"unicode/utf8"
)

const summaryPromptVersion = 3
const maxSummaryTextBytes = 1 << 20
const summaryLeafBytes = 7000
const summaryLeafCount = 24
const summaryReduceCount = 6
const summaryPointIndexLimit = 64

const defaultSummaryPrompt = "请用简体中文总结提供的内容，并按 TextFold 式内容树输出 Topic、Takeaway、Key、Brief、Full 五级信息。只输出最终摘要，不要提及提示词、模型、生成过程或中间结果。Full 必须保留输入原文，Brief 只能选择 Full 中的原文句子；每个 AI 结论都要引用真实输入索引。Takeaway 是一句结论；Key 必须是独立的‘结论 + 主要依据、机制或因果关系’，不能复制 Takeaway，也不能只改写标题；如果无法提炼独立依据就留空，不要伪造。请同时输出 overview、key_points（或 points）、outline，以及 document（可选）。document 的 sections/units 应按来源顺序组织，包含稳定的 sectionId、unitId、topic、takeaway、key、full、sentences、briefSentenceIds 和 evidence。大纲至少输出一个一级节点；有时间戳或页码时请关联。不要编造事实、索引、时间戳或引用。"

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
	key := summaryKey(in, segments, SummaryCacheSystemPrompt(c), summaryBudgetKey(c.SummaryContextBudget), SummaryCacheUserPrompt(c))
	groups, calls := summaryPlan(segments, summaryContextBudget(c.SummaryContextBudget))
	cache, err := s.summaryLoad(ctx, key, in, segments, c)
	if errors.Is(err, ErrNotFound) {
		cache.Result = SummaryResult{Key: key, ContentID: in.ContentID, Source: in.Source, BaseURL: c.BaseURL, Model: c.Model, TotalCalls: calls, FormatVersion: summaryFormatVersion, Progress: SummaryProgress{TotalCalls: calls, TotalSections: summarySectionCount(segments, in.Source, groups)}}
	} else if err != nil {
		return SummaryInspection{}, err
	}
	if cache.Result.TotalCalls != calls || !validCachedSummary(cache.Result, segments) {
		return SummaryInspection{}, ErrStorage
	}
	if cache.Result.Complete && cache.Result.FormatVersion != summaryFormatVersion {
		invalidateLegacySummaryResult(&cache.Result, calls, summarySectionCount(segments, in.Source, groups))
	}
	// A process restart can leave a checkpoint marked as in-flight even though
	// no request is running anymore. Do not present that stale state as an
	// active spinner; the next generation call will resume from the checkpoint.
	if !cache.Result.Complete && active != "summary:"+key && cache.Result.Progress.CurrentCall > 0 {
		cache.Result.Progress.CurrentCall = 0
		cache.Result.Progress.StartedAt = 0
		if cache.Result.Progress.Phase != "error" {
			cache.Result.Progress.Phase = "checkpoint"
			cache.Result.Progress.Error = "上次整理未完成，可继续生成；已完成内容已保留。"
		}
	}
	normalizeSummaryDocumentLevels(cache.Result.Document)
	if err := s.applySummaryTitleOverrides(ctx, &cache.Result); err != nil {
		return SummaryInspection{}, err
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
	result := cache.Result
	if result.Complete && result.FormatVersion != summaryFormatVersion {
		invalidateLegacySummaryResult(&result, result.TotalCalls, 0)
	}
	normalizeSummaryDocumentLevels(result.Document)
	if err := s.applySummaryTitleOverrides(ctx, &result); err != nil {
		return SummaryResult{}, err
	}
	return result, nil
}

// normalizeSummaryDocumentLevels keeps older generated trees from pretending
// that a copied Takeaway is an independently extracted Key or Reason.
func normalizeSummaryDocumentLevels(document *SummaryDocument) {
	if document == nil {
		return
	}
	var walk func([]SummarySection)
	walk = func(sections []SummarySection) {
		for index := range sections {
			for unitIndex := range sections[index].Units {
				unit := &sections[index].Units[unitIndex]
				normalizeSummaryUnitLevels(unit)
			}
			walk(sections[index].Children)
		}
	}
	walk(document.Sections)
}

// normalizeSummaryUnitLevels repairs older model output that copied Takeaway
// into Key. A Key without a distinct claim is not useful at the zoomed-in
// level, so derive a small, source-backed explanation from the unit evidence
// instead of showing the conclusion twice or inventing a new fact.
func normalizeSummaryUnitLevels(unit *SummaryUnit) {
	if unit == nil {
		return
	}
	takeaway := strings.TrimSpace(unit.Takeaway)
	key := strings.TrimSpace(unit.Key)
	reason := strings.TrimSpace(unit.Reason)
	if reason == takeaway {
		reason = ""
		unit.Reason = ""
	}
	if key != "" && key != takeaway {
		return
	}
	if takeaway == "" {
		unit.Key = ""
		return
	}
	if reason != "" {
		unit.Key = takeaway + "；依据是" + reason
		return
	}
	if evidence := summaryUnitReasonEvidence(*unit); evidence != "" {
		unit.Key = takeaway + "；依据是" + evidence
		return
	}
	unit.Key = ""
}

func summaryUnitReasonEvidence(unit SummaryUnit) string {
	type candidate struct {
		text  string
		score int
		order int
	}
	markers := []string{"因为", "所以", "由于", "因此", "否则", "缺点", "优点", "好处", "成本", "适合", "影响", "导致", "通过", "利用", "除了", "其他", "才能", "会让", "不易"}
	candidates := make([]candidate, 0, len(unit.Sentences)+len(unit.Evidence))
	seen := map[string]bool{}
	add := func(text string, order int) {
		text = strings.TrimSpace(text)
		if text == "" || text == strings.TrimSpace(unit.Takeaway) || seen[text] {
			return
		}
		seen[text] = true
		score := 0
		for _, marker := range markers {
			if strings.Contains(text, marker) {
				score += 3
			}
		}
		if len([]rune(text)) >= 8 {
			score++
		}
		candidates = append(candidates, candidate{text: text, score: score, order: order})
	}
	for index, sentence := range unit.Sentences {
		add(sentence.Text, index)
	}
	for index, evidence := range unit.Evidence {
		add(evidence.Quote, len(unit.Sentences)+index)
	}
	if len(candidates) == 0 {
		return ""
	}
	sort.SliceStable(candidates, func(left, right int) bool {
		if candidates[left].score != candidates[right].score {
			return candidates[left].score > candidates[right].score
		}
		return candidates[left].order < candidates[right].order
	})
	return candidates[0].text
}

func invalidateLegacySummaryResult(result *SummaryResult, totalCalls, totalSections int) {
	if result == nil {
		return
	}
	result.Complete = false
	result.CompletedCalls = 0
	result.Overview = ""
	result.Points = nil
	result.Citations = nil
	result.Outline = nil
	result.Document = nil
	result.FormatVersion = summaryFormatVersion
	result.Progress = SummaryProgress{TotalCalls: totalCalls, TotalSections: totalSections}
}

func (s *Service) SummaryTitleOverrides(ctx context.Context, key string) (SummaryTitleOverrides, error) {
	if !keyPattern.MatchString(key) {
		return SummaryTitleOverrides{}, ErrInvalid
	}
	row, err := s.results.AIResult(ctx, kindSummaryTitle, key, targetChinese)
	if errors.Is(err, ErrNotFound) {
		return SummaryTitleOverrides{Key: key, Overrides: []SummaryTitleOverride{}}, nil
	}
	if err != nil || len(row.Value) > 128<<10 {
		return SummaryTitleOverrides{}, ErrStorage
	}
	var result SummaryTitleOverrides
	if json.Unmarshal([]byte(row.Value), &result) != nil || result.Key != key || !validSummaryTitleOverrides(result.Overrides) {
		return SummaryTitleOverrides{}, ErrStorage
	}
	return result, nil
}

func (s *Service) PutSummaryTitleOverride(ctx context.Context, key string, override SummaryTitleOverride) (SummaryTitleOverrides, error) {
	if !keyPattern.MatchString(key) || !validSummaryTitleOverride(override) {
		return SummaryTitleOverrides{}, ErrInvalid
	}
	current, err := s.SummaryTitleOverrides(ctx, key)
	if err != nil {
		return SummaryTitleOverrides{}, err
	}
	updated := false
	for index := range current.Overrides {
		if current.Overrides[index].NodeID == override.NodeID {
			current.Overrides[index] = override
			updated = true
			break
		}
	}
	if !updated {
		current.Overrides = append(current.Overrides, override)
	}
	return s.writeSummaryTitleOverrides(ctx, key, current)
}

func (s *Service) DeleteSummaryTitleOverride(ctx context.Context, key, nodeID string) (SummaryTitleOverrides, error) {
	if !keyPattern.MatchString(key) || !validSummaryNodeID(nodeID) {
		return SummaryTitleOverrides{}, ErrInvalid
	}
	current, err := s.SummaryTitleOverrides(ctx, key)
	if err != nil {
		return SummaryTitleOverrides{}, err
	}
	filtered := current.Overrides[:0]
	for _, override := range current.Overrides {
		if override.NodeID != nodeID {
			filtered = append(filtered, override)
		}
	}
	current.Overrides = filtered
	return s.writeSummaryTitleOverrides(ctx, key, current)
}

func (s *Service) writeSummaryTitleOverrides(ctx context.Context, key string, result SummaryTitleOverrides) (SummaryTitleOverrides, error) {
	value, err := json.Marshal(result)
	if err != nil || len(value) > 128<<10 {
		return SummaryTitleOverrides{}, ErrStorage
	}
	if err := s.results.PutAIResult(ctx, StoredResult{Kind: kindSummaryTitle, SourceHash: key, Target: targetChinese, Value: string(value)}); err != nil {
		return SummaryTitleOverrides{}, ErrStorage
	}
	return result, nil
}

func (s *Service) applySummaryTitleOverrides(ctx context.Context, result *SummaryResult) error {
	if result == nil || result.Document == nil {
		return nil
	}
	overrides, err := s.SummaryTitleOverrides(ctx, result.Key)
	if err != nil {
		return err
	}
	byID := make(map[string]string, len(overrides.Overrides))
	for _, override := range overrides.Overrides {
		byID[override.NodeID] = override.Title
	}
	var apply func([]SummarySection)
	apply = func(sections []SummarySection) {
		for index := range sections {
			if title, ok := byID[sections[index].SectionID]; ok {
				sections[index].Title = title
			}
			for unitIndex := range sections[index].Units {
				if title, ok := byID[sections[index].Units[unitIndex].UnitID]; ok {
					sections[index].Units[unitIndex].Topic = title
				}
			}
			apply(sections[index].Children)
		}
	}
	apply(result.Document.Sections)
	return nil
}

func validSummaryTitleOverrides(overrides []SummaryTitleOverride) bool {
	if len(overrides) > 256 {
		return false
	}
	seen := make(map[string]bool, len(overrides))
	for _, override := range overrides {
		if !validSummaryTitleOverride(override) || seen[override.NodeID] {
			return false
		}
		seen[override.NodeID] = true
	}
	return true
}

func validSummaryTitleOverride(override SummaryTitleOverride) bool {
	return validSummaryNodeID(override.NodeID) && utf8.ValidString(override.Title) && strings.TrimSpace(override.Title) != "" && len(override.Title) <= 400
}

func validSummaryNodeID(value string) bool {
	return utf8.ValidString(value) && strings.TrimSpace(value) != "" && len(value) <= 200 && !strings.ContainsAny(value, "\r\n\x00")
}
