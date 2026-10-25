package shell

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"devboard/internal/integration"
	"devboard/internal/workspace"
)

type projectMoves struct {
	sync.Mutex
	pending map[string]*projectMove
}
type projectMove struct {
	Source, Destination, Project, SourceName, DestinationName string
	Snapshot                                                  integration.ProjectTransfer
	At                                                        time.Time
	Result                                                    *ProjectMoveResult
}
type ProjectMovePreview struct {
	Token       string                      `json:"token"`
	Source      string                      `json:"source"`
	Destination string                      `json:"destination"`
	Snapshot    integration.ProjectTransfer `json:"snapshot"`
}
type ProjectMoveResult struct {
	ID        string `json:"id"`
	Archived  bool   `json:"archived"`
	Warning   string `json:"warning,omitempty"`
	Cancelled bool   `json:"cancelled,omitempty"`
}

func (s *Shell) transfer(ctx context.Context, where, operation, id string, in *integration.ProjectTransfer) (integration.ProjectTransfer, string, error) {
	kind, ok := s.o.Workspaces.Kind(where)
	if !ok {
		return integration.ProjectTransfer{}, "", errors.New("that workspace is not connected")
	}
	if kind == workspace.KindIndividual {
		c, ok := s.o.Grants.(interface {
			ProjectTransfer(context.Context, string, string, *integration.ProjectTransfer) (integration.ProjectTransfer, string, error)
		})
		if ok {
			return c.ProjectTransfer(ctx, operation, id, in)
		}
	} else if kind == workspace.KindTeam {
		c, ok := s.o.Team.(interface {
			ProjectTransfer(context.Context, string, string, string, *integration.ProjectTransfer) (integration.ProjectTransfer, string, error)
		})
		if ok {
			return c.ProjectTransfer(ctx, where, operation, id, in)
		}
	}
	return integration.ProjectTransfer{}, "", errors.New("project moves are unavailable")
}

// PreviewProjectMove reads from a registered workspace only and keeps the reviewed
// snapshot in native memory. The page cannot replace it at confirmation time.
func (s *Shell) PreviewProjectMove(source, destination, project string) (ProjectMovePreview, error) {
	var out ProjectMovePreview
	if !analyticsProject.MatchString(project) {
		return out, errors.New("invalid project")
	}
	ctx, cancel := s.timeout(20 * time.Second)
	defer cancel()
	view := s.o.Workspaces.Refresh(ctx)
	var a, b workspace.Entry
	for _, w := range view.Items {
		if w.ID == source {
			a = w.Entry
		}
		if w.ID == destination {
			b = w.Entry
		}
	}
	if a.ID == "" || b.ID == "" || a.State != workspace.StateReady || b.State != workspace.StateReady || a.Kind == b.Kind || (a.Kind != workspace.KindIndividual && b.Kind != workspace.KindIndividual) {
		return out, errors.New("choose Personal and a connected Team")
	}
	snapshot, _, err := s.transfer(ctx, source, "export", project, nil)
	if err != nil {
		return out, err
	}
	snapshot.Source = source + "/" + project + "/" + destination
	if err := snapshot.Validate(); err != nil {
		return out, err
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return out, err
	}
	token := hex.EncodeToString(nonce[:])
	s.projectMoves.Lock()
	defer s.projectMoves.Unlock()
	if s.projectMoves.pending == nil {
		s.projectMoves.pending = map[string]*projectMove{}
	}
	for key, p := range s.projectMoves.pending {
		if time.Since(p.At) > 15*time.Minute || p.Source == source && p.Project == project {
			delete(s.projectMoves.pending, key)
		}
	}
	if len(s.projectMoves.pending) >= 20 {
		return out, errors.New("close earlier project reviews or wait 15 minutes")
	}
	s.projectMoves.pending[token] = &projectMove{Source: source, Destination: destination, Project: project, SourceName: a.Name, DestinationName: b.Name, Snapshot: snapshot, At: time.Now()}
	return ProjectMovePreview{Token: token, Source: a.Name, Destination: b.Name, Snapshot: snapshot}, nil
}

// ConfirmProjectMove is shell-only and always requires a native confirmation.
// Import commits the entire board atomically; archiving compares the reviewed
// text inside the source transaction. A failed archive keeps both copies safe.
func (s *Shell) ConfirmProjectMove(token string) (ProjectMoveResult, error) {
	s.projectMoves.Lock()
	defer s.projectMoves.Unlock()
	p := s.projectMoves.pending[token]
	if p == nil || time.Since(p.At) > 15*time.Minute {
		return ProjectMoveResult{}, errors.New("this review expired; review the project again")
	}
	if p.Result != nil {
		return *p.Result, nil
	}
	choice := s.o.UI.Ask(Dialog{Kind: Question, Title: "Move " + p.Snapshot.Name + "?", Message: fmt.Sprintf("From %s to %s.\n\nThe reviewed project name, repository reference and %d ticket texts will be copied. Tickets start in Backlog / Available as human work. The source is archived after the destination is saved.\n\nRuns, history, files, agent output, comments, labels, dates, assignments and schedules stay at the source. The destination has no local checkout. Text may contain private information; only move text you reviewed.", p.SourceName, p.DestinationName, len(p.Snapshot.Tickets)), Buttons: []string{"Move project", "Cancel"}, Default: "Cancel", Cancel: "Cancel"})
	if choice != "Move project" {
		return ProjectMoveResult{Cancelled: true}, nil
	}
	ctx, cancel := s.timeout(45 * time.Second)
	defer cancel()
	live, _, err := s.transfer(ctx, p.Source, "export", p.Project, nil)
	if err != nil {
		return ProjectMoveResult{}, err
	}
	if !live.Equal(p.Snapshot) {
		return ProjectMoveResult{}, errors.New("the project changed; close this review and review it again")
	}
	_, id, err := s.transfer(ctx, p.Destination, "import", "", &p.Snapshot)
	if err != nil {
		return ProjectMoveResult{}, fmt.Errorf("destination could not be confirmed: %w. The source stays active. Retry this review to safely check the same import", err)
	}
	if !analyticsProject.MatchString(id) {
		return ProjectMoveResult{}, errors.New("destination returned an invalid project; check both boards before retrying")
	}
	result := ProjectMoveResult{ID: id}
	_, _, err = s.transfer(ctx, p.Source, "archive", p.Project, &p.Snapshot)
	if err != nil {
		result.Warning = "Destination saved. Source archive could not be confirmed: " + err.Error() + ". Check both projects before making another move."
	} else {
		result.Archived = true
	}
	p.Result = &result
	return result, nil
}
