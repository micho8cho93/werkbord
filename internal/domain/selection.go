package domain

import "context"

// SelectionMode is the user's objective, independent of a provider's model names.
type SelectionMode string

const (
	SelectionLowestCost   SelectionMode = "lowest_cost"
	SelectionMaxReasoning SelectionMode = "max_reasoning"
	SelectionIntelligence SelectionMode = "highest_intelligence"
	SelectionManual       SelectionMode = "manual"
)

func (m SelectionMode) Valid() bool {
	return m == SelectionLowestCost || m == SelectionMaxReasoning || m == SelectionIntelligence || m == SelectionManual
}
func (r Resolved) SelectionMode() SelectionMode {
	if r.Mode != "" {
		return r.Mode
	}
	if r.Agent != "" {
		return SelectionManual
	}
	return SelectionLowestCost
}

// AgentSelectionPreference is an explicit inclusion and billing declaration. Unknown billing never implies free usage.
type AgentSelectionPreference struct {
	// DirectCLI is independent of automatic model selection and restricted assistant support.
	DirectCLI bool `json:"directCLI,omitempty"`
	// CLISignedIn is an explicit owner declaration, never a detected authentication claim.
	CLISignedIn bool                      `json:"cliSignedIn,omitempty"`
	Enabled     bool                      `json:"enabled"`
	Billing     string                    `json:"billing"` // auto, subscription, metered, excluded
	Models      map[string]ModelEconomics `json:"models,omitempty"`
}
type ModelEconomics struct {
	// Prices are USD per million tokens, supplied by the owner, not invented by the classifier.
	Input        *float64 `json:"input,omitempty"`
	Output       *float64 `json:"output,omitempty"`
	Intelligence int      `json:"intelligence,omitempty"` // optional capability tier: 1 light, 2 general, 3 strongest
}
type SelectionSettings struct {
	AllowMetered bool                                `json:"allowMetered"`
	Agents       map[string]AgentSelectionPreference `json:"agents"`
}
type SelectionRequest struct {
	Mode      SelectionMode
	Text      string
	Purpose   string // assistant or coding
	Runner    string
	TaskID    string
	ProjectID string
	// Excluded contains candidates proven unavailable before any task prompt was sent.
	Excluded []string
}
type SelectionDecision struct {
	Agent     string        `json:"agent"`
	Model     string        `json:"model"`
	Reasoning string        `json:"reasoning"`
	Runner    string        `json:"runner,omitempty"`
	Mode      SelectionMode `json:"mode"`
	Method    string        `json:"method"`
	Reason    string        `json:"reason"`
}
type SelectionResolver func(context.Context, SelectionRequest) (SelectionDecision, error)
