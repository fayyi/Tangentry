package ai

import "testing"

func TestRenderPromptSubstitutesVariablesAndRejectsUnknown(t *testing.T) {
	got, err := RenderPrompt("标题={{title}}\n正文={{content}}", map[string]string{"title": "标题", "content": "正文"})
	if err != nil || got != "标题=标题\n正文=正文" {
		t.Fatalf("render=%q err=%v", got, err)
	}
	for _, template := range []string{"{{missing}}", "{{content", "{{}}"} {
		if _, err := RenderPrompt(template, map[string]string{"content": "正文"}); err == nil {
			t.Fatalf("template %q should fail", template)
		}
	}
}

func TestDefaultPromptTemplatesAreRenderable(t *testing.T) {
	templates := DefaultPromptTemplates()
	variables := map[string]string{"title": "标题", "source_type": "文章", "content": "正文", "target_language": "简体中文"}
	for _, template := range []PromptTemplate{templates.Summary, templates.Translation, templates.Chat} {
		if !validPromptTemplate(template) {
			t.Fatal("default template rejected")
		}
		if _, err := RenderPrompt(template.User, variables); err != nil {
			t.Fatal(err)
		}
	}
}

func TestChatModelSkipsLikelyTranslationModel(t *testing.T) {
	value := Config{Model: "bilibili/Index-Translate-35B-A3B", EnabledModels: []string{"bilibili/Index-Translate-35B-A3B", "general-chat"}}
	if got := chatModel(value); got != "general-chat" {
		t.Fatalf("chat model=%q", got)
	}
}
