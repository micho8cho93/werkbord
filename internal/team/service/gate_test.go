package service

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"devboard/internal/team/domain"
	"devboard/internal/team/infra/rqlite"
	"devboard/internal/team/infra/rqlite/rqlitetest"
	"devboard/internal/team/license"
	"devboard/internal/team/store"
	"devboard/internal/team/store/replicated"
)

func isolatedLicenseWorld(t *testing.T) *world {
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

func signedLicense(t *testing.T, key ed25519.PrivateKey, seats int, issued, expires time.Time) []byte {
	t.Helper()
	b, err := license.Issue(license.Claims{Schema: license.Schema, Product: "werkbord-team", Edition: license.EditionTeam, ID: "lic_test", Customer: "org_test", Seats: seats, IssuedAt: issued, ExpiresAt: expires}, key)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestLicenseSeatsAreAtomicAndEveryMemberInsertionIsGuarded(t *testing.T) {
	w := isolatedLicenseWorld(t)
	pub, key, _ := ed25519.GenerateKey(rand.Reader)
	w.svc.EnforceLicense(pub, signedLicense(t, key, 3, time.Now().Add(-time.Hour), time.Time{}))
	owner, _ := w.workspace("Licensed", "Owner")
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
			} else if !errors.Is(err, domain.ErrForbidden) {
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
			t.Fatalf("seat overflow: %d", len(ms))
		}
		return err
	})
	if _, err := w.svc.InstallLicense(bg, owner, signedLicense(t, key, 2, time.Now().Add(-time.Hour), time.Time{})); !errors.Is(err, domain.ErrForbidden) {
		t.Fatal("seat downgrade accepted", err)
	}
}

func TestLicenseExpiryRenewalAndExistingDatabaseMigration(t *testing.T) {
	w := isolatedLicenseWorld(t)
	owner, _ := w.workspace("Existing", "Owner")
	member, _ := w.member(owner, "Member")
	pub, key, _ := ed25519.GenerateKey(rand.Reader)
	at := time.Now().UTC().Truncate(time.Second)
	w.svc.now = func() time.Time { return at }
	w.svc.EnforceLicense(pub, signedLicense(t, key, 2, at.Add(-time.Hour), at.Add(time.Hour)))
	if _, err := w.svc.CreateProject(bg, owner, ProjectInput{Name: "Migrate"}); err != nil {
		t.Fatal(err)
	}
	recordsErr := w.db.View(bg, func(tx store.Tx) error {
		rs, err := tx.LicenseRecords(bg)
		if err == nil && len(rs) != 1 {
			t.Fatal("legacy license was not replicated")
		}
		return err
	})
	if recordsErr != nil {
		t.Fatal(recordsErr)
	}
	at = at.Add(2 * time.Hour)
	if _, err := w.svc.CreateProject(bg, owner, ProjectInput{Name: "Expired"}); !errors.Is(err, domain.ErrForbidden) {
		t.Fatal("expired license permitted a write", err)
	}
	renewal := signedLicense(t, key, 3, at.Add(-time.Hour), time.Time{})
	if _, err := w.svc.InstallLicense(bg, member, renewal); !errors.Is(err, domain.ErrForbidden) {
		t.Fatal("member renewed license", err)
	}
	if _, err := w.svc.InstallLicense(AuthenticatedContext(bg, owner), owner, renewal); err != nil {
		t.Fatal("owner cannot recover expired license", err)
	}
	if _, err := w.svc.AddMember(bg, owner, "Third", "", domain.RoleMember); err != nil {
		t.Fatal(err)
	}
	// A restart uses the replicated document, with no license file or vendor connection.
	restarted := New(w.db)
	restarted.now = w.svc.now
	restarted.EnforceLicense(pub, nil)
	if _, err := restarted.CreateProject(bg, owner, ProjectInput{Name: "Offline restart"}); err != nil {
		t.Fatal(err)
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

func TestExpiredLicenseStillAllowsContainmentWithoutGrantingAuthority(t *testing.T) {
	w := isolatedLicenseWorld(t)
	owner, _ := w.workspace("Containment", "Owner")
	member, token := w.member(owner, "Member")
	admin := w.admin(owner, "Admin")
	dev := w.mustRegister(member, laptop(t, "stolen"), domain.CapabilityRunner)
	credential, err := w.svc.IssueLocalDeviceCredential(bg, owner.Workspace.ID, dev.ID)
	if err != nil {
		t.Fatal(err)
	}
	pub, key, _ := ed25519.GenerateKey(rand.Reader)
	at := time.Now().UTC().Truncate(time.Second)
	w.svc.now = func() time.Time { return at }
	w.svc.EnforceLicense(pub, signedLicense(t, key, 3, at.Add(-time.Hour), at.Add(time.Hour)))
	if _, err := w.svc.CreateProject(bg, owner, ProjectInput{Name: "Before expiry"}); err != nil {
		t.Fatal(err)
	}
	at = at.Add(2 * time.Hour)
	ctx := AuthenticatedContext(bg, owner)
	if _, err := w.svc.CreateProject(ctx, owner, ProjectInput{Name: "Expired"}); !errors.Is(err, domain.ErrForbidden) {
		t.Fatal("expired license allowed coordination writes", err)
	}
	if _, err := w.svc.SetMemberRole(ctx, owner, member.Member.ID, domain.RoleAdmin); !errors.Is(err, domain.ErrForbidden) {
		t.Fatal("expired license granted authority", err)
	}
	if _, err := w.svc.RevokeDevice(ctx, owner, dev.ID); err != nil {
		t.Fatal("license blocked device containment", err)
	}
	if _, err := w.svc.Authenticate(bg, credential); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatal("revoked device still authenticated", err)
	}
	if _, err := w.svc.ReissueToken(ctx, owner, member.Member.ID); err != nil {
		t.Fatal("license blocked replacement of a leaked member credential", err)
	}
	if _, err := w.svc.Authenticate(bg, token); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatal("old member credential still authenticated", err)
	}
	if _, err := w.svc.SetMemberRole(ctx, owner, admin.Member.ID, domain.RoleMember); err != nil {
		t.Fatal("license blocked admin demotion", err)
	}
	if err := w.svc.RemoveMember(ctx, owner, member.Member.ID); err != nil {
		t.Fatal("license blocked member containment", err)
	}
}
