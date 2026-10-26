package service

import (
	"context"
	"fmt"
	"time"

	"werkbord/internal/entitlement"
	"werkbord/internal/team/domain"
	"werkbord/internal/team/store"
)

// The plan rules (docs/PLANS.md). A team of up to entitlement.FreeTeamLimit people is free. A team of more than that needs
// every member to hold a valid Premium unlock key, which the member presents to the team's host (PresentPremium) or brings
// when they join:
//
//   - Adding the person who would take a team past the limit needs the workspace owner on Premium (the owner's computer is
//     where the team started and normally the first Workspace Host), and, when the person joins by themselves (an enrollment or
//     a project invitation), a valid key of their own too. A member an administrator creates by name has no key to give yet:
//     they join, and are read-only until they present it.
//   - In a team over the limit, a member without a key is read-only: what they do that would change the team's coordination
//     is refused with ErrPremiumRequired. Reading is untouched, and so is everything that keeps the team running or safe:
//     containment (removing a member, revoking a device, replacing a credential), a device's heartbeat and network upkeep,
//     and presenting a key. The API names those routes (internal/team/api); anything it does not name is refused.
//
// A service built without EnforcePlan has no limits (service tests, and the test-only unmetered build).

// PlanVerifier checks a pasted unlock key and returns the key's ID. The service holds only the ID, never the key.
type PlanVerifier func(key string) (id string, err error)

// EnforcePlan is called by production wiring before serving or creating a workspace.
func (s *Service) EnforcePlan(v PlanVerifier) {
	s.plan = v
	s.db = authorizedStore{planStore{Store: s.base}}
}

type exemptKey struct{}

// PlanExempt marks a request as one a read-only member may make: it keeps the team running or safe rather than changing its
// coordination. The API calls it for the routes it lists.
func PlanExempt(ctx context.Context) context.Context {
	return context.WithValue(ctx, exemptKey{}, true)
}

type joinerKey struct{}

type joiner struct {
	keyID   string
	require bool
}

// withJoiner tells InsertMember who is joining: the ID of the key they presented, and whether they must have presented one.
func withJoiner(ctx context.Context, keyID string, require bool) context.Context {
	return context.WithValue(ctx, joinerKey{}, joiner{keyID: keyID, require: require})
}

type planStore struct{ store.Store }

func (d planStore) FreshView(ctx context.Context, fn func(store.Tx) error) error {
	return freshView(ctx, d.Store, fn)
}

func (d planStore) Update(ctx context.Context, fn func(store.Tx) error) error {
	return d.Store.Update(ctx, func(tx store.Tx) error {
		if a, ok := ctx.Value(sessionKey{}).(Actor); ok && ctx.Value(exemptKey{}) == nil {
			if err := writable(ctx, tx, a.Workspace.ID, a.Member.ID); err != nil {
				return err
			}
		}
		return fn(planTx{Tx: tx})
	})
}

type planTx struct{ store.Tx }

func (t planTx) InsertMember(ctx context.Context, m domain.Member, hash string) error {
	j, _ := ctx.Value(joinerKey{}).(joiner)
	if err := admit(ctx, t.Tx, m.WorkspaceID, j); err != nil {
		return err
	}
	if err := t.Tx.InsertMember(ctx, m, hash); err != nil {
		return err
	}
	if j.keyID != "" {
		return t.Tx.SetMemberPremium(ctx, m.WorkspaceID, m.ID, j.keyID, time.Now())
	}
	return nil
}

// admit says whether one more person may join the workspace.
func admit(ctx context.Context, tx store.Tx, workspaceID string, j joiner) error {
	ms, err := tx.Members(ctx, workspaceID)
	if err != nil {
		return err
	}
	if len(ms) < entitlement.FreeTeamLimit {
		return nil
	}
	premiums, err := tx.MemberPremiums(ctx, workspaceID)
	if err != nil {
		return err
	}
	for _, m := range ms {
		if m.Role == domain.RoleOwner && premiums[m.ID] == "" {
			return premiumRequired(fmt.Sprintf("a team of %d or more needs Premium: the team's owner has to unlock Premium before anyone else can be added", entitlement.FreeTeamLimit+1))
		}
	}
	if j.require && j.keyID == "" {
		return premiumRequired(fmt.Sprintf("this team already has %d people, which is what the Free plan allows: to join it you need Premium (enter your unlock key in Settings, then use the invitation again)", entitlement.FreeTeamLimit))
	}
	return nil
}

