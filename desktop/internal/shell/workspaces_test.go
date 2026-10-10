package shell

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"devboard/desktop/internal/teamlink"
	"devboard/desktop/internal/workspaces"
	"devboard/internal/workspace"
)

// ---- fakes of what the shell is given ----

type fakeRegistry struct {
	mu       sync.Mutex
	items    []workspaces.Item
	problems []workspaces.Problem
	selected string
	calls    []string
	remember map[string]string
}

func (r *fakeRegistry) rec(c string) { r.mu.Lock(); r.calls = append(r.calls, c); r.mu.Unlock() }
func (r *fakeRegistry) Refresh(context.Context) workspaces.View {
	r.rec("Refresh")
	return workspaces.View{Items: r.items, Selected: r.selected, Problems: r.problems}
}
func (r *fakeRegistry) Select(_ context.Context, id string) (workspaces.View, error) {
	r.rec("Select " + id)
	for _, it := range r.items {
		if it.ID == id {
			r.selected = id
			return workspaces.View{Items: r.items, Selected: id}, nil
		}
	}
	return workspaces.View{}, errors.New("that workspace is not on this computer")
}
func (r *fakeRegistry) Target(_ context.Context, id, join string) (workspaces.Target, error) {
	r.rec("Target " + id + " " + join)
	return workspaces.Target{ID: id, URL: "http://127.0.0.1:1/#token=T"}, nil
}
func (r *fakeRegistry) Overview(context.Context) workspaces.Overview {
	r.rec("Overview")
	return workspaces.Overview{}
}
func (r *fakeRegistry) Kind(id string) (workspace.Kind, bool) {
	for _, it := range r.items {
		if it.ID == id {
			return it.Kind, true
		}
	}
	return "", false
}
func (r *fakeRegistry) Remember(id, place string) error {
	r.rec("Remember " + id + " " + place)
	if !workspace.ValidHref(place) {
		return errors.New("not a place")
	}
	return nil
}
func (r *fakeRegistry) Forget(id string) { r.rec("Forget " + id) }

type fakeTeam struct {
	mu       sync.Mutex
	added    string
	addErr   error
	forgot   []string
	grants   []string // "<id> <token> <base>"
	grantErr error
	status   map[error]string
}

func (t *fakeTeam) StatusOf(err error) teamlink.Status {
	switch {
	case err == nil:
		return teamlink.Status{State: "ready"}
	case errors.Is(err, workspaces.ErrNotRunning):
		return teamlink.Status{State: "not_installed", Detail: "Team is not set up on this computer."}
	case errors.Is(err, workspaces.ErrOutdated):
		return teamlink.Status{State: "outdated", Detail: "Team's service is older than this app."}
	}
	return teamlink.Status{State: "stopped", Detail: err.Error()}
}
func (t *fakeTeam) AddWorkspace(context.Context) (string, error) { return t.added, t.addErr }
func (t *fakeTeam) ForgetWorkspace(_ context.Context, id string) error {
	t.forgot = append(t.forgot, id)
	return nil
}
func (t *fakeTeam) DeliverGrant(_ context.Context, id, token, base string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.grantErr != nil {
		return t.grantErr
	}
	t.grants = append(t.grants, id+" "+token+" "+base)
	return nil
}

type fakeInstaller struct {
	found    bool
	activate int
	services []string
	err      error
}

func (i *fakeInstaller) Find() (string, error) {
	if !i.found {
		return "", errors.New("this build of Werkbord does not carry Team's service")
	}
	return "/Applications/Werkbord.app/Contents/MacOS/Werkbord", nil
}
func (i *fakeInstaller) Activate(context.Context) (teamlink.Result, error) {
	i.activate++
	return teamlink.Result{OK: i.err == nil}, i.err
}
func (i *fakeInstaller) Service(_ context.Context, a string) (teamlink.Result, error) {
	i.services = append(i.services, a)
	return teamlink.Result{OK: i.err == nil}, i.err
}

