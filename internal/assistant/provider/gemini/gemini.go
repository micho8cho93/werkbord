// Package gemini adapts Gemini CLI's ACP mode for isolated assistant turns.
package gemini

import (
	"werkbord/internal/agentcatalog"
	"werkbord/internal/agentkit"
	"werkbord/internal/assistant/provider"
	"werkbord/internal/assistant/provider/acpcli"
)

// ID is the provider's key, the same one the coding-agent adapter uses.
const ID = "gemini-cli"

type Config struct {
	Command   string
	Models    []provider.Model
	Reasoning []string
	// Prober is shared with the coding-agent adapter for the same CLI; nil gives the provider one of its own.
	Prober *agentkit.Prober
}

func New(cfg Config) *acpcli.Provider {
	d := agentcatalog.GeminiCLI()
	command := cfg.Command
	if command == "" {
		command = d.Command
	}
	return acpcli.New(acpcli.Config{
		ID: d.ID, Name: d.Name, Command: command, DocsURL: d.DocsURL, Guidance: d.Setup,
		Args: d.Args, ModelFlag: d.ModelFlag, Models: cfg.Models,
		Reasoning: cfg.Reasoning, CustomModels: true, Prober: cfg.Prober,
	})
}
