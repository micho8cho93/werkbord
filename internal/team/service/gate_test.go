package service

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"devboard/internal/team/domain"
	"devboard/internal/team/infra/rqlite"
	"devboard/internal/team/infra/rqlite/rqlitetest"
	"devboard/internal/team/store"
	"devboard/internal/team/store/replicated"
)

func isolatedWorld(t *testing.T) *world {
	if sharedCluster == nil {
		return newWorld(t)
	}
	c := rqlitetest.New(t, rqlitetest.Options{Nodes: 1})
	dir := t.TempDir()
	db, err := replicated.Open(bg, replicated.Options{Dir: dir, Nodes: []string{c.Node(0).HTTP.String()}, Auth: replicated.Auth{User: rqlite.UserApp, Pass: c.Creds.App}, HostID: c.Node(0).ID, Create: true, Poll: 30 * time.Millisecond, WaitForCluster: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return &world{t: t, db: db, svc: New(db), path: filepath.Join(dir, "replica.db")}
}

// fakePlan accepts any key that starts with "good-" and names it by the rest, so the rules are tested without signatures (the
// key format has its own tests in internal/entitlement).
func fakePlan(key string) (string, error) {
	if id, ok := strings.CutPrefix(key, "good-"); ok && id != "" {
		return id, nil
	}
	return "", errors.New("not an unlock key")
}

func (w *world) premium(a Actor, id string) {
	w.t.Helper()
	if _, err := w.svc.PresentPremium(bg, a, "good-"+id); err != nil {
		w.t.Fatal(err)
	}
}

func TestEveryMemberInsertionIsGuardedAtomically(t *testing.T) {
	w := isolatedWorld(t)
	w.svc.EnforcePlan(fakePlan)
	owner, _ := w.workspace("Sized", "Owner")
	p := w.project(owner, "P")
	invite, err := w.svc.CreateInvite(bg, owner, p.ID, InviteInput{Role: domain.ProjectContributor, MaxUses: 10})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	var successes atomic.Int32
	errs := make(chan error, 8)
	for i := range 8 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			var err error
			if i%2 == 0 {
				_, err = w.svc.AddMember(bg, owner, fmt.Sprintf("Direct%d", i), "", domain.RoleMember)
			} else {
				_, err = w.svc.RedeemInviteWith(bg, invite.Code, fmt.Sprintf("Invite%d", i), "", fmt.Sprintf("good-key%d", i))
			}
			if err == nil {
				successes.Add(1)
			} else if !errors.Is(err, domain.ErrPremiumRequired) {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	if successes.Load() != 1 {
		t.Fatalf("%d insertions won, want 1: the owner has no key, so the team stops at a pair", successes.Load())
	}
	_ = w.db.View(bg, func(tx store.Tx) error {
		ms, err := tx.Members(bg, owner.Workspace.ID)
		if err == nil && len(ms) != 2 {
			t.Fatalf("size overflow: %d", len(ms))
		}
		return err
	})
}

func TestAThirdPersonNeedsTheOwnersPremiumAndTheirOwnKey(t *testing.T) {
	w := isolatedWorld(t)
	w.svc.EnforcePlan(fakePlan)
	owner, _ := w.workspace("Pair", "Owner")
	if _, err := w.svc.AddMember(bg, owner, "Partner", "", domain.RoleMember); err != nil {
		t.Fatal("the second person is the Free plan's partner", err)
	}
	_, err := w.svc.AddMember(bg, owner, "Third person", "", domain.RoleMember)
	if !errors.Is(err, domain.ErrPremiumRequired) {
		t.Fatal("a third person was added with the owner on Free", err)
	}
	if errors.Is(err, domain.ErrForbidden) {
		t.Fatal("a plan limit must not read as a permission failure")
	}

	w.premium(owner, "owner-key")
	p := w.project(owner, "P")
	invite, err := w.svc.CreateInvite(bg, owner, p.ID, InviteInput{Role: domain.ProjectContributor, MaxUses: 10})
	if err != nil {
		t.Fatal(err)
	}
	// A person who joins by themselves brings a key.
	if _, err := w.svc.RedeemInvite(bg, invite.Code, "Without key", ""); !errors.Is(err, domain.ErrPremiumRequired) {
		t.Fatal("a third person joined by themselves without a key", err)
	}
	if _, err := w.svc.RedeemInviteWith(bg, invite.Code, "Bad key", "", "nonsense"); !errors.Is(err, domain.ErrInvalid) {
		t.Fatal("a key that does not check was accepted", err)
	}
	if _, err := w.svc.RedeemInviteWith(bg, invite.Code, "Owner's key", "", "good-owner-key"); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("one key was used by two people of one team", err)
	}
	joined, err := w.svc.RedeemInviteWith(bg, invite.Code, "Third person", "", "good-third-key")
	if err != nil {
		t.Fatal(err)
	}
	if st, err := w.svc.PlanState(bg, w.signIn(joined.Token)); err != nil || !st.Premium || st.ReadOnly || st.People != 3 {
		t.Fatalf("the person who joined with a key is Premium: %+v, %v", st, err)
	}
	// A member an administrator creates by name has no key to give yet: they join, and are read-only until they present one.
	named, err := w.svc.AddMember(bg, owner, "Named by an admin", "", domain.RoleMember)
	if err != nil {
		t.Fatal("the owner is Premium, so the team may grow", err)
	}
	if st, _ := w.svc.PlanState(bg, w.signIn(named.Token)); st.Premium || !st.ReadOnly {
		t.Fatalf("a member named by an admin is read-only until they have a key: %+v", st)
	}
	members, err := w.svc.ListMembers(bg, owner)
	if err != nil {
		t.Fatal(err)
	}
	premium := 0
	for _, m := range members {
		if m.Premium {
			premium++
		}
	}
	if premium != 2 {
		t.Fatalf("%d members are listed as Premium, want 2 (the owner and the person who joined with a key)", premium)
	}
}

func TestInATeamOverThePlanAMemberWithoutAKeyReadsButDoesNotChangeIt(t *testing.T) {
	w := isolatedWorld(t)
	w.svc.EnforcePlan(fakePlan)
	owner, _ := w.workspace("Big", "Owner")
	w.premium(owner, "owner")
	partner := w.admin(owner, "Partner")
	w.member(owner, "Third")
	w.project(owner, "Shared")
	ctx := AuthenticatedContext(bg, partner)
	if _, err := w.svc.CreateProject(ctx, partner, ProjectInput{Name: "Mine"}); !errors.Is(err, domain.ErrPremiumRequired) {
		t.Fatal("a member without a key changed a team over the plan", err)
	}
	if _, err := w.svc.ListProjects(ctx, partner); err != nil {
		t.Fatal("reading is never refused", err)
	}
	if _, err := w.svc.CreateProject(AuthenticatedContext(bg, owner), owner, ProjectInput{Name: "Owner's"}); err != nil {
		t.Fatal("a member with a key is not read-only", err)
	}
	// The routes that keep the team running mark their requests exempt.
	if _, err := w.svc.CreateProject(PlanExempt(ctx), partner, ProjectInput{Name: "Exempt"}); err != nil {
		t.Fatal("an exempt request was refused", err)
	}
	if st, _ := w.svc.PlanState(ctx, partner); !st.ReadOnly || st.Premium || st.People != 3 || st.FreeLimit != 2 || !st.OwnerPremium {
		t.Fatalf("plan state: %+v", st)
	}
	// Presenting a key is how they stop being read-only, and it is allowed while they are.
	if _, err := w.svc.PresentPremium(ctx, partner, "good-partner"); err != nil {
		t.Fatal(err)
	}
	if _, err := w.svc.CreateProject(ctx, partner, ProjectInput{Name: "Mine now"}); err != nil {
		t.Fatal("the member who presented a key is still read-only", err)
	}
	if _, err := w.svc.PresentPremium(ctx, partner, "good-owner"); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("a key already used by another member was accepted", err)
	}
	if _, err := w.svc.PresentPremium(ctx, partner, "wrong"); !errors.Is(err, domain.ErrInvalid) {
		t.Fatal("a key that does not check was accepted", err)
	}
}

func TestATeamOfTwoIsFreeAndAServiceWithoutAPlanHasNoLimits(t *testing.T) {
	free := isolatedWorld(t)
	free.svc.EnforcePlan(fakePlan)
	owner, _ := free.workspace("Pair", "Owner")
	partner := free.admin(owner, "Partner")
	for _, a := range []Actor{owner, partner} {
		if _, err := free.svc.CreateProject(AuthenticatedContext(bg, a), a, ProjectInput{Name: "P-" + a.Member.Name}); err != nil {
			t.Fatal("a team of two works fully on the Free plan", err)
		}
	}
	if st, _ := free.svc.PlanState(bg, partner); st.ReadOnly || st.People != 2 {
		t.Fatalf("a pair is never read-only: %+v", st)
	}

	none := isolatedWorld(t)
	big, _ := none.workspace("Big", "Owner")
	for i := range 6 {
		none.member(big, fmt.Sprintf("Member%d", i))
	}
	if st, _ := none.svc.PlanState(bg, big); st.Enforced || st.ReadOnly {
		t.Fatalf("a service without a plan applies none: %+v", st)
	}
	if _, err := none.svc.PresentPremium(bg, big, "good-x"); !errors.Is(err, domain.ErrInvalid) {
		t.Fatal("a service without a plan cannot check a key", err)
	}
}

func TestAdditionalIndividualDevicesDoNotCountAsPeople(t *testing.T) {
	w := isolatedWorld(t)
	w.svc.EnforcePlan(fakePlan)
	owner, _ := w.workspace("Devices", "Owner")
	member, _ := w.member(owner, "Member")
	w.mustRegister(member, laptop(t, "first-personal-device"), domain.CapabilityRunner)
	w.mustRegister(member, laptop(t, "second-personal-device"), domain.CapabilityRunner)
	if _, err := w.svc.AddMember(bg, owner, "Third person", "", domain.RoleMember); !errors.Is(err, domain.ErrPremiumRequired) {
		t.Fatal("additional devices bypassed the person limit", err)
	}
}

func TestRevocationAndRoleChangeInvalidateAnAlreadyAuthenticatedSession(t *testing.T) {
	w := newWorld(t)
	owner, _ := w.workspace("Sessions", "Owner")
	member, token := w.member(owner, "Member")
	id := laptop(t, "stolen")
	dev := w.mustRegister(member, id, domain.CapabilityRunner)
	deviceToken, err := w.svc.IssueLocalDeviceCredential(bg, owner.Workspace.ID, dev.ID)
	if err != nil {
		t.Fatal(err)
	}
	a := w.signIn(deviceToken)
	ctx, cancel := context.WithTimeout(AuthenticatedContext(bg, a), 4*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := w.svc.Inbox(ctx, a, 3*time.Second); done <- err }()
	time.Sleep(30 * time.Millisecond)
	if _, err := w.svc.RevokeDevice(bg, owner, dev.ID); err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatalf("active inbox survived revocation: %v", err)
	}
	stale := w.signIn(token)
	if err := w.svc.RemoveMember(bg, owner, member.Member.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := w.svc.ListMembers(AuthenticatedContext(bg, stale), stale); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatal("removed member read with old session", err)
	}
	admin := w.admin(owner, "Admin")
	if _, err := w.svc.SetMemberRole(bg, owner, admin.Member.ID, domain.RoleMember); err != nil {
		t.Fatal(err)
	}
	if _, err := w.svc.AddMember(AuthenticatedContext(bg, admin), admin, "Escalate", "", domain.RoleMember); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatal("demoted admin used stale permissions", err)
	}
}

