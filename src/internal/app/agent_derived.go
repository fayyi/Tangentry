package app

import (
	"context"
	"errors"
	"net"
	"net/url"
	"strings"
	"time"

	"tangentry/internal/agent"
	"tangentry/internal/ai"
)

func (p *agentProvider) PrepareDerived(ctx context.Context, action string, input agent.DerivedRequest) (agent.DerivedPlan, error) {
	switch action {
	case "transcribe_content":
		status := p.services.features.transcription.Status()
		if !status.Ready || status.Config.Cloud() && !input.AllowCloud {
			return agent.DerivedPlan{}, agent.ErrUnsupported
		}
		// The transcription resolver can fall back to a platform media lookup.
		// When network fetching was not explicitly requested, require a completed
		// local media artifact before starting the job so a derived call cannot
		// smuggle a network request through the resolver.
		if !input.Fetch && !p.localMediaAvailable(ctx, input.ContentID) {
			return agent.DerivedPlan{}, agent.ErrUnsupported
		}
		cloud := status.Config.Cloud()
		source, cost, duration := "local_transcription", "local", "minutes"
		if cloud {
			source, cost, duration = "configured_cloud", "remote", "minutes"
		}
		return agent.DerivedPlan{Source: source, CostClass: cost, EstimatedDuration: duration, ExternalProcessing: cloud, Run: func(ctx context.Context) error {
			job, err := p.services.features.transcription.StartWithUploadConsent(ctx, input.ContentID, false, input.AllowCloud, status.Config.Backend)
			if err != nil {
				return err
			}
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			for {
				current, err := p.services.features.transcription.Get(job.ID)
				if err != nil {
					return err
				}
				switch current.State {
				case "completed":
					return nil
				case "failed":
					return errors.New(current.Error)
				case "canceled", "cancelled":
					return context.Canceled
				}
				select {
				case <-ctx.Done():
					_ = p.services.features.transcription.Cancel(job.ID)
					return ctx.Err()
				case <-ticker.C:
				}
			}
		}}, nil
	case "summarize_content", "translate_content":
		service := p.services.features.api.ai
		if service == nil {
			return agent.DerivedPlan{}, agent.ErrUnsupported
		}
		status := service.Status()
		local := localAIEndpoint(status.BaseURL)
		if !local && !input.AllowCloud {
			return agent.DerivedPlan{}, agent.ErrUnsupported
		}
		if action == "summarize_content" && !status.SummaryReady || action == "translate_content" && !status.Ready {
			return agent.DerivedPlan{}, agent.ErrUnsupported
		}
		if action == "translate_content" && input.TargetLanguage == "" {
			return agent.DerivedPlan{}, agent.ErrInvalid
		}
		source, costClass, external := "configured_cloud", "remote", true
		if local {
			source, costClass, external = "local_ai", "local", false
		}
		return agent.DerivedPlan{Source: source, CostClass: costClass, EstimatedDuration: "minutes", ExternalProcessing: external, Run: func(ctx context.Context) error {
			result, err := p.Subtitles(ctx, input.ContentID, "", input.Fetch)
			if err != nil {
				return err
			}
			if len(result.Document.Tracks) == 0 {
				return agent.ErrUnsupported
			}
			track := result.Document.Tracks[0]
			if action == "translate_content" {
				_, err := service.Translate(ctx, ai.Input{ContentID: input.ContentID, Track: track, TargetLanguage: input.TargetLanguage, Revision: status.Revision})
				return err
			}
			segments := make([]ai.SummarySegment, 0, len(track.Cues))
			for index, cue := range track.Cues {
				segments = append(segments, ai.SummarySegment{Index: index, Start: cue.Start, End: cue.End, Text: cue.Text, Kind: "subtitle"})
			}
			_, err = service.SummaryGenerate(ctx, ai.SummaryInput{ContentID: input.ContentID, Source: track.Language, Segments: segments, Revision: status.Revision})
			return err
		}}, nil
	default:
		return agent.DerivedPlan{}, agent.ErrInvalid
	}
}

var _ agent.DerivedProvider = (*agentProvider)(nil)

func localAIEndpoint(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Hostname() == "" || u.User != nil {
		return false
	}
	if strings.EqualFold(u.Hostname(), "localhost") {
		return true
	}
	return net.ParseIP(u.Hostname()) != nil && net.ParseIP(u.Hostname()).IsLoopback()
}

func (p *agentProvider) localMediaAvailable(ctx context.Context, id string) bool {
	item, err := p.services.features.content.Get(ctx, id)
	if err != nil {
		item, err = p.services.features.api.inbox.Item(ctx, id)
	}
	if err != nil || item.URL == "" {
		return false
	}
	_, err = p.services.features.downloads.CompletedPath(ctx, item.URL)
	return err == nil
}
