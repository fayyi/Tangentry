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
	if len(in.Segments) > 0 && (in.Text != "" || len(in.Blocks) > 0) || len(in.Blocks) > 0 && (in.Text != "" || len(in.Segments) > 0) || len(in.Segments) == 0 && len(in.Blocks) == 0 && strings.TrimSpace(in.Text) == "" {
		return nil, ErrInvalid
	}
	if len(in.Segments) > 0 {
		if len(in.Segments) > 10000 {
			return nil, ErrInvalid
		}
		out := append([]SummarySegment(nil), in.Segments...)
		bytes := 0
		for index, segment := range out {
			if segment.Index != index || segment.Page < 0 || math.IsNaN(segment.Start) || math.IsNaN(segment.End) || math.IsInf(segment.Start, 0) || math.IsInf(segment.End, 0) || segment.Start < 0 || segment.End < segment.Start || segment.End > 7*24*3600 || !utf8.ValidString(segment.Text) || strings.TrimSpace(segment.Text) == "" || len(segment.Text) > 8000 {
				return nil, ErrInvalid
			}
			if len(segment.CueIDs) > 0 {
				seenCueIDs := make(map[string]bool, len(segment.CueIDs))
				for _, cueID := range segment.CueIDs {
					if !utf8.ValidString(cueID) || strings.TrimSpace(cueID) == "" || len(cueID) > 200 || seenCueIDs[cueID] {
						return nil, ErrInvalid
					}
					seenCueIDs[cueID] = true
				}
			}
			if index > 0 && segment.Page == 0 && out[index-1].Page == 0 && segment.Start < out[index-1].Start {
				return nil, ErrInvalid
			}
			bytes += len(segment.Text)
		}
		if bytes > maxSummaryTextBytes {
			return nil, ErrInvalid
		}
		return out, nil
	}
	if len(in.Blocks) > 0 {
		segments := make([]SummarySegment, 0, len(in.Blocks))
		globalIndexes := make(map[int]bool)
		lastSourceIndex := -1
		for _, block := range in.Blocks {
			if !utf8.ValidString(block.Kind) || len(block.Kind) > 40 || strings.ContainsAny(block.Kind, "\r\n\x00") || !utf8.ValidString(block.Heading) || !utf8.ValidString(block.Text) || strings.TrimSpace(block.Text) == "" || len(block.Text) > 8000 || block.Page < 0 {
				return nil, ErrInvalid
			}
			text := strings.TrimSpace(block.Text)
			if strings.TrimSpace(block.Heading) != "" && strings.TrimSpace(block.Heading) != text {
				text = strings.TrimSpace(block.Heading) + "\n" + text
			}
			indexes := append([]int(nil), block.SegmentIndexes...)
			if len(indexes) == 0 {
				nextIndex := len(segments)
				if lastSourceIndex >= 0 {
					nextIndex = lastSourceIndex + 1
				}
				indexes = []int{nextIndex}
			}
			seenIndexes := make(map[int]bool, len(indexes))
			for _, value := range indexes {
				if value < 0 || seenIndexes[value] || globalIndexes[value] || (lastSourceIndex >= 0 && value < lastSourceIndex) {
					return nil, ErrInvalid
				}
				seenIndexes[value] = true
				globalIndexes[value] = true
				lastSourceIndex = value
			}
			segments = append(segments, SummarySegment{Index: len(segments), Page: block.Page, Kind: block.Kind, Heading: block.Heading, SegmentIndexes: indexes, Text: text})
		}
		if len(segments) == 0 {
			return nil, ErrInvalid
		}
		return segments, nil
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
	userPrompt := ""
	if len(prompts) > 0 && strings.TrimSpace(prompts[0]) != "" {
		prompt = strings.TrimSpace(prompts[0])
	}
	if len(prompts) > 1 {
		if value, err := strconv.Atoi(prompts[1]); err == nil {
			budget = value
		}
	}
	if len(prompts) > 2 {
		userPrompt = strings.TrimSpace(prompts[2])
	}
	return hashSource(struct {
		Version    int
		Source     string
		Segments   []SummarySegment
		Prompt     string
		UserPrompt string
		Budget     int
	}{summaryPromptVersion, in.Source, segments, prompt, userPrompt, budget})
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

// summarySectionCount provides a deterministic progress denominator before the
// model has produced the final outline. Structured blocks and subtitle cues
// already have source-derived chapter boundaries; plain prose falls back to
// the resumable leaf batches until the reducer can publish the final count.
func summarySectionCount(segments []SummarySegment, source string, groups [][]SummarySegment) int {
	if chapterGroups := structuredSectionGroups(segments, source); len(chapterGroups) > 0 {
		return len(chapterGroups)
	}
	if isSubtitleSource(source) {
		if chapters := subtitleChapterIndices(segments); len(chapters) > 0 {
			return len(chapters)
		}
	}
	return len(groups)
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
			out = append(out, SummaryCitation{Index: index, Start: segment.Start, End: segment.End, Page: segment.Page, Quote: segment.Text})
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
	if result.Document != nil && !validSummaryDocument(result.Document, segments) {
		return false
	}
	return true
}

func validSummaryDocument(document *SummaryDocument, segments []SummarySegment) bool {
	if document == nil || document.FormatVersion != summaryFormatVersion || len(document.Levels) != len(summaryLevels) {
		return false
	}
	for index, level := range summaryLevels {
		if document.Levels[index] != level {
			return false
		}
	}
	allowed := make(map[int]SummarySegment, len(segments))
	for _, segment := range segments {
		allowed[segment.Index] = segment
	}
	seenSections := make(map[string]bool)
	seenUnits := make(map[string]bool)
	var walk func([]SummarySection) bool
	walk = func(sections []SummarySection) bool {
		if len(sections) > 256 {
			return false
		}
		for _, section := range sections {
			if strings.TrimSpace(section.SectionID) == "" || seenSections[section.SectionID] || strings.TrimSpace(section.Title) == "" || section.Level < 0 || section.Level > 8 {
				return false
			}
			seenSections[section.SectionID] = true
			for _, evidence := range append(append([]SummaryEvidence{}, section.Source...), section.Evidence...) {
				if !validSummaryEvidence(evidence, allowed) {
					return false
				}
			}
			for _, unit := range section.Units {
				if strings.TrimSpace(unit.UnitID) == "" || seenUnits[unit.UnitID] || strings.TrimSpace(unit.Full) == "" {
					return false
				}
				seenUnits[unit.UnitID] = true
				for _, evidence := range append(append([]SummaryEvidence{}, unit.Source...), unit.Evidence...) {
					if !validSummaryEvidence(evidence, allowed) {
						return false
					}
				}
				for _, sentence := range unit.Sentences {
					if strings.TrimSpace(sentence.ID) == "" || strings.TrimSpace(sentence.Text) == "" || !strings.Contains(unit.Full, sentence.Text) {
						return false
					}
					for _, evidence := range sentence.Evidence {
						if !validSummaryEvidence(evidence, allowed) {
							return false
						}
					}
				}
				for _, id := range unit.BriefSentenceIDs {
					found := false
					for _, sentence := range unit.Sentences {
						if sentence.ID == id {
							found = true
							break
						}
					}
					if !found {
						return false
					}
				}
			}
			if !walk(section.Children) {
				return false
			}
		}
		return true
	}
	return walk(document.Sections)
}

func validSummaryEvidence(evidence SummaryEvidence, allowed map[int]SummarySegment) bool {
	segment, ok := allowed[evidence.Index]
	if !ok || strings.TrimSpace(evidence.Quote) == "" || evidence.Quote != segment.Text {
		return false
	}
	if evidence.Start != segment.Start || evidence.End != segment.End || evidence.Page != segment.Page {
		return false
	}
	return true
}
