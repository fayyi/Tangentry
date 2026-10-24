package ai

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

var summaryLevels = []string{"Topic", "Takeaway", "Key", "Brief", "Full"}

// buildSummaryDocument projects the validated legacy draft onto the stable
// TextFold-style tree. The projection is deterministic: IDs are derived from
// outline/point order and source segment indexes, never from model output.
func buildSummaryDocument(result SummaryResult, draft SummaryDraft, segments []SummarySegment) *SummaryDocument {
	byIndex := make(map[int]SummarySegment, len(segments))
	for _, segment := range segments {
		byIndex[segment.Index] = segment
	}
	if draft.Document != nil {
		if document := projectProvidedDocument(result, *draft.Document, segments, byIndex); document != nil {
			return document
		}
	}
	points := append([]SummaryPoint(nil), draft.Points...)
	sections := make([]SummarySection, 0, len(draft.Outline))
	if groups := structuredSectionGroups(segments, result.Source); len(groups) > 0 {
		for index, group := range groups {
			sections = append(sections, buildStructuredSection("section-"+sourceRangeID(sourcePositions(group, byIndex)), index, group, points, byIndex, draft.Overview))
		}
	} else if len(draft.Outline) > 0 {
		for i, node := range draft.Outline {
			fallback := "section-" + strconv.Itoa(i+1)
			sections = append(sections, buildSummarySection(fallback, node, points, byIndex, draft.Overview))
		}
	}
	if len(sections) == 0 {
		if strings.Contains(strings.ToLower(result.Source), "subtitle") || strings.Contains(strings.ToLower(result.Source), "video") || strings.Contains(strings.ToLower(result.Source), "podcast") {
			for index, indices := range subtitleChapterIndices(segments) {
				section := SummarySection{SectionID: "section-" + sourceRangeID(sourcePositions(indices, byIndex)), Title: fmt.Sprintf("章节 %d", index+1), Level: 0, Topic: fmt.Sprintf("章节 %d", index+1), Takeaway: strings.TrimSpace(draft.Overview)}
				section.Source, _ = summaryEvidenceForIndices(indices, byIndex)
				section.Evidence = append([]SummaryEvidence(nil), section.Source...)
				for pointIndex, point := range points {
					if intersects(indices, point.Indices) {
						unitID := stableUnitID(section.SectionID, point, pointIndex, byIndex)
						for _, existing := range section.Units {
							if existing.UnitID == unitID {
								unitID = fmt.Sprintf("%s-p%d", unitID, pointIndex+1)
								break
							}
						}
						section.Units = append(section.Units, buildSummaryUnit(unitID, point, byIndex))
					}
				}
				if len(indices) > 0 {
					first, last := byIndex[indices[0]], byIndex[indices[len(indices)-1]]
					section.Start, section.End, section.Page = first.Start, last.End, first.Page
				}
				sections = append(sections, section)
			}
		}
	}
	if len(sections) == 0 {
		indices := make([]int, 0, len(segments))
		for _, segment := range segments {
			indices = append(indices, segment.Index)
		}
		node := SummaryOutline{Title: "内容摘要", Level: 0, Indices: indices}
		sections = []SummarySection{buildSummarySection("section-1", node, points, byIndex, draft.Overview)}
	}
	topicParts := make([]string, 0, len(sections))
	for _, section := range sections {
		if strings.TrimSpace(section.Topic) != "" {
			topicParts = append(topicParts, section.Topic)
		}
	}
	return &SummaryDocument{
		FormatVersion: summaryFormatVersion,
		ContentID:     result.ContentID,
		Source:        result.Source,
		Model:         result.Model,
		Provenance:    map[string]string{"generator": "tangentry", "summaryPromptVersion": fmt.Sprintf("%d", summaryPromptVersion)},
		Levels:        append([]string(nil), summaryLevels...),
		Topic:         strings.Join(topicParts, " · "),
		Sections:      sections,
	}
}