type fakeGrants struct {
	names []string
	err   error
}

func (g *fakeGrants) MintExecutionGrant(_ context.Context, name string) (string, string, error) {
	g.names = append(g.names, name)
	if g.err != nil && !strings.Contains(g.err.Error(), "earlier") {
		return "", "", g.err
	}
	return "wba_" + strings.ReplaceAll(name, " ", "_"), "http://127.0.0.1:7420", g.err
}

type rig struct {
	*Shell
	reg *fakeRegistry
	tm  *fakeTeam
	ins *fakeInstaller
	gr  *fakeGrants
	ui  *fakeUI
	inv *Invites
}

func personalItem() workspaces.Item {
	return workspaces.Item{Entry: workspace.Entry{ID: "personal", Kind: workspace.KindPersonal, Name: "Personal", State: workspace.StateReady}, Source: "Werkbord"}
}
func teamItem(slot, name string, st workspace.State) workspaces.Item {
	return workspaces.Item{Entry: workspace.Entry{ID: "team:" + slot, Kind: workspace.KindTeam, Name: name, State: st}, Source: "Team"}
}

func newRig(t *testing.T, answers ...string) *rig {
	t.Helper()
	r := &rig{
		reg: &fakeRegistry{items: []workspaces.Item{personalItem(), teamItem("main", "Acme", workspace.StateReady), teamItem("ws_2", "Globex", workspace.StateReady)}},
		tm:  &fakeTeam{added: "team:ws_3"}, ins: &fakeInstaller{found: true}, gr: &fakeGrants{}, ui: &fakeUI{answers: answers}, inv: &Invites{},
	}
	l := &fakeLauncher{}
	r.Shell = New(Options{Launcher: l, UI: r.ui, Version: "v1.8.0", Platform: "darwin", Workspaces: r.reg, Team: r.tm, TeamInstaller: r.ins, Grants: r.gr, Invites: r.inv})
	return r
}

func args(vs ...any) []json.RawMessage {
	var out []json.RawMessage
	for _, v := range vs {
		b, _ := json.Marshal(v)
		out = append(out, b)
	}
	return out
}

// ---- switching ----

func TestSwitchingWorkspaceIsAChoiceOfWhatToShowAndNothingMore(t *testing.T) {
	r := newRig(t)
	for _, id := range []string{"team:main", "personal", "team:ws_2", "personal"} {
		if _, err := r.OpenWorkspace(id); err != nil {
			t.Fatal(err)
		}
	}
	// Choosing asks the list and loads a page; it never reaches into the program that runs anyone's work: no grant is made,
	// delivered or revoked, no service is started or stopped, no dialog is shown, no summary is read.
	if len(r.gr.names) != 0 || len(r.tm.grants) != 0 || len(r.ins.services) != 0 || r.ins.activate != 0 || len(r.ui.asked) != 0 {
		t.Fatalf("switching changed something: %v %v %v %d %v", r.gr.names, r.tm.grants, r.ins.services, r.ins.activate, r.ui.asked)
	}
	for _, c := range r.reg.calls {
		if c == "Overview" || strings.HasPrefix(c, "Forget") || strings.HasPrefix(c, "Remember") {
			t.Fatalf("switching did %s", c)
		}
	}
	if _, err := r.OpenWorkspace("team:gone"); err == nil {
		t.Fatal("a workspace that is not here was opened")
	}
	// A page for a workspace that is loaded but not the open one can be asked for without making it the open one.
	r.reg.calls = nil
	if _, err := r.LoadWorkspace("team:main"); err != nil {
		t.Fatal(err)
	}
	for _, c := range r.reg.calls {
		if strings.HasPrefix(c, "Select") {
			t.Fatal("loading a workspace's page changed which is open")
		}
	}
}

