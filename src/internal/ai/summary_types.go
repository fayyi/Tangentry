package ai

import (
	"context"
	"encoding/json"
	"io"
	"strings"
)

type SummarySegment struct {
	Index int     `json:"index"`
	Start float64 `json:"start"`
	End   float64 `json:"end"`
	Page  int     `json:"page,omitempty"`
	Text  string  `json:"text"`
}

type SummaryInput struct {
	ContentID string           `json:"contentId"`
	Source    string           `json:"source"`
	Text      string           `json:"text,omitempty"`
	Segments  []SummarySegment `json:"segments,omitempty"`
	Revision  int64            `json:"revision"`
	// Regenerate discards the stored summary of this text first.
	Regenerate bool `json:"regenerate,omitempty"`
}

type SummaryPoint struct {
	Text    string `json:"text"`
	Indices []int  `json:"indices"`
}

type SummaryDraft struct {
	Overview string           `json:"overview"`
	Points   []SummaryPoint   `json:"points"`
	Outline  []SummaryOutline `json:"outline,omitempty"`
}

type SummaryOutline struct {
	Title    string           `json:"title"`
	Level    int              `json:"level"`
	Indices  []int            `json:"indices,omitempty"`
	Start    float64          `json:"start,omitempty"`
	End      float64          `json:"end,omitempty"`
	Page     int              `json:"page,omitempty"`
	Anchor   string           `json:"anchor,omitempty"`
	Children []SummaryOutline `json:"children,omitempty"`
}

type SummaryCitation struct {
	Index int     `json:"index"`
	Start float64 `json:"start,omitempty"`
	End   float64 `json:"end,omitempty"`
	Quote string  `json:"quote"`
	Page  int     `json:"page,omitempty"`
}

// A timed citation may start at zero. Keep article citations untimed without
// dropping the first video's exact start from the wire or persisted cache.
func (citation SummaryCitation) MarshalJSON() ([]byte, error) {
	type wireCitation struct {
		Index int      `json:"index"`
		Start *float64 `json:"start,omitempty"`
		End   *float64 `json:"end,omitempty"`
		Quote string   `json:"quote"`
		Page  int      `json:"page,omitempty"`
	}
	value := wireCitation{Index: citation.Index, Quote: citation.Quote, Page: citation.Page}
	if citation.End > citation.Start {
		value.Start, value.End = &citation.Start, &citation.End
	}
	return json.Marshal(value)
}

type SummaryResult struct {
	Key            string            `json:"key"`
	ContentID      string            `json:"contentId"`
	Source         string            `json:"source"`
	BaseURL        string            `json:"baseUrl"`
	Model          string            `json:"model"`
	TotalCalls     int               `json:"totalCalls"`
	CompletedCalls int               `json:"completedCalls"`
	Complete       bool              `json:"complete"`
	Overview       string            `json:"overview,omitempty"`
	Points         []SummaryPoint    `json:"points,omitempty"`
	Citations      []SummaryCitation `json:"citations,omitempty"`
	Outline        []SummaryOutline  `json:"outline,omitempty"`
	UpdatedAt      int64             `json:"updatedAt"`
}

type SummaryInspection struct {
	Settings Status        `json:"settings"`
	Result   SummaryResult `json:"result"`
	Active   bool          `json:"active"`
}

type SummaryRequest struct {
	Mode     string
	Prompt   string
	Segments []SummarySegment
	Drafts   []SummaryDraft
}

type Summarizer interface {
	Summarize(context.Context, Config, SummaryRequest) (SummaryDraft, error)
}

type summaryCache struct {
	Result  SummaryResult  `json:"result"`
	Current []SummaryDraft `json:"current"`
	Next    []SummaryDraft `json:"next,omitempty"`
}

func (in *SummaryInput) UnmarshalJSON(data []byte) error {
	var raw struct {
		ContentID  *string           `json:"contentId"`
		Source     *string           `json:"source"`
		Text       *string           `json:"text"`
		Segments   *[]SummarySegment `json:"segments"`
		Revision   *int64            `json:"revision"`
		Regenerate bool              `json:"regenerate"`
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&raw) != nil || decoder.Decode(new(any)) != io.EOF || raw.ContentID == nil || raw.Source == nil || raw.Revision == nil || raw.Text == nil && raw.Segments == nil {
		return ErrInvalid
	}
	*in = SummaryInput{ContentID: *raw.ContentID, Source: *raw.Source, Revision: *raw.Revision, Regenerate: raw.Regenerate}
	if raw.Text != nil {
		in.Text = *raw.Text
	}
	if raw.Segments != nil {
		in.Segments = *raw.Segments
	}
	return nil
}
