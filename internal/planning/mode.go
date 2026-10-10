package planning

// ExecutionMode says who is expected to do a piece of work. It is a classification of the work
// itself and is deliberately not a label: a label is a name the person chose and may rename or
// delete, while the mode is one of three fixed answers that the products act on.
type ExecutionMode string

const (
	// ModeHuman is work a person does. No agent is ever started on it.
	ModeHuman ExecutionMode = "human"
	// ModeAgent is work an agent does, started by hand or by schedule.
	ModeAgent ExecutionMode = "agent"
	// ModeHybrid is work an agent may start on and a person finishes or checks: an agent may be
	// started on it, exactly as for ModeAgent, and the mode tells the people involved that a person
	// is still part of it.
	ModeHybrid ExecutionMode = "hybrid"
)

// ExecutionModes lists the modes in the order an interface offers them.
var ExecutionModes = []ExecutionMode{ModeHuman, ModeAgent, ModeHybrid}

// Valid reports whether m is one of the three modes.
func (m ExecutionMode) Valid() bool {
	return m == ModeHuman || m == ModeAgent || m == ModeHybrid
}

// AllowsAgent reports whether an agent may be started on work of this mode.
func (m ExecutionMode) AllowsAgent() bool { return m == ModeAgent || m == ModeHybrid }

// ParseExecutionMode validates a mode received from outside the process.
func ParseExecutionMode(s string) (ExecutionMode, error) {
	m := ExecutionMode(s)
	if !m.Valid() {
		return "", invalid("unknown execution mode %q (modes: human, agent, hybrid)", s)
	}
	return m, nil
}