func TestTheSwitcherSaysWhatIsWrongWithTeamsServiceAndWhetherItsInstallerIsHere(t *testing.T) {
	r := newRig(t)
	r.reg.problems = []workspaces.Problem{{Source: "Team", Kind: "not_running", Detail: "x"}}
	v := r.Workspaces()
	if v.Team.State != "not_installed" || !v.TeamInstaller || v.Invitation {
		t.Fatalf("%+v", v)
	}
	r.reg.problems = nil
	r.ins.found = false
	r.inv.Receive("werkbord://join/abc")
	v = r.Workspaces()
	if v.Team.State != "ready" || v.TeamInstaller || !v.Invitation {
		t.Fatalf("%+v", v)
	}
	if !r.inv.Peek() {
		t.Fatal("looking at the list used up the invitation")
	}
}

// ---- adding, activating, leaving ----

func TestAddingATeamOpensItsSetupAndHandsOverTheInvitationOnce(t *testing.T) {
	r := newRig(t)
	if !r.inv.Receive("werkbord://join/abc") {
		t.Fatal("a real invitation was refused")
	}
	r.reg.items = append(r.reg.items, teamItem("ws_3", "New Team workspace", workspace.StateSetup))
	tgt, err := r.AddTeam()
	if err != nil || tgt.ID != "team:ws_3" {
		t.Fatalf("%+v %v", tgt, err)
	}
	var target string
	for _, c := range r.reg.calls {
		if strings.HasPrefix(c, "Target") {
			target = c
		}
	}
	if target != "Target team:ws_3 werkbord://join/abc" {
		t.Fatalf("the invitation went to %q", target)
	}
	if r.inv.Peek() {
		t.Fatal("the invitation was not used up")
	}
	// Nothing was installed to do it.
	if r.ins.activate != 0 || len(r.ui.asked) != 0 {
		t.Fatal("adding a Team installed something")
	}
	// With Team's service not running the person is told, and nothing is installed behind their back.
	r.tm.addErr = workspaces.ErrNotRunning
	if _, err := r.AddTeam(); err == nil || !strings.Contains(err.Error(), "not set up") {
		t.Fatalf("%v", err)
	}
	if r.ins.activate != 0 {
		t.Fatal("a failed add activated Team")
	}
}

func TestInvitationsAreOnlyInvitations(t *testing.T) {
	var i Invites
	for _, bad := range []string{"", "https://evil.example/", "werkbord://update", "werkbord://join/ x", "werkbord://join/a\nb", "javascript:alert(1)", "werkbord://join/" + strings.Repeat("a", 20000), "werkbord://join/<script>"} {
		if i.Receive(bad) {
			t.Errorf("%q was kept", bad)
		}
	}
	if i.Peek() || i.Take() != "" {
		t.Fatal("something was kept")
	}
	if !i.Receive("werkbord://join/AbC-123_xyz") || i.Take() != "werkbord://join/AbC-123_xyz" || i.Take() != "" {
		t.Fatal("an invitation is handed over once")
	}
}

func TestTeamIsSetUpOnlyWhenThePersonSaysSoInADialogAPageCannotPress(t *testing.T) {
	r := newRig(t) // the dialog is answered with its Cancel button
	if _, err := r.ActivateTeam(); err == nil {
		t.Fatal("declining set Team up")
	}
	if r.ins.activate != 0 {
		t.Fatal("Team was set up without being allowed to")
	}
	if len(r.ui.asked) != 1 || r.ui.asked[0].Default != "Not now" || !strings.Contains(r.ui.asked[0].Message, "Nothing is installed until you choose") || !strings.Contains(r.ui.asked[0].Message, "never sees your Werkbord's credentials") {
		t.Fatalf("%+v", r.ui.asked)
	}
	r = newRig(t, "Set up Team")
	if _, err := r.ActivateTeam(); err != nil || r.ins.activate != 1 {
		t.Fatalf("%v %d", err, r.ins.activate)
	}
	// With no installer on this Mac the person is told where to get it, without a dialog.
	r = newRig(t, "Set up Team")
	r.ins.found = false
	if _, err := r.ActivateTeam(); err == nil || len(r.ui.asked) != 0 {
		t.Fatalf("%v %v", err, r.ui.asked)
	}
	// A failure of the installer is the person's to read.
	r = newRig(t, "Set up Team")
	r.ins.err = errors.New("service installation was cancelled")
	if st, err := r.ActivateTeam(); err == nil || st.State != "not_installed" || !strings.Contains(st.Detail, "cancelled") {
		t.Fatalf("%+v %v", st, err)
	}
}

