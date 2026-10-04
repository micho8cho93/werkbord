package domain

import (
	"errors"
	"testing"
)

func lv(src Source, c ExecutionConfig) Level { return Level{Source: src, Config: c} }

func TestResolveExecutionDefaults(t *testing.T) {
	r := ResolveExecution()
	if r.Agent != "" || r.Model != "" || r.Reasoning != "" || r.Interaction != InteractionInteractive || r.Priority != PriorityNormal {
		t.Fatalf("nothing configured = %+v", r)
	}
	want := ResolvedSources{SourceDefault, SourceDefault, SourceDefault, SourceDefault, SourceDefault}
	if r.Sources != want {
		t.Fatalf("sources = %+v", r.Sources)
	}
}

func TestResolveExecutionHierarchy(t *testing.T) {
	global := ExecutionConfig{Agent: "claude-code", Model: "sonnet", Reasoning: "medium", Interaction: InteractionAutonomous, Priority: PriorityLow}
	project := ExecutionConfig{Agent: "claude-code", Model: "opus", Priority: PriorityHigh}
	task := ExecutionConfig{Reasoning: "", Interaction: InteractionInteractive}

	cases := []struct {
		name   string
		levels []Level
		want   Resolved
	}{
		{"global only", []Level{lv(SourceTask, ExecutionConfig{}), lv(SourceProject, ExecutionConfig{}), lv(SourceGlobal, global)},
			Resolved{Agent: "claude-code", Model: "sonnet", Reasoning: "medium", Interaction: InteractionAutonomous, Priority: PriorityLow,
				Sources: ResolvedSources{SourceGlobal, SourceGlobal, SourceGlobal, SourceGlobal, SourceGlobal}}},
		{"project over global", []Level{lv(SourceTask, ExecutionConfig{}), lv(SourceProject, project), lv(SourceGlobal, global)},
			Resolved{Agent: "claude-code", Model: "opus", Reasoning: "medium", Interaction: InteractionAutonomous, Priority: PriorityHigh,
				Sources: ResolvedSources{SourceProject, SourceProject, SourceGlobal, SourceGlobal, SourceProject}}},
		{"task over project over global", []Level{lv(SourceTask, task), lv(SourceProject, project), lv(SourceGlobal, global)},
			Resolved{Agent: "claude-code", Model: "opus", Reasoning: "medium", Interaction: InteractionInteractive, Priority: PriorityHigh,
				Sources: ResolvedSources{SourceProject, SourceProject, SourceGlobal, SourceTask, SourceProject}}},
		{"a run beats a task", []Level{lv(SourceRun, ExecutionConfig{Interaction: InteractionAutonomousStopIfBlocked}), lv(SourceTask, task), lv(SourceGlobal, global)},
			Resolved{Agent: "claude-code", Model: "sonnet", Reasoning: "medium", Interaction: InteractionAutonomousStopIfBlocked, Priority: PriorityLow,
				Sources: ResolvedSources{SourceGlobal, SourceGlobal, SourceGlobal, SourceRun, SourceGlobal}}},
		{"a task's full override wins everywhere",
			[]Level{lv(SourceTask, ExecutionConfig{Agent: "claude-code", Model: "haiku", Reasoning: "low", Interaction: InteractionAutonomous, Priority: PriorityNormal}), lv(SourceProject, project), lv(SourceGlobal, global)},
			Resolved{Agent: "claude-code", Model: "haiku", Reasoning: "low", Interaction: InteractionAutonomous, Priority: PriorityNormal,
				Sources: ResolvedSources{SourceTask, SourceTask, SourceTask, SourceTask, SourceTask}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ResolveExecution(tc.levels...); got != tc.want {
				t.Errorf("got  %+v\nwant %+v", got, tc.want)
			}
		})
	}
}

func TestAgentDefaultOverridesAnInheritedModel(t *testing.T) {
	project := ExecutionConfig{Agent: "codex", Model: "gpt-x", Reasoning: "high"}
	task := ExecutionConfig{Agent: "codex", Model: AgentDefault, Reasoning: AgentDefault}
	r := ResolveExecution(lv(SourceTask, task), lv(SourceProject, project))
	if r.Model != "" || r.Reasoning != "" || r.Sources.Model != SourceTask || r.Sources.Reasoning != SourceTask {
		t.Fatalf("explicit agent default = %+v", r)
	}
}

