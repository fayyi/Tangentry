package aichat

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"tangentry/internal/ai"
)

func (c *Client) SupportsSummary(config ai.Config) bool { return !textTranslationModel(config.Model) }

func (c *Client) SupportsSummaryDocument(config ai.Config) bool { return c.SupportsSummary(config) }

func (c *Client) Summarize(ctx context.Context, config ai.Config, request ai.SummaryRequest) (ai.SummaryDraft, error) {
	if !ai.ValidEndpoint(config.BaseURL) {
		return ai.SummaryDraft{}, ai.ErrInvalid
	}
	if !c.SupportsSummary(config) {
		return ai.SummaryDraft{}, ai.ErrNotReady
	}
	input := map[string]any{"mode": request.Mode}
	if request.Mode == "leaf" || request.Mode == "document" {
		input["segments"] = request.Segments
	} else {
		input["drafts"] = request.Drafts
	}
	raw, _ := json.Marshal(input)
	limitParameter := "max_completion_tokens"
	if config.Protocol == "compatible" {
		limitParameter = "max_tokens"
	}
	user := strings.TrimSpace(request.UserPrompt)
	if user == "" {
		user = string(raw)
	} else {
		user += "\n\n输入数据（JSON）：" + string(raw)
	}
	systemPrompt := aiSummarySystemPrompt(request.Prompt)
	if request.Mode == "document" {
		systemPrompt += " 当前任务是 document：必须输出完整 document，sections 至少一个，并且每个 unit 都要有可验证的原文 evidence；不要只返回 legacy overview/points。"
	}
	payload := map[string]any{"model": config.Model, "stream": false, limitParameter: 8192, "response_format": map[string]string{"type": "json_object"}, "messages": []map[string]string{{"role": "system", "content": systemPrompt}, {"role": "user", "content": user}}}
	content, err := c.complete(ctx, config, payload)
	if errors.Is(err, ai.ErrJSONModeUnsupported) {
		delete(payload, "response_format")
		content, err = c.complete(ctx, config, payload)
	}
	if err != nil {
		return ai.SummaryDraft{}, err
	}
	var output struct {
		Overview string `json:"overview"`
		Points   []struct {
			Text    string `json:"text"`
			Indices []*int `json:"indices"`
		} `json:"points"`
		KeyPoints []struct {
			Text    string `json:"text"`
			Indices []*int `json:"indices"`
		} `json:"key_points"`
		Outline  []ai.SummaryOutline `json:"outline"`
		Document *ai.SummaryDocument `json:"document"`
	}
	decoder := json.NewDecoder(strings.NewReader(structuredJSON(content)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&output) != nil || decoder.Decode(new(any)) != io.EOF {
		return ai.SummaryDraft{}, ai.ErrFormat
	}
	points := output.Points
	if len(points) == 0 {
		points = output.KeyPoints
	}
	draft := ai.SummaryDraft{Overview: output.Overview, Points: make([]ai.SummaryPoint, len(points)), Outline: output.Outline, Document: output.Document}
	for i, point := range points {
		draft.Points[i] = ai.SummaryPoint{Text: point.Text, Indices: make([]int, len(point.Indices))}
		for j, index := range point.Indices {
			if index == nil {
				return ai.SummaryDraft{}, ai.ErrFormat
			}
			draft.Points[i].Indices[j] = *index
		}
	}
	return draft, nil
}

func aiSummarySystemPrompt(custom string) string {
	if strings.TrimSpace(custom) == "" {
		custom = "请用简体中文总结提供的内容"
	}
	return custom + `。只返回严格 JSON：{"overview":"一句话总结","key_points":[{"text":"关键点","indices":[0]}],"outline":[{"title":"章节","level":0,"indices":[0],"children":[]}],"document":{"formatVersion":2,"levels":["Topic","Takeaway","Key","Brief","Full"],"sections":[]}}。document 可选，但若输出必须按来源顺序组织 sections/units，并给每个 AI 结论提供 evidence。Takeaway 是一句结论；Key 必须是独立的‘结论 + 主要依据、机制或因果关系’，不得复制 Takeaway；无法提炼独立依据时将 key 留空。Full 必须是输入原文，Brief 只能选择 Full 中的原文句子。overview 不超过 1200 UTF-8 bytes（UTF-8 字节），key_points 为 1 到 5 条且每条不超过 600 UTF-8 bytes（UTF-8 字节）；never exceed 64 个引用。copy the original supplied index values exactly，不要把索引重新从零开始。outline 必须至少输出一个一级节点。每个节点必须引用输入中真实索引，level 从 0 开始，children 继续递增；有时间戳或页码时使用输入字段。不得编造索引、时间戳、事实或引用，保留不确定性和专有名词。`
}
