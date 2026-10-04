package service

import (
	"context"
	"fmt"

	"devboard/internal/domain"
	"devboard/internal/runnerwire"
)

// refreshDistributed is called under the project lock before a review or merge.
// Fetch changes tracking refs only; it never checks out or merges user files.
func (s *GitControl) refreshDistributed(ctx context.Context, projectID string) error {
	a, err := s.associations(ctx, projectID)
	if err != nil || len(a.remoteRuns) == 0 {
		return err
	}
	t, err := s.resolve(ctx, projectID)
	if err != nil {
		return err
	}
	for _, remote := range t.repo.Remotes {
		if !runnerwire.SafeRemote(remote.URL) {
			return fmt.Errorf("%w: remote %s cannot be refreshed safely", domain.ErrConflict, remote.Name)
		}
		got, err := s.Git.Fetch(ctx, t.root, remote.Name)
		if err != nil || got.Outcome != domain.OutcomeDone {
			return fmt.Errorf("%w: cannot refresh %s; reconnect and fetch before reviewing distributed work", domain.ErrConflict, remote.Name)
		}
	}
	return nil
}

// refreshReview serializes a fresh remote snapshot with other repository actions.
func (s *GitControl) refreshReview(ctx context.Context, projectID string) (func(), error) {
	unlock, err := s.lock(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if err := s.refreshDistributed(ctx, projectID); err != nil {
		unlock()
		return nil, err
	}
	return unlock, nil
}
