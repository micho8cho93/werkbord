package domain

import (
	"fmt"
	"strings"
	"time"
)

// InteractionPolicy says how much an agent may lean on the user during a run.
// It is only about *conversation*: whether the agent may stop and ask. It never
// grants or removes a capability. What an agent may do (edit files, run
// commands, touch Git) is decided by its own permission system, which the
// controller leaves as configured, and a permission request is put to the user
// under every policy.
type InteractionPolicy string

const (
	// InteractionInteractive: the agent may ask for clarification or a decision,
	// and the run waits for the answer. This is the default.
	InteractionInteractive InteractionPolicy = "interactive"
	// InteractionAutonomous: the user does not want routine questions. The agent
	// is told to investigate and decide for itself and to keep going until it
	// considers the task complete. If it asks anyway, the controller answers on
	// the user's behalf (see agent.HandleQuestion).
	InteractionAutonomous InteractionPolicy = "autonomous"
	// InteractionAutonomousStopIfBlocked: as autonomous, but a decision the agent
	// cannot safely infer stops the run instead of being guessed. The run becomes
	// RunBlocked with a structured Blocker.
	InteractionAutonomousStopIfBlocked InteractionPolicy = "autonomous_stop_if_blocked"
)

// InteractionPolicies lists the policies in the order an interface offers them.
var InteractionPolicies = []InteractionPolicy{InteractionInteractive, InteractionAutonomous, InteractionAutonomousStopIfBlocked}

// Valid reports whether p is a known policy.
func (p InteractionPolicy) Valid() bool {
	switch p {
	case InteractionInteractive, InteractionAutonomous, InteractionAutonomousStopIfBlocked:
		return true
	}
	return false
}

// Autonomous reports whether the user asked not to be asked routine questions.
func (p InteractionPolicy) Autonomous() bool {
	return p == InteractionAutonomous || p == InteractionAutonomousStopIfBlocked
}

// ExecutionPolicy is how a task is to be carried out. A task has one, as the
// default for its runs; each run keeps the one it was started with, so changing
// a task never changes a session that is already working.
//
// It is a struct, stored whole as JSON, so that finer-grained permissions (what
// an agent may do to the filesystem, to Git, to the network) can be added as
// further fields without a migration. The zero value of every field must mean
// "as before", and an ExecutionPolicy read from storage is always Normalized.
type ExecutionPolicy struct {
	Interaction InteractionPolicy `json:"interaction"`
}

// DefaultExecutionPolicy is what a task has unless the user chose otherwise.
func DefaultExecutionPolicy() ExecutionPolicy {
	return ExecutionPolicy{Interaction: InteractionInteractive}
}

// Normalized returns p with every unset field at its default.
func (p ExecutionPolicy) Normalized() ExecutionPolicy {
	if p.Interaction == "" {
		p.Interaction = InteractionInteractive
	}
	return p
}

// Validate reports whether p, once normalized, names only known values.
func (p ExecutionPolicy) Validate() error {
	if n := p.Normalized(); !n.Interaction.Valid() {
		return fmt.Errorf("%w: unknown interaction policy %q (use %s)", ErrInvalid, p.Interaction, policyNames())
	}
	return nil
}

func policyNames() string {
	names := make([]string, len(InteractionPolicies))
	for i, p := range InteractionPolicies {
		names[i] = string(p)
	}
	return strings.Join(names, ", ")
}

// ParseInteraction validates a policy received from outside the process.
func ParseInteraction(s string) (InteractionPolicy, error) {
	p := InteractionPolicy(s)
	if !p.Valid() {
		return "", fmt.Errorf("%w: unknown interaction policy %q (use %s)", ErrInvalid, s, policyNames())
	}
	return p, nil
}

// BlockerSource says how the controller learned that a run is blocked.
type BlockerSource string

const (
	// BlockerQuestion: the agent asked the user something its policy does not
	// allow it to ask, so the controller stopped the run instead.
	BlockerQuestion BlockerSource = "question"
	// BlockerReport: the agent ended its turn with a blocker report, as it was
	// told to when it could not decide.
	BlockerReport BlockerSource = "report"
)

// Blocker explains why a run stopped rather than guess. It is persisted on the
// run, so the explanation survives a restart and is the same on every device.
type Blocker struct {
	// Summary is what is blocking the run, in a sentence.
	Summary string `json:"summary"`
	// Detail is what the user needs to decide: context, what was tried, what the
	// agent would have done. Plain text, optional.
	Detail string `json:"detail,omitempty"`
	// Options are the choices the agent saw, when it had put some forward.
	Options []string `json:"options,omitempty"`
	// Source says how the controller found out.
	Source BlockerSource `json:"source"`
	// Kind is the kind of question the agent asked, for BlockerQuestion.
	Kind QuestionKind `json:"kind,omitempty"`
	// RaisedAt is when the run became blocked.
	RaisedAt time.Time `json:"raisedAt"`
}

// Validate checks a blocker before it is stored.
func (b *Blocker) Validate() error {
	if strings.TrimSpace(b.Summary) == "" {
		return fmt.Errorf("%w: a blocker needs a summary", ErrInvalid)
	}
	switch b.Source {
	case BlockerQuestion, BlockerReport:
	default:
		return fmt.Errorf("%w: unknown blocker source %q", ErrInvalid, b.Source)
	}
	return nil
}
