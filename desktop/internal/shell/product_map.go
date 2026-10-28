package shell

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"time"
	"werkbord/desktop/internal/teamlink"
	"werkbord/internal/integration"
	"werkbord/internal/workspace"
)

type productMapShares struct {
	sync.Mutex
	pending map[string]*productMapShare
}
type productMapShare struct {
	Workspace, Project, Proposal string
	Snapshot                     integration.ProductMapSnapshot
	Expected                     int64
	At                           time.Time
	Result                       string
}
type ProductMapSharePreview struct {
	Token     string                         `json:"token"`
	Workspace string                         `json:"workspace"`
	Project   string                         `json:"project"`
	Snapshot  integration.ProductMapSnapshot `json:"snapshot"`
}

// PreviewProductMapShare mints a one-use, exact-snapshot publication grant. Only
// the trusted shell can use it; neither workspace frame gets a general relay.
func (s *Shell) PreviewProductMapShare(local, w, p string) (ProductMapSharePreview, error) {
	var out ProductMapSharePreview
	if !analyticsProject.MatchString(local) || !analyticsProject.MatchString(p) {
		return out, errors.New("invalid project")
	}
	ctx, cancel := s.timeout(20 * time.Second)
	defer cancel()
	view := s.o.Workspaces.Refresh(ctx)
	ready := false
	for _, entry := range view.Items {
		if entry.ID == w && entry.Kind == workspace.KindTeam && entry.State == workspace.StateReady {
			ready = true
		}
	}
	if !ready {
		return out, errors.New("connect the Team workspace first")
	}
	source, ok := s.o.Grants.(interface {
		ProductMapSnapshot(context.Context, string) (integration.ProductMapSnapshot, error)
	})
	if !ok {
		return out, errors.New("local Product Maps are unavailable")
	}
	target, ok := s.o.Team.(interface {
		ProductMapTarget(context.Context, string, string) (teamlink.ProductMapTarget, error)
	})
	if !ok {
		return out, errors.New("Team Product Maps are unavailable")
	}
	snapshot, err := source.ProductMapSnapshot(ctx, local)
	if err != nil {
		return out, err
	}
	if err = snapshot.Validate(); err != nil {
		return out, err
	}
	scope, err := target.ProductMapTarget(ctx, w, p)
	if err != nil {
		return out, err
	}
	if !scope.CanPropose || scope.Repository != snapshot.Repository {
		return out, errors.New("choose a local map for the same repository and a Team project you may propose to")
	}
	expected := int64(0)
	for _, r := range scope.Records {
		if r.State == "published" && r.Version > expected {
			expected = r.Version
		}
	}
	var tokenBytes [24]byte
	if _, err = rand.Read(tokenBytes[:]); err != nil {
		return out, err
	}
	token := hex.EncodeToString(tokenBytes[:])
	s.mapShares.Lock()
	defer s.mapShares.Unlock()
	if s.mapShares.pending == nil {
		s.mapShares.pending = map[string]*productMapShare{}
	}
	for key, r := range s.mapShares.pending {
		if time.Since(r.At) > 10*time.Minute {
			delete(s.mapShares.pending, key)
		}
	}
	if len(s.mapShares.pending) >= 20 {
		return out, errors.New("close old map reviews before opening another")
	}
	s.mapShares.pending[token] = &productMapShare{Workspace: w, Project: p, Proposal: "mapshare_" + token[:24], Snapshot: snapshot, Expected: expected, At: time.Now()}
	return ProductMapSharePreview{Token: token, Workspace: w, Project: p, Snapshot: snapshot}, nil
}
func (s *Shell) ConfirmProductMapShare(token string) (string, error) {
	s.mapShares.Lock()
	defer s.mapShares.Unlock()
	grant := s.mapShares.pending[token]
	if grant == nil || time.Since(grant.At) > 10*time.Minute {
		return "", errors.New("this map review expired; review the snapshot again")
	}
	if grant.Result != "" {
		return grant.Result, nil
	}
	if s.o.UI.Ask(Dialog{Kind: Question, Title: "Share Product Map", Message: "Share the reviewed feature descriptions, scenarios and source references with this Team project? Source files and agent transcripts stay local.", Buttons: []string{"Cancel", "Share map"}, Default: "Cancel", Cancel: "Cancel"}) != "Share map" {
		return "", nil
	}
	target, ok := s.o.Team.(interface {
		ProposeProductMap(context.Context, string, string, string, int64, integration.ProductMapSnapshot) (string, error)
	})
	if !ok {
		return "", errors.New("Team Product Maps are unavailable")
	}
	ctx, cancel := s.timeout(20 * time.Second)
	defer cancel()
	result, err := target.ProposeProductMap(ctx, grant.Workspace, grant.Project, grant.Proposal, grant.Expected, grant.Snapshot)
	if err == nil {
		grant.Result = result
	}
	return result, err
}
