package agent

import (
	"werkbord/internal/agentkit"
	"werkbord/internal/domain"
)

// FromFacts turns what agentkit found out about an agent's command line into the form the API and the run screens use.
func FromFacts(f agentkit.Facts) domain.Agent {
	a := domain.Agent{ID: f.ID, Name: f.Name, DocsURL: f.DocsURL, Installed: f.Installed, Version: f.Version, Detail: f.Detail, Guidance: f.Guidance, Available: f.Available()}
	switch {
	case !f.Installed:
	case f.SignedIn == nil:
		a.SignIn = domain.SignInUnknown
	case *f.SignedIn:
		a.SignIn = domain.SignedIn
	default:
		a.SignIn = domain.SignedOut
	}
	return a
}
