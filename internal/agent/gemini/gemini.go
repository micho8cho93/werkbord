// Package gemini adapts Gemini CLI's ACP mode for coding runs.
package gemini

import (
	"devboard/internal/agent/acp"
	"devboard/internal/domain"
)

const ID = "gemini-cli"
const docsURL = "https://geminicli.com/docs/cli/acp-mode/"

type Config struct {
	Command   string
	Model     string
	Models    []domain.AgentOption
	Reasoning []string
}

func New(cfg Config) *acp.Adapter {
	command := cfg.Command
	if command == "" {
		command = "gemini"
	}
	return acp.New(acp.Config{ID: ID, Name: "Gemini CLI", Command: command, DocsURL: docsURL, Args: []string{"--acp"}, ModelFlag: "--model", Model: cfg.Model, Models: cfg.Models})
}