// projectProvidedDocument keeps the model-authored five-level content when it
// satisfies the source/evidence contract, while replacing model-generated IDs
// with deterministic IDs derived from source ranges.
func projectProvidedDocument(result SummaryResult, input SummaryDocument, segments []SummarySegment, byIndex map[int]SummarySegment) *SummaryDocument {
	if len(input.Sections) == 0 {
		return nil
	}
	document := &SummaryDocument{
		FormatVersion: summaryFormatVersion,
		ContentID:     result.ContentID,
		Source:        result.Source,
		Model:         result.Model,
		Provenance:    map[string]string{"generator": "tangentry", "summaryPromptVersion": fmt.Sprintf("%d", summaryPromptVersion), "source": "model-document"},
		Levels:        append([]string(nil), summaryLevels...),
	}
	sectionOrdinal := 0
	var projectSections func([]SummarySection, int) []SummarySection
	projectSections = func(inputSections []SummarySection, parentLevel int) []SummarySection {
		output := make([]SummarySection, 0, len(inputSections))
		for _, source := range inputSections {
			if !validProvidedSectionEvidence(source, byIndex) {
				continue
			}
			indices := sectionSourceIndices(source, byIndex)
			if len(indices) == 0 {
				continue
			}
			level := source.Level
			if level < parentLevel || level > 8 {
				level = parentLevel
			}
			section := source
			section.Level = level
			section.SectionID = fmt.Sprintf("section-%s", sourceRangeID(sourcePositions(indices, byIndex)))
			section.Title = strings.TrimSpace(section.Title)
			if section.Title == "" {
				section.Title = fmt.Sprintf("章节 %d", sectionOrdinal+1)
			}
			sectionOrdinal++
			section.Topic = topicFromText(section.Takeaway)
			if section.Topic == "" {
				section.Topic = strings.TrimSpace(source.Topic)
			}
			section.Source, _ = summaryEvidenceForIndices(indices, byIndex)
			section.Evidence = append([]SummaryEvidence(nil), section.Source...)
			section.Start, section.End, section.Page = sourceRangeMetadata(indices, byIndex)
			seenUnits := map[string]bool{}
			units := make([]SummaryUnit, 0, len(source.Units))
			for ordinal, sourceUnit := range source.Units {
				unitIndices := unitSourceIndices(sourceUnit, byIndex)
				if len(unitIndices) == 0 {
					continue
				}
				unit := buildSummaryUnit("", SummaryPoint{Indices: unitIndices}, byIndex)
				unit.UnitID = stableUnitID(section.SectionID, SummaryPoint{Indices: unitIndices}, ordinal, byIndex)
				if seenUnits[unit.UnitID] {
					unit.UnitID = fmt.Sprintf("%s-p%d", unit.UnitID, ordinal+1)
				}
				seenUnits[unit.UnitID] = true
				unit.Type = sourceUnit.Type
				unit.Role = sourceUnit.Role
				unit.Takeaway = strings.TrimSpace(sourceUnit.Takeaway)
				unit.Key = strings.TrimSpace(sourceUnit.Key)
				unit.Reason = strings.TrimSpace(sourceUnit.Reason)
				if unit.Key == unit.Takeaway {
					unit.Key = ""
				}
				if unit.Reason == unit.Takeaway {
					unit.Reason = ""
				}
				unit.Topic = topicFromText(unit.Takeaway)
				if unit.Topic == "" {
					unit.Topic = strings.TrimSpace(sourceUnit.Topic)
				}
				unit.Evidence, _ = summaryEvidenceForIndices(unitIndices, byIndex)
				unit.Source = append([]SummaryEvidence(nil), unit.Evidence...)
				units = append(units, unit)
			}
			section.Units = units
			section.Children = projectSections(source.Children, level+1)
			output = append(output, section)
		}
		return output
	}
	document.Sections = projectSections(input.Sections, 0)
	if len(document.Sections) == 0 {
		return nil
	}
	topics := make([]string, 0, len(document.Sections))
	for _, section := range document.Sections {
		if strings.TrimSpace(section.Topic) != "" {
			topics = append(topics, section.Topic)
		}
	}
	document.Topic = strings.Join(topics, " · ")
	if !validSummaryDocument(document, segments) {
		return nil
	}
	return document
}

