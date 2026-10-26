// Package opencode adapts OpenCode's ACP mode for coding runs.
package opencode

import (
	"werkbord/internal/agent/acp"
	"werkbord/internal/agentcatalog"
	"werkbord/internal/agentkit"
	"werkbord/internal/domain"
)

// ID is the adapter's stable key, stored on runs.
const ID = "opencode"

type Config struct {
	Command   string
	Model     string
	Models    []domain.AgentOption
	Reasoning []string
	// Prober is shared with the assistant's provider for the same CLI; nil gives the adapter one of its own.
	Prober *agentkit.Prober
}

func New(cfg Config) *acp.Adapter {
	d := agentcatalog.OpenCode()
	command := cfg.Command
	if command == "" {
		command = d.Command
	}
	return acp.New(acp.Config{ID: d.ID, Name: d.Name, Command: command, DocsURL: d.DocsURL, Guidance: d.Setup, Args: d.Args, SessionOptions: d.SessionOptions, Model: cfg.Model, Models: cfg.Models, Reasoning: cfg.Reasoning, ListModels: d.ListModels, Prober: cfg.Prober})
}