func TestATeamOverThePlanCanStillContainAccess(t *testing.T) {
	w := isolatedWorld(t)
	owner, _ := w.workspace("Containment", "Owner")
	member, token := w.member(owner, "Member")
	admin := w.admin(owner, "Admin")
	dev := w.mustRegister(member, laptop(t, "stolen"), domain.CapabilityRunner)
	credential, err := w.svc.IssueLocalDeviceCredential(bg, owner.Workspace.ID, dev.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Three people already, then the plan is applied (an older team, or a restart on a build with a plan): nobody has a key.
	w.svc.EnforcePlan(fakePlan)
	ctx := AuthenticatedContext(bg, owner)
	if _, err := w.svc.AddMember(ctx, owner, "Fourth", "", domain.RoleMember); !errors.Is(err, domain.ErrPremiumRequired) {
		t.Fatal("a team over the plan grew", err)
	}
	if _, err := w.svc.CreateProject(ctx, owner, ProjectInput{Name: "Still coordinating"}); !errors.Is(err, domain.ErrPremiumRequired) {
		t.Fatal("a team over the plan, with nobody on Premium, is read-only", err)
	}
	if _, err := w.svc.RevokeDevice(ctx, owner, dev.ID); err != nil {
		t.Fatal("the plan blocked device containment", err)
	}
	if _, err := w.svc.Authenticate(bg, credential); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatal("revoked device still authenticated", err)
	}
	if _, err := w.svc.ReissueToken(ctx, owner, member.Member.ID); err != nil {
		t.Fatal("the plan blocked replacement of a leaked member credential", err)
	}
	if _, err := w.svc.Authenticate(bg, token); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatal("old member credential still authenticated", err)
	}
	if _, err := w.svc.SetMemberRole(ctx, owner, admin.Member.ID, domain.RoleMember); err != nil {
		t.Fatal("the plan blocked admin demotion", err)
	}
	if err := w.svc.RemoveMember(ctx, owner, member.Member.ID); err != nil {
		t.Fatal("the plan blocked member removal", err)
	}
}