func validProvidedSectionEvidence(section SummarySection, byIndex map[int]SummarySegment) bool {
	validEntries := func(entries []SummaryEvidence) bool {
		for _, evidence := range entries {
			segment, ok := byIndex[evidence.Index]
			if !ok || strings.TrimSpace(evidence.Quote) == "" || evidence.Quote != segment.Text || evidence.Start != segment.Start || evidence.End != segment.End || evidence.Page != segment.Page {
				return false
			}
		}
		return true
	}
	if !validEntries(section.Source) || !validEntries(section.Evidence) {
		return false
	}
	for _, unit := range section.Units {
		if !validEntries(unit.Source) || !validEntries(unit.Evidence) {
			return false
		}
		for _, sentence := range unit.Sentences {
			if !validEntries(sentence.Evidence) {
				return false
			}
		}
	}
	for _, child := range section.Children {
		if !validProvidedSectionEvidence(child, byIndex) {
			return false
		}
	}
	return true
}

func sectionSourceIndices(section SummarySection, byIndex map[int]SummarySegment) []int {
	indices := make([]int, 0)
	for _, evidence := range append(append([]SummaryEvidence{}, section.Source...), section.Evidence...) {
		indices = append(indices, evidence.Index)
	}
	for _, unit := range section.Units {
		indices = append(indices, unitSourceIndices(unit, byIndex)...)
	}
	return uniqueSortedInts(indices)
}

func unitSourceIndices(unit SummaryUnit, byIndex map[int]SummarySegment) []int {
	indices := make([]int, 0)
	for _, evidence := range append(append([]SummaryEvidence{}, unit.Source...), unit.Evidence...) {
		indices = append(indices, evidence.Index)
	}
	for _, sentence := range unit.Sentences {
		for _, evidence := range sentence.Evidence {
			indices = append(indices, evidence.Index)
		}
	}
	return uniqueSortedInts(indices)
}

func sourceRangeMetadata(indices []int, byIndex map[int]SummarySegment) (float64, float64, int) {
	if len(indices) == 0 {
		return 0, 0, 0
	}
	first, last := byIndex[indices[0]], byIndex[indices[len(indices)-1]]
	return first.Start, last.End, first.Page
}

func structuredSectionGroups(segments []SummarySegment, source string) [][]int {
	if len(segments) == 0 || isSubtitleSource(source) {
		return nil
	}
	hasHeading := false
	for _, segment := range segments {
		if strings.EqualFold(strings.TrimSpace(segment.Kind), "heading") || strings.TrimSpace(segment.Heading) != "" {
			hasHeading = true
			break
		}
	}
	if !hasHeading {
		return nil
	}
	groups := make([][]int, 0, 8)
	for _, segment := range segments {
		isHeading := strings.EqualFold(strings.TrimSpace(segment.Kind), "heading") || strings.TrimSpace(segment.Heading) != ""
		if isHeading {
			groups = append(groups, nil)
		} else if len(groups) == 0 {
			groups = append(groups, nil)
		}
		groups[len(groups)-1] = append(groups[len(groups)-1], segment.Index)
	}
	return compactGroups(groups)
}

func compactGroups(groups [][]int) [][]int {
	out := make([][]int, 0, len(groups))
	for _, group := range groups {
		if len(group) > 0 {
			out = append(out, uniqueSortedInts(group))
		}
	}
	return out
}

func buildStructuredSection(id string, index int, indices []int, points []SummaryPoint, byIndex map[int]SummarySegment, overview string) SummarySection {
	first := byIndex[indices[0]]
	title := strings.TrimSpace(first.Heading)
	if title == "" && strings.EqualFold(strings.TrimSpace(first.Kind), "heading") {
		title = strings.TrimSpace(first.Text)
	}
	if title == "" {
		title = fmt.Sprintf("章节 %d", index+1)
	}
	last := byIndex[indices[len(indices)-1]]
	section := SummarySection{SectionID: id, Title: title, Level: 0, Topic: title, Takeaway: strings.TrimSpace(overview), Start: first.Start, End: last.End, Page: first.Page}
	section.Source, _ = summaryEvidenceForIndices(indices, byIndex)
	section.Evidence = append([]SummaryEvidence(nil), section.Source...)
	for pointIndex, point := range points {
		if intersects(indices, point.Indices) {
			unitID := stableUnitID(id, point, pointIndex, byIndex)
			for _, existing := range section.Units {
				if existing.UnitID == unitID {
					unitID = fmt.Sprintf("%s-p%d", unitID, pointIndex+1)
					break
				}
			}
			section.Units = append(section.Units, buildSummaryUnit(unitID, point, byIndex))
		}
	}
	return section
}

