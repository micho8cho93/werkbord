package shell

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"time"

	"werkbord/desktop/internal/teamlink"
	"werkbord/internal/workspace"
)

var analyticsProject = regexp.MustCompile(`^[A-Za-z0-9_-]{1,80}$`)

// Analytics reads one workspace's analytics report for the window's one Analytics page: the person's own (Personal) or a team's
// coordination figures, as the workspace itself reports them. It is a read, safe to offer the shell page: the workspace is chosen by
// the app's own list (never an address from the page), the project is a plain identifier, the range is one of the three the pages
// offer, and the workspace applies the person's own permissions, so a team never shows a project they cannot see.
func (s *Shell) Analytics(id, project string, days int) (json.RawMessage, error) {
	if days != 7 && days != 30 && days != 90 {
		return nil, errors.New("analytics cover 7, 30 or 90 days")
	}
	if project != "" && !analyticsProject.MatchString(project) {
		return nil, errors.New("that is not a project")
	}
	kind, ok := s.o.Workspaces.Kind(id)
	if !ok {
		return nil, errors.New("that workspace is not on this computer")
	}
	ctx, cancel := s.timeout(20 * time.Second)
	defer cancel()
	switch kind {
	case workspace.KindIndividual:
		r, ok := s.o.Grants.(interface {
			Analytics(context.Context, string, int) (json.RawMessage, error)
		})
		if !ok {
			return nil, errors.New("analytics are unavailable")
		}
		return r.Analytics(ctx, project, days)
	case workspace.KindTeam:
		if _, ok := teamlink.SlotOf(id); !ok {
			return nil, errors.New("not a Team workspace")
		}
		r, ok := s.o.Team.(interface {
			Analytics(context.Context, string, string, int) (json.RawMessage, error)
		})
		if !ok {
			return nil, errors.New("analytics are unavailable")
		}
		return r.Analytics(ctx, id, project, days)
	}
	return nil, errors.New("analytics are unavailable")
}
