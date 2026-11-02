package shell

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// ProjectHead reads where one of the person's own projects stands in Git: the branch that is checked out and the short commit, for the
// header. It is Personal only (a Team workspace never reaches into a computer), a read, and narrow: the project is a plain identifier
// the app checks, and what comes back is a branch name and a short commit, never a path, a remote or a diff.
func (s *Shell) ProjectHead(project string) (json.RawMessage, error) {
	if !analyticsProject.MatchString(project) {
		return nil, errors.New("that is not a project")
	}
	r, ok := s.o.Grants.(interface {
		ProjectHead(context.Context, string) (json.RawMessage, error)
	})
	if !ok {
		return nil, errors.New("git is unavailable")
	}
	ctx, cancel := s.timeout(20 * time.Second)
	defer cancel()
	return r.ProjectHead(ctx, project)
}
