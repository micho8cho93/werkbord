package service

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"time"

	"devboard/internal/team/domain"
	"devboard/internal/team/license"
	"devboard/internal/team/store"
)

// EnforceLicense is called by production wiring before serving or creating a workspace.
// The signed document is replicated with workspace data, so failover never needs a vendor service.
func (s *Service) EnforceLicense(key ed25519.PublicKey, initial []byte) {
	s.licenseKey = append(ed25519.PublicKey(nil), key...)
	s.db = authorizedStore{licensedStore{Store: s.base, key: s.licenseKey, initial: append([]byte(nil), initial...), now: func() time.Time { return s.now() }}}
}

type licensedStore struct {
	store.Store
	key     ed25519.PublicKey
	initial []byte
	now     func() time.Time
}

func (d licensedStore) FreshView(ctx context.Context, fn func(store.Tx) error) error {
	return freshView(ctx, d.Store, fn)
}

func licenseFailure(err error) error {
	return fmt.Errorf("%w: workspace license: %v", domain.ErrForbidden, err)
}

func (d licensedStore) Update(ctx context.Context, fn func(store.Tx) error) error {
	return d.Store.Update(ctx, func(tx store.Tx) error {
		records, err := tx.LicenseRecords(ctx)
		if err != nil {
			return err
		}
		claims := map[string]license.Claims{}
		missing, err := tx.UnlicensedWorkspaces(ctx)
		if err != nil {
			return err
		}
		for _, ws := range missing {
			c, err := license.Verify(d.initial, d.key, d.now())
			if err != nil {
				return licenseFailure(err)
			}
			ms, err := tx.Members(ctx, ws)
			if err != nil {
				return err
			}
			if len(ms) > c.Seats {
				return licenseFailure(errors.New("the imported license has fewer seats than the existing workspace"))
			}
			if err := tx.SetLicense(ctx, ws, d.initial); err != nil {
				return err
			}
			claims[ws] = c
		}
		for _, r := range records {
			c, err := license.Verify(r.Document, d.key, d.now())
			if err != nil {
				return licenseFailure(err)
			}
			claims[r.WorkspaceID] = c
		}
		if len(records) == 0 {
			if _, err := license.Verify(d.initial, d.key, d.now()); err != nil {
				return licenseFailure(err)
			}
		}
		return fn(licensedTx{Tx: tx, policy: d, claims: claims})
	})
}

type licensedTx struct {
	store.Tx
	policy licensedStore
	claims map[string]license.Claims
}

// Containment must remain possible when a license expires or is damaged. These
// callers only remove authority or replace an existing credential; normal role,
// workspace, quorum and current-session checks still apply inside the transaction.
func (s *Service) containmentUpdate(ctx context.Context, fn func(store.Tx) error) error {
	return (authorizedStore{s.base}).Update(ctx, fn)
}

func (t licensedTx) InsertWorkspace(ctx context.Context, w domain.Workspace) error {
	c, err := license.Verify(t.policy.initial, t.policy.key, t.policy.now())
	if err != nil {
		return licenseFailure(err)
	}
	if err := t.Tx.InsertWorkspace(ctx, w); err != nil {
		return err
	}
	if err := t.Tx.SetLicense(ctx, w.ID, t.policy.initial); err != nil {
		return err
	}
	t.claims[w.ID] = c
	return nil
}

func (t licensedTx) InsertMember(ctx context.Context, m domain.Member, hash string) error {
	c, ok := t.claims[m.WorkspaceID]
	if !ok {
		return licenseFailure(errors.New("no signed license for this workspace"))
	}
	ms, err := t.Tx.Members(ctx, m.WorkspaceID)
	if err != nil {
		return err
	}
	if len(ms) >= c.Seats {
		return licenseFailure(errors.New("the seat limit is reached; import a larger license or remove a member"))
	}
	return t.Tx.InsertMember(ctx, m, hash)
}

// InstallLicense is the recovery path even for an expired license. Only the owner can replace it.
func (s *Service) InstallLicense(ctx context.Context, a Actor, raw []byte) (license.Claims, error) {
	c, err := license.Verify(raw, s.licenseKey, s.now())
	if err != nil {
		return license.Claims{}, licenseFailure(err)
	}
	err = s.base.Update(ctx, func(tx store.Tx) error {
		if err := checkSession(ctx, tx); err != nil {
			return err
		}
		m, err := tx.Member(ctx, a.Workspace.ID, a.Member.ID)
		if err != nil || m.Role != domain.RoleOwner {
			return forbidden("replace the workspace license")
		}
		ms, err := tx.Members(ctx, a.Workspace.ID)
		if err != nil {
			return err
		}
		if len(ms) > c.Seats {
			return licenseFailure(errors.New("the new seat limit is below the current member count"))
		}
		return tx.SetLicense(ctx, a.Workspace.ID, raw)
	})
	return c, err
}
