// Package opencode adapts OpenCode's ACP mode for coding runs.
package opencode

import (
	"werkbord/internal/agent/acp"
	"werkbord/internal/domain"
)

const ID = "opencode"
const docsURL = "https://opencode.ai/docs/acp/"

type Config struct {
	Command   string
	Model     string
	Models    []domain.AgentOption
	Reasoning []string
}

func New(cfg Config) *acp.Adapter {
	command := cfg.Command
	if command == "" {
		command = "opencode"
	}
	return acp.New(acp.Config{ID: ID, Name: "OpenCode", Command: command, DocsURL: docsURL, Args: []string{"--pure", "acp"}, SessionOptions: true, Model: cfg.Model, Models: cfg.Models, Reasoning: cfg.Reasoning, ListModels: true})
}
