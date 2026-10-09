package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"devboard/internal/team/config"
	"devboard/internal/team/domain"
	"devboard/internal/team/infra/pki"
	"devboard/internal/team/service"
)

func testHub(t *testing.T) *Hub {
	t.Helper()
	c := config.Default()
	c.DataDir = t.TempDir()
	h, err := NewHub(DaemonOptions{Config: c, Log: quiet(), Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	// No loops run in these tests, but the operations a window starts need a context to run in.
	h.slots[MainSlot].d.ctx = bg
	return h
}

func hubCall(h *Hub, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://127.0.0.1:7431"+path, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+h.key)
	w := httptest.NewRecorder()
	h.Handler().ServeHTTP(w, r)
	return w
}

type listed struct {
	Workspaces []SlotSummary `json:"workspaces"`
}

func hubList(t *testing.T, h *Hub) []SlotSummary {
	t.Helper()
	w := hubCall(h, "GET", "/api/device/v1/workspaces", "")
	if w.Code != 200 {
		t.Fatalf("list: %d %s", w.Code, w.Body)
	}
	var l listed
	if err := json.Unmarshal(w.Body.Bytes(), &l); err != nil {
		t.Fatal(err)
	}
	return l.Workspaces
}

func addSlot(t *testing.T, h *Hub) string {
	t.Helper()
	w := hubCall(h, "POST", "/api/device/v1/workspaces", "{}")
	if w.Code != 200 && w.Code != 201 {
		t.Fatalf("add: %d %s", w.Code, w.Body)
	}
	var out struct {
		Slot    string `json:"slot"`
		Created bool   `json:"created"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	if s := h.slots[out.Slot]; s != nil && s.d.ctx == nil {
		s.d.ctx = bg
	}
	return out.Slot
}

// joinSlot enrolls the hub's slot in a workspace and brings the device up, as the service's loop would.
func joinSlot(t *testing.T, h *Hub, slot string, host *host, created service.Created, name string) {
	t.Helper()
	invite := host.invite(created, service.EnrollInviteInput{Label: name, ForMemberID: created.Owner.ID, RequireApproval: true})
	in, _ := json.Marshal(map[string]string{"link": invite.Link, "name": "Ada", "deviceName": name})
	w := hubCall(h, "POST", "/w/"+slot+"/api/device/v1/join", string(in))
	if w.Code != 202 {
		t.Fatalf("join %s: %d %s", slot, w.Code, w.Body)
	}
	d := h.slots[slot].d
	d.job.Wait()
	list, err := host.svc.ListEnrollments(bg, host.owner(created), domain.EnrollmentPending)
	if err != nil || len(list) != 1 {
		t.Fatalf("pending: %+v %v", list, err)
	}
	if _, err := host.svc.ApproveEnrollment(bg, host.owner(created), list[0].ID); err != nil {
		t.Fatal(err)
	}
	if err := d.resumeJoin(bg); err != nil {
		t.Fatal(err)
	}
	if err := d.loadDevice(); err != nil {
		t.Fatal(err)
	}
}

// anotherWorkspace makes a second, separate workspace whose private network does not overlap range: a chance of one in
// 127 for two random ranges, and a person who met it would be told so (TestTwoWorkspacesMayNotShareAPrivateNetworkRange).
func anotherWorkspace(t *testing.T, range1 string) (*host, service.Created) {
	t.Helper()
	for i := 0; i < 20; i++ {
		h, created, nc := newFirstHost(t, "127.0.0.1")
		if !netip.MustParsePrefix(nc.Range).Overlaps(netip.MustParsePrefix(range1)) {
			h.serve()
			return h, created
		}
	}
	t.Fatal("twenty workspaces in a row took the same range")
	return nil, service.Created{}
}

// occupyAsHost gives a slot's vault a workspace of its own whose authority is this device, as if it had created it.
func occupyAsHost(t *testing.T, v *pki.Vault, id, prefix string) {
	addr := netip.MustParsePrefix(prefix).Addr().Next().Next().String()
	t.Helper()
	if _, err := v.Create(id, "Workspace "+id, netip.MustParsePrefix(prefix), time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := v.SaveDeviceToken("device-credential"); err != nil {
		t.Fatal(err)
	}
	if err := v.SaveSecret("member", []byte("tmb_owner")); err != nil {
		t.Fatal(err)
	}
	if err := v.SaveJoinInfo(pki.JoinInfo{APIAddrs: []string{addr}, APIPort: 7430}); err != nil {
		t.Fatal(err)
	}
}

func TestTheHubNeedsTheDevicesOwnCredentialAndRefusesOtherPages(t *testing.T) {
	h := testHub(t)
	for _, auth := range []string{"", "Bearer nothing"} {
		r := httptest.NewRequest("GET", "http://127.0.0.1:7431/api/device/v1/workspaces", nil)
		if auth != "" {
			r.Header.Set("Authorization", auth)
		}
		w := httptest.NewRecorder()
		h.Handler().ServeHTTP(w, r)
		if w.Code != 401 {
			t.Fatalf("list without the credential: %d", w.Code)
		}
	}
	r := httptest.NewRequest("POST", "http://127.0.0.1:7431/api/device/v1/workspaces", strings.NewReader("{}"))
	r.Header.Set("Authorization", "Bearer "+h.key)
	r.Header.Set("Origin", "https://attacker.example")
	w := httptest.NewRecorder()
	h.Handler().ServeHTTP(w, r)
	if w.Code != 403 || len(hubList(t, h)) != 1 {
		t.Fatalf("a foreign page added a workspace: %d", w.Code)
	}
	// The same credential opens every workspace; none opens without it.
	slot := addSlot(t, h)
	r = httptest.NewRequest("GET", "http://127.0.0.1:7431/w/"+slot+"/api/device/v1/state", nil)
	w = httptest.NewRecorder()
	h.Handler().ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatalf("a workspace answered without the credential: %d", w.Code)
	}
}

func TestADeviceInstalledBeforeSeveralWorkspacesKeepsItsDataInTheFirstSlot(t *testing.T) {
	c := config.Default()
	c.DataDir = t.TempDir()
	// An earlier single-workspace installation: its license and local state sit directly in the data directory.
	if err := os.WriteFile(filepath.Join(c.DataDir, "license.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := NewHub(DaemonOptions{Config: c, Log: quiet(), Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if got := h.slotDir(MainSlot); got != c.DataDir {
		t.Fatalf("the first slot lives at %s, not where the earlier installation put its data", got)
	}
	// The unprefixed paths an older window uses reach it.
	w := hubCall(h, "GET", "/api/device/v1/state", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"slot":"main"`) {
		t.Fatalf("legacy state: %d %s", w.Code, w.Body)
	}
	w = hubCall(h, "GET", "/w/main/api/device/v1/state", "")
	if w.Code != 200 {
		t.Fatalf("prefixed state: %d", w.Code)
	}
	if w := hubCall(h, "GET", "/w/nope/api/device/v1/state", ""); w.Code != 404 {
		t.Fatalf("an unknown workspace answered: %d", w.Code)
	}
	if w := hubCall(h, "GET", "/w/main", ""); w.Code != http.StatusTemporaryRedirect || w.Header().Get("Location") != "/w/main/" {
		t.Fatalf("the workspace page without a slash: %d %q", w.Code, w.Header().Get("Location"))
	}
	// The console is served per workspace, and at the root for an older window.
	for _, path := range []string{"/", "/w/main/", "/w/main/console.js"} {
		if w := hubCall(h, "GET", path, ""); w.Code != 200 {
			t.Fatalf("%s: %d", path, w.Code)
		}
	}
}

func TestWorkspacesAreSlotsThatSurviveARestartAndAnEmptyOneIsReused(t *testing.T) {
	h := testHub(t)
	first := addSlot(t, h)
	// The first slot is empty, so "add a Team" opens it rather than making a second one.
	if first != MainSlot {
		t.Fatalf("an empty first slot was not offered: %s", first)
	}
	// Make main look occupied, so a second add has to create one.
	if err := os.MkdirAll(h.slots[MainSlot].d.workspaceConfig().PKIDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(h.slots[MainSlot].d.workspaceConfig().PKIDir(), "workspace.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	second := addSlot(t, h)
	if second == MainSlot || !slotIDPattern.MatchString(second) {
		t.Fatalf("second slot = %q", second)
	}
	if again := addSlot(t, h); again != second {
		t.Fatalf("a second add made yet another slot: %s then %s", second, again)
	}
	if got := hubList(t, h); len(got) != 2 || got[0].Slot != MainSlot || got[1].Slot != second {
		t.Fatalf("list = %+v", got)
	}
	if _, err := os.Stat(filepath.Join(h.o.Config.DataDir, "slots", second)); err != nil {
		t.Fatal("the new workspace has no directory of its own", err)
	}
	h2, err := NewHub(h.o)
	if err != nil {
		t.Fatal(err)
	}
	if got := hubList(t, h2); len(got) != 2 || got[1].Slot != second {
		t.Fatalf("a restart forgot a workspace: %+v", got)
	}
	// Empty slots other than the first can be forgotten; the first stays, and an occupied one is refused.
	if w := hubCall(h2, "DELETE", "/api/device/v1/workspaces/"+MainSlot, ""); w.Code != 409 {
		t.Fatalf("removing the first slot: %d", w.Code)
	}
	if w := hubCall(h2, "DELETE", "/api/device/v1/workspaces/"+second, ""); w.Code != 200 {
		t.Fatalf("removing an empty slot: %d %s", w.Code, w.Body)
	}
	if _, err := os.Stat(filepath.Join(h.o.Config.DataDir, "slots", second)); !os.IsNotExist(err) {
		t.Fatal("the removed workspace's directory is still there")
	}
	if w := hubCall(h2, "GET", "/w/"+second+"/api/device/v1/state", ""); w.Code != 404 {
		t.Fatalf("a removed workspace answered: %d", w.Code)
	}
}

func TestOnlyOneWorkspaceOnAComputerCanBeAWorkspaceHost(t *testing.T) {
	h := testHub(t)
	main := h.slots[MainSlot]
	v, err := main.d.setupVault(main.d.workspaceConfig())
	if err != nil {
		t.Fatal(err)
	}
	occupyAsHost(t, v, "tws_hosted", "10.230.0.0/16")
	if err := main.d.loadDevice(); err != nil || !main.d.mat.Meta.Authority {
		t.Fatalf("the first workspace is not a host: %v", err)
	}
	// Add a second workspace slot. Its device is told it cannot host, and says so in its profile.
	second := addSlot(t, h)
	if second == MainSlot {
		t.Fatal("main is occupied, so adding a workspace must make a new slot")
	}
	d2 := h.slots[second].d
	if err := d2.hostingAllowed(); err == nil {
		t.Fatal("a second workspace could take the host ports")
	}
	if !d2.profile().HostConflict {
		t.Fatal("the device did not tell the workspace it cannot host")
	}
	if main.d.profile().HostConflict {
		t.Fatal("the workspace that does host was told it cannot")
	}
	w := hubCall(h, "POST", "/w/"+second+"/api/device/v1/create", `{"name":"Second","owner":"Ada"}`)
	if w.Code != 409 || !strings.Contains(w.Body.String(), "can host only one") {
		t.Fatalf("creating a second hosted workspace: %d %s", w.Code, w.Body)
	}
	for _, s := range hubList(t, h) {
		switch s.Slot {
		case MainSlot:
			if s.HostingBlocked != "" || !s.WorkspaceHost {
				t.Fatalf("main = %+v", s)
			}
		default:
			if s.HostingBlocked == "" {
				t.Fatalf("%s does not say it cannot host: %+v", s.Slot, s)
			}
		}
	}
	// An administrator is told, before choosing it, that the device cannot be a Host.
	if fit := domain.HostFitOf(&domain.DeviceProfile{Platform: "darwin", Form: domain.FormDesktop, HostConflict: true}); fit.Possible || len(fit.Reasons) != 1 || fit.Reasons[0] != domain.AdviceHostsElsewhere {
		t.Fatalf("a device that already hosts elsewhere is offered as a host: %+v", fit)
	}
}

func TestAPersonJoinsTwoWorkspacesAndLeavingOneLeavesTheOtherAlone(t *testing.T) {
	hostA, createdA, ncA := newFirstHost(t, "127.0.0.1")
	hostA.serve()
	hostB, createdB := anotherWorkspace(t, ncA.Range)
	h := testHub(t)

	joinSlot(t, h, MainSlot, hostA, createdA, "Ada's Mac in A")
	second := addSlot(t, h)
	if second == MainSlot {
		t.Fatal("the first slot is occupied; the second workspace needs its own")
	}
	joinSlot(t, h, second, hostB, createdB, "Ada's Mac in B")

	got := hubList(t, h)
	if len(got) != 2 || !got[0].Enrolled || !got[1].Enrolled || got[0].Workspace.ID == got[1].Workspace.ID {
		t.Fatalf("list = %+v", got)
	}
	// Each workspace has its own device identity, vault and license directory: nothing is shared but the window's credential.
	a, b := h.slots[MainSlot].d, h.slots[second].d
	if a.mat.Host.DeviceID() == b.mat.Host.DeviceID() {
		t.Fatal("one device identity is used in two workspaces")
	}
	if a.workspaceConfig().DataDir == b.workspaceConfig().DataDir || a.workspaceConfig().SecureStorageID == b.workspaceConfig().SecureStorageID {
		t.Fatal("two workspaces share a directory or a key-storage identity")
	}
	if a.mat.Meta.Authority || b.mat.Meta.Authority {
		t.Fatal("joining made a member a Workspace Host")
	}
	// A license imported for one workspace's slot is not another's.
	if err := writeDaemonFile(filepath.Join(a.o.Config.DataDir, "license.json"), []byte("{}")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(b.o.Config.DataDir, "license.json")); !os.IsNotExist(err) {
		t.Fatal("a license reached another workspace")
	}
	// The workspaces overlap in neither network nor hosting, so both are accepted. Leaving B (here, the step after
	// the workspace has revoked the device) leaves A running, and its slot can then be forgotten.
	plan := leavePlan{RemoveData: false, Archive: "left-workspace-fixture", Revoked: true}
	pb, _ := json.Marshal(plan)
	if err := writeDaemonFile(filepath.Join(b.o.Config.DataDir, "leaving"), pb); err != nil {
		t.Fatal(err)
	}
	if err := b.finishLeave(context.Background()); err != nil {
		t.Fatal(err)
	}
	after := hubList(t, h)
	if !after[0].Enrolled || after[0].Workspace == nil || after[0].Workspace.ID != got[0].Workspace.ID {
		t.Fatalf("leaving one workspace changed the other: %+v", after[0])
	}
	if after[1].Enrolled || after[1].Workspace != nil {
		t.Fatalf("the workspace that was left is still shown: %+v", after[1])
	}
	if w := hubCall(h, "GET", "/w/"+MainSlot+"/api/device/v1/state", ""); w.Code != 200 {
		t.Fatal("the other workspace stopped answering")
	}
	w := hubCall(h, "DELETE", "/api/device/v1/workspaces/"+second, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"archiveKept":true`) {
		t.Fatalf("forgetting the workspace that was left: %d %s", w.Code, w.Body)
	}
	if _, err := os.Stat(filepath.Join(h.slotDir(second), "left-workspace-fixture")); err != nil {
		t.Fatal("the archive the person chose to keep was deleted", err)
	}
	// A restart does not bring the forgotten workspace back, nor lose the archive.
	h2, err := NewHub(h.o)
	if err != nil {
		t.Fatal(err)
	}
	if got := hubList(t, h2); len(got) != 1 || got[0].Slot != MainSlot {
		t.Fatalf("after restart: %+v", got)
	}
	if _, err := os.Stat(filepath.Join(h.slotDir(second), "left-workspace-fixture")); err != nil {
		t.Fatal("the archive disappeared on restart", err)
	}
}

func TestAnOccupiedWorkspaceIsNeverRemovedFromTheList(t *testing.T) {
	hostA, createdA, ncA := newFirstHost(t, "127.0.0.1")
	hostA.serve()
	h := testHub(t)
	addSlot(t, h)
	// Occupy main, then add a second slot and occupy it too.
	joinSlot(t, h, MainSlot, hostA, createdA, "Ada's Mac")
	hostB, createdB := anotherWorkspace(t, ncA.Range)
	second := addSlot(t, h)
	joinSlot(t, h, second, hostB, createdB, "Ada's other Mac")
	if w := hubCall(h, "DELETE", "/api/device/v1/workspaces/"+second, ""); w.Code != 409 {
		t.Fatalf("an enrolled workspace was removed: %d %s", w.Code, w.Body)
	}
	if len(hubList(t, h)) != 2 {
		t.Fatal("an enrolled workspace left the list")
	}
}

func TestTwoWorkspacesMayNotShareAPrivateNetworkRange(t *testing.T) {
	h := testHub(t)
	main := h.slots[MainSlot]
	v, err := main.d.setupVault(main.d.workspaceConfig())
	if err != nil {
		t.Fatal(err)
	}
	occupyAsHost(t, v, "tws_one", "10.231.0.0/16")
	if err := main.d.loadDevice(); err != nil {
		t.Fatal(err)
	}
	second := addSlot(t, h)
	guard := h.slots[second].d.o.NetworkGuard
	if err := guard(netip.MustParsePrefix("10.231.4.0/24")); err == nil {
		t.Fatal("an overlapping private network was accepted")
	}
	if err := guard(netip.MustParsePrefix("10.232.0.0/16")); err != nil {
		t.Fatalf("a separate private network was refused: %v", err)
	}
	if err := h.slots[MainSlot].d.o.NetworkGuard(netip.MustParsePrefix("10.231.0.0/16")); err != nil {
		t.Fatalf("a workspace was refused its own network: %v", err)
	}
}

func TestTheHubServesEveryWorkspaceFromOneLocalAddress(t *testing.T) {
	c := config.Default()
	c.DataDir = t.TempDir()
	addr := fmt.Sprintf("127.0.0.1:%d", freeTCPPort(t))
	h, err := NewHub(DaemonOptions{Config: c, LocalAddr: addr, Log: quiet(), Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(bg)
	done := make(chan error, 1)
	go func() { done <- h.Run(ctx) }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		r, _ := http.NewRequest("GET", "http://"+addr+"/api/device/v1/workspaces", nil)
		r.Header.Set("Authorization", "Bearer "+h.key)
		if res, err := http.DefaultClient.Do(r); err == nil {
			res.Body.Close()
			if res.StatusCode == 200 {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("the hub never answered")
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("the hub did not stop")
	}
	if _, err := NewHub(DaemonOptions{Config: c, LocalAddr: "0.0.0.0:7431"}); err == nil {
		t.Fatal("the hub could be served to other computers")
	}
}
