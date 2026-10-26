package claude

import (
	"context"

	"werkbord/internal/agent"
	"werkbord/internal/agentkit"
	"werkbord/internal/domain"
)

var _ agent.Optioner = (*Adapter)(nil)

// builtinModels are Claude Code's own aliases (see agentkit.ClaudeModels).
func builtinModels() []domain.AgentOption {
	var out []domain.AgentOption
	for _, m := range agentkit.ClaudeModels() {
		out = append(out, domain.AgentOption{ID: m.ID, Name: m.Name, Description: m.Description})
	}
	return out
}

// supportsEffort reports whether the installed CLI takes --effort.
func (a *Adapter) supportsEffort(ctx context.Context) bool {
	return len(a.cfg.Reasoning) > 0 || len(a.effortLevels(ctx)) > 0
}

// Options implements agent.Optioner. Claude Code cannot list its models, so
// they are the user's configured list or its aliases; the reasoning levels are
// the ones the installed CLI reports for --effort.
func (a *Adapter) Options(ctx context.Context) domain.AgentOptions {
	opts := domain.AgentOptions{AgentID: ID, CustomModels: true, ModelsSource: domain.OptionsBuiltIn, ReasoningSource: domain.OptionsBuiltIn}
	opts.Models = builtinModels()
	if len(a.cfg.Models) > 0 {
		opts.Models, opts.ModelsSource = a.cfg.Models, domain.OptionsConfigured
	}
	levels, src := a.effortLevels(ctx), domain.OptionsFromAgent
	if len(a.cfg.Reasoning) > 0 {
		levels, src = a.cfg.Reasoning, domain.OptionsConfigured
	}
	for _, l := range levels {
		opts.Reasoning = append(opts.Reasoning, domain.AgentOption{ID: l, Name: capitalize(l)})
	}
	if len(levels) == 0 {
		opts.Note = "This version of Claude Code does not list reasoning levels."
	} else {
		opts.ReasoningSource = src
	}
	return opts
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	if s[0] >= 'a' && s[0] <= 'z' {
		return string(s[0]-'a'+'A') + s[1:]
	}
	return s
}
