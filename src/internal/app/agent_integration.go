package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"tangentry/internal/agent"
	"tangentry/internal/agent/gateway"
)

const agentSettingsFileMode = 0600

// AgentIntegration exposes the user-controlled lifecycle of the local Agent
// gateway. It never returns the bearer token; clients use the connection file
// created by the gateway and the stdio bridge reads it directly.
func (a *Application) AgentIntegration(ctx context.Context) (agent.IntegrationSettings, agent.IntegrationStatus, error) {
	if err := ctx.Err(); err != nil {
		return agent.IntegrationSettings{}, agent.IntegrationStatus{}, err
	}
	if a == nil {
		return agent.IntegrationSettings{}, agent.IntegrationStatus{}, errors.New("application unavailable")
	}
	a.agentMu.Lock()
	defer a.agentMu.Unlock()
	return a.agentIntegrationStatus(ctx)
}

func (a *Application) agentIntegrationStatus(ctx context.Context) (agent.IntegrationSettings, agent.IntegrationStatus, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	settings := a.agentIntegrationSettingsLocked()
	status := agent.IntegrationStatus{Enabled: settings.Enabled, ProtocolVersion: gateway.ProtocolVersion, Scopes: append([]string(nil), settings.Scopes...)}
	if a.agentGateway != nil {
		connection := a.agentGateway.Connection()
		status.Running = true
		status.Endpoint = connection.Endpoint
		status.ConnectionFile = filepath.Join(a.dataDir, "agent", "connection.json")
		status.Scopes = append([]string(nil), connection.Scopes...)
	}
	return settings, status, nil
}

func (a *Application) ConfigureAgentIntegration(ctx context.Context, settings agent.IntegrationSettings) (agent.IntegrationStatus, error) {
	if err := ctx.Err(); err != nil {
		return agent.IntegrationStatus{}, err
	}
	if a == nil {
		return agent.IntegrationStatus{}, errors.New("application unavailable")
	}
	a.agentMu.Lock()
	defer a.agentMu.Unlock()
	scopes := normalizeAgentScopes(settings.Scopes)
	if settings.Enabled && len(scopes) == 0 {
		scopes = []string{agent.ScopeContentRead, agent.ScopeAgentWrite}
	}
	a.mu.Lock()
	old := a.agentGateway
	a.agentGateway = nil
	a.mu.Unlock()
	if old != nil {
		_ = old.Close()
	}
	var started *gateway.Gateway
	if settings.Enabled {
		var err error
		started, err = gateway.StartWithScopes(a.agent, a.dataDir, scopes)
		if err != nil {
			return agent.IntegrationStatus{}, err
		}
	}
	settings.Scopes = scopes
	if err := a.saveAgentIntegrationSettings(settings); err != nil {
		if started != nil {
			_ = started.Close()
		}
		return agent.IntegrationStatus{}, err
	}
	a.mu.Lock()
	a.agentGateway = started
	a.mu.Unlock()
	_, status, err := a.agentIntegrationStatus(ctx)
	return status, err
}

func (a *Application) agentIntegrationSettingsLocked() agent.IntegrationSettings {
	settings := agent.IntegrationSettings{Scopes: []string{agent.ScopeContentRead, agent.ScopeAgentWrite}}
	data, err := os.ReadFile(filepath.Join(a.dataDir, "agent", "settings.json"))
	if err == nil {
		var stored agent.IntegrationSettings
		if json.Unmarshal(data, &stored) == nil {
			settings = stored
		}
	}
	settings.Scopes = normalizeAgentScopes(settings.Scopes)
	if settings.Enabled && len(settings.Scopes) == 0 {
		settings.Scopes = defaultAgentScopes()
	}
	return settings
}

func (a *Application) saveAgentIntegrationSettings(settings agent.IntegrationSettings) error {
	directory := filepath.Join(a.dataDir, "agent")
	if err := os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	data, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(directory, ".settings-*")
	if err != nil {
		return err
	}
	path := tmp.Name()
	defer os.Remove(path)
	if err := tmp.Chmod(agentSettingsFileMode); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(path, filepath.Join(directory, "settings.json"))
}

func normalizeAgentScopes(scopes []string) []string {
	allowed := map[string]bool{agent.ScopeContentRead: true, agent.ScopeNetworkFetch: true, agent.ScopeImageRead: true, agent.ScopePrivateNotes: true, agent.ScopeDerived: true, agent.ScopeAgentWrite: true}
	seen := map[string]bool{}
	result := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		if allowed[scope] && !seen[scope] {
			seen[scope] = true
			result = append(result, scope)
		}
	}
	return result
}

func defaultAgentScopes() []string {
	return []string{agent.ScopeContentRead, agent.ScopeAgentWrite}
}

func (a *Application) startConfiguredAgentGateway() error {
	a.agentMu.Lock()
	defer a.agentMu.Unlock()
	a.mu.Lock()
	settings := a.agentIntegrationSettingsLocked()
	a.mu.Unlock()
	if !settings.Enabled {
		return nil
	}
	if len(settings.Scopes) == 0 {
		settings.Scopes = defaultAgentScopes()
	}
	gatewayInstance, err := gateway.StartWithScopes(a.agent, a.dataDir, settings.Scopes)
	if err != nil {
		return err
	}
	a.mu.Lock()
	a.agentGateway = gatewayInstance
	a.mu.Unlock()
	return nil
}