func TestAModelBelongsToItsAgentNotToTheHierarchy(t *testing.T) {
	project := ExecutionConfig{Agent: "claude-code", Model: "opus", Reasoning: "high"}
	global := ExecutionConfig{Agent: "claude-code", Model: "sonnet"}

	// A task that switches agent gets neither the project's nor the global model.
	r := ResolveExecution(lv(SourceTask, ExecutionConfig{Agent: "codex"}), lv(SourceProject, project), lv(SourceGlobal, global))
	if r.Agent != "codex" || r.Model != "" || r.Reasoning != "" || r.Sources.Model != SourceDefault {
		t.Fatalf("switched agent = %+v", r)
	}
	// Its own model, for its own agent, is used.
	r = ResolveExecution(lv(SourceTask, ExecutionConfig{Agent: "codex", Model: "gpt-x"}), lv(SourceProject, project))
	if r.Model != "gpt-x" || r.Reasoning != "" {
		t.Fatalf("own model = %+v", r)
	}
	// A project on another agent does not leak its model into a global default that names the same agent as the winner.
	r = ResolveExecution(lv(SourceTask, ExecutionConfig{}), lv(SourceProject, ExecutionConfig{Agent: "codex", Model: "gpt-x"}), lv(SourceGlobal, global))
	if r.Agent != "codex" || r.Model != "gpt-x" {
		t.Fatalf("project agent wins with its model = %+v", r)
	}
	// A task that names the same agent as the project but no model still inherits the project's model.
	r = ResolveExecution(lv(SourceTask, ExecutionConfig{Agent: "claude-code"}), lv(SourceProject, project))
	if r.Model != "opus" || r.Reasoning != "high" {
		t.Fatalf("same agent inherits = %+v", r)
	}
}

func TestExecutionConfigValidate(t *testing.T) {
	ok := []ExecutionConfig{
		{},
		{Agent: "claude-code", Model: "opus", Reasoning: "high", Interaction: InteractionAutonomous, Priority: PriorityHigh},
		{Agent: "codex", Model: AgentDefault, Reasoning: AgentDefault},
		{Agent: "codex", Model: "gpt-5.5"},
		{Agent: "claude-code", Model: "claude-opus-4-1-20250805"},
		{Agent: "claude-code", Model: "sonnet[1m]"},
		{Agent: "x", Model: "us.anthropic.claude-sonnet:0"},
		{Interaction: InteractionInteractive},
	}
	for _, c := range ok {
		if err := c.Validate(); err != nil {
			t.Errorf("%+v: %v", c, err)
		}
	}
	bad := []ExecutionConfig{
		{Model: "opus"},                                  // a model needs an agent
		{Reasoning: "high"},                              // so does a reasoning level
		{Agent: "Claude Code"},                           // not an ID
		{Agent: "codex", Model: "--dangerously"},         // looks like a flag
		{Agent: "codex", Model: "two words"},             // whitespace
		{Agent: "codex", Model: "a\nb"},                  // control character
		{Agent: "codex", Model: string(make([]byte, 5))}, // NULs
		{Agent: "codex", Reasoning: "-1"},
		{Agent: "codex", Reasoning: "very high"},
		{Interaction: "reckless"},
		{Priority: "yesterday"},
	}
	for _, c := range bad {
		if err := c.Validate(); !errors.Is(err, ErrInvalid) {
			t.Errorf("%+v accepted: %v", c, err)
		}
	}
	long := make([]byte, 101)
	for i := range long {
		long[i] = 'a'
	}
	if err := (ExecutionConfig{Agent: "codex", Model: string(long)}).Validate(); !errors.Is(err, ErrInvalid) {
		t.Errorf("a model name of 101 characters was accepted")
	}
}

func TestAgentOptionsAlwaysStartWithTheAgentDefault(t *testing.T) {
	o := AgentOptions{
		Models:    []AgentOption{{ID: "m1", Name: "M1"}, {ID: AgentDefault, Name: "stale"}},
		Reasoning: []AgentOption{{ID: "low"}, {ID: "high"}},
	}.WithAgentDefault()
	if o.Models[0].ID != AgentDefault || o.Models[0].Name != "Agent default" || len(o.Models) != 2 || o.Models[1].ID != "m1" {
		t.Fatalf("models = %+v", o.Models)
	}
	if o.Reasoning[0].ID != AgentDefault || len(o.Reasoning) != 3 {
		t.Fatalf("reasoning = %+v", o.Reasoning)
	}
	empty := AgentOptions{}.WithAgentDefault()
	if len(empty.Models) != 1 || len(empty.Reasoning) != 1 {
		t.Fatalf("an agent that lists nothing still offers the default: %+v", empty)
	}
	if !empty.HasReasoning("anything") {
		t.Error("with no known levels nothing may be rejected")
	}
	if o.HasReasoning("turbo") || !o.HasReasoning("high") || !o.HasReasoning(AgentDefault) || !o.HasReasoning("") {
		t.Error("HasReasoning is wrong")
	}
}
