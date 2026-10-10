package main

// These tests host both independent APIs in the developer-client test harness.
// Neither shipped product links the other product's backend.
import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"devboard/internal/agent"
	"devboard/internal/agent/fake"
	localapi "devboard/internal/api"
	"devboard/internal/deviceid"
	localdomain "devboard/internal/domain"
	"devboard/internal/events"
	"devboard/internal/gitrepo"
	"devboard/internal/localaccess"
	"devboard/internal/runner"
	localservice "devboard/internal/service"
	localsqlite "devboard/internal/store/sqlite"
	"devboard/internal/team/connector"
	"devboard/internal/team/devicestate"
	"devboard/internal/team/domain"
	"devboard/internal/team/hostclient"
	"devboard/internal/team/infra/pki"
	"devboard/internal/team/localwerkbord"
	"devboard/internal/team/server"
	"devboard/internal/team/service"
	teamstore "devboard/internal/team/store"
)

type integrationLocal struct {
	url     string
	project string
	tasks   *localservice.Tasks
	runs    *localservice.Runs
	mgr     *runner.Manager
	adapter *fake.Adapter
	bridge  *localwerkbord.Client
	owner   string
}

func integrationGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null", "GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=t@example.test", "GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=t@example.test")
	raw, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git: %v %s", err, raw)
	}
	return strings.TrimSpace(string(raw))
}
func newIntegrationLocal(t *testing.T) *integrationLocal {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	integrationGit(t, dir, "init", "-q", "-b", "main")
	integrationGit(t, dir, "commit", "--allow-empty", "-qm", "initial")
	integrationGit(t, dir, "remote", "add", "origin", "git@github.com:acme/sync.git")
	db, err := localsqlite.Open(ctx, filepath.Join(t.TempDir(), "local.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	bus := events.NewBroker()
	t.Cleanup(bus.Close)
	deps := localservice.Deps{Store: db, Bus: bus}
	adapter := &fake.Adapter{}
	registry := agent.NewRegistry()
	if err := registry.Register(adapter); err != nil {
		t.Fatal(err)
	}
	projects := &localservice.Projects{Deps: deps, Git: &gitrepo.CLI{}, Catalog: registry}
	tasks := &localservice.Tasks{Deps: deps, Catalog: registry}
	runs := &localservice.Runs{Deps: deps}
	settings := &localservice.Settings{Deps: deps, Catalog: registry}
	if _, err := settings.RegisterRunner(ctx); err != nil {
		t.Fatal(err)
	}
	p, err := projects.Register(ctx, dir, "local")
	if err != nil {
		t.Fatal(err)
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	trees := &localservice.Worktrees{Deps: deps, Root: root}
	git := &localservice.GitControl{Deps: deps, Git: &gitrepo.CLI{}, Worktrees: trees}
	mgr := runner.New(runner.Options{Runs: runs, Tasks: tasks, Projects: projects, Settings: settings, Worktrees: trees, Git: &gitrepo.CLI{}, Agents: registry, WorktreeRoot: root, FlushInterval: time.Millisecond})
	t.Cleanup(func() { mgr.Shutdown(ctx) })
	grants, err := localaccess.Open(filepath.Join(t.TempDir(), "grants.json"))
	if err != nil {
		t.Fatal(err)
	}
	owner := "LOCAL_CONTROLLER_SECRET_NEVER_SHARE"
	ts := httptest.NewServer(localapi.New(localapi.Options{Store: db, Events: bus, Projects: projects, Tasks: tasks, Runs: runs, Settings: settings, Runner: mgr, Agents: registry, Git: git, Version: "test", AuthRequired: true, Token: owner, LocalAccess: grants}).Handler())
	t.Cleanup(ts.Close)
	token, err := localwerkbord.ConnectSync(ctx, ts.URL, owner)
	if err != nil {
		t.Fatal(err)
	}
	bridge, err := localwerkbord.New(ts.URL, token)
	if err != nil {
		t.Fatal(err)
	}
	if err := bridge.RequireIntegrationAccess(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := bridge.StartRun(ctx, p.ID, "tsk_x"); err == nil {
		t.Fatal("metadata-only grant could launch a run")
	}
	return &integrationLocal{url: ts.URL, project: p.ID, tasks: tasks, runs: runs, mgr: mgr, adapter: adapter, bridge: bridge, owner: owner}
}

type integrationTeam struct {
	svc        *service.Service
	db         *teamstore.DB
	ownerToken string
	owner      service.Actor
	member     service.Actor
	project    domain.Project
	ticket     domain.Ticket
	device     domain.Device
	host       *hostclient.Client
	offline    atomic.Bool
	loseAck    atomic.Bool
}

func newIntegrationTeam(t *testing.T, name string) *integrationTeam {
	t.Helper()
	ctx := context.Background()
	db, err := teamstore.Open(ctx, filepath.Join(t.TempDir(), "team.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	svc := service.New(db)
	w, err := svc.CreateWorkspace(ctx, name, "Owner", "")
	if err != nil {
		t.Fatal(err)
	}
	owner, err := svc.Authenticate(ctx, w.Token)
	if err != nil {
		t.Fatal(err)
	}
	b, err := svc.AddMember(ctx, owner, "Member", "", domain.RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	member, err := svc.Authenticate(ctx, b.Token)
	if err != nil {
		t.Fatal(err)
	}
	p, err := svc.CreateProject(ctx, owner, service.ProjectInput{Name: "Sync", Repository: "https://github.com/acme/sync"})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.AddProjectMember(ctx, owner, p.ID, member.Member.ID, domain.ProjectContributor); err != nil {
		t.Fatal(err)
	}
	k, err := svc.CreateTicket(ctx, owner, p.ID, service.TicketInput{Title: "same task", Description: "Implement safely", Status: domain.TicketAvailable})
	if err != nil {
		t.Fatal(err)
	}
	k, err = svc.ClaimTicket(ctx, member, p.ID, k.ID)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := pki.NewHostKeys()
	if err != nil {
		t.Fatal(err)
	}
	pub := deviceid.Public{ID: deviceid.ID(keys.DeviceID()), Name: "Connector", PublicKey: deviceid.EncodePublicKey(keys.PublicKey()), CreatedAt: time.Now()}
	device, err := svc.RegisterDevice(ctx, member, service.DeviceInput{Device: pub, Proof: keys.Sign(deviceid.RegistrationStatement(w.Workspace.ID, member.Member.ID, pub.ID, pub.Name, keys.PublicKey())), Capabilities: []domain.Capability{domain.CapabilityRunner}})
	if err != nil {
		t.Fatal(err)
	}
	credential, err := svc.IssueLocalDeviceCredential(ctx, w.Workspace.ID, device.ID)
	if err != nil {
		t.Fatal(err)
	}
	x := &integrationTeam{svc: svc, db: db, ownerToken: w.Token, owner: owner, member: member, project: p, ticket: k, device: device}
	handler := server.Handler(db, svc, nil, "test")
	ts := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if x.offline.Load() {
			http.Error(rw, "no quorum", 503)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/progress") && r.Method == "PUT" && x.loseAck.Swap(false) {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, r)
			http.Error(rw, "answer lost", 503)
			return
		}
		handler.ServeHTTP(rw, r)
	}))
	t.Cleanup(ts.Close)
	x.host, err = hostclient.New(hostclient.Options{Bases: []string{ts.URL}, Token: credential, Signer: keys, WorkspaceID: w.Workspace.ID, UserID: member.Member.ID})
	if err != nil {
		t.Fatal(err)
	}
	return x
}
func makeIntegrationConnector(t *testing.T, local *integrationLocal, team *integrationTeam, j *devicestate.SyncJournal) *connector.Connector {
	t.Helper()
	return &connector.Connector{WorkspaceID: team.owner.Workspace.ID, MemberID: team.member.Member.ID, DeviceID: team.device.ID, Host: team.host, Local: local.bridge, Journal: j, Projects: map[string]string{team.project.ID: local.project}}
}
func integrationWait(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition did not become true")
}
func TestConnectorClaimExecutionRecoveryConflictAndRevocation(t *testing.T) {
	ctx := context.Background()
	local := newIntegrationLocal(t)
	team := newIntegrationTeam(t, "Acme")
	path := filepath.Join(t.TempDir(), "sync.db")
	j, err := devicestate.OpenSyncJournal(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { j.Close() }()
	c := makeIntegrationConnector(t, local, team, j)
	if err := c.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	as, err := j.Associations(ctx, c.WorkspaceID)
	if err != nil || len(as) != 1 {
		t.Fatalf("associations %v %v", as, err)
	}
	a := as[0]
	if err := c.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	tasks, _ := local.tasks.List(ctx, local.project)
	if len(tasks) != 1 || len(local.adapter.Sessions()) != 0 {
		t.Fatal("import duplicated tasks or launched an agent")
	}
	team.loseAck.Store(true)
	run, err := local.mgr.Start(ctx, runner.StartInput{TaskID: a.TaskID, AgentID: "fake"})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Tick(ctx); err == nil {
		t.Fatal("lost acknowledgment was hidden")
	}
	pending, err := j.Pending(ctx, a.Key(), time.Now().Add(time.Hour))
	if err != nil || len(pending) == 0 {
		t.Fatalf("outbound not durable: %v %v", pending, err)
	}
	team.offline.Store(true)
	secret := "RAW_TRANSCRIPT_ENV_CREDENTIAL_DO_NOT_REPORT"
	local.adapter.Last().Assistant(secret)
	local.adapter.Last().Ask("question", secret)
	integrationWait(t, func() bool {
		r, _ := local.runs.Get(ctx, run.ID)
		return r != nil && r.State == localdomain.RunWaitingForUser
	})
	_ = c.Tick(ctx)
	local.adapter.Last().Exit(0, "")
	integrationWait(t, func() bool {
		r, _ := local.runs.Get(ctx, run.ID)
		return r != nil && r.State == localdomain.RunCompleted
	})
	_ = c.Tick(ctx)
	if err := j.Close(); err != nil {
		t.Fatal(err)
	}
	j, err = devicestate.OpenSyncJournal(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	c.Journal = j
	c.Now = func() time.Time { return time.Now().Add(time.Hour) }
	team.offline.Store(false)
	for range 3 {
		if err := c.Tick(ctx); err != nil {
			t.Fatal(err)
		}
	}
	records, err := team.svc.TicketProgress(ctx, team.owner, team.project.ID, team.ticket.ID)
	if err != nil || len(records) != 1 || records[0].Execution.State != "completed" {
		t.Fatalf("progress %v %v", records, err)
	}
	ticket, _ := team.svc.GetTicket(ctx, team.member, team.project.ID, team.ticket.ID)
	if ticket.Status != domain.TicketInProgress {
		t.Fatal("run completion completed ticket")
	}
	b, _ := json.Marshal(records)
	if strings.Contains(string(b), secret) || strings.Contains(string(b), local.owner) || strings.Contains(string(b), "prompt") {
		t.Fatalf("metadata leak: %s", b)
	}
	text := "Team changed requirements"
	if _, err := team.svc.UpdateTicket(ctx, team.owner, team.project.ID, team.ticket.ID, service.TicketPatch{Requirements: &text, Version: &ticket.Version}); err != nil {
		t.Fatal(err)
	}
	if err := c.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	as, _ = j.Associations(ctx, c.WorkspaceID)
	if !as[0].Conflict {
		t.Fatal("Team edit overwrote executed local task")
	}
	if _, err := team.svc.RevokeDevice(ctx, team.member, team.device.ID); err != nil {
		t.Fatal(err)
	}
	if err := c.Tick(ctx); !hostclient.IsStatus(err, 401) {
		t.Fatalf("revocation: %v", err)
	}
	as, _ = j.Associations(ctx, c.WorkspaceID)
	if as[0].Suspended == "" {
		t.Fatal("revocation not persisted")
	}
	if len(local.adapter.Sessions()) != 1 {
		t.Fatal("connector relaunched work")
	}
}

func TestConnectorMultipleWorkspacesAreIsolated(t *testing.T) {
	ctx := context.Background()
	local := newIntegrationLocal(t)
	a := newIntegrationTeam(t, "One")
	b := newIntegrationTeam(t, "Two")
	j, err := devicestate.OpenSyncJournal(ctx, filepath.Join(t.TempDir(), "sync.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	ca, cb := makeIntegrationConnector(t, local, a, j), makeIntegrationConnector(t, local, b, j)
	if err := ca.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if err := cb.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	aa, _ := j.Associations(ctx, ca.WorkspaceID)
	bb, _ := j.Associations(ctx, cb.WorkspaceID)
	if len(aa) != 1 || len(bb) != 1 || aa[0].TaskID == bb[0].TaskID {
		t.Fatal("workspaces shared a task")
	}
	a.offline.Store(true)
	if err := ca.Tick(ctx); err == nil {
		t.Fatal("offline team accepted a tick")
	}
	if err := cb.Tick(ctx); err != nil {
		t.Fatal("offline team stopped another workspace", err)
	}
	if _, err := a.svc.ReleaseTicket(ctx, a.member, a.project.ID, a.ticket.ID); err != nil {
		t.Fatal(err)
	}
	a.offline.Store(false)
	if err := ca.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	aa, _ = j.Associations(ctx, ca.WorkspaceID)
	if aa[0].Suspended == "" {
		t.Fatal("reassignment not suspended")
	}
	latest, err := b.svc.GetTicket(ctx, b.owner, b.project.ID, b.ticket.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.svc.ArchiveTicket(ctx, b.owner, b.project.ID, b.ticket.ID, latest.Version, true); err != nil {
		t.Fatal(err)
	}
	if err := cb.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	bb, _ = j.Associations(ctx, cb.WorkspaceID)
	if bb[0].Suspended == "" {
		t.Fatal("archived ticket was not suspended")
	}
}

// Browser QA starts the same signed connector and real APIs as the integration
// suite, in disposable directories. Nothing is installed on this computer.
func TestConnectorBrowserFixture(t *testing.T) {
	dir := os.Getenv("WERKBORD_CONNECTOR_BROWSER_FIXTURE")
	if dir == "" {
		t.Skip("browser fixture is opt-in")
	}
	ctx := context.Background()
	local := newIntegrationLocal(t)
	team := newIntegrationTeam(t, "Browser fixture")
	j, err := devicestate.OpenSyncJournal(ctx, filepath.Join(t.TempDir(), "sync.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	c := makeIntegrationConnector(t, local, team, j)
	if err := c.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	as, _ := j.Associations(ctx, c.WorkspaceID)
	run, err := local.mgr.Start(ctx, runner.StartInput{TaskID: as[0].TaskID, AgentID: "fake"})
	if err != nil {
		t.Fatal(err)
	}
	local.adapter.Last().Ask("browser-question", "This question stays local")
	integrationWait(t, func() bool { r, _ := local.runs.Get(ctx, run.ID); return r.State == localdomain.RunWaitingForUser })
	if err := c.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]string{"url": team.host.Bases()[0], "token": team.ownerToken, "project": team.project.ID, "ticket": team.ticket.ID})
	if err := os.WriteFile(filepath.Join(dir, "ready.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(filepath.Join(dir, "stop")); err == nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("browser fixture not stopped")
}