func isSubtitleSource(source string) bool {
	lower := strings.ToLower(source)
	return strings.Contains(lower, "subtitle") || strings.Contains(lower, "video") || strings.Contains(lower, "podcast")
}

func subtitleChapterIndices(segments []SummarySegment) [][]int {
	if len(segments) == 0 {
		return nil
	}
	count := 1
	if len(segments) >= 5 {
		count = (len(segments) + 23) / 24
		if count < 5 {
			count = 5
		}
		if count > 12 {
			count = 12
		}
		if count > len(segments) {
			count = len(segments)
		}
	}
	chapters := make([][]int, 0, count)
	for chapter := 0; chapter < count; chapter++ {
		start := chapter * len(segments) / count
		end := (chapter + 1) * len(segments) / count
		indices := make([]int, 0, end-start)
		for _, segment := range segments[start:end] {
			indices = append(indices, segment.Index)
		}
		if len(indices) > 0 {
			chapters = append(chapters, indices)
		}
	}
	return chapters
}

func buildSummarySection(id string, node SummaryOutline, points []SummaryPoint, byIndex map[int]SummarySegment, overview string) SummarySection {
	indices := uniqueSortedInts(node.Indices)
	if len(indices) > 0 {
		id = "section-" + sourceRangeID(sourcePositions(indices, byIndex))
	}
	section := SummarySection{
		SectionID: id,
		Title:     strings.TrimSpace(node.Title),
		Level:     node.Level,
		Topic:     strings.TrimSpace(node.Title),
		Takeaway:  strings.TrimSpace(overview),
		Start:     node.Start,
		End:       node.End,
		Page:      node.Page,
	}
	section.Source, _ = summaryEvidenceForIndices(indices, byIndex)
	section.Evidence = append([]SummaryEvidence(nil), section.Source...)
	for pointIndex, point := range points {
		if !intersects(indices, point.Indices) {
			continue
		}
		unitID := stableUnitID(id, point, pointIndex, byIndex)
		for _, existing := range section.Units {
			if existing.UnitID == unitID {
				unitID = fmt.Sprintf("%s-p%d", unitID, pointIndex+1)
				break
			}
		}
		section.Units = append(section.Units, buildSummaryUnit(unitID, point, byIndex))
	}
	for childIndex, child := range node.Children {
		childFallback := fmt.Sprintf("%s-child-%d", id, childIndex+1)
		section.Children = append(section.Children, buildSummarySection(childFallback, child, points, byIndex, overview))
	}
	return section
}

func stableUnitID(sectionID string, point SummaryPoint, ordinal int, byIndex map[int]SummarySegment) string {
	if indices := sourcePositions(point.Indices, byIndex); len(indices) > 0 {
		return sectionID + "-unit-" + sourceRangeID(indices)
	}
	return fmt.Sprintf("%s-unit-%d", sectionID, ordinal+1)
}

func sourcePositions(indices []int, byIndex map[int]SummarySegment) []int {
	positions := make([]int, 0, len(indices))
	for _, index := range indices {
		segment, ok := byIndex[index]
		if !ok || len(segment.SegmentIndexes) == 0 {
			positions = append(positions, index)
			continue
		}
		positions = append(positions, segment.SegmentIndexes...)
	}
	return uniqueSortedInts(positions)
}

// sourceRangeID is intentionally derived only from source positions. Contiguous
// runs use the compact "start-end" form while disjoint positions retain all
// runs, keeping IDs stable when titles or model wording changes.
func sourceRangeID(indices []int) string {
	values := uniqueSortedInts(indices)
	if len(values) == 0 {
		return "empty"
	}
	parts := make([]string, 0, len(values))
	start, previous := values[0], values[0]
	flush := func() {
		if start == previous {
			parts = append(parts, strconv.Itoa(start))
			return
		}
		parts = append(parts, fmt.Sprintf("%d-%d", start, previous))
	}
	for _, value := range values[1:] {
		if value == previous+1 {
			previous = value
			continue
		}
		flush()
		start, previous = value, value
	}
	flush()
	return strings.Join(parts, "_")
}

