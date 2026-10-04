package domain

// SignIn is whether an agent CLI is signed in to the user's own account. Dev
// Board never asks for an API key: it uses whatever the CLI is already signed
// in with, and can only report what the CLI is willing to say.
type SignIn string

const (
	SignedIn  SignIn = "signed_in"
	SignedOut SignIn = "signed_out"
	// SignInUnknown: the CLI has no safe way to ask (an older version, or a
	// failure that is not evidence of anything). The agent is treated as usable
	// and a real failure shows up when a session starts.
	SignInUnknown SignIn = "unknown"
)

// Agent describes a coding agent the controller knows how to drive, such as
// Claude Code or Codex. Agents are discovered at runtime from the registered
// adapters and are not persisted; runs refer to them by ID.
type Agent struct {
	ID   string `json:"id"`   // stable key, e.g. "claude-code"
	Name string `json:"name"` // display name
	// Installed: the executable was found and runs.
	Installed bool `json:"installed"`
	// SignIn is empty when the agent is not installed.
	SignIn SignIn `json:"signIn,omitempty"`
	// Available: it can be started now (installed and not known to be signed out).
	Available bool   `json:"available"`
	Version   string `json:"version,omitempty"`
	Detail    string `json:"detail,omitempty"` // human-readable reason when unavailable
	// Guidance is the next step when the agent is not available: how to install
	// it or sign in. It never installs anything itself.
	Guidance string `json:"guidance,omitempty"`
	DocsURL  string `json:"docsUrl,omitempty"`
}

// AgentOption is one choice in an agent's list of models or reasoning levels.
type AgentOption struct {
	// ID is what is stored and passed to the agent. AgentDefault means "pass nothing".
	ID   string `json:"id"`
	Name string `json:"name"`
	// Description is one line, if the agent supplied one.
	Description string `json:"description,omitempty"`
	// Default marks the option the agent itself uses when nothing is passed.
	Default bool `json:"default,omitempty"`
	// Reasoning, on a model, lists the reasoning levels it supports when the
	// agent says (an empty list means "all the agent's levels, or unknown").
	Reasoning []string `json:"reasoning,omitempty"`
}

// OptionsSource says where an agent's list of options came from.
type OptionsSource string

const (
	// OptionsFromAgent: the agent reported them itself, so they are current.
	OptionsFromAgent OptionsSource = "agent"
	// OptionsConfigured: the user listed them in config.json.
	OptionsConfigured OptionsSource = "configured"
	// OptionsBuiltIn: the adapter's own short list of stable aliases; the agent
	// could not be asked.
	OptionsBuiltIn OptionsSource = "builtin"
)

// AgentOptions is what can be chosen for an agent. Models and Reasoning always
// begin with the "Agent default" option, which passes nothing.
type AgentOptions struct {
	AgentID string `json:"agentId"`
	// Models and Reasoning describe themselves; the first of each is Agent default.
	Models    []AgentOption `json:"models"`
	Reasoning []AgentOption `json:"reasoning"`
	// ModelsSource and ReasoningSource say how current each list is.
	ModelsSource    OptionsSource `json:"modelsSource"`
	ReasoningSource OptionsSource `json:"reasoningSource"`
	// CustomModels says that a model that is not listed may still be typed in: the
	// lists are suggestions, and the agent has the last word.
	CustomModels bool `json:"customModels"`
	// Note explains a gap, e.g. why the list could not be fetched.
	Note string `json:"note,omitempty"`
}

// WithAgentDefault returns opts with the "Agent default" option first in each
// list, replacing any the adapter put there.
func (o AgentOptions) WithAgentDefault() AgentOptions {
	def := func(name string) AgentOption {
		return AgentOption{ID: AgentDefault, Name: name, Description: "Let the agent choose"}
	}
	strip := func(in []AgentOption) []AgentOption {
		out := make([]AgentOption, 0, len(in)+1)
		for _, x := range in {
			if x.ID != AgentDefault && x.ID != "" {
				out = append(out, x)
			}
		}
		return out
	}
	o.Models = append([]AgentOption{def("Agent default")}, strip(o.Models)...)
	o.Reasoning = append([]AgentOption{def("Agent default")}, strip(o.Reasoning)...)
	return o
}

// HasReasoning reports whether id is one of the agent's reasoning levels (or
// the default). A list with nothing but the default means the agent has none
// the controller knows about, in which case nothing is rejected.
func (o AgentOptions) HasReasoning(id string) bool {
	if id == "" || id == AgentDefault || len(o.Reasoning) <= 1 {
		return true
	}
	for _, r := range o.Reasoning {
		if r.ID == id {
			return true
		}
	}
	return false
}
