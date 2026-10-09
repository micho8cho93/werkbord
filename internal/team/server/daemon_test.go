package server

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"devboard/internal/deviceid"
	"devboard/internal/team/config"
	"devboard/internal/team/domain"
	"devboard/internal/team/hostclient"
	"devboard/internal/team/infra/pki"
	"devboard/internal/team/license"
	"devboard/internal/team/service"
)

func daemonCall(d *Daemon, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://127.0.0.1:7431"+path, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+d.key())
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, r)
	return w
}

func testDaemon(t *testing.T) *Daemon {
	t.Helper()
	c := config.Default()
	c.DataDir = t.TempDir()
	d, err := NewDaemon(DaemonOptions{Config: c, Log: quiet(), Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	d.ctx = context.Background()
	return d
}

func TestDeviceAPIRequiresLocalAuthenticationAndSameOrigin(t *testing.T) {
	d := testDaemon(t)
	for _, token := range []string{"", "workspace-token"} {
		r := httptest.NewRequest("POST", "/api/device/v1/settings", strings.NewReader(`{}`))
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		d.Handler().ServeHTTP(w, r)
		if w.Code != 401 {
			t.Fatalf("unauthenticated: %d", w.Code)
		}
	}
	for _, headers := range [][]string{{d.key()}, {"Bearer " + d.key(), "Bearer " + d.key()}} {
		r := httptest.NewRequest("GET", "/api/device/v1/state", nil)
		for _, value := range headers {
			r.Header.Add("Authorization", value)
		}
		w := httptest.NewRecorder()
		d.Handler().ServeHTTP(w, r)
		if w.Code != 401 {
			t.Fatalf("ambiguous authorization accepted: %d", w.Code)
		}
	}
	r := httptest.NewRequest("GET", "http://127.0.0.1:7431/api/device/v1/state", nil)
	r.Header.Set("Authorization", "Bearer "+d.key())
	r.Header.Set("Origin", "https://attacker.example")
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatalf("foreign origin: %d", w.Code)
	}
	w = daemonCall(d, "GET", "/api/device/v1/state", "")
	if w.Code != 200 || strings.Contains(w.Body.String(), d.key()) {
		t.Fatalf("state leaks or is unavailable: %s", w.Body)
	}
	w = daemonCall(d, "PUT", "/api/device/v1/settings", `{"remoteStart":"auto","werkbordBase":"http://192.168.1.1:7420"}`)
	if w.Code != 409 || d.state.Settings().RemoteStart != "ask" {
		t.Fatal("a network controller changed local execution policy")
	}
	w = daemonCall(d, "POST", "/api/device/v1/create", `{"name":"Team","owner":"Ada"}`)
	if w.Code != 409 {
		t.Fatal("a workspace was created without an active license")
	}
	c := d.o.Config
	if _, err := NewDaemon(DaemonOptions{Config: c, LocalAddr: "0.0.0.0:7431"}); err == nil {
		t.Fatal("the device API could be served to other computers")
	}
}

func TestRoutingHostCannotReplaceALocallyTrustedSigningKey(t *testing.T) {
	d := testDaemon(t)
	self, _ := pki.NewHostKeys()
	sender, _ := pki.NewHostKeys()
	rogue, _ := pki.NewHostKeys()
	key := deviceid.EncodePublicKey(sender.PublicKey())
	if err := d.state.NoteDevice(sender.DeviceID(), "My other Mac", key); err != nil {
		t.Fatal(err)
	}
	w := daemonCall(d, "POST", "/api/device/v1/senders/"+sender.DeviceID()+"/trust", `{"publicKey":"`+key+`"}`)
	if w.Code != 204 {
		t.Fatal(w.Body)
	}
	if err := d.state.NoteDevice(sender.DeviceID(), "Same label", deviceid.EncodePublicKey(rogue.PublicKey())); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]domain.Device{{ID: sender.DeviceID(), MemberID: "tmb_me", PublicKey: deviceid.EncodePublicKey(rogue.PublicKey())}})
	}))
	defer srv.Close()
	c, _ := hostclient.New(hostclient.Options{Bases: []string{srv.URL}, Token: "device", Network: netip.MustParsePrefix("10.222.0.0/16")})
	x := workspaceDirectory{d: d, client: c, self: &pki.Material{Host: self, Meta: pki.Meta{WorkspaceID: "tws_one"}}, memberID: "tmb_me"}
	dev, err := x.Device(bg, "tws_one", sender.DeviceID())
	if err != nil || !dev.Revoked || !bytes.Equal(dev.PublicKey, sender.PublicKey()) {
		t.Fatalf("host substituted key: %+v %v", dev, err)
	}
	w = daemonCall(d, "GET", "/api/device/v1/state", "")
	if !strings.Contains(w.Body.String(), `"approved":true`) || !strings.Contains(w.Body.String(), deviceid.Fingerprint(sender.PublicKey())) {
		t.Fatal("trust/identity is not reported honestly")
	}
}

