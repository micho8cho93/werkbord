package domain

import (
	"fmt"
	"regexp"
	"strings"
)

// Priority is a scheduling preference after due time and execution order.
// It does not imply dependencies or change the earliest start time.
type Priority string

const (
	PriorityLow    Priority = "low"
	PriorityNormal Priority = "normal"
	PriorityHigh   Priority = "high"
)

// Priorities lists the priorities in the order an interface offers them.
var Priorities = []Priority{PriorityLow, PriorityNormal, PriorityHigh}

// Valid reports whether p is a known priority.
func (p Priority) Valid() bool {
	switch p {
	case PriorityLow, PriorityNormal, PriorityHigh:
		return true
	}
	return false
}

// AgentDefault is the model or reasoning value that means "do not pass the
// setting; let the agent use whatever it would have". It is stored and sent as
// this word because an empty value already means "not set here, ask the next
// level", and a task must be able to say "the agent's own default" even when its
// project names a model.
const AgentDefault = "default"

// ExecutionConfig is how work is to be carried out, at one level of the
// hierarchy. Every field is optional and the zero value of a field means "not
// set at this level": the next level decides. It is stored whole as JSON, so a
// new setting is a new field and never a migration.
//
// The levels, highest first, are the run being started, its task, its
// project, and the global defaults; the first level that sets a field wins
// (see ResolveExecution).
//
// Model and Reasoning belong to an agent: a model name means nothing to another
// one. So a level that sets either must also set Agent, and a level's model and
// reasoning are only used when the agent that wins is that level's own agent.
// This is what stops a task that switches from Claude Code to Codex from being
// handed a project default of "opus".
type ExecutionConfig struct {
	// Runner is "automatic", a runner ID, or empty to inherit. No implicit fallback.
	Runner string `json:"runner,omitempty"`
	// Agent is the adapter ID ("claude-code", "codex").
	Agent string `json:"agent,omitempty"`
	// Model is one of the agent's model IDs or aliases, or AgentDefault.
	Model string `json:"model,omitempty"`
	// Reasoning is one of the agent's reasoning or effort levels, or AgentDefault.
	Reasoning string `json:"reasoning,omitempty"`
	// Interaction is whether the agent may stop and ask the user.
	Interaction InteractionPolicy `json:"interaction,omitempty"`
	// Priority is the task's importance. At the global and project level it is
	// the priority new tasks start with.
	Priority Priority `json:"priority,omitempty"`
}

var (
	agentIDRE   = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)
	modelNameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/\[\]+@-]{0,99}$`)
	reasonRE    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,39}$`)
)

// ValidModelName reports whether s could be passed to an agent as a model: it
// is handed to a command line, so it may not look like a flag, contain
// whitespace or control characters, or be absurdly long. Whether the agent
// knows the model is the agent's business; names change faster than this code.
func ValidModelName(s string) bool { return modelNameRE.MatchString(s) }

// ValidReasoning reports whether s is shaped like a reasoning level.
func ValidReasoning(s string) bool { return reasonRE.MatchString(s) }

// Normalized trims the free-text fields.
func (c ExecutionConfig) Normalized() ExecutionConfig {
	c.Agent = strings.TrimSpace(c.Agent)
	c.Model = strings.TrimSpace(c.Model)
	c.Reasoning = strings.TrimSpace(c.Reasoning)
	return c
}

// IsZero reports whether nothing is set.
func (c ExecutionConfig) IsZero() bool { return c == ExecutionConfig{} }

// Validate checks the shape of every field that is set. It does not know which
// agents or models exist; the service checks the agent, and the agent itself has
// the last word on a model.
func (c ExecutionConfig) Validate() error {
	c = c.Normalized()
	if c.Runner != "" && c.Runner != "automatic" && (!strings.HasPrefix(c.Runner, "rnr_") || len(c.Runner) > 100) {
		return fmt.Errorf("%w: invalid runner ID", ErrInvalid)
	}
	if c.Agent != "" && !agentIDRE.MatchString(c.Agent) {
		return fmt.Errorf("%w: %q is not an agent ID", ErrInvalid, c.Agent)
	}
	if c.Model != "" && c.Model != AgentDefault && !ValidModelName(c.Model) {
		return fmt.Errorf("%w: %q is not a usable model name", ErrInvalid, c.Model)
	}
	if c.Reasoning != "" && c.Reasoning != AgentDefault && !ValidReasoning(c.Reasoning) {
		return fmt.Errorf("%w: %q is not a usable reasoning level", ErrInvalid, c.Reasoning)
	}
	if c.Agent == "" && (c.Model != "" || c.Reasoning != "") {
		return fmt.Errorf("%w: choose an agent to choose its model or reasoning level: they belong to an agent", ErrInvalid)
	}
	if c.Interaction != "" && !c.Interaction.Valid() {
		return fmt.Errorf("%w: unknown interaction policy %q (use %s)", ErrInvalid, c.Interaction, policyNames())
	}
	if c.Priority != "" && !c.Priority.Valid() {
		return fmt.Errorf("%w: unknown priority %q (use low, normal or high)", ErrInvalid, c.Priority)
	}
	return nil
}

