package service

import (
	"context"
	"errors"
	"fmt"

	"devboard/internal/domain"
	"devboard/internal/store"
)

// AgentCatalog is what the services need to know about the agents: which exist,
// and what can be chosen for them. agent.Registry implements it; a nil catalog
// means only the shape of a configuration is checked.
type AgentCatalog interface {
	// Known reports whether an agent with this ID is registered.
	Known(id string) bool
	// Options lists what can be chosen for an agent, with Agent default first. The
	// bool is false for an unknown agent.
	Options(ctx context.Context, id string) (domain.AgentOptions, bool)
}

// validateExecution checks a configuration that is about to be stored: its
// shape, that the agent exists, and, when the agent can say, that the reasoning
// level is one it has. A model is only checked for shape: agents add models
// faster than this code, and the agent has the last word on a name it was handed.
func validateExecution(ctx context.Context, cat AgentCatalog, cfg domain.ExecutionConfig) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	if cat == nil || cfg.Agent == "" {
		return nil
	}
	if !cat.Known(cfg.Agent) {
		return fmt.Errorf("%w: unknown agent %q", domain.ErrInvalid, cfg.Agent)
	}
	if cfg.Reasoning != "" && cfg.Reasoning != domain.AgentDefault {
		if opts, ok := cat.Options(ctx, cfg.Agent); ok && !opts.HasReasoning(cfg.Reasoning) {
			return fmt.Errorf("%w: %s has no reasoning level %q (it offers %s)", domain.ErrInvalid, cfg.Agent, cfg.Reasoning, reasoningNames(opts))
		}
	}
	return nil
}

func reasoningNames(o domain.AgentOptions) string {
	s := ""
	for _, r := range o.Reasoning {
		if r.ID == domain.AgentDefault {
			continue
		}
		if s != "" {
			s += ", "
		}
		s += r.ID
	}
	return s
}

// Levels are the stored levels of the execution hierarchy above a task: the
// global defaults and the task's project.
type Levels struct {
	Global  domain.ExecutionConfig
	Project domain.ExecutionConfig
}

// loadLevels reads the global defaults and a project's defaults.
func loadLevels(ctx context.Context, tx store.Tx, projectID string) (Levels, error) {
	var lv Levels
	if err := tx.Settings().Get(ctx, domain.SettingExecution, &lv.Global); err != nil && !errors.Is(err, domain.ErrNotFound) {
		return lv, err
	}
	if projectID != "" {
		p, err := tx.Projects().Get(ctx, projectID)
		if err != nil {
			return lv, err
		}
		lv.Project = p.Execution
	}
	return lv, nil
}

// Resolve applies the hierarchy to a task, with run on top for choices made
// when starting a run. Task override always wins over the project, and the
// project over the global defaults.
func (l Levels) Resolve(task domain.ExecutionConfig, run domain.ExecutionConfig) domain.Resolved {
	return domain.ResolveExecution(
		domain.Level{Source: domain.SourceRun, Config: run},
		domain.Level{Source: domain.SourceTask, Config: task},
		domain.Level{Source: domain.SourceProject, Config: l.Project},
		domain.Level{Source: domain.SourceGlobal, Config: l.Global},
	)
}
