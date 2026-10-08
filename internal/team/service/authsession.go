package service

import (
	"context"
	"time"

	"devboard/internal/team/domain"
	"devboard/internal/team/store"
)

type sessionKey struct{}

// AuthenticatedContext pins the identity that was authenticated for this request.
// Every later read and write rechecks it inside its transaction, including long polls.
func AuthenticatedContext(ctx context.Context, a Actor) context.Context {
	return context.WithValue(ctx, sessionKey{}, a)
}

func checkSession(ctx context.Context, tx store.Tx) error {
	a, ok := ctx.Value(sessionKey{}).(Actor)
	if !ok {
		return nil
	} // local bootstrap and internal maintenance have no remote actor
	m, err := tx.Member(ctx, a.Workspace.ID, a.Member.ID)
	if err != nil || m.Role != a.Member.Role {
		return domain.ErrUnauthenticated
	}
	if a.Device != nil {
		d, err := tx.DeviceByCredentialHash(ctx, a.credentialHash)
		if err != nil || d.Revoked() || d.ID != a.Device.ID || d.WorkspaceID != a.Workspace.ID || d.MemberID != m.ID || d.PublicKey != a.Device.PublicKey {
			return domain.ErrUnauthenticated
		}
	} else {
		current, err := tx.MemberByTokenHash(ctx, a.credentialHash)
		if err != nil || current.ID != m.ID {
			return domain.ErrUnauthenticated
		}
	}
	return nil
}

type authorizedStore struct{ store.Store }

func freshView(ctx context.Context, db store.Store, fn func(store.Tx) error) error {
	if d, ok := db.(interface {
		FreshView(context.Context, func(store.Tx) error) error
	}); ok {
		return d.FreshView(ctx, fn)
	}
	return db.View(ctx, fn)
}

func (d authorizedStore) View(ctx context.Context, fn func(store.Tx) error) error {
	call := func(tx store.Tx) error {
		if err := checkSession(ctx, tx); err != nil {
			return err
		}
		return fn(tx)
	}
	if _, ok := ctx.Value(sessionKey{}).(Actor); ok {
		return freshView(ctx, d.Store, call)
	}
	return d.Store.View(ctx, call)
}

func (d authorizedStore) Update(ctx context.Context, fn func(store.Tx) error) error {
	return d.Store.Update(ctx, func(tx store.Tx) error {
		if err := checkSession(ctx, tx); err != nil {
			return err
		}
		return fn(tx)
	})
}

// ConsumeAPINonce records a verified mutating request across all hosts and restarts.
// Expired licenses can still be renewed through this authentication path.
func (s *Service) ConsumeAPINonce(ctx context.Context, a Actor, nonce string, expires time.Time) error {
	return s.base.Update(ctx, func(tx store.Tx) error {
		if err := checkSession(AuthenticatedContext(ctx, a), tx); err != nil {
			return err
		}
		ok, err := tx.UseAPINonce(ctx, a.Workspace.ID, a.Device.ID, nonce, expires, s.now())
		if err != nil {
			return err
		}
		if !ok {
			return domain.ErrUnauthenticated
		}
		return nil
	})
}
