package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"werkbord/internal/deviceid"
	localdomain "werkbord/internal/domain"
	"werkbord/internal/integration"
	localservice "werkbord/internal/service"
	localstore "werkbord/internal/store"
	localsqlite "werkbord/internal/store/sqlite"
	"werkbord/internal/team/connector"
	"werkbord/internal/team/domain"
	"werkbord/internal/team/hostclient"
	"werkbord/internal/team/infra/pki"
	"werkbord/internal/team/localwerkbord"
	"werkbord/internal/team/server"
	"werkbord/internal/team/service"
)

func memoryOwnerCall(t *testing.T, l *integrationLocal, method, path string, body any, out any) {
	t.Helper()
	raw, _ := json.Marshal(body)
	r, _ := http.NewRequest(method, l.url+path, strings.NewReader(string(raw)))
	r.Header.Set("Authorization", "Bearer "+l.owner)
	r.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	if res.StatusCode >= 300 {
		t.Fatalf("%s %s: %d %s", method, path, res.StatusCode, data)
	}
	if out != nil {
		if err = json.Unmarshal(data, out); err != nil {
			t.Fatal(err)
		}
	}
}
func memoryBridge(t *testing.T, l *integrationLocal, w string) *localwerkbord.Client {
	t.Helper()
	var grant struct {
		Token string `json:"token"`
	}
	memoryOwnerCall(t, l, "POST", "/api/local-access", map[string]string{"name": "Memory test", "scope": "memory-v1", "workspaceId": w}, &grant)
	c, err := localwerkbord.New(l.url, grant.Token)
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func seedPublication(t *testing.T, l *integrationLocal, sha string) *localdomain.Memory {
	t.Helper()
	now := time.Now()
	m, err := l.memory.Memory.Create(context.Background(), localdomain.MemoryScope{ProjectID: l.project, Repository: "github.com/acme/sync"}, localdomain.MemoryContent{Type: "git_head", Authority: localdomain.MemoryObservation, Source: localdomain.MemoryProvenance{Type: localdomain.MemorySourceGit, Reference: "git:" + sha, GitSHA: sha}, Summary: "private summary with credential NEVER_EXPORT and /Users/private/path", State: localdomain.MemoryCurrent, VerifiedAt: &now})
	if err != nil {
		t.Fatal(err)
	}
	if err = l.db.Update(context.Background(), func(tx localstore.Tx) error {
		return tx.Memory().SaveFreshness(context.Background(), &localdomain.MemoryFreshness{Scope: m.Scope, Revision: localdomain.MemoryRepositoryRevision{Head: sha, Refs: map[string]string{"refs/heads/main": sha}}, CheckedAt: now, SucceededAt: &now})
	}); err != nil {
		t.Fatal(err)
	}
	return m
}
func enablePublication(t *testing.T, l *integrationLocal, team *integrationTeam) localdomain.PublicationGrant {
	t.Helper()
	g := localdomain.PublicationGrant{WorkspaceID: team.owner.Workspace.ID, ProjectID: team.project.ID, LocalProjectID: l.project, Repository: "github.com/acme/sync", Enabled: true}
	if err := l.memory.Memory.SetPublicationGrant(context.Background(), g); err != nil {
		t.Fatal(err)
	}
	return g
}
func TestMemorySyncTwoControllersOfflineRestartReplayAndRevocation(t *testing.T) {
	ctx := context.Background()
	team := newIntegrationTeam(t, "Shared memory")
	a, b := newIntegrationLocal(t), newIntegrationLocal(t)
	// A second member keeps their own controller, private database and runner.
	second, err := team.svc.AddMember(ctx, team.owner, "Second member", "", domain.RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	memberB, err := team.svc.Authenticate(ctx, second.Token)
	if err != nil {
		t.Fatal(err)
	}
	if err = team.svc.AddProjectMember(ctx, team.owner, team.project.ID, memberB.Member.ID, domain.ProjectContributor); err != nil {
		t.Fatal(err)
	}
	// The second controller enrolls with its member's distinct device identity.
	keys, _ := pki.NewHostKeys()
	pub := deviceid.Public{ID: deviceid.ID(keys.DeviceID()), Name: "Second controller", PublicKey: deviceid.EncodePublicKey(keys.PublicKey()), CreatedAt: time.Now()}
	device, err := team.svc.RegisterDevice(ctx, memberB, service.DeviceInput{Device: pub, Proof: keys.Sign(deviceid.RegistrationStatement(team.owner.Workspace.ID, memberB.Member.ID, pub.ID, pub.Name, keys.PublicKey())), Capabilities: []domain.Capability{domain.CapabilityRunner}})
	if err != nil {
		t.Fatal(err)
	}
	credential, err := team.svc.IssueLocalDeviceCredential(ctx, team.owner.Workspace.ID, device.ID)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(server.Handler(team.db, team.svc, nil, "test"))
	defer ts.Close()
	hostB, err := hostclient.New(hostclient.Options{Bases: []string{ts.URL}, Token: credential, Signer: keys, WorkspaceID: team.owner.Workspace.ID, UserID: memberB.Member.ID})
	if err != nil {
		t.Fatal(err)
	}
	ma := seedPublication(t, a, strings.Repeat("a", 40))
	mb := seedPublication(t, b, strings.Repeat("b", 40))
	ga := enablePublication(t, a, team)
	enablePublication(t, b, team)
	ba, bb := memoryBridge(t, a, ga.WorkspaceID), memoryBridge(t, b, ga.WorkspaceID)
	syncA := func() error {
		return connector.SyncMemory(ctx, team.host, ba, ga.WorkspaceID, team.member.Member.ID, team.device.ID)
	}
	syncB := func() error {
		return connector.SyncMemory(ctx, hostB, bb, ga.WorkspaceID, memberB.Member.ID, device.ID)
	}
	team.offline.Store(true)
	if err = syncA(); err == nil {
		t.Fatal("offline delivery succeeded")
	}
	events, err := a.memory.Memory.PublicationEvents(ctx, ga.WorkspaceID)
	if err != nil || len(events.Events) != 1 {
		t.Fatalf("offline journal: %+v %v", events, err)
	}
	raw, _ := json.Marshal(events)
	if strings.Contains(string(raw), "NEVER_EXPORT") || strings.Contains(string(raw), "/Users/") {
		t.Fatal("private fields crossed allowlist")
	}
	if err = syncB(); err != nil {
		t.Fatal(err)
	} // independent device/stream continues
	// Restart the first controller's memory service over its durable database.
	if err = a.db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := localsqlite.Open(ctx, a.dbPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	a.memory.Memory, err = localservice.NewMemory(localservice.Deps{Store: reopened}, "individual", localdomain.MemoryLimits{})
	if err != nil {
		t.Fatal(err)
	}
	team.offline.Store(false)
	team.loseAck.Store(true)
	if err = syncA(); err == nil {
		t.Fatal("lost response was not reported")
	}
	if err = syncA(); err != nil {
		t.Fatal(err)
	}
	if err = syncA(); err != nil {
		t.Fatal(err)
	}
	scope := integration.SharedMemoryScope{WorkspaceID: ga.WorkspaceID, Kind: "repository", ProjectID: ga.ProjectID, Repository: ga.Repository}
	page, err := team.svc.ReadMemories(ctx, team.member, scope, "", false)
	if err != nil || len(page.Records) != 2 {
		t.Fatalf("duplicated or missing knowledge: %+v %v", page, err)
	}
	// Publishing shares sanitized knowledge, never private storage or execution.
	if a.dbPath == b.dbPath || a.mgr == b.mgr || a.adapter == b.adapter {
		t.Fatal("members share an execution environment")
	}
	for _, local := range []struct {
		l *integrationLocal
		m *localdomain.Memory
	}{{a, ma}, {b, mb}} {
		private, err := local.l.memory.Memory.Get(ctx, local.m.Scope, local.m.ID)
		if err != nil || !strings.Contains(private.Summary, "NEVER_EXPORT") {
			t.Fatalf("private memory changed during publication: %+v %v", private, err)
		}
	}
	if _, err = b.memory.Memory.Get(ctx, mb.Scope, ma.ID); err == nil {
		t.Fatal("second controller can read first controller's private memory")
	}
	authors := map[string]bool{}
	for _, r := range page.Records {
		authors[r.AuthorID] = true
	}
	if !authors[team.member.Member.ID] || !authors[memberB.Member.ID] {
		t.Fatal("lost member provenance", authors)
	}
	var record integration.SharedMemoryRecord
	for _, r := range page.Records {
		if r.Content.Source.ID == ma.ID {
			record = r
		}
	}
	approved, err := team.svc.ChangeMemory(ctx, team.owner, record.ID, integration.MemoryCommand{Schema: integration.MemorySchema, Scope: scope, Version: record.Version, Action: "approve"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = team.svc.ChangeMemory(ctx, team.owner, record.ID, integration.MemoryCommand{Schema: integration.MemorySchema, Scope: scope, Version: approved.Version, Action: "publish"})
	if err != nil {
		t.Fatal(err)
	}

	// The curator publishes both members' independently sourced knowledge.
	for _, candidate := range page.Records {
		if candidate.AuthorID != memberB.Member.ID {
			continue
		}
		for _, action := range []string{"approve", "publish"} {
			candidate, err = team.svc.ChangeMemory(ctx, team.owner, candidate.ID, integration.MemoryCommand{Schema: integration.MemorySchema, Scope: scope, Version: candidate.Version, Action: action})
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	published, err := team.svc.ReadMemories(ctx, team.member, scope, "", true)
	if err != nil || len(published.Records) != 2 {
		t.Fatal("both members' knowledge was not published", published, err)
	}
	if err = syncB(); err != nil {
		t.Fatal(err)
	}
	if err = syncA(); err != nil {
		t.Fatal(err)
	}
	ps, err := a.memory.Memory.ReconcilePublication(ctx, ga.WorkspaceID)
	if err != nil || ps[0].State != "published" {
		t.Fatalf("publication receipt not refreshed: %+v %v", ps, err)
	}
	// A correction creates a fresh reviewed suggestion and explicitly supersedes
	// the published predecessor when the curator approves the new version.
	oldID := record.ID
	original, e := a.memory.Memory.Get(ctx, ma.Scope, ma.ID)
	if e != nil {
		t.Fatal(e)
	}
	content := original.MemoryContent
	content.Source.GitSHA = strings.Repeat("d", 40)
	content.Source.Reference = "git:" + content.Source.GitSHA
	verified := time.Now()
	content.VerifiedAt = &verified
	corrected, e := a.memory.Memory.Update(ctx, ma.Scope, ma.ID, original.Version, content)
	if e != nil {
		t.Fatal(e)
	}
	if e = reopened.Update(ctx, func(tx localstore.Tx) error {
		f, e := tx.Memory().Freshness(ctx, ma.Scope)
		if e != nil {
			return e
		}
		f.Revision.Head = content.Source.GitSHA
		return tx.Memory().SaveFreshness(ctx, f)
	}); e != nil {
		t.Fatal(e)
	}
	if err = syncA(); err != nil {
		t.Fatal(err)
	}
	page, err = team.svc.ReadMemories(ctx, team.member, scope, "", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range page.Records {
		if r.Content.Source.ID == ma.ID && r.ID != oldID {
			record = r
		}
	}
	if record.ID == oldID || record.Supersedes != oldID || record.Content.Source.Revision != corrected.Source.GitSHA {
		t.Fatalf("correction lost source revision/supersession: %+v", record)
	}
	approved, err = team.svc.ChangeMemory(ctx, team.owner, record.ID, integration.MemoryCommand{Schema: integration.MemorySchema, Scope: scope, Version: record.Version, Action: "approve"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = team.svc.ChangeMemory(ctx, team.owner, record.ID, integration.MemoryCommand{Schema: integration.MemorySchema, Scope: scope, Version: approved.Version, Action: "publish"})
	if err != nil {
		t.Fatal(err)
	}
	if err = syncA(); err != nil {
		t.Fatal(err)
	}
	if err = a.memory.Memory.RevokePublication(ctx, ga.WorkspaceID, ga.ProjectID, ma.ID); err != nil {
		t.Fatal(err)
	}
	team.loseAck.Store(true)
	if err = syncA(); err == nil {
		t.Fatal("lost revocation reply unreported")
	}
	if err = syncA(); err != nil {
		t.Fatal(err)
	}
	page, err = team.svc.ReadMemories(ctx, team.member, scope, "", false)
	if err != nil || len(page.Records) != 1 {
		t.Fatalf("revocation did not withdraw: %+v %v", page, err)
	}
	ps, err = a.memory.Memory.ReconcilePublication(ctx, ga.WorkspaceID)
	if err != nil || ps[0].State != "revoked" {
		t.Fatalf("revocation acknowledgement: %+v %v", ps, err)
	}
	if err = b.memory.Memory.Delete(ctx, mb.Scope, mb.ID, mb.Version); err != nil {
		t.Fatal(err)
	}
	if err = syncB(); err != nil {
		t.Fatal(err)
	}
	page, err = team.svc.ReadMemories(ctx, team.member, scope, "", false)
	if err != nil || len(page.Records) != 0 {
		t.Fatalf("source deletion was not synchronised: %+v %v", page, err)
	}
	if _, err = team.svc.RevokeDevice(ctx, team.owner, device.ID); err != nil {
		t.Fatal(err)
	}
	if err = syncB(); err == nil {
		t.Fatal("revoked device kept synchronizing")
	}
}

func TestMemorySyncDisableBeforeDeliverySendsOnlyTombstone(t *testing.T) {
	ctx := context.Background()
	team := newIntegrationTeam(t, "Cancelled memory")
	l := newIntegrationLocal(t)
	seedPublication(t, l, strings.Repeat("c", 40))
	g := enablePublication(t, l, team)
	first, err := l.memory.Memory.PublicationEvents(ctx, g.WorkspaceID)
	if err != nil || len(first.Events) != 1 {
		t.Fatalf("queue: %+v %v", first, err)
	}
	g.Enabled = false
	if err = l.memory.Memory.SetPublicationGrant(ctx, g); err != nil {
		t.Fatal(err)
	}
	cancelled, err := l.memory.Memory.PublicationEvents(ctx, g.WorkspaceID)
	if err != nil || len(cancelled.Events) != 2 || cancelled.Events[0].Action != "cancel" {
		t.Fatalf("cancel: %+v %v", cancelled, err)
	}
	bridge := memoryBridge(t, l, g.WorkspaceID)
	if err = connector.SyncMemory(ctx, team.host, bridge, g.WorkspaceID, team.member.Member.ID, team.device.ID); err != nil {
		t.Fatal(err)
	}
	page, err := team.svc.ReadMemories(ctx, team.member, integration.SharedMemoryScope{WorkspaceID: g.WorkspaceID, Kind: "repository", ProjectID: g.ProjectID, Repository: g.Repository}, "", false)
	if err != nil || len(page.Records) != 0 {
		t.Fatalf("disabled memory was exposed: %+v %v", page, err)
	}
	// Re-enabling is another affirmative choice, with a fresh event after the withdrawal.
	g.Enabled = true
	if err = l.memory.Memory.SetPublicationGrant(ctx, g); err != nil {
		t.Fatal(err)
	}
	if err = connector.SyncMemory(ctx, team.host, bridge, g.WorkspaceID, team.member.Member.ID, team.device.ID); err != nil {
		t.Fatal(err)
	}
	page, err = team.svc.ReadMemories(ctx, team.member, integration.SharedMemoryScope{WorkspaceID: g.WorkspaceID, Kind: "repository", ProjectID: g.ProjectID, Repository: g.Repository}, "", false)
	if err != nil || len(page.Records) != 1 {
		t.Fatalf("explicit re-enable: %+v %v", page, err)
	}
	// Cancellation also withdraws an uncertain original commit whose reply was lost.
	if first.Events[0].ID != cancelled.Events[0].ID {
		t.Fatal(fmt.Sprint("unstable cancellation identity"))
	}
}
