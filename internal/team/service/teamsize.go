package service

import (
	"context"
	"fmt"

	"devboard/internal/team/domain"
	"devboard/internal/team/store"
)

// EnforceTeamSize is called by production wiring before serving or creating a workspace. limit is how many people a team
// may have (entitlement.TeamLimit): the Free plan's two, or no limit. The rule is applied where a person is added, inside
// the same transaction, so concurrent hosts and concurrent requests cannot take a team past it.
func (s *Service) EnforceTeamSize(limit int) {
	s.db = authorizedStore{sizedStore{Store: s.base, limit: limit}}
}

type sizedStore struct {
	store.Store
	limit int
}

func (d sizedStore) FreshView(ctx context.Context, fn func(store.Tx) error) error {
	return freshView(ctx, d.Store, fn)
}

func (d sizedStore) Update(ctx context.Context, fn func(store.Tx) error) error {
	return d.Store.Update(ctx, func(tx store.Tx) error {
		return fn(sizedTx{Tx: tx, limit: d.limit})
	})
}

type sizedTx struct {
	store.Tx
	limit int
}

func (t sizedTx) InsertMember(ctx context.Context, m domain.Member, hash string) error {
	ms, err := t.Tx.Members(ctx, m.WorkspaceID)
	if err != nil {
		return err
	}
	if len(ms) >= t.limit {
		return premiumRequired(t.limit)
	}
	return t.Tx.InsertMember(ctx, m, hash)
}

func premiumRequired(limit int) error {
	return fmt.Errorf("%w: the Free plan allows a team of up to %d people; Premium is needed for a team of %d or more", domain.ErrPremiumRequired, limit, limit+1)
}

// Containment must remain possible whatever a team's size or plan. These callers only remove authority or replace an
// existing credential; normal role, workspace, quorum and current-session checks still apply inside the transaction.
func (s *Service) containmentUpdate(ctx context.Context, fn func(store.Tx) error) error {
	return (authorizedStore{s.base}).Update(ctx, fn)
}