func TestPendingEnrollmentSurvivesRestartAndFinishesWithoutANewInvitation(t *testing.T) {
	h, created, _ := newFirstHost(t, "127.0.0.1")
	h.serve()
	invite := h.invite(created, service.EnrollInviteInput{Label: "Another computer", ForMemberID: created.Owner.ID, RequireApproval: true})
	d := testDaemon(t)
	in, _ := json.Marshal(map[string]string{"link": invite.Link, "name": "Ada", "deviceName": "Office Mac"})
	w := daemonCall(d, "POST", "/api/device/v1/join", string(in))
	if w.Code != 202 {
		t.Fatal(w.Body)
	}
	d.job.Wait()
	v, _ := d.pendingVault()
	raw, err := v.Secret("join")
	if err != nil {
		t.Fatal(err)
	}
	var pending pendingJoin
	_ = json.Unmarshal(raw, &pending)
	keys, _ := pki.ParseHostKeys(pending.Keys)
	if pending.EnrollmentID == "" || pending.Response == nil {
		t.Fatalf("pending enrollment lost: %+v", pending)
	}
	list, err := h.svc.ListEnrollments(bg, h.owner(created), domain.EnrollmentPending)
	if err != nil || len(list) != 1 {
		t.Fatalf("pending: %+v %v", list, err)
	}
	if _, err := h.svc.ApproveEnrollment(bg, h.owner(created), list[0].ID); err != nil {
		t.Fatal(err)
	}
	d2, err := NewDaemon(d.o)
	if err != nil {
		t.Fatal(err)
	}
	if err := d2.resumeJoin(bg); err != nil {
		t.Fatal(err)
	}
	if err := d2.loadDevice(); err != nil {
		t.Fatal(err)
	}
	if d2.mat.Host.DeviceID() != keys.DeviceID() || d2.mat.Meta.Authority {
		t.Fatal("joining changed identity or implicitly made a member a Host")
	}
	if _, err := os.Stat(filepath.Join(d.o.Config.DataDir, "pending-join")); !os.IsNotExist(err) {
		t.Fatal("pending marker not cleared")
	}
	// A crash after the install rename but before cleanup does not issue a second credential or overwrite keys.
	v, _ = d2.pendingVault()
	if err := v.SaveSecret("join", raw); err != nil {
		t.Fatal(err)
	}
	if err := writeDaemonFile(filepath.Join(d.o.Config.DataDir, "pending-join"), []byte("waiting")); err != nil {
		t.Fatal(err)
	}
	if err := d2.resumeJoin(bg); err != nil {
		t.Fatal(err)
	}
	if d2.mat.Host.DeviceID() != keys.DeviceID() {
		t.Fatal("recovery replaced the device identity")
	}
}

