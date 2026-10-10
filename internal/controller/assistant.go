package controller

import (
	"log/slog"
	"path/filepath"
	"time"

	"devboard/internal/appops"
	"devboard/internal/assistant"
	"devboard/internal/assistant/provider"
	claudeprov "devboard/internal/assistant/provider/claude"
	codexprov "devboard/internal/assistant/provider/codex"
	"devboard/internal/config"
	"devboard/internal/runner"
	"devboard/internal/service"
	"devboard/internal/store"
)

// newAssistant builds the conversational assistant on the controller's own services. It uses the Claude Code and Codex
// that are already on this computer, with the sign-in the person gave them: no account, key or model of its own. What
// it can do to the board is only what appops offers, through the same domain services the app uses.
func newAssistant(cfg config.Config, db store.Store, log *slog.Logger, svc assistantServices) (*assistant.Engine, error) {
	cl, cx := cfg.Agents[config.AgentClaudeCode], cfg.Agents[config.AgentCodex]
	reg := provider.NewRegistry()
	for _, p := range []provider.Provider{
		claudeprov.New(claudeprov.Config{Command: cl.Command, Model: cl.Model, Models: providerModels(cl.Models), Reasoning: cl.Reasoning}),
		codexprov.New(codexprov.Config{Command: cx.Command, Model: cx.Model, Models: providerModels(cx.Models)}),
	} {
		if err := reg.Register(p); err != nil {
			return nil, err
		}
	}
	services := appops.Services{Projects: svc.projects, Tasks: svc.tasks, Labels: svc.labels, Runs: svc.runs, Control: svc.control}
	if svc.runner != nil {
		// Answering a question reaches the agent through the runner it is on, as it does from the app.
		services.Answer = svc.runner.Answer
	}
	ops := appops.New(appops.Config{Backend: appops.NewBackend(services), Store: db, Log: log})

	a := cfg.Assistant
	eng := assistant.New(assistant.Config{
		Providers: reg, Ops: ops, Store: db, Log: log,
		WorkDir:     filepath.Join(cfg.DataDir, "assistant"),
		Grants:      assistantGrants(a),
		Projects:    assistantProjects(a),
		TurnTimeout: time.Duration(a.TurnTimeoutSeconds) * time.Second,
		IdleTimeout: time.Duration(a.IdleTimeoutSeconds) * time.Second,
	})
	return eng, nil
}

// assistantGrants is what the assistant may do: everything the operations offer, or, when the person asked for a read-only
// assistant, only the looking.
func assistantGrants(a config.AssistantConfig) []appops.Permission {
	if !a.ReadOnly {
		return appops.AllPermissions
	}
	return []appops.Permission{appops.PermProjectsRead, appops.PermTicketsRead, appops.PermScheduleRead, appops.PermRunsRead, appops.PermQuestionsRead}
}

// assistantProjects is the projects it may see: nil for all of them, which is not the same as an empty list (none).
func assistantProjects(a config.AssistantConfig) []string {
	if len(a.Projects) == 0 {
		return nil
	}
	return append([]string{}, a.Projects...)
}

// assistantServices are the controller's domain services the assistant's operations are made of.
type assistantServices struct {
	projects *service.Projects
	tasks    *service.Tasks
	labels   *service.Labels
	runs     *service.Runs
	control  *service.ControlCenter
	runner   *runner.Manager
}

func providerModels(in []config.ModelChoice) []provider.Model {
	out := make([]provider.Model, 0, len(in))
	for _, m := range in {
		name := m.Name
		if name == "" {
			name = m.ID
		}
		out = append(out, provider.Model{ID: m.ID, Name: name, Description: m.Description})
	}
	return out
}