// Source says which level of the hierarchy supplied a resolved setting.
type Source string

const (
	SourceRun     Source = "run"
	SourceTask    Source = "task"
	SourceProject Source = "project"
	SourceGlobal  Source = "global"
	// SourceDefault: nothing set it, so the built-in default applies. For a
	// model or reasoning level that is the agent's own.
	SourceDefault Source = "default"
)

// Resolved is the outcome of resolving the hierarchy: what a run will use.
type Resolved struct {
	Runner string `json:"runner"`
	// Agent is empty when no level chose one; the controller then uses the first
	// agent that can be used.
	Agent string `json:"agent"`
	// Model and Reasoning are empty for "the agent's default": nothing is passed.
	Model       string            `json:"model"`
	Reasoning   string            `json:"reasoning"`
	Interaction InteractionPolicy `json:"interaction"`
	Priority    Priority          `json:"priority"`

	Sources ResolvedSources `json:"sources"`
}

// ResolvedSources names the level that supplied each field of a Resolved.
type ResolvedSources struct {
	Agent       Source `json:"agent"`
	Model       Source `json:"model"`
	Reasoning   Source `json:"reasoning"`
	Interaction Source `json:"interaction"`
	Priority    Source `json:"priority"`
}

// Policy is the resolved interaction as the execution policy a run carries.
func (r Resolved) Policy() ExecutionPolicy { return ExecutionPolicy{Interaction: r.Interaction} }

// Level is one level of the hierarchy, named for Resolved.Sources.
type Level struct {
	Source Source
	Config ExecutionConfig
}

// ResolveExecution applies the hierarchy. levels are in order of precedence,
// highest first; typically run, task, project, global. The first level that
// sets a field supplies it ("task override always wins"), with two rules for the
// fields that belong to an agent: the agent is resolved first, and a level's
// model or reasoning is only considered if that level's own agent is the one
// that won. When no level names an agent, Resolved.Agent is empty and the
// caller picks one.
func ResolveExecution(levels ...Level) Resolved {
	r := Resolved{
		Interaction: InteractionInteractive, Priority: PriorityNormal,
		Sources: ResolvedSources{Agent: SourceDefault, Model: SourceDefault, Reasoning: SourceDefault, Interaction: SourceDefault, Priority: SourceDefault},
	}
	for _, l := range levels {
		if l.Config.Runner != "" {
			r.Runner = l.Config.Runner
			break
		}
	}
	for _, l := range levels {
		if l.Config.Agent != "" {
			r.Agent, r.Sources.Agent = l.Config.Agent, l.Source
			break
		}
	}
	for _, l := range levels {
		if l.Config.Interaction != "" {
			r.Interaction, r.Sources.Interaction = l.Config.Interaction, l.Source
			break
		}
	}
	for _, l := range levels {
		if l.Config.Priority != "" {
			r.Priority, r.Sources.Priority = l.Config.Priority, l.Source
			break
		}
	}
	for _, l := range levels {
		if l.Config.Agent != r.Agent {
			continue // another agent's model means nothing here
		}
		if l.Config.Model != "" {
			r.Sources.Model = l.Source
			if l.Config.Model != AgentDefault {
				r.Model = l.Config.Model
			}
			break
		}
	}
	for _, l := range levels {
		if l.Config.Agent != r.Agent {
			continue
		}
		if l.Config.Reasoning != "" {
			r.Sources.Reasoning = l.Source
			if l.Config.Reasoning != AgentDefault {
				r.Reasoning = l.Config.Reasoning
			}
			break
		}
	}
	return r
}
