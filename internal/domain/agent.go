package domain

// Agent describes a coding agent the controller knows how to drive, such as
// Claude Code or Codex. Agents are discovered at runtime from the registered
// adapters and are not persisted; runs refer to them by ID.
type Agent struct {
	ID        string `json:"id"`   // stable key, e.g. "claude-code"
	Name      string `json:"name"` // display name
	Available bool   `json:"available"`
	Version   string `json:"version,omitempty"`
	Detail    string `json:"detail,omitempty"` // human-readable reason when unavailable
}