// writable says whether the member may change the team's coordination: any member may while the team has no more people than
// the Free plan allows, and after that only a member who holds a key.
func writable(ctx context.Context, tx store.Tx, workspaceID, memberID string) error {
	ms, err := tx.Members(ctx, workspaceID)
	if err != nil || len(ms) <= entitlement.FreeTeamLimit {
		return err
	}
	premiums, err := tx.MemberPremiums(ctx, workspaceID)
	if err != nil || premiums[memberID] != "" {
		return err
	}
	return premiumRequired(fmt.Sprintf("this team has more than %d people, so everyone in it needs Premium. You can read it, but not change it, until you unlock Premium in Settings", entitlement.FreeTeamLimit))
}

func premiumRequired(msg string) error { return fmt.Errorf("%w: %s", domain.ErrPremiumRequired, msg) }

// verifyKey checks a presented key, or says there is none to check (no key given is not an error).
func (s *Service) verifyKey(key string) (string, error) {
	if key == "" {
		return "", nil
	}
	if s.plan == nil {
		return "", nil
	}
	id, err := s.plan(key)
	if err != nil {
		return "", fmt.Errorf("%w: %v", domain.ErrInvalid, err)
	}
	return id, nil
}

// PlanState is what a member is told about their team's plan.
type PlanState struct {
	// Enforced is false on a service that applies no plan rules (a test build).
	Enforced bool `json:"enforced"`
	// FreeLimit is how many people a team may have without Premium.
	FreeLimit int `json:"freeLimit"`
	// People is how many people the team has.
	People int `json:"people"`
	// Premium says the member has presented a valid key to this team.
	Premium bool `json:"premium"`
	// OwnerPremium says the owner has.
	OwnerPremium bool `json:"ownerPremium"`
	// ReadOnly says the member can read the team but not change it, until they present a key.
	ReadOnly bool `json:"readOnly"`
}

// PlanState reports the plan as the member sees it.
func (s *Service) PlanState(ctx context.Context, a Actor) (PlanState, error) {
	st := PlanState{Enforced: s.plan != nil, FreeLimit: entitlement.FreeTeamLimit}
	err := s.db.View(ctx, func(tx store.Tx) error {
		ms, err := tx.Members(ctx, a.Workspace.ID)
		if err != nil {
			return err
		}
		premiums, err := tx.MemberPremiums(ctx, a.Workspace.ID)
		if err != nil {
			return err
		}
		st.People = len(ms)
		st.Premium = premiums[a.Member.ID] != ""
		for _, m := range ms {
			if m.Role == domain.RoleOwner {
				st.OwnerPremium = premiums[m.ID] != ""
			}
		}
		st.ReadOnly = st.Enforced && st.People > st.FreeLimit && !st.Premium
		return nil
	})
	return st, err
}

// PresentPremium gives the team the member's unlock key. The key is checked here and only its ID is kept. A member who
// presents a key is no longer read-only, in this team and in every team whose host sees the same key.
func (s *Service) PresentPremium(ctx context.Context, a Actor, key string) (PlanState, error) {
	if s.plan == nil {
		return PlanState{}, fmt.Errorf("%w: this service applies no plan rules", domain.ErrInvalid)
	}
	id, err := s.plan(key)
	if err != nil {
		return PlanState{}, fmt.Errorf("%w: %v", domain.ErrInvalid, err)
	}
	err = (authorizedStore{s.base}).Update(ctx, func(tx store.Tx) error {
		return tx.SetMemberPremium(ctx, a.Workspace.ID, a.Member.ID, id, s.stamp())
	})
	s.changed(a.Workspace.ID, err)
	if err != nil {
		return PlanState{}, err
	}
	return s.PlanState(ctx, a)
}

// Containment, and what a read-only member may still do, must keep working whatever a team's size or plan. Callers only remove
// authority or replace an existing credential; normal role, workspace, quorum and current-session checks still apply.
func (s *Service) containmentUpdate(ctx context.Context, fn func(store.Tx) error) error {
	return (authorizedStore{s.base}).Update(ctx, fn)
}