func TestCreateTeamRunsInDaemonAndTheLastHostCannotBeRemoved(t *testing.T) {
	cfg := storageCfg(t)
	cfg.RunNode = false // Tests have no right to install a network interface or OS service.
	cfg.Addr = fmt.Sprintf("127.0.0.1:%d", freeTCPPort(t))
	cfg.BootstrapAddr = fmt.Sprintf("127.0.0.1:%d", freeTCPPort(t))
	pub, key, _ := ed25519.GenerateKey(rand.Reader)
	d, err := NewDaemon(DaemonOptions{Config: cfg, LocalAddr: fmt.Sprintf("127.0.0.1:%d", freeTCPPort(t)), LicenseKey: pub, Log: quiet(), Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	claims, _ := json.Marshal(license.Claims{Product: "werkbord-team", ID: "lic_test", Customer: "Test", Seats: 5, IssuedAt: time.Now().Add(-time.Hour), ExpiresAt: time.Now().Add(time.Hour)})
	doc := `{"claims":` + string(claims) + `,"signature":"` + base64.RawURLEncoding.EncodeToString(ed25519.Sign(key, license.SigningBytes(claims))) + `"}`
	input, _ := json.Marshal(map[string]string{"document": doc})
	if w := daemonCall(d, "POST", "/api/device/v1/license", string(input)); w.Code != 200 {
		t.Fatal(w.Body)
	}
	run := func(d *Daemon) (context.CancelFunc, <-chan error) {
		ctx, cancel := context.WithCancel(bg)
		done := make(chan error, 1)
		go func() { done <- d.Run(ctx) }()
		t.Cleanup(func() { cancel() })
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			d.mu.RLock()
			ready := d.ctx != nil
			d.mu.RUnlock()
			if ready {
				return cancel, done
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("daemon did not start")
		return cancel, done
	}
	cancel, done := run(d)
	requestCtx, closeWindow := context.WithCancel(bg)
	r := httptest.NewRequest("POST", "/api/device/v1/create", strings.NewReader(`{"name":"Acme","owner":"Ada"}`)).WithContext(requestCtx)
	r.Header.Set("Authorization", "Bearer "+d.key())
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, r)
	closeWindow()
	if w.Code != 202 {
		t.Fatal(w.Body)
	}
	deadline := time.Now().Add(35 * time.Second)
	for time.Now().Before(deadline) {
		d.mu.RLock()
		ready := d.mat != nil && !d.lastSync.IsZero()
		problem := d.lastError
		d.mu.RUnlock()
		if ready {
			break
		}
		if time.Now().After(deadline.Add(-time.Second)) {
			t.Fatalf("workspace never became ready: %s", problem)
		}
		time.Sleep(30 * time.Millisecond)
	}
	w = daemonCall(d, "GET", "/api/team/v1/me", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"role":"owner"`) {
		t.Fatal(w.Body)
	}
	token, _ := d.vault.DeviceToken()
	w = daemonCall(d, "GET", "/api/device/v1/state", "")
	if strings.Contains(w.Body.String(), token) {
		t.Fatal("workspace credential reached the GUI")
	}
	w = daemonCall(d, "GET", "/api/device/v1/removal", "")
	if !strings.Contains(w.Body.String(), `"canRemoveData":false`) {
		t.Fatal("the last workspace copy could be deleted")
	}
	w = daemonCall(d, "POST", "/api/device/v1/leave", `{"confirm":"leave workspace","removeData":true}`)
	if w.Code != 409 {
		t.Fatalf("last host left: %d %s", w.Code, w.Body)
	}
	id := d.mat.Host.DeviceID()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("daemon did not shut down")
	}
	d2, err := NewDaemon(d.o)
	if err != nil {
		t.Fatal(err)
	}
	if err := d2.loadDevice(); err != nil || d2.mat.Host.DeviceID() != id {
		t.Fatal("restart lost workspace/device identity", err)
	}
	if _, err := os.Stat(filepath.Join(d2.workspaceConfig().PKIDir(), "workspace.json")); err != nil {
		t.Fatal("stopping service removed workspace data", err)
	}
}

func TestWorkspaceDepartureRecoversAfterAnArchiveMove(t *testing.T) {
	d := testDaemon(t)
	src := d.workspaceConfig().DataDir
	if err := os.MkdirAll(src, 0700); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(src, "retained"), []byte("copy"), 0600)
	plan := leavePlan{Archive: "left-workspace-fixture", Revoked: true}
	b, _ := json.Marshal(plan)
	_ = writeDaemonFile(filepath.Join(d.o.Config.DataDir, "leaving"), b)
	if err := os.Rename(src, filepath.Join(d.o.Config.DataDir, plan.Archive)); err != nil {
		t.Fatal(err)
	}
	if err := d.finishLeave(bg); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(d.o.Config.DataDir, plan.Archive, "retained"))
	if err != nil || string(b) != "copy" {
		t.Fatal("archive was not retained", err)
	}
	if _, err := os.Stat(filepath.Join(d.o.Config.DataDir, "leaving")); !os.IsNotExist(err) {
		t.Fatal("departure not completed")
	}
}

// This fixture is invoked only by scripts/test-team-desktop-browser.cjs. It runs real APIs and a pinned database in
// disposable directories, but installs no service or network interface. The second device uses the first Host's
// literal loopback API instead of a privileged tunnel in this test only. No test issuer or bypass reaches a release.
func TestTeamDesktopBrowserFixture(t *testing.T) {
	dir := os.Getenv("WERKBORD_TEAM_BROWSER_FIXTURE")
	if dir == "" {
		t.Skip("browser harness only")
	}
	pub, key, _ := ed25519.GenerateKey(rand.Reader)
	claims, _ := json.MarshalIndent(license.Claims{Product: "werkbord-team", ID: "lic_fixture", Customer: "Browser test", Seats: 10, IssuedAt: time.Now().Add(-time.Hour), ExpiresAt: time.Now().Add(time.Hour)}, "", "  ")
	doc := []byte(`{"claims":` + string(claims) + `,"signature":"` + base64.RawURLEncoding.EncodeToString(ed25519.Sign(key, license.SigningBytes(claims))) + `"}`)
	if err := os.WriteFile(filepath.Join(dir, "license.json"), doc, 0600); err != nil {
		t.Fatal(err)
	}
	makeDaemon := func() *Daemon {
		cfg := storageCfg(t)
		cfg.RunNode = false
		cfg.Addr = fmt.Sprintf("127.0.0.1:%d", freeTCPPort(t))
		cfg.BootstrapAddr = fmt.Sprintf("127.0.0.1:%d", freeTCPPort(t))
		d, err := NewDaemon(DaemonOptions{Config: cfg, LocalAddr: fmt.Sprintf("127.0.0.1:%d", freeTCPPort(t)), LicenseKey: pub, Log: quiet(), Version: "browser-fixture", RunnerConfigPath: filepath.Join(dir, "runner-config.json")})
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	first, second := makeDaemon(), makeDaemon()
	// Map the private tunnel's TCP destination to the real Host API. URL validation,
	// workspace authentication, API responses, database and signatures remain real.
	second.hostDial = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, first.o.Config.Addr)
	}
	ctx, cancel := context.WithCancel(bg)
	defer cancel()
	done := make(chan error, 2)
	go func() { done <- first.Run(ctx) }()
	go func() { done <- second.Run(ctx) }()
	meta, _ := json.Marshal(map[string]string{"first": "http://" + first.o.LocalAddr, "second": "http://" + second.o.LocalAddr, "firstKey": first.key(), "secondKey": second.key()})
	if err := os.WriteFile(filepath.Join(dir, "ready.json"), meta, 0600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Minute)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(filepath.Join(dir, "done")); err == nil {
			cancel()
			for i := 0; i < 2; i++ {
				if err := <-done; err != nil {
					t.Fatal(err)
				}
			}
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("browser harness did not complete")
}

func TestDemotionRetainsDataAndCanResumeAfterMetadataChanges(t *testing.T) {
	d := testDaemon(t)
	cfg := d.workspaceConfig()
	v, err := d.setupVault(cfg)
	if err != nil {
		t.Fatal(err)
	}
	mat, err := v.Create("tws_demote", "Demotion", netip.MustParsePrefix("10.221.0.0/16"), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	_ = v.SaveDeviceToken("device-credential")
	_ = v.SaveSecret("member", []byte("tmb_owner"))
	_ = v.SaveJoinInfo(pki.JoinInfo{APIAddrs: []string{"10.221.0.2"}, APIPort: 7430})
	_ = os.MkdirAll(cfg.StorageDir(), 0700)
	_ = os.WriteFile(filepath.Join(cfg.StorageDir(), "retained.db"), []byte("safe copy"), 0600)
	_ = writeMarker(cfg, storageMarker{Kind: config.StorageReplicated, Role: RoleRemoved})
	_ = writeDaemonFile(filepath.Join(cfg.DataDir, "demoting"), []byte("retired-storage-fixture"))
	// A crash after role metadata changes, but before the durable local transition finishes, can still complete.
	if err := v.Demote(time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := d.finishDemotion(); err != nil {
		t.Fatal(err)
	}
	if d.mat.Meta.Authority || d.mat.Host.DeviceID() != mat.Host.DeviceID() {
		t.Fatal("demotion lost identity or kept authority")
	}
	if _, err := os.Stat(cfg.StorageMarkerPath()); !os.IsNotExist(err) {
		t.Fatal("removed membership marker prevents re-promotion")
	}
	b, err := os.ReadFile(filepath.Join(cfg.DataDir, "retired-storage-fixture", "retained.db"))
	if err != nil || string(b) != "safe copy" {
		t.Fatal("old database not preserved", err)
	}
}

// A genuine multi-workspace Hub, plus an independent second customer's host.
// The browser test substitutes only private-network TCP routing and native dialogs.
func TestUnifiedDesktopBrowserFixture(t *testing.T) {
	dir := os.Getenv("WERKBORD_UNIFIED_BROWSER_FIXTURE")
	if dir == "" {
		t.Skip("browser harness only")
	}
	pub, key, _ := ed25519.GenerateKey(rand.Reader)
	claims, _ := json.Marshal(license.Claims{Product: "werkbord-team", ID: "lic_fixture", Customer: "Shell fixture", Seats: 10, IssuedAt: time.Now().Add(-time.Hour), ExpiresAt: time.Now().Add(time.Hour)})
	doc := []byte(`{"claims":` + string(claims) + `,"signature":"` + base64.RawURLEncoding.EncodeToString(ed25519.Sign(key, license.SigningBytes(claims))) + `"}`)
	_ = os.WriteFile(filepath.Join(dir, "license.json"), doc, 0600)
	options := func() DaemonOptions {
		cfg := storageCfg(t)
		cfg.RunNode = false
		cfg.Addr = fmt.Sprintf("127.0.0.1:%d", freeTCPPort(t))
		cfg.BootstrapAddr = fmt.Sprintf("127.0.0.1:%d", freeTCPPort(t))
		cfg.EmbedOrigins = []string{os.Getenv("WERKBORD_BROWSER_SHELL_ORIGIN")}
		return DaemonOptions{Config: cfg, LocalAddr: fmt.Sprintf("127.0.0.1:%d", freeTCPPort(t)), LicenseKey: pub, Log: quiet(), Version: "browser-fixture"}
	}
	hub, err := NewHub(options())
	if err != nil {
		t.Fatal(err)
	}
	other, err := NewDaemon(options())
	if err != nil {
		t.Fatal(err)
	}
	hub.hostDial = func(ctx context.Context, network, address string) (net.Conn, error) {
		// The second workspace has a disjoint overlay range.
		other.mu.RLock()
		var prefix netip.Prefix
		if other.mat != nil {
			prefix, _ = netip.ParsePrefix(other.mat.Meta.NetworkPrefix)
		}
		other.mu.RUnlock()
		host, _, _ := net.SplitHostPort(address)
		ip, _ := netip.ParseAddr(host)
		if prefix.IsValid() && prefix.Contains(ip) {
			address = other.o.Config.Addr
		} else {
			address = hub.o.Config.Addr
		}
		return (&net.Dialer{}).DialContext(ctx, network, address)
	}
	hub.slots[MainSlot].d.hostDial = hub.hostDial
	ctx, cancel := context.WithCancel(bg)
	defer cancel()
	done := make(chan error, 2)
	go func() { done <- hub.Run(ctx) }()
	go func() { done <- other.Run(ctx) }()
	meta, _ := json.Marshal(map[string]string{"first": "http://" + hub.o.LocalAddr, "firstKey": hub.key, "other": "http://" + other.o.LocalAddr, "otherKey": other.key()})
	_ = os.WriteFile(filepath.Join(dir, "team-ready.json"), meta, 0600)
	deadline := time.Now().Add(10 * time.Minute)
	if m, err := time.ParseDuration(os.Getenv("WERKBORD_FIXTURE_MINUTES") + "m"); err == nil && m > 0 {
		deadline = time.Now().Add(m)
	}
	for time.Now().Before(deadline) {
		if _, err := os.Stat(filepath.Join(dir, "done")); err == nil {
			cancel()
			for i := 0; i < 2; i++ {
				if err := <-done; err != nil {
					t.Fatal(err)
				}
			}
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("unified browser harness did not finish")
}
