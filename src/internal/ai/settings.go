// Package ai owns explicit, resumable language assistance. It never fetches
// media or platform credentials; providers are injected at the composition root.
package ai

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

var (
	ErrInvalid             = errors.New("invalid AI input")
	ErrConflict            = errors.New("AI configuration changed")
	ErrNotReady            = errors.New("AI not configured")
	ErrBusy                = errors.New("AI operation busy")
	ErrStorage             = errors.New("AI local storage unavailable")
	ErrProvider            = errors.New("AI provider unavailable")
	ErrRequestRejected     = errors.New("AI provider rejected request parameters")
	ErrJSONModeUnsupported = errors.New("AI provider does not support JSON response format")
	ErrTranslationLanguage = errors.New("AI provider cannot determine target translation language")
	ErrGatewayTimeout      = errors.New("AI provider gateway timed out")
	ErrFormat              = errors.New("AI returned invalid translations")
	ErrIncomplete          = fmt.Errorf("AI batch omitted segments: %w", ErrFormat)
	ErrTruncated           = fmt.Errorf("AI output exceeded the token limit: %w", ErrFormat)
	ErrRefused             = fmt.Errorf("AI provider declined the task: %w", ErrFormat)
	ErrNotFound            = errors.New("AI translation not found")
	ErrAuth                = errors.New("AI provider authentication failed")
	ErrRate                = errors.New("AI provider quota or rate limit")
	ErrSummaryNotFound     = errors.New("AI summary not found")
)

type Config struct {
	Protocol             string `json:"protocol"`
	BaseURL              string `json:"baseUrl"`
	Model                string `json:"model"`
	SummaryModel         string `json:"summaryModel,omitempty"`
	SummaryPrompt        string `json:"summaryPrompt,omitempty"`
	SummaryContextBudget int    `json:"summaryContextBudget,omitempty"`
	Key                  string `json:"key"`
	Revision             int64  `json:"revision"`
}
type Status struct {
	Protocol             string `json:"protocol"`
	Translating          bool   `json:"translating"`
	BaseURL              string `json:"baseUrl"`
	Model                string `json:"model"`
	SummaryModel         string `json:"summaryModel"`
	SummaryPrompt        string `json:"summaryPrompt"`
	SummaryContextBudget int    `json:"summaryContextBudget"`
	SummaryReady         bool   `json:"summaryReady"`
	HasKey               bool   `json:"hasKey"`
	Ready                bool   `json:"ready"`
	Revision             int64  `json:"revision"`
}
type ConfigureInput struct {
	Protocol             string  `json:"protocol"`
	BaseURL              string  `json:"baseUrl"`
	Model                string  `json:"model"`
	SummaryModel         *string `json:"summaryModel,omitempty"`
	SummaryPrompt        *string `json:"summaryPrompt,omitempty"`
	SummaryContextBudget *int    `json:"summaryContextBudget,omitempty"`
	Key                  *string `json:"key,omitempty"`
	Revision             int64   `json:"revision"`
}

func ValidEndpoint(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || len(raw) > 2048 || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.RawFragment != "" || strings.ContainsAny(raw, "\r\n\t ") || u.Opaque != "" {
		return false
	}
	if u.Scheme == "https" {
		return true
	}
	ip := net.ParseIP(u.Hostname())
	return u.Scheme == "http" && (u.Hostname() == "localhost" || ip != nil && ip.IsLoopback())
}
func validModel(model string) bool {
	return utf8.ValidString(model) && len(model) <= 200 && !strings.ContainsAny(model, "\r\n\x00")
}
func validConfig(c Config) bool {
	return (c.Protocol == "" || c.Protocol == "openai" || c.Protocol == "compatible") && ValidEndpoint(c.BaseURL) && validModel(c.Model) && validModel(c.SummaryModel) && validPrompt(c.SummaryPrompt) && validContextBudget(c.SummaryContextBudget) && utf8.ValidString(c.Key) && len(c.Key) <= 4096 && !strings.ContainsAny(c.Key, "\r\n\x00") && c.Revision > 0 && c.Revision < 9007199254740991
}
func public(c Config) Status {
	return Status{Protocol: c.Protocol, BaseURL: c.BaseURL, Model: c.Model, SummaryModel: c.SummaryModel, SummaryPrompt: summaryPrompt(c.SummaryPrompt), SummaryContextBudget: summaryContextBudget(c.SummaryContextBudget), HasKey: c.Key != "", Ready: c.Model != "", Revision: c.Revision}
}

func validPrompt(prompt string) bool {
	return utf8.ValidString(prompt) && len(prompt) <= 12000 && !strings.ContainsAny(prompt, "\r\x00")
}

func summaryPrompt(prompt string) string {
	if strings.TrimSpace(prompt) == "" {
		return defaultSummaryPrompt
	}
	return strings.TrimSpace(prompt)
}

func validContextBudget(budget int) bool { return budget == 0 || budget >= 2000 && budget <= 64000 }

func summaryContextBudget(budget int) int {
	if budget < 2000 {
		return 28000
	}
	return budget
}
func summaryConfig(c Config) Config {
	if c.SummaryModel != "" {
		c.Model = c.SummaryModel
	}
	return c
}
func (s *Service) publicStatus(c Config) Status {
	status := public(c)
	effective := summaryConfig(c)
	status.SummaryReady = effective.Model != "" && s.summarizer != nil
	if provider, ok := s.summarizer.(interface{ SupportsSummary(Config) bool }); ok {
		status.SummaryReady = status.SummaryReady && provider.SupportsSummary(effective)
	}
	return status
}
func (s *Service) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	status := s.publicStatus(s.config)
	status.Translating = s.active != ""
	return status
}
func (s *Service) Configure(in ConfigureInput) (Status, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return Status{}, ErrStorage
	}
	if s.active != "" {
		return Status{}, ErrBusy
	}
	if in.Revision != s.config.Revision {
		return Status{}, ErrConflict
	}
	protocol := in.Protocol
	if protocol == "" {
		protocol = "openai"
	}
	c := Config{Protocol: protocol, BaseURL: strings.TrimRight(strings.TrimSpace(in.BaseURL), "/"), Model: strings.TrimSpace(in.Model), SummaryModel: s.config.SummaryModel, SummaryPrompt: s.config.SummaryPrompt, SummaryContextBudget: s.config.SummaryContextBudget, Key: s.config.Key, Revision: s.config.Revision + 1}
	if in.SummaryModel != nil {
		c.SummaryModel = strings.TrimSpace(*in.SummaryModel)
	}
	if in.SummaryPrompt != nil {
		c.SummaryPrompt = strings.TrimSpace(*in.SummaryPrompt)
	}
	if in.SummaryContextBudget != nil {
		c.SummaryContextBudget = *in.SummaryContextBudget
	}
	// A saved key may only be retained for the exact same endpoint. An endpoint
	// change requires an explicitly supplied replacement (or empty local key).
	if in.Key == nil && c.BaseURL != s.config.BaseURL && c.Key != "" {
		return Status{}, ErrInvalid
	}
	if in.Key != nil {
		c.Key = strings.TrimSpace(*in.Key)
	}
	if !validConfig(c) {
		return Status{}, ErrInvalid
	}
	if err := atomicJSON(s.settingsPath, c); err != nil {
		return Status{}, ErrStorage
	}
	s.config = c
	return s.publicStatus(c), nil
}
func atomicJSON(path string, value any) error {
	data, err := json.Marshal(value)
	if len(data) > 9<<20 {
		return ErrStorage
	}
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".ai-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), path)
}