func TestAnOlderServiceIsUpdatedNotSetUpAndARefusedUpdateLeavesItAnOlderService(t *testing.T) {
	older := func(t *testing.T, answers ...string) *rig {
		r := newRig(t, answers...)
		r.reg.problems = []workspaces.Problem{{Source: "Team", Kind: "outdated", Detail: "older"}}
		return r
	}
	// The person is asked about an update, in words about an update, and may decline it.
	r := older(t)
	if _, err := r.ActivateTeam(); err == nil || r.ins.activate != 0 {
		t.Fatalf("declining updated Team: %v %d", err, r.ins.activate)
	}
	ask := r.ui.asked[0]
	if !strings.Contains(ask.Title, "Update") || strings.Contains(ask.Title, "Set up") || !reflect.DeepEqual(ask.Buttons, []string{"Update Team", "Not now"}) || ask.Default != "Not now" ||
		!strings.Contains(ask.Message, "belongs to a Team workspace") || !strings.Contains(ask.Message, "Nothing is replaced until you choose Update") {
		t.Fatalf("%+v", ask)
	}
	// The set-up button does not answer an update question.
	r = older(t, "Set up Team")
	if _, err := r.ActivateTeam(); err == nil || r.ins.activate != 0 {
		t.Fatalf("%v %d", err, r.ins.activate)
	}
	r = older(t, "Update Team")
	if st, err := r.ActivateTeam(); err != nil || r.ins.activate != 1 {
		t.Fatalf("%+v %v %d", st, err, r.ins.activate)
	}
	// A service that belongs to a workspace is not replaced: the person reads the installer's words and the service is still the older one.
	r = older(t, "Update Team")
	r.ins.err = errors.New("Team's service on this Mac belongs to a Team workspace, so it will not be replaced or removed automatically.")
	if st, err := r.ActivateTeam(); err == nil || st.State != "outdated" || !strings.Contains(st.Detail, "belongs to a Team workspace") || strings.Contains(st.Detail, "execution error") {
		t.Fatalf("%+v %v", st, err)
	}
}

func TestStoppingAndRemovingTheServiceAreAskedAboutAndNothingElseIsAnAction(t *testing.T) {
	for _, tc := range []struct{ action, yes string }{{"start", "Start Team"}, {"stop", "Stop Team"}, {"uninstall", "Remove Team service"}} {
		r := newRig(t, "Cancel")
		if err := r.TeamService(tc.action); err != nil || len(r.ins.services) != 0 {
			t.Fatalf("%s: declining ran it: %v %v", tc.action, err, r.ins.services)
		}
		r = newRig(t, tc.yes)
		if err := r.TeamService(tc.action); err != nil || !reflect.DeepEqual(r.ins.services, []string{tc.action}) {
			t.Fatalf("%s: %v %v", tc.action, err, r.ins.services)
		}
		if tc.action == "stop" && !strings.Contains(r.ui.asked[0].Message, "Workspace Host") {
			t.Fatal("stopping does not say what the person loses if this Mac hosts")
		}
	}
	r := newRig(t, "Stop Team")
	for _, bad := range []string{"", "install", "restart", "stop; reboot"} {
		if err := r.TeamService(bad); err == nil {
			t.Errorf("%q was an action", bad)
		}
	}
	if len(r.ui.asked) != 0 || len(r.ins.services) != 0 {
		t.Fatal("an unknown action showed a dialog or ran")
	}
}

