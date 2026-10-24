package ai

import "testing"

func TestSummaryDocumentIDsFollowSourceRanges(t *testing.T) {
	segments := []SummarySegment{
		{Index: 0, Kind: "heading", Heading: "一", Text: "一"},
		{Index: 1, Text: "甲"},
		{Index: 2, Kind: "heading", Heading: "二", Text: "二"},
		{Index: 3, Text: "乙"},
	}
	draft := SummaryDraft{Overview: "总览", Points: []SummaryPoint{{Text: "甲", Indices: []int{1}}, {Text: "乙", Indices: []int{3}}}}
	result := SummaryResult{ContentID: "article:stable", Source: "article:original", Model: "fixture"}
	first := buildSummaryDocument(result, draft, segments)
	second := buildSummaryDocument(result, draft, segments)
	if len(first.Sections) != 2 || first.Sections[0].SectionID != "section-0-1" || first.Sections[1].SectionID != "section-2-3" {
		t.Fatalf("unexpected section ids: %#v", first.Sections)
	}
	if first.Sections[0].Units[0].UnitID != "section-0-1-unit-1" || first.Sections[1].Units[0].UnitID != "section-2-3-unit-3" {
		t.Fatalf("unexpected unit ids: %#v", first.Sections)
	}
	if first.Sections[0].SectionID != second.Sections[0].SectionID || first.Sections[0].Units[0].UnitID != second.Sections[0].Units[0].UnitID {
		t.Fatal("same source ranges must produce stable ids")
	}
	rename := draft
	rename.Outline = []SummaryOutline{{Title: "改名", Level: 0, Indices: []int{0, 1}}}
	rename.Points = draft.Points[:1]
	if got := buildSummaryDocument(result, rename, segments).Sections[0].SectionID; got != "section-0-1" {
		t.Fatalf("title changes must not change source id: %s", got)
	}
}

func TestSourceRangeIDCompactsRuns(t *testing.T) {
	for _, test := range []struct {
		indices []int
		want    string
	}{
		{[]int{3, 1, 2, 2}, "1-3"},
		{[]int{1, 4, 6, 7}, "1_4_6-7"},
		{nil, "empty"},
	} {
		if got := sourceRangeID(test.indices); got != test.want {
			t.Fatalf("sourceRangeID(%v) = %q, want %q", test.indices, got, test.want)
		}
	}
}

func TestProvidedDocumentPreservesAIClaimsAndRebuildsOriginalLayers(t *testing.T) {
	segments := []SummarySegment{{Index: 0, Text: "原文第一句。"}, {Index: 1, Text: "原文第二句。"}}
	result := SummaryResult{ContentID: "article:provided", Source: "article:original", Model: "fixture"}
	draft := SummaryDraft{Overview: "总览", Points: []SummaryPoint{{Text: "旧 fallback", Indices: []int{0}}}, Document: &SummaryDocument{Sections: []SummarySection{{SectionID: "model-section", Title: "模型章节", Takeaway: "模型结论", Units: []SummaryUnit{{UnitID: "model-unit", Takeaway: "模型单元结论", Key: "主要理由", Full: "模型改写，不是原文", Sentences: []SummarySentence{{ID: "model-sentence", Text: "原文第一句。", Evidence: []SummaryEvidence{{Index: 0, Quote: segments[0].Text}}}}, BriefSentenceIDs: []string{"model-sentence"}}}}}}}
	document := buildSummaryDocument(result, draft, segments)
	if document == nil || len(document.Sections) != 1 || document.Sections[0].Topic != "模型结论" {
		t.Fatalf("provided document claims not preserved: %#v", document)
	}
	unit := document.Sections[0].Units[0]
	if unit.Full != segments[0].Text || len(unit.Sentences) != 1 || unit.Sentences[0].Text != segments[0].Text || len(unit.BriefSentenceIDs) != 1 {
		t.Fatalf("original layers were not rebuilt: %#v", unit)
	}
}

func TestSummaryUnitDoesNotPretendTakeawayIsKey(t *testing.T) {
	segments := []SummarySegment{{Index: 0, Text: "原文第一句。"}}
	result := SummaryResult{ContentID: "article:key-level", Source: "article:original", Model: "fixture"}
	draft := SummaryDraft{Overview: "总览", Points: []SummaryPoint{{Text: "结论", Indices: []int{0}}}}
	document := buildSummaryDocument(result, draft, segments)
	unit := document.Sections[0].Units[0]
	if unit.Takeaway != "结论" || unit.Key != "" {
		t.Fatalf("fallback projection must leave key empty instead of copying takeaway: %#v", unit)
	}
}

func TestNormalizeSummaryDocumentLevelsClearsCopiedKey(t *testing.T) {
	document := &SummaryDocument{Sections: []SummarySection{{Units: []SummaryUnit{{Takeaway: "结论", Key: "结论", Reason: "结论", Sentences: []SummarySentence{{Text: "因为材料层会影响清晰度"}}}}}}}
	normalizeSummaryDocumentLevels(document)
	unit := document.Sections[0].Units[0]
	if unit.Key != "结论；依据是因为材料层会影响清晰度" || unit.Reason != "" {
		t.Fatalf("copied key was not repaired from evidence: %#v", unit)
	}
}