func buildSummaryUnit(id string, point SummaryPoint, byIndex map[int]SummarySegment) SummaryUnit {
	indices := uniqueSortedInts(point.Indices)
	evidence, _ := summaryEvidenceForIndices(indices, byIndex)
	sentences := make([]SummarySentence, 0, len(indices))
	briefIDs := make([]string, 0, minInt(3, len(indices)))
	for _, index := range indices {
		segment, ok := byIndex[index]
		if !ok {
			continue
		}
		sentenceID := fmt.Sprintf("%s-sentence-%d", id, index)
		cueIDs := append([]string(nil), segment.CueIDs...)
		if len(cueIDs) == 0 && segment.Start < segment.End {
			cueIDs = []string{fmt.Sprintf("cue-%d", index)}
		}
		segmentIndexes := append([]int(nil), segment.SegmentIndexes...)
		if len(segmentIndexes) == 0 {
			segmentIndexes = []int{index}
		}
		sentenceEvidence := []SummaryEvidence{{ID: evidenceID(index), Index: index, Quote: segment.Text, Start: segment.Start, End: segment.End, Page: segment.Page, CueIDs: cueIDs, SegmentIndexes: segmentIndexes}}
		sentences = append(sentences, SummarySentence{ID: sentenceID, Text: segment.Text, Evidence: sentenceEvidence, Start: segment.Start, End: segment.End, Page: segment.Page})
		if len(briefIDs) < 3 {
			briefIDs = append(briefIDs, sentenceID)
		}
	}
	fullParts := make([]string, 0, len(sentences))
	for _, sentence := range sentences {
		fullParts = append(fullParts, sentence.Text)
	}
	text := strings.TrimSpace(point.Text)
	return SummaryUnit{
		UnitID:           id,
		Type:             "point",
		Role:             "key-point",
		Topic:            topicFromText(text),
		Takeaway:         text,
		Key:              "",
		Full:             strings.Join(fullParts, "\n"),
		Sentences:        sentences,
		BriefSentenceIDs: briefIDs,
		Evidence:         evidence,
		Source:           evidence,
	}
}

func summaryEvidenceForIndices(indices []int, byIndex map[int]SummarySegment) ([]SummaryEvidence, []int) {
	evidence := make([]SummaryEvidence, 0, len(indices))
	for _, index := range indices {
		segment, ok := byIndex[index]
		if !ok {
			continue
		}
		segmentIndexes := append([]int(nil), segment.SegmentIndexes...)
		if len(segmentIndexes) == 0 {
			segmentIndexes = []int{index}
		}
		cueIDs := append([]string(nil), segment.CueIDs...)
		if len(cueIDs) == 0 && segment.Start < segment.End {
			cueIDs = []string{fmt.Sprintf("cue-%d", index)}
		}
		evidence = append(evidence, SummaryEvidence{ID: evidenceID(index), Index: index, Quote: segment.Text, Start: segment.Start, End: segment.End, Page: segment.Page, CueIDs: cueIDs, SegmentIndexes: segmentIndexes})
	}
	return evidence, indices
}

func evidenceID(index int) string { return fmt.Sprintf("evidence-%d", index) }

func topicFromText(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	for _, separator := range []string{"：", ":", "，", ",", "。", ".", "；", ";"} {
		if index := strings.Index(text, separator); index > 0 {
			return strings.TrimSpace(text[:index])
		}
	}
	if len([]rune(text)) > 24 {
		return string([]rune(text)[:24])
	}
	return text
}

func intersects(left, right []int) bool {
	if len(left) == 0 {
		return true
	}
	allowed := make(map[int]bool, len(left))
	for _, value := range left {
		allowed[value] = true
	}
	for _, value := range right {
		if allowed[value] {
			return true
		}
	}
	return false
}

func uniqueSortedInts(values []int) []int {
	seen := make(map[int]bool, len(values))
	result := make([]int, 0, len(values))
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	sort.Ints(result)
	return result
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}
