package shell

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// AgentsAndConnections reads what the header's "Agents & connections" popover shows for one of the person's own projects: the coding
// agents found, the connected tools and the last thirty days of usage, already reduced by the app (no command, environment, secret or
// path). It is Personal only (a Team workspace never reaches into a computer), a read, and the project is a plain identifier the app
// checks.
func (s *Shell) AgentsAndConnections(project string) (json.RawMessage, error) {
	if !analyticsProject.MatchString(project) {
		return nil, errors.New("that is not a project")
	}
	r, ok := s.o.Grants.(interface {
		AgentsAndConnections(context.Context, string) (json.RawMessage, error)
	})
	if !ok {
		return nil, errors.New("agents and connections are unavailable")
	}
	ctx, cancel := s.timeout(20 * time.Second)
	defer cancel()
	return r.AgentsAndConnections(ctx, project)
}
