// Package opencode adapts OpenCode's ACP mode for isolated assistant turns.
package opencode

import (
	"werkbord/internal/assistant/provider"
	"werkbord/internal/assistant/provider/acpcli"
)

const ID = "opencode"
const docsURL = "https://opencode.ai/docs/acp/"

type Config struct {
	Command   string
	Models    []provider.Model
	Reasoning []string
}

func New(cfg Config) *acpcli.Provider {
	command := cfg.Command
	if command == "" {
		command = "opencode"
	}
	return acpcli.New(acpcli.Config{
		ID: ID, Name: "OpenCode", Command: command, DocsURL: docsURL,
		Args: []string{"--pure", "acp"}, SessionOptions: true,
		Models: cfg.Models, Reasoning: cfg.Reasoning, CustomModels: true, ListModels: true,
	})
}