func TestOnlyATeamWorkspaceCanBeRemovedFromTheList(t *testing.T) {
	r := newRig(t)
	if err := r.ForgetWorkspace("personal"); err == nil {
		t.Fatal("Personal was removed")
	}
	if err := r.ForgetWorkspace("team:unknown"); err == nil {
		t.Fatal("a workspace that is not here was removed")
	}
	if err := r.ForgetWorkspace("team:ws_2"); err != nil || !reflect.DeepEqual(r.tm.forgot, []string{"team:ws_2"}) {
		t.Fatalf("%v %v", err, r.tm.forgot)
	}
	forgot := false
	for _, c := range r.reg.calls {
		forgot = forgot || c == "Forget team:ws_2"
	}
	if !forgot {
		t.Fatal("what the shell remembered about it was kept")
	}
}

// ---- what a workspace's page may ask ----

func TestAPageMayAskOnlyWhatItsKindOfWorkspaceMayAndTheKindIsTheAppsKnowledge(t *testing.T) {
	r := newRig(t, "Connect runner")
	// A Personal page cannot ask for what is a Team workspace's, even if it knows the name.
	for _, m := range []string{"ConnectRunner", "TeamService", "PendingInvitation", "AddTeam", "ActivateTeam", "ForgetWorkspace", "OpenWorkspace", "Workspaces", "Overview", "Relay", "Connect", "Reload", "Diagnostics", "ShowDiagnostics", "OpenLogs", "OpenInBrowser", "CheckForUpdates", "UpdatePersonal"} {
		if _, err := r.Relay("personal", m, nil); err == nil {
			t.Errorf("a Personal page asked for %s", m)
		}
	}
	// A Team page cannot choose a folder or update the app.
	for _, m := range []string{"ChooseDirectory", "RequestUpdate", "UpdateStatus", "Workspaces", "AddTeam", "ActivateTeam", "ForgetWorkspace", "Relay", "Reload", "UpdatePersonal"} {
		if _, err := r.Relay("team:main", m, args(false)); err == nil {
			t.Errorf("a Team page asked for %s", m)
		}
	}
	// The workspace must be one the app knows; a page cannot name its way into another kind.
	for _, id := range []string{"team:unknown", "", "personal ", "TEAM:main"} {
		if _, err := r.Relay(id, "Info", nil); err == nil {
			t.Errorf("%q was believed", id)
		}
	}
	if len(r.ui.asked) != 0 || len(r.gr.names) != 0 {
		t.Fatalf("refused calls still did something: %v %v", r.ui.asked, r.gr.names)
	}
	// What each may do works.
	if v, err := r.Relay("personal", "Info", nil); err != nil || v.(AppInfo).Version != "v1.8.0" {
		t.Fatalf("%v %v", v, err)
	}
	if _, err := r.Relay("team:main", "OpenExternal", args("https://example.com/")); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Relay("team:main", "OpenExternal", args("file:///etc/passwd")); err == nil {
		t.Fatal("a Team page opened a file")
	}
	// Arguments are exactly what the call takes.
	if _, err := r.Relay("team:main", "OpenExternal", args("https://a/", "extra")); err == nil {
		t.Fatal("an extra argument was ignored")
	}
	if _, err := r.Relay("team:main", "OpenExternal", args(42)); err == nil {
		t.Fatal("a number was taken for an address")
	}
	if _, err := r.Relay("team:main", "PendingInvitation", args("x")); err == nil {
		t.Fatal("an argument to a call that takes none")
	}
}

