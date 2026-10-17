package ai

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"
)

type CheckInput struct {
	Task     string `json:"task"`
	Revision int64  `json:"revision"`
}

type CheckResult struct {
	Task      string `json:"task"`
	Model     string `json:"model"`
	ElapsedMS int64  `json:"elapsedMs"`
	Revision  int64  `json:"revision"`
}

// Check uses fixed synthetic text and the same provider contract as reading
// tasks, without reading or writing any article, note or generation cache.
func (s *Service) Check(parent context.Context, in CheckInput) (CheckResult, error) {
	if in.Task != "translation" && in.Task != "summary" {
		return CheckResult{}, ErrInvalid
	}
	if err := parent.Err(); err != nil {
		return CheckResult{}, err
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return CheckResult{}, ErrStorage
	}
	c := s.config
	if in.Revision != c.Revision {
		s.mu.Unlock()
		return CheckResult{}, ErrConflict
	}
	status := s.publicStatus(c)
	if in.Task == "translation" && (!status.Ready || s.provider == nil) || in.Task == "summary" && !status.SummaryReady {
		s.mu.Unlock()
		return CheckResult{}, ErrNotReady
	}
	if s.active != "" {
		s.mu.Unlock()
		return CheckResult{}, ErrBusy
	}
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	s.active = "check:" + in.Task
	s.cancel = cancel
	s.wait.Add(1)
	s.mu.Unlock()
	defer func() {
		cancel()
		s.mu.Lock()
		s.active = ""
		s.cancel = nil
		s.mu.Unlock()
		s.wait.Done()
	}()

	start := time.Now()
	const text = "The reader keeps your notes with the original article."
	if in.Task == "summary" {
		c = summaryConfig(c)
		draft, err := s.summarizer.Summarize(ctx, c, SummaryRequest{Mode: "leaf", Segments: []SummarySegment{{Index: 0, Text: text}}})
		if err != nil {
			return CheckResult{}, err
		}
		if !validateDraft(draft, map[int]bool{0: true}) {
			return CheckResult{}, ErrFormat
		}
	} else {
		output, err := s.provider.Translate(ctx, c, []Segment{{Index: 0, Text: text}})
		if err != nil {
			return CheckResult{}, err
		}
		if len(output) != 1 || output[0].Index != 0 || !utf8.ValidString(output[0].Text) || strings.TrimSpace(output[0].Text) == "" || len(output[0].Text) > 24000 {
			return CheckResult{}, ErrFormat
		}
	}
	if err := ctx.Err(); err != nil {
		return CheckResult{}, err
	}
	return CheckResult{Task: in.Task, Model: c.Model, ElapsedMS: time.Since(start).Milliseconds(), Revision: c.Revision}, nil
}
