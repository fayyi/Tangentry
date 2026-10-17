package ai

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"
)

func normalizeSummaryInput(in SummaryInput) ([]SummarySegment, error) {
	if !contentIDValid(in.ContentID) || len(in.Source) < 2 || len(in.Source) > 80 || strings.ContainsAny(in.Source, "\r\n\x00") || in.Revision < 0 {
		return nil, ErrInvalid
	}
	if len(in.Segments) > 0 && in.Text != "" || len(in.Segments) == 0 && strings.TrimSpace(in.Text) == "" {
		return nil, ErrInvalid
	}
	if len(in.Segments) > 0 {
		if len(in.Segments) > 10000 {
			return nil, ErrInvalid
		}
		out := append([]SummarySegment(nil), in.Segments...)
		bytes := 0
		for index, segment := range out {
			if segment.Index != index || math.IsNaN(segment.Start) || math.IsNaN(segment.End) || math.IsInf(segment.Start, 0) || math.IsInf(segment.End, 0) || segment.Start < 0 || segment.End <= segment.Start || segment.End > 7*24*3600 || !utf8.ValidString(segment.Text) || strings.TrimSpace(segment.Text) == "" || len(segment.Text) > 8000 {
				return nil, ErrInvalid
			}
			if index > 0 && segment.Start < out[index-1].Start {
				return nil, ErrInvalid
			}
			bytes += len(segment.Text)
		}
		if bytes > maxSummaryTextBytes {
			return nil, ErrInvalid
		}
		return out, nil
	}
	if len(in.Text) > maxSummaryTextBytes || !utf8.ValidString(in.Text) {
		return nil, ErrInvalid
	}
	// Citations must remain exact substrings of the stored original. Preserve
	// paragraph separators and whitespace, preferring line boundaries when possible.
	segments := make([]SummarySegment, 0)
	for start := 0; start < len(in.Text); {
		end := min(start+summaryLeafBytes, len(in.Text))
		if end < len(in.Text) {
			for !utf8.RuneStart(in.Text[end]) {
				end--
			}
			if newline := strings.LastIndexByte(in.Text[start:end], '\n'); newline >= 0 {
				end = start + newline + 1
			}
		}
		part := in.Text[start:end]
		if strings.TrimSpace(part) != "" {
			segments = append(segments, SummarySegment{Index: len(segments), Text: part})
		}
		start = end
	}
	if len(segments) == 0 {
		return nil, ErrInvalid
	}
	return segments, nil
}

func contentIDValid(id string) bool {
	return len(id) > 3 && len(id) <= 300 && !strings.ContainsAny(id, "\r\n\x00")
}

// summaryKey identifies the summarized text and its timing, which citations
// carry, independent of content, provider and model.
func summaryKey(in SummaryInput, segments []SummarySegment, prompts ...string) string {
	prompt := defaultSummaryPrompt
	budget := 28000
	if len(prompts) > 0 && strings.TrimSpace(prompts[0]) != "" {
		prompt = strings.TrimSpace(prompts[0])
	}
	if len(prompts) > 1 {
		if value, err := strconv.Atoi(prompts[1]); err == nil {
			budget = value
		}
	}
	return hashSource(struct {
		Version  int
		Source   string
		Segments []SummarySegment
		Prompt   string
		Budget   int
	}{summaryPromptVersion, in.Source, segments, prompt, budget})
}

func summaryBudgetKey(budget int) string {
	if budget <= 0 {
		return ""
	}
	return strconv.Itoa(summaryContextBudget(budget))
}

// Earlier file caches were keyed by these identities.
func summaryIdentity(in SummaryInput, segments []SummarySegment, c Config) string {
	in.Revision = 0
	return legacySummaryIdentity(in, segments, c)
}

