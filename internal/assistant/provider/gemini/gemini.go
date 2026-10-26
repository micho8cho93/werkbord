// Package gemini adapts Gemini CLI's ACP mode for isolated assistant turns.
package gemini

import (
	"werkbord/internal/assistant/provider"
	"werkbord/internal/assistant/provider/acpcli"
)

const ID = "gemini-cli"
const docsURL = "https://geminicli.com/docs/cli/acp-mode/"

type Config struct {
	Command   string
	Models    []provider.Model
	Reasoning []string
}

func New(cfg Config) *acpcli.Provider {
	command := cfg.Command
	if command == "" {
		command = "gemini"
	}
	return acpcli.New(acpcli.Config{
		ID: ID, Name: "Gemini CLI", Command: command, DocsURL: docsURL,
		Args: []string{"--acp"}, ModelFlag: "--model", Models: cfg.Models,
		Reasoning: cfg.Reasoning, CustomModels: true,
	})
}
