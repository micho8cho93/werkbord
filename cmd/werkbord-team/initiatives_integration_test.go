package main

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"devboard/internal/deviceid"
	localdomain "devboard/internal/domain"
	"devboard/internal/runner"
	"devboard/internal/team/devicestate"
	"devboard/internal/team/domain"
	"devboard/internal/team/hostclient"
	"devboard/internal/team/infra/pki"
	"devboard/internal/team/server"
	"devboard/internal/team/service"
)

// Real Git repositories, independent Individual APIs/databases/runners, two
// member-signed connectors and the Team HTTP API. No production credentials.
func TestInitiativeAcrossRepositoriesAndIndependentMemberRunners(t *testing.T) {
	ctx := context.Background()
	front := newIntegrationLocal(t)
	back := newIntegrationLocalRepository(t, "git@github.com:acme/backend.git")
	team := newIntegrationTeam(t, "Initiative E2E")
	second, e := team.svc.AddMember(ctx, team.owner, "Backend member", "", domain.RoleMember)
	if e != nil {
		t.Fatal(e)
	}
	member, e := team.svc.Authenticate(ctx, second.Token)
	if e != nil {
		t.Fatal(e)
	}
	backend, e := team.svc.CreateProject(ctx, team.owner, service.ProjectInput{Name: "Backend", Repository: "https://github.com/acme/backend"})
	if e != nil {
		t.Fatal(e)
	}
	for _, p := range []string{team.project.ID, backend.ID} {
		for _, a := range []service.Actor{team.member, member} {
			if e = team.svc.AddProjectMember(ctx, team.owner, p, a.Member.ID, domain.ProjectContributor); e != nil {
				t.Fatal(e)
			}
		}
	}
	k, e := team.svc.CreateTicket(ctx, team.owner, backend.ID, service.TicketInput{Title: "Backend auth", Status: domain.TicketAvailable})
	if e != nil {
		t.Fatal(e)
	}
	k, e = team.svc.ClaimTicket(ctx, member, backend.ID, k.ID)
	if e != nil {
		t.Fatal(e)
	}
	keys, e := pki.NewHostKeys()
	if e != nil {
		t.Fatal(e)
	}
	pub := deviceid.Public{ID: deviceid.ID(keys.DeviceID()), Name: "Backend runner", PublicKey: deviceid.EncodePublicKey(keys.PublicKey()), CreatedAt: time.Now()}
	device, e := team.svc.RegisterDevice(ctx, member, service.DeviceInput{Device: pub, Proof: keys.Sign(deviceid.RegistrationStatement(team.owner.Workspace.ID, member.Member.ID, pub.ID, pub.Name, keys.PublicKey())), Capabilities: []domain.Capability{domain.CapabilityRunner}})
	if e != nil {
		t.Fatal(e)
	}
	credential, e := team.svc.IssueLocalDeviceCredential(ctx, team.owner.Workspace.ID, device.ID)
	if e != nil {
		t.Fatal(e)
	}
	ts := httptest.NewServer(server.Handler(team.db, team.svc, nil, "test"))
	defer ts.Close()
	host, e := hostclient.New(hostclient.Options{Bases: []string{ts.URL}, Token: credential, Signer: keys, WorkspaceID: team.owner.Workspace.ID, UserID: member.Member.ID})
	if e != nil {
		t.Fatal(e)
	}
	secondTeam := &integrationTeam{svc: team.svc, owner: team.owner, member: member, project: backend, ticket: k, device: device, host: host}
	j1, e := devicestate.OpenSyncJournal(ctx, filepath.Join(t.TempDir(), "front.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer j1.Close()
	j2, e := devicestate.OpenSyncJournal(ctx, filepath.Join(t.TempDir(), "back.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer j2.Close()
	c1, c2 := makeIntegrationConnector(t, front, team, j1), makeIntegrationConnector(t, back, secondTeam, j2)
	if e = c1.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	if e = c2.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	a1, _ := j1.Associations(ctx, c1.WorkspaceID)
	a2, _ := j2.Associations(ctx, c2.WorkspaceID)
	if len(a1) != 1 || len(a2) != 1 || a1[0].TicketID == a2[0].TicketID {
		t.Fatal("runner associations crossed", a1, a2)
	}
	input := domain.Initiative{Title: "Authentication Upgrade", Objective: "Coordinate frontend and backend authentication", ProjectIDs: []string{team.project.ID, backend.ID}, Tickets: []domain.InitiativeTicket{{ProjectID: team.project.ID, TicketID: team.ticket.ID}, {ProjectID: backend.ID, TicketID: k.ID}}}
	input.Dependencies = []domain.InitiativeDependency{{Before: input.Tickets[1], After: input.Tickets[0]}}
	var created domain.Initiative
	if e = team.host.Do(ctx, "POST", "/initiatives", input, &created); e != nil {
		t.Fatal(e)
	}
	var views []service.InitiativeView
	if e = host.Do(ctx, "GET", "/initiatives", nil, &views); e != nil || len(views) != 1 || len(views[0].Blockers) != 1 {
		t.Fatal(views, e)
	}
	if len(front.adapter.Sessions()) != 0 || len(back.adapter.Sessions()) != 0 {
		t.Fatal("initiative triggered execution")
	}
	run, e := front.mgr.Start(ctx, runner.StartInput{TaskID: a1[0].TaskID, AgentID: "fake"})
	if e != nil {
		t.Fatal(e)
	}
	front.adapter.Last().Exit(0, "")
	integrationWait(t, func() bool {
		r, _ := front.runs.Get(ctx, run.ID)
		return r != nil && r.State == localdomain.RunCompleted
	})
	if e = c1.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	if e = host.Do(ctx, "GET", "/initiatives", nil, &views); e != nil || views[0].Done != 0 {
		t.Fatal("runner completion bypassed review", views, e)
	}
	raw, _ := json.Marshal(views)
	if strings.Contains(string(raw), front.owner) || strings.Contains(string(raw), back.owner) {
		t.Fatal("controller credentials leaked")
	}
	if len(back.adapter.Sessions()) != 0 {
		t.Fatal("other runner executed dependent work")
	}
	if e = team.svc.RemoveProjectMember(ctx, team.owner, backend.ID, team.member.Member.ID); e != nil {
		t.Fatal(e)
	}
	if e = team.host.Do(ctx, "GET", "/initiatives", nil, &views); e != nil || len(views) != 0 {
		t.Fatal("restricted initiative identity leaked", views, e)
	}
	if e = host.Do(ctx, "GET", "/initiatives", nil, &views); e != nil || len(views) != 1 {
		t.Fatal("independent member lost access", views, e)
	}
	latest, e := team.svc.GetTicket(ctx, team.owner, backend.ID, k.ID)
	if e != nil || latest.AssigneeID != member.Member.ID {
		t.Fatal("initiative changed ticket ownership", latest, e)
	}
}