func TestConnectingARunnerIsAskedAboutNamedForItsWorkspaceAndDeliveredToThatWorkspaceAlone(t *testing.T) {
	// Declined: nothing is made.
	r := newRig(t)
	if _, err := r.Relay("team:main", "ConnectRunner", nil); err == nil {
		t.Fatal("declining connected the runner")
	}
	if len(r.gr.names) != 0 || len(r.tm.grants) != 0 {
		t.Fatalf("%v %v", r.gr.names, r.tm.grants)
	}
	if d := r.ui.asked[0]; d.Default != "Cancel" || !strings.Contains(d.Message, "revoke it") || !strings.Contains(d.Message, "cannot see your files") {
		t.Fatalf("%+v", d)
	}
	// Accepted from one Team's page: a grant named for that Team goes to that Team's service and to no other.
	r = newRig(t, "Connect runner")
	if _, err := r.Relay("team:ws_2", "ConnectRunner", nil); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(r.gr.names, []string{"Team:ws_2: Globex"}) || !reflect.DeepEqual(r.tm.grants, []string{"team:ws_2 wba_Team:ws_2:_Globex http://127.0.0.1:7420"}) {
		t.Fatalf("%v %v", r.gr.names, r.tm.grants)
	}
	// A failure to make the grant delivers nothing.
	r = newRig(t, "Connect runner")
	r.gr.err = errors.New("Werkbord would not make the grant")
	if _, err := r.Relay("team:main", "ConnectRunner", nil); err == nil || len(r.tm.grants) != 0 {
		t.Fatalf("%v %v", err, r.tm.grants)
	}
	// A failure to deliver is the person's to read.
	r = newRig(t, "Connect runner")
	r.tm.grantErr = errors.New("this grant is not a per-workspace execution grant")
	if _, err := r.Relay("team:main", "ConnectRunner", nil); err == nil || !strings.Contains(err.Error(), "execution grant") {
		t.Fatalf("%v", err)
	}
	// An earlier grant that could not be revoked is reported, but the new one is in place.
	r = newRig(t, "Connect runner")
	r.gr.err = errors.New("the new grant works, but 1 earlier one(s) with the same name could not be revoked")
	if _, err := r.Relay("team:main", "ConnectRunner", nil); err == nil || !strings.Contains(err.Error(), "could not be revoked") || len(r.tm.grants) != 1 {
		t.Fatalf("%v %v", err, r.tm.grants)
	}
	// A very long workspace name still makes a grant name Werkbord accepts.
	r = newRig(t, "Connect runner")
	r.reg.items[1].Name = strings.Repeat("Long name ", 20)
	if _, err := r.Relay("team:main", "ConnectRunner", nil); err != nil {
		t.Fatal(err)
	}
	if len([]rune(r.gr.names[0])) > 60 {
		t.Fatalf("%q", r.gr.names[0])
	}
}

func TestAPageCanMakeTheShellRememberAPlaceInsideItselfOnly(t *testing.T) {
	r := newRig(t)
	if err := r.RememberPlace("team:main", "?tab=board&project=tpj_1"); err != nil {
		t.Fatal(err)
	}
	if err := r.RememberPlace("team:main", "https://evil.example/"); err == nil {
		t.Fatal("an address was remembered as a place")
	}
}

func TestIdenticallyNamedWorkspacesHaveIndependentGrantNames(t *testing.T) {
	r := newRig(t, "Connect runner", "Connect runner")
	r.reg.items[1].Name = "Same name"
	r.reg.items[2].Name = "Same name"
	for _, id := range []string{"team:main", "team:ws_2"} {
		if _, err := r.Relay(id, "ConnectRunner", nil); err != nil {
			t.Fatal(err)
		}
	}
	if r.gr.names[0] == r.gr.names[1] {
		t.Fatal("connecting one workspace could revoke another's grant")
	}
	if !strings.HasPrefix(r.tm.grants[0], "team:main ") || !strings.HasPrefix(r.tm.grants[1], "team:ws_2 ") {
		t.Fatalf("wrong authority destinations: %v", r.tm.grants)
	}
}
