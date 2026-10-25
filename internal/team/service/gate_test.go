package service

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"devboard/internal/entitlement"
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

func TestTeamSizeIsAtomicAndEveryMemberInsertionIsGuarded(t *testing.T) {
	w := isolatedWorld(t)
	w.svc.EnforceTeamSize(3)
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
				_, err = w.svc.RedeemInvite(bg, invite.Code, fmt.Sprintf("Invite%d", i), "")
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
	if successes.Load() != 2 {
		t.Fatalf("%d insertions won, want 2", successes.Load())
	}
	_ = w.db.View(bg, func(tx store.Tx) error {
		ms, err := tx.Members(bg, owner.Workspace.ID)
		if err == nil && len(ms) != 3 {
			t.Fatalf("size overflow: %d", len(ms))
		}
		return err
	})
}

func TestTheFreePlanIsAPairAndPremiumHasNoLimit(t *testing.T) {
	free := isolatedWorld(t)
	free.svc.EnforceTeamSize(entitlement.TeamLimit(false))
	owner, _ := free.workspace("Pair", "Owner")
	if _, err := free.svc.AddMember(bg, owner, "Partner", "", domain.RoleMember); err != nil {
		t.Fatal("the second person is the Free plan's partner", err)
	}
	_, err := free.svc.AddMember(bg, owner, "Third person", "", domain.RoleMember)
	if !errors.Is(err, domain.ErrPremiumRequired) {
		t.Fatal("a third person was added on the Free plan", err)
	}
	if errors.Is(err, domain.ErrForbidden) {
		t.Fatal("a plan limit must not read as a permission failure")
	}

	premium := isolatedWorld(t)
	premium.svc.EnforceTeamSize(entitlement.TeamLimit(true))
	big, _ := premium.workspace("Team", "Owner")
	for i := range 6 {
		if _, err := premium.svc.AddMember(bg, big, fmt.Sprintf("Member%d", i), "", domain.RoleMember); err != nil {
			t.Fatal("Premium refused a member", err)
		}
	}
}

func TestAdditionalIndividualDevicesDoNotCountAsPeople(t *testing.T) {
	w := isolatedWorld(t)
	w.svc.EnforceTeamSize(2)
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

func TestATeamOverTheLimitKeepsWorkingAndCanStillContainAccess(t *testing.T) {
	w := isolatedWorld(t)
	owner, _ := w.workspace("Containment", "Owner")
	member, token := w.member(owner, "Member")
	admin := w.admin(owner, "Admin")
	dev := w.mustRegister(member, laptop(t, "stolen"), domain.CapabilityRunner)
	credential, err := w.svc.IssueLocalDeviceCredential(bg, owner.Workspace.ID, dev.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Three people already, then the Free limit is applied (an older team, or a restart on a free build).
	w.svc.EnforceTeamSize(entitlement.FreeTeamLimit)
	ctx := AuthenticatedContext(bg, owner)
	if _, err := w.svc.AddMember(ctx, owner, "Fourth", "", domain.RoleMember); !errors.Is(err, domain.ErrPremiumRequired) {
		t.Fatal("a team over the limit grew", err)
	}
	if _, err := w.svc.CreateProject(ctx, owner, ProjectInput{Name: "Still coordinating"}); err != nil {
		t.Fatal("a team over the limit lost its other writes", err)
	}
	if _, err := w.svc.RevokeDevice(ctx, owner, dev.ID); err != nil {
		t.Fatal("the limit blocked device containment", err)
	}
	if _, err := w.svc.Authenticate(bg, credential); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatal("revoked device still authenticated", err)
	}
	if _, err := w.svc.ReissueToken(ctx, owner, member.Member.ID); err != nil {
		t.Fatal("the limit blocked replacement of a leaked member credential", err)
	}
	if _, err := w.svc.Authenticate(bg, token); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatal("old member credential still authenticated", err)
	}
	if _, err := w.svc.SetMemberRole(ctx, owner, admin.Member.ID, domain.RoleMember); err != nil {
		t.Fatal("the limit blocked admin demotion", err)
	}
	if err := w.svc.RemoveMember(ctx, owner, member.Member.ID); err != nil {
		t.Fatal("the limit blocked member removal", err)
	}
}
