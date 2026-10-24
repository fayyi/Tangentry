package app

import (
	"strings"
	"testing"

	"tangentry/internal/ai"
)

func TestSummaryNoteBodyExportsContentTreeLevelsAndEvidence(t *testing.T) {
	result := ai.SummaryResult{
		ContentID: "video:fixture",
		Document: &ai.SummaryDocument{
			Topic: "主题地图",
			Sections: []ai.SummarySection{{
				SectionID: "section-1", Title: "第一章", Topic: "章节主题", Takeaway: "章节结论。", Start: 2.5, End: 4.25,
				Evidence: []ai.SummaryEvidence{{ID: "e1", Quote: "章节证据。", Start: 2.5, End: 4.25}},
				Units: []ai.SummaryUnit{{
					UnitID: "unit-1", Topic: "单元主题", Takeaway: "单元结论。", Key: "关键原因。", Full: "原文全文。",
					Sentences:        []ai.SummarySentence{{ID: "sentence-1", Text: "原文证据句。", Evidence: []ai.SummaryEvidence{{ID: "e2", Quote: "原文证据句。", Start: 3, End: 3.5}}}},
					BriefSentenceIDs: []string{"sentence-1"},
				}},
			}},
		},
	}

	body := summaryNoteBody(result)
	for _, want := range []string{
		"# AI 主题地图", "## Topic", "主题地图", "## 第一章", "**Topic:** 章节主题",
		"**Takeaway:** 章节结论。", "时间：2.500–4.250 秒", "### 单元主题",
		"**Key:** 关键原因。", "**Brief:**", "> 原文证据句。", "**Full:**", "原文全文。",
		"章节证据。 · 2.500–4.250 秒", "原文证据句。 · 3.000–3.500 秒",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("summary note missing %q:\n%s", want, body)
		}
	}
}

func TestSummaryNoteBodyKeepsLegacyFallback(t *testing.T) {
	body := summaryNoteBody(ai.SummaryResult{Overview: "旧概述", Points: []ai.SummaryPoint{{Text: "旧要点"}}})
	if !strings.Contains(body, "# AI 摘要") || !strings.Contains(body, "旧概述") || !strings.Contains(body, "1. 旧要点") {
		t.Fatalf("legacy summary note changed unexpectedly: %s", body)
	}
}