func legacySummaryIdentity(in SummaryInput, segments []SummarySegment, c Config) string {
	data, _ := json.Marshal(struct {
		Version        int
		Input          legacySummaryInput
		Segments       []SummarySegment
		BaseURL, Model string
	}{summaryPromptVersion, legacySummaryInput{in.ContentID, in.Source, in.Text, in.Segments, in.Revision}, segments, c.BaseURL, c.Model})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// legacySummaryInput keeps the field set legacy identities were hashed over.
type legacySummaryInput struct {
	ContentID string           `json:"contentId"`
	Source    string           `json:"source"`
	Text      string           `json:"text,omitempty"`
	Segments  []SummarySegment `json:"segments,omitempty"`
	Revision  int64            `json:"revision"`
}

func summaryPlan(segments []SummarySegment, budgets ...int) ([][]SummarySegment, int) {
	leafBytes := summaryLeafBytes
	if len(budgets) > 0 && budgets[0] > 0 {
		leafBytes = budgets[0]
	}
	groups := make([][]SummarySegment, 0)
	totalBytes := 0
	for _, segment := range segments {
		totalBytes += len(segment.Text)
	}
	if totalBytes <= leafBytes && len(segments) <= summaryLeafCount {
		return [][]SummarySegment{append([]SummarySegment(nil), segments...)}, 1
	}
	for start := 0; start < len(segments); {
		end, bytes := start, 0
		for end < len(segments) && end-start < summaryLeafCount && (end == start || bytes+len(segments[end].Text) <= leafBytes) {
			bytes += len(segments[end].Text)
			end++
		}
		groups = append(groups, append([]SummarySegment(nil), segments[start:end]...))
		start = end
	}
	calls := len(groups)
	for count := len(groups); count > 1; count = (count + summaryReduceCount - 1) / summaryReduceCount {
		calls += (count + summaryReduceCount - 1) / summaryReduceCount
	}
	return groups, calls
}

func validateDraft(draft SummaryDraft, allowed map[int]bool) bool {
	if !utf8.ValidString(draft.Overview) || strings.TrimSpace(draft.Overview) == "" || len(draft.Overview) > 1200 || len(draft.Points) == 0 || len(draft.Points) > 5 {
		return false
	}
	for _, point := range draft.Points {
		if !utf8.ValidString(point.Text) || strings.TrimSpace(point.Text) == "" || len(point.Text) > 600 || len(point.Indices) == 0 || len(point.Indices) > summaryPointIndexLimit {
			return false
		}
		seen := map[int]bool{}
		for _, index := range point.Indices {
			if !allowed[index] || seen[index] {
				return false
			}
			seen[index] = true
		}
	}
	if !validateOutline(draft.Outline, allowed, 0) {
		return false
	}
	return true
}

func validateOutline(nodes []SummaryOutline, allowed map[int]bool, parent int) bool {
	if len(nodes) > 128 {
		return false
	}
	for _, node := range nodes {
		if !utf8.ValidString(node.Title) || strings.TrimSpace(node.Title) == "" || len(node.Title) > 400 || node.Level < 0 || node.Level > 8 || node.Level < parent || len(node.Indices) > summaryPointIndexLimit {
			return false
		}
		if parent == 0 && node.Level != 0 {
			return false
		}
		seen := map[int]bool{}
		for _, index := range node.Indices {
			if !allowed[index] || seen[index] {
				return false
			}
			seen[index] = true
		}
		if !validateOutline(node.Children, allowed, node.Level+1) {
			return false
		}
	}
	return true
}

func buildCitations(draft SummaryDraft, segments []SummarySegment) []SummaryCitation {
	byIndex := make(map[int]SummarySegment, len(segments))
	for _, segment := range segments {
		byIndex[segment.Index] = segment
	}
	seen := map[int]bool{}
	out := make([]SummaryCitation, 0)
	for _, point := range draft.Points {
		for _, index := range point.Indices {
			if seen[index] {
				continue
			}
			seen[index] = true
			segment := byIndex[index]
			out = append(out, SummaryCitation{Index: index, Start: segment.Start, End: segment.End, Quote: segment.Text})
		}
	}
	return out
}

func validCachedSummary(result SummaryResult, segments []SummarySegment) bool {
	if !result.Complete {
		return true
	}
	allowed := make(map[int]bool, len(segments))
	for _, segment := range segments {
		allowed[segment.Index] = true
	}
	draft := SummaryDraft{Overview: result.Overview, Points: result.Points, Outline: result.Outline}
	if result.CompletedCalls != result.TotalCalls || !validateDraft(draft, allowed) {
		return false
	}
	expected := buildCitations(draft, segments)
	if len(expected) != len(result.Citations) {
		return false
	}
	for i, citation := range expected {
		if citation != result.Citations[i] {
			return false
		}
	}
	return true
}
