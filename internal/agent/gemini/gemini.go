// Package gemini adapts Gemini CLI's ACP mode for coding runs.
package gemini

import (
	"werkbord/internal/agent/acp"
	"werkbord/internal/agentcatalog"
	"werkbord/internal/agentkit"
	"werkbord/internal/domain"
)

// ID is the adapter's stable key, stored on runs.
const ID = "gemini-cli"

type Config struct {
	Command   string
	Model     string
	Models    []domain.AgentOption
	Reasoning []string
	// Prober is shared with the assistant's provider for the same CLI; nil gives the adapter one of its own.
	Prober *agentkit.Prober
}

func New(cfg Config) *acp.Adapter {
	d := agentcatalog.GeminiCLI()
	command := cfg.Command
	if command == "" {
		command = d.Command
	}
	return acp.New(acp.Config{ID: d.ID, Name: d.Name, Command: command, DocsURL: d.DocsURL, Guidance: d.Setup, Args: d.Args, ModelFlag: d.ModelFlag, Model: cfg.Model, Models: cfg.Models, Prober: cfg.Prober})
}
