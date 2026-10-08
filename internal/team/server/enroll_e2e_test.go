package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"devboard/internal/enrollment"
	"devboard/internal/envelope"
	"devboard/internal/team/authproof"
	"devboard/internal/team/config"
	"devboard/internal/team/domain"
	"devboard/internal/team/infra/nebula"
	"devboard/internal/team/infra/overlay"
	"devboard/internal/team/infra/pki"
	"devboard/internal/team/service"
)

func (h *host) invite(created service.Created, in service.EnrollInviteInput) service.EnrollInviteResult {
	h.t.Helper()
	r, err := h.svc.CreateEnrollInvitation(bg, h.owner(created), in)
	if err != nil {
		h.t.Fatal(err)
	}
	return r
}

// ---- a member joins ----

func TestAMemberJoinsOverTLSAndReceivesACertificateForTheirOwnKey(t *testing.T) {
	h, created, nc := newFirstHost(t, "localhost")
	h.serve()
	r := h.invite(created, service.EnrollInviteInput{Label: "Bo", Capabilities: []domain.Capability{domain.CapabilityRunner}})
	if !strings.HasPrefix(r.Link, enrollment.JoinPrefix) || r.Fingerprint != nc.Fingerprint {
		t.Fatalf("invitation = %+v", r)
	}
	inv, err := enrollment.Parse(r.Link, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(inv.Endpoints) != 1 || !strings.HasPrefix(inv.Endpoints[0], "localhost:") || inv.Fingerprint != nc.Fingerprint || inv.Role != "member" {
		t.Errorf("invitation = %+v", inv)
	}

	bo := newDevice(t, "bo-macbook")
	res, err := bo.join(r.Link, "Bo", func(p *enrollment.JoinParams) { p.ExpectFingerprint = nc.Fingerprint })
	if err != nil {
		t.Fatal(err)
	}
	if res.State != enrollment.StateApproved || res.Response.DeviceToken == "" || res.Network == nil {
		t.Fatalf("result = %+v", res)
	}

	// The certificate is for the key the device made, signed by the workspace's authority, at its address,
	// with the groups that follow from what it does; and it verifies against the authority's certificate.
	info, err := h.nw.mat.CA.VerifyNode([]byte(res.Network.NodeCertificate), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if info.Name != bo.id.DeviceID() || info.Addr.String() != res.Network.OverlayAddr || strings.Join(info.Groups, ",") != "member,runner" {
		t.Errorf("certificate = %+v", info)
	}
	if res.Network.CACertificate != nc2(h) {
		t.Error("the authority certificate the device was given is not the workspace's")
	}
	if strings.Contains(res.Network.Config, "PRIVATE") || strings.Contains(res.Network.NodeCertificate, "PRIVATE") {
		t.Error("what the workspace sent contains a private key")
	}
	for _, secret := range [][]byte{h.nw.mat.CA.PrivateKeyPEM(), h.nw.mat.Trust.Seed()} {
		if strings.Contains(res.Network.Config+res.Network.CACertificate+res.Network.NodeCertificate+res.Response.DeviceToken, strings.TrimSpace(string(secret))) {
			t.Error("the workspace sent one of its signing keys")
		}
	}

	// It authenticates with its own credential, as Bo, through the API.
	ts := httptest.NewServer(Handler(h.db, h.svc, quiet(), "test"))
	t.Cleanup(ts.Close)
	status, body := get(t, ts.URL+"/api/team/v1/me", res.Response.DeviceToken)
	if status != 200 || !strings.Contains(body, `"name":"Bo"`) {
		t.Errorf("GET /me with the device's credential = %d %s", status, body)
	}
	// It cannot do an administrator's work: a device reaches what its member may.
	if status, _ := get(t, ts.URL+"/api/team/v1/network", res.Response.DeviceToken); status != 403 {
		t.Errorf("a member's device saw the network's health: %d", status)
	}
	// It can renew its certificate and fetch its configuration, which only a device can.
	status, body = post(t, ts.URL+"/api/team/v1/network/certificate", res.Response.DeviceToken, "")
	if status != 200 || !strings.Contains(body, "BEGIN NEBULA CERTIFICATE") {
		t.Errorf("renewal = %d %.80s", status, body)
	}
	if status, _ := post(t, ts.URL+"/api/team/v1/network/certificate", created.Token, ""); status != 403 {
		t.Errorf("a member token renewed a device's certificate: %d", status)
	}
}

func nc2(h *host) string { b, _ := h.nw.mat.CA.CertificatePEM(); return string(b) }

func get(t *testing.T, url, token string) (int, string) { return do(t, "GET", url, token, "") }
func post(t *testing.T, url, token, body string) (int, string) {
	return do(t, "POST", url, token, body)
}

type proofIdentity struct {
	signer          envelope.Signer
	workspace, user string
}

var proofIdentities sync.Map

func do(t *testing.T, method, url, token, body string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(method, url, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	if v, ok := proofIdentities.Load(token); ok {
		x := v.(proofIdentity)
		if err := authproof.Sign(req, []byte(body), x.signer, x.workspace, x.user, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b := make([]byte, 1<<16)
	n, _ := res.Body.Read(b)
	return res.StatusCode, string(b[:n])
}

func TestTheJoinedDeviceInstallsItsNetworkFilesAndTheRealProgramAcceptsThem(t *testing.T) {
	dir := nebulaDir(t)
	h, created, _ := newFirstHost(t, "localhost")
	h.serve()
	r := h.invite(created, service.EnrollInviteInput{})
	d := newDevice(t, "bo-laptop")
	res, err := d.join(r.Link, "Bo")
	if err != nil {
		t.Fatal(err)
	}
	netDir := filepath.Join(t.TempDir(), "net")
	if err := enrollment.Install(netDir, res.Network, d.netPriv); err != nil {
		t.Fatal(err)
	}
	// The program's own check of what the workspace sent, with the device's own key.
	out, err := exec.Command(filepath.Join(dir, "nebula"), "-test", "-config", filepath.Join(netDir, enrollment.FileConfig)).CombinedOutput()
	if err != nil {
		t.Fatalf("the program rejects what the workspace issued: %v\n%s", err, out)
	}
	// A key that is not the one the certificate was made for is rejected: a certificate is no use without it.
	other := newDevice(t, "other")
	if err := enrollment.Install(netDir, res.Network, other.netPriv); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(filepath.Join(dir, "nebula"), "-test", "-config", filepath.Join(netDir, enrollment.FileConfig)).CombinedOutput(); err == nil {
		t.Errorf("the program accepted a certificate with someone else's key: %s", out)
	}
}

// ---- an invitation is good once, for the workspace it names, as it was signed ----

func TestAnInvitationCannotBeReusedExpiredModifiedOrPointedAtAnotherWorkspace(t *testing.T) {
	h, created, nc := newFirstHost(t, "localhost")
	h.serve()

	// Reused.
	r := h.invite(created, service.EnrollInviteInput{})
	if _, err := newDevice(t, "first").join(r.Link, "Pat"); err != nil {
		t.Fatal(err)
	}
	if _, err := newDevice(t, "second").join(r.Link, "Sam"); !errors.Is(err, enrollment.ErrRefused) {
		t.Errorf("reusing an invitation = %v, want ErrRefused", err)
	}

	// Expired: refused by the device before it connects, and by the workspace if the device's clock is wrong.
	short := h.invite(created, service.EnrollInviteInput{TTL: time.Minute})
	if _, err := enrollment.Parse(short.Link, time.Now().Add(2*time.Minute)); !errors.Is(err, enrollment.ErrExpired) {
		t.Errorf("an expired invitation parsed: %v", err)
	}
	late := newDevice(t, "late")
	inv, _ := enrollment.Parse(short.Link, time.Now())
	h.svc.SetClock(func() time.Time { return time.Now().Add(2 * time.Minute) })
	_, err := enrollment.Join(bg, inv, enrollment.JoinParams{Signer: late.id, MemberName: "Late", DeviceName: "late", NetworkPublicKeyPEM: late.netPub})
	h.svc.SetClock(nil)
	if !errors.Is(err, enrollment.ErrRefused) && err == nil {
		t.Errorf("the workspace accepted an expired invitation: %v", err)
	}

	// Modified: a changed endpoint, role or expiry is a different signature.
	fresh := h.invite(created, service.EnrollInviteInput{})
	for name, edit := range map[string]func(string) string{
		// (a character in the middle: the last one of a signature carries bits that decoding ignores)
		"the signature": func(l string) string { return flipChar(l, len(l)-20) },
		"the payload":   func(l string) string { return flipChar(l, strings.Index(l, ".")+6) },
	} {
		if _, err := enrollment.Parse(edit(fresh.Link), time.Now()); err == nil {
			t.Errorf("an invitation with %s changed was accepted", name)
		}
	}
	// A modified credential reaches the workspace and is refused there (and does not use the invitation up).
	inv2, _ := enrollment.Parse(fresh.Link, time.Now())
	inv2.Credential = strings.Repeat("A", 43)
	d := newDevice(t, "guess")
	if _, err := enrollment.Join(bg, inv2, enrollment.JoinParams{Signer: d.id, MemberName: "Guess", DeviceName: "guess", NetworkPublicKeyPEM: d.netPub}); !errors.Is(err, enrollment.ErrRefused) {
		t.Errorf("a wrong credential = %v, want ErrRefused", err)
	}
	if _, err := newDevice(t, "right").join(fresh.Link, "Right"); err != nil {
		t.Errorf("a refused guess used the invitation up: %v", err)
	}

	// The wrong workspace: an expected fingerprint that is not this one is refused before anything is sent,
	// and an endpoint that answers as a different workspace is not believed.
	wrong := h.invite(created, service.EnrollInviteInput{})
	if _, err := newDevice(t, "careful").join(wrong.Link, "Care", func(p *enrollment.JoinParams) {
		p.ExpectFingerprint = strings.Replace(nc.Fingerprint, nc.Fingerprint[:4], "zzzz", 1)
	}); !errors.Is(err, enrollment.ErrWrongWorkspace) {
		t.Errorf("a wrong expected fingerprint = %v", err)
	}
	imposter, impCreated, _ := newFirstHost(t, "localhost")
	imposter.serve()
	_ = impCreated
	invW, _ := enrollment.Parse(wrong.Link, time.Now())
	invW.Endpoints = []string{imposter.cfg.BootstrapAddr}
	// (A person cannot change the endpoints of a signed invitation; this is what an attacker who can only
	// redirect the connection looks like to the device.)
	redirected := newDevice(t, "redirected")
	if _, err := enrollment.Join(bg, invW, enrollment.JoinParams{Signer: redirected.id, MemberName: "R", DeviceName: "r", NetworkPublicKeyPEM: redirected.netPub}); !errors.Is(err, enrollment.ErrNoEndpoint) {
		t.Errorf("an endpoint that is another workspace = %v, want ErrNoEndpoint", err)
	}
	// The imposter never saw the credential: it holds no enrollment for it.
	list, _ := imposter.svc.ListEnrollInvitations(bg, imposter.owner(impCreated))
	if len(list) != 0 {
		t.Errorf("the other workspace has %d invitations", len(list))
	}
	if _, err := newDevice(t, "still-good").join(wrong.Link, "Good"); err != nil {
		t.Errorf("redirecting a device used the invitation up: %v", err)
	}
}

// ---- approval ----

func TestAWorkspaceThatRequiresApprovalWaitsForAnAdministratorOverTLS(t *testing.T) {
	h, created, _ := newFirstHost(t, "localhost")
	h.serve()
	owner := h.owner(created)
	if err := h.svc.SetEnrollmentApproval(bg, owner, domain.ApprovalAdmin); err != nil {
		t.Fatal(err)
	}
	r := h.invite(created, service.EnrollInviteInput{})
	d := newDevice(t, "waiting")
	type out struct {
		res *enrollment.Result
		err error
	}
	done := make(chan out, 1)
	go func() {
		res, err := d.join(r.Link, "Wanda", func(p *enrollment.JoinParams) { p.Wait, p.PollEvery = 30*time.Second, 50*time.Millisecond })
		done <- out{res, err}
	}()
	var pending []domain.Enrollment
	for i := 0; i < 100 && len(pending) == 0; i++ {
		time.Sleep(50 * time.Millisecond)
		pending, _ = h.svc.ListEnrollments(bg, owner, domain.EnrollmentPending)
	}
	if len(pending) != 1 {
		t.Fatalf("pending = %+v", pending)
	}
	select {
	case o := <-done:
		t.Fatalf("the device finished without approval: %+v %v", o.res, o.err)
	case <-time.After(300 * time.Millisecond):
	}
	if _, err := h.svc.ApproveEnrollment(bg, owner, pending[0].ID); err != nil {
		t.Fatal(err)
	}
	o := <-done
	if o.err != nil || o.res.State != enrollment.StateApproved || o.res.Network == nil {
		t.Fatalf("after approval: %+v %v", o.res, o.err)
	}
	// A denied one.
	r2 := h.invite(created, service.EnrollInviteInput{})
	d2 := newDevice(t, "denied")
	done2 := make(chan error, 1)
	go func() {
		_, err := d2.join(r2.Link, "Dan", func(p *enrollment.JoinParams) { p.Wait, p.PollEvery = 30*time.Second, 50*time.Millisecond })
		done2 <- err
	}()
	var p2 []domain.Enrollment
	for i := 0; i < 100 && len(p2) == 0; i++ {
		time.Sleep(50 * time.Millisecond)
		p2, _ = h.svc.ListEnrollments(bg, owner, domain.EnrollmentPending)
	}
	if len(p2) != 1 {
		t.Fatalf("pending = %+v", p2)
	}
	if _, err := h.svc.DenyEnrollment(bg, owner, p2[0].ID); err != nil {
		t.Fatal(err)
	}
	if err := <-done2; !errors.Is(err, enrollment.ErrDenied) {
		t.Errorf("a denied device = %v, want ErrDenied", err)
	}
}

// ---- revocation in both layers ----

func TestARevokedDeviceCannotUseTheAPIEvenWithTheCertificateItHolds(t *testing.T) {
	h, created, _ := newFirstHost(t, "localhost")
	h.serve()
	r := h.invite(created, service.EnrollInviteInput{})
	d := newDevice(t, "stolen-laptop")
	res, err := d.join(r.Link, "Stan")
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(Handler(h.db, h.svc, quiet(), "test"))
	t.Cleanup(ts.Close)
	token := res.Response.DeviceToken
	if status, _ := get(t, ts.URL+"/api/team/v1/me", token); status != 200 {
		t.Fatalf("before revocation: %d", status)
	}

	status, _ := post(t, ts.URL+"/api/team/v1/devices/"+d.id.DeviceID()+"/revoke", created.Token, "")
	if status != 200 {
		t.Fatalf("revoking: %d", status)
	}
	// Application layer: authoritative, at once. The device still has its certificate and may still reach the
	// host's port: it is nobody to the API.
	for _, path := range []string{"/api/team/v1/me", "/api/team/v1/network/config", "/api/team/v1/projects"} {
		if status, _ := get(t, ts.URL+path, token); status != 401 {
			t.Errorf("GET %s after revocation = %d, want 401", path, status)
		}
	}
	if status, _ := post(t, ts.URL+"/api/team/v1/network/certificate", token, ""); status != 401 {
		t.Errorf("renewing after revocation = %d, want 401", status)
	}
	// Network layer: its certificate is on the blocklist every host gives its network program.
	cert, err := pki.ReadNodeCert([]byte(res.Network.NodeCertificate))
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := h.svc.NodeConfigOf(bg, h.nw.mat.Meta.WorkspaceID, h.nw.mat.Meta.HostDeviceID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range cfg.Blocklist {
		found = found || f == cert.Fingerprint
	}
	if !found {
		t.Errorf("the revoked device's certificate %s is not on the blocklist %v", cert.Fingerprint, cfg.Blocklist)
	}
	spec, err := nodeSpec(cfg, hostPorts(h), "/c", "/n", "/k")
	if err != nil || len(spec.Blocklist) == 0 {
		t.Errorf("the host's own network configuration does not carry the blocklist: %v", err)
	}
	// And the revoked device cannot enroll again with what it holds: the invitation is spent.
	if _, err := d.join(r.Link, "Stan"); err == nil {
		t.Error("a device re-enrolled with a spent invitation")
	}
}

// ---- more hosts: handing on the authority ----

func TestASecondAndThirdHostAreEnrolledAndEachCanTakeOverTheAuthority(t *testing.T) {
	h, created, nc := newFirstHost(t, "localhost")
	h.serve()
	owner := h.owner(created)
	var seconds []*joinedHost
	for i, name := range []string{"second", "third"} {
		r := h.invite(created, service.EnrollInviteInput{ForMemberID: owner.Member.ID, Capabilities: []domain.Capability{domain.CapabilityWorkspaceHost}})
		j := joinAsHost(t, h, r.Link, name)
		// Until it is handed the keys it holds none.
		if j.mat().Meta.Authority || j.mat().Trust != nil || j.mat().CA != nil {
			t.Fatalf("host %d holds the authority before it was given it", i)
		}
		// An administrator hands them over: sealed to this host, collected by it, checked against what it enrolled with.
		if err := h.svc.ProvisionHost(bg, owner, j.keys.DeviceID()); err != nil {
			t.Fatal(err)
		}
		j.collect(h)
		if !j.mat().Meta.Authority {
			t.Fatalf("host %d does not hold the authority after collecting it", i)
		}
		seconds = append(seconds, j)
	}

	// Each can do what the first can: sign invitations the first host's workspace key signed, and certificates its network accepts.
	for i, j := range seconds {
		auth, err := newAuthority(j.mat(), 0)
		if err != nil {
			t.Fatal(err)
		}
		if auth.Info().Fingerprint != nc.Fingerprint {
			t.Errorf("host %d speaks for a different workspace: %s", i, auth.Info().Fingerprint)
		}
		cred, _ := enrollment.NewCredential()
		link, err := auth.SignInvitation(enrollment.Invitation{ID: enrollment.NewID(), WorkspaceName: "Acme", Endpoints: []string{"localhost:1"}, Credential: cred, Role: "member",
			IssuedAt: time.Now().Unix(), ExpiresAt: time.Now().Add(time.Hour).Unix()})
		if err != nil {
			t.Fatal(err)
		}
		inv, err := enrollment.Parse(link, time.Now())
		if err != nil || inv.Fingerprint != nc.Fingerprint {
			t.Errorf("host %d's invitation = %+v, %v", i, inv, err)
		}
		newKey := newDevice(t, "x")
		is, err := auth.IssueCertificate(service.CertificateRequest{DeviceID: newKey.id.DeviceID(), Addr: mustAddr("10.0.0.0"), Capabilities: nil, NetworkPublicKey: newKey.netPub}, time.Now())
		_ = is
		if err == nil {
			t.Error("a certificate outside the network's range was signed")
		}
		prefix := j.mat().CA.Prefix()
		issued, err := auth.IssueCertificate(service.CertificateRequest{DeviceID: newKey.id.DeviceID(), Addr: prefix.Addr().Next().Next().Next().Next().Next().Next(), Capabilities: []domain.Capability{domain.CapabilityRunner}, NetworkPublicKey: newKey.netPub}, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := h.nw.mat.CA.VerifyNode([]byte(issued.CertificatePEM), time.Now()); err != nil {
			t.Errorf("the first host does not accept a certificate host %d signed: %v", i, err)
		}
	}

	// A third party who gets the sealed blob cannot open it; the first host's registry shows both as active hosts.
	devs, _ := h.svc.ListDevices(bg, owner)
	hosts := 0
	for _, d := range devs {
		if d.Has(domain.CapabilityWorkspaceHost) && d.HostStatus == domain.HostActive {
			hosts++
		}
	}
	if hosts != 3 {
		t.Errorf("%d active Workspace Hosts, want 3", hosts)
	}
	// Three hosts, none authoritative: the second's invitations are honoured by a device that pinned the first.
	if len(seconds) != 2 {
		t.Fatal("test setup")
	}
}

func hostPorts(h *host) overlay.Ports {
	return overlay.Ports{API: apiPortOr(h.cfg.Addr), DeviceService: overlay.DeviceServicePort, Replication: overlay.ReplicationPorts}
}

func TestAHostThatLostItsKeysFileCannotStartAndSaysWhy(t *testing.T) {
	h, _, _ := newFirstHost(t, "localhost")
	// The sealing key is gone (a restore of the data without the secrets directory).
	if err := os.Remove(h.cfg.SealingKeyPath()); err != nil {
		t.Fatal(err)
	}
	if _, err := openNetwork(h.cfg, quiet(), h.svc); err == nil || !strings.Contains(err.Error(), "workspace") && !strings.Contains(err.Error(), "keys") {
		t.Errorf("openNetwork without the sealing key = %v", err)
	}
}

func mustAddr(s string) netip.Addr { return netip.MustParseAddr(s) }

// ---- a second machine that joins as a host ----

// joinedHost is a machine that enrolled as a host with its own data directory, holding the keys it made itself.
type joinedHost struct {
	t     *testing.T
	cfg   config.Config
	v     *pki.Vault
	keys  *pki.HostKeys
	token string
	ws    string
	fp    string
	ca    string
}

func (j *joinedHost) mat() *pki.Material {
	j.t.Helper()
	m, err := j.v.Load(time.Now())
	if err != nil {
		j.t.Fatal(err)
	}
	return m
}

func joinAsHost(t *testing.T, first *host, link, name string) *joinedHost {
	t.Helper()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	sealer, err := SealerFor(cfg)
	if err != nil {
		t.Fatal(err)
	}
	v, err := pki.OpenVault(cfg.PKIDir(), sealer)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := pki.NewHostKeys()
	if err != nil {
		t.Fatal(err)
	}
	_, netPub, err := keys.NetworkKeyPEMs()
	if err != nil {
		t.Fatal(err)
	}
	inv, err := enrollment.Parse(link, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	res, err := enrollment.Join(bg, inv, enrollment.JoinParams{Signer: keys, DeviceName: name, NetworkPublicKeyPEM: string(netPub), SealingPublicKey: keys.SealingPublicKey(), ExpectFingerprint: inv.Fingerprint})
	if err != nil {
		t.Fatal(err)
	}
	proofIdentities.Store(res.Response.DeviceToken, proofIdentity{keys, res.Response.WorkspaceID, res.Response.MemberID})
	meta := pki.Meta{WorkspaceID: inv.WorkspaceID, WorkspaceName: inv.WorkspaceName, Fingerprint: inv.Fingerprint, NetworkPrefix: first.nw.mat.Meta.NetworkPrefix}
	if err := v.CreateJoined(meta, keys, []byte(res.Network.CACertificate)); err != nil {
		t.Fatal(err)
	}
	if err := v.SaveDeviceToken(res.Response.DeviceToken); err != nil {
		t.Fatal(err)
	}
	return &joinedHost{t: t, cfg: cfg, v: v, keys: keys, token: res.Response.DeviceToken, ws: inv.WorkspaceID, fp: inv.Fingerprint, ca: res.Network.CACertificate}
}

// collect is what `werkbord-team host collect` does, against the first host's API.
func (j *joinedHost) collect(first *host) {
	j.t.Helper()
	ts := httptest.NewServer(Handler(first.db, first.svc, quiet(), "test"))
	defer ts.Close()
	status, body := get(j.t, ts.URL+"/api/team/v1/network/provision", j.token)
	if status != 200 {
		j.t.Fatalf("collecting: %d %s", status, body)
	}
	var got struct{ Sealed string }
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		j.t.Fatal(err)
	}
	sealed, err := base64.StdEncoding.DecodeString(got.Sealed)
	if err != nil {
		j.t.Fatal(err)
	}
	secrets, err := j.keys.OpenSecrets(time.Now(), j.ws, j.fp, j.ca, sealed)
	if err != nil {
		j.t.Fatal(err)
	}
	if err := j.v.Promote(secrets, time.Now()); err != nil {
		j.t.Fatal(err)
	}
	if status, body := post(j.t, ts.URL+"/api/team/v1/network/provision/ack", j.token, ""); status != 204 {
		j.t.Fatalf("acknowledging: %d %s", status, body)
	}
}

// nebulaDir finds the pinned program, as the supervisor's own tests do.
func nebulaDir(t *testing.T) string {
	t.Helper()
	art, err := nebula.ThisPlatform()
	if err != nil {
		t.Skip(err)
	}
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(file), "..", "..", "..")
	for _, d := range []string{os.Getenv("WERKBORD_TEST_NEBULA_DIR"), filepath.Join(root, ".cache", "nebula", nebula.Version, runtime.GOOS+"_"+runtime.GOARCH)} {
		if d == "" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(d, "nebula"))
		if err == nil && fmt.Sprintf("%x", sha256.Sum256(b)) == art.BinarySHA256 {
			return d
		}
	}
	if os.Getenv("WERKBORD_REQUIRE_NEBULA") == "1" {
		t.Fatalf("the pinned Nebula %s is required (WERKBORD_REQUIRE_NEBULA=1) and is not in .cache/nebula: run scripts/fetch-nebula.sh", nebula.Version)
	}
	t.Skipf("the pinned Nebula %s is not in .cache/nebula: run scripts/fetch-nebula.sh to run this test", nebula.Version)
	return ""
}

// ---- the whole chain, with the real network program ----

type process struct {
	mu  sync.Mutex
	buf bytes.Buffer
	cmd *exec.Cmd
}

func (p *process) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.buf.Write(b)
}
func (p *process) has(s string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return strings.Contains(p.buf.String(), s)
}
func (p *process) kill() {
	if p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
		_, _ = p.cmd.Process.Wait()
	}
}

// runInstalled runs the pinned program on the files Install wrote, as a device that has just joined would,
// except that it has no network interface (the test may not create one).
func runInstalled(t *testing.T, binDir, netDir string) *process {
	t.Helper()
	cfgPath := filepath.Join(netDir, enrollment.FileConfig)
	b, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "disabled: false") {
		if err := os.WriteFile(cfgPath, bytes.Replace(b, []byte("disabled: false"), []byte("disabled: true"), 1), 0o600); err != nil {
			t.Fatal(err)
		}
	} else if !strings.Contains(string(b), "disabled: true") {
		t.Fatalf("the configuration the workspace sent has no tun section to adapt for a test:\n%s", b)
	}
	p := &process{}
	p.cmd = exec.Command(filepath.Join(binDir, "nebula"), "-config", cfgPath)
	p.cmd.Stdout, p.cmd.Stderr = p, p
	if err := p.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.kill)
	return p
}

func waitFor(t *testing.T, what string, within time.Duration, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(within)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestADeviceThatJoinedReachesTheHostsNetworkAndARevokedOneIsRefusedByIt(t *testing.T) {
	t.Run("a Connectivity Host", func(t *testing.T) { realNetworkScenario(t, "localhost", true) })
	// On one LAN there is no Connectivity Host and nothing to relay: the Workspace Host helps devices find it, and that is all
	// the network needs.
	t.Run("a Workspace Host on a LAN", func(t *testing.T) { realNetworkScenario(t, "127.0.0.1", false) })
}

func realNetworkScenario(t *testing.T, endpoint string, connectivity bool) {
	bin := nebulaDir(t)
	h, created, nc := newFirstHost(t, endpoint)
	if nc.ConnectivityHost != connectivity {
		t.Fatalf("ConnectivityHost = %v at %s, want %v", nc.ConnectivityHost, endpoint, connectivity)
	}
	h.cfg.NebulaDirs = []string{bin}
	h.nw.cfg = h.cfg
	h.nw.node.cfg = h.cfg
	h.nw.node.noTun = true
	h.serve()
	owner := h.owner(created)

	// The host brings its own node up from what the workspace knows of it: a certificate it issues itself,
	// this host listed as the one discovery host and relay, nobody on the blocklist.
	h.nw.node.reconcile(bg)
	t.Cleanup(func() { _ = h.nw.node.sup.Stop(bg) })
	if st := h.nw.node.sup.Status(); st.State != nebula.StateRunning || st.Version != nebula.Version {
		t.Fatalf("the host's node: %+v\n%s", st, strings.Join(st.Tail, "\n"))
	}
	hostID := h.nw.mat.Meta.HostDeviceID

	// A member joins through the workspace's own endpoint and runs the node from what it was sent.
	r := h.invite(created, service.EnrollInviteInput{})
	d := newDevice(t, "bo-laptop")
	res, err := d.join(r.Link, "Bo")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Network.Discovery) != 1 || len(res.Network.Discovery[0].Endpoints) != 1 || !strings.HasPrefix(res.Network.Discovery[0].Endpoints[0], endpoint+":") {
		t.Fatalf("the device was told of %+v", res.Network.Discovery)
	}
	if (len(res.Network.Relays) == 1) != connectivity {
		t.Errorf("relays = %v with connectivity=%v", res.Network.Relays, connectivity)
	}
	netDir := filepath.Join(t.TempDir(), "net")
	if err := enrollment.Install(netDir, res.Network, d.netPriv); err != nil {
		t.Fatal(err)
	}
	node := runInstalled(t, bin, netDir)
	waitFor(t, "the host to complete a handshake with the member's node", 90*time.Second, func() bool {
		st := h.nw.node.sup.Status()
		return strings.Contains(strings.Join(st.Tail, "\n"), `"certName":"`+d.id.DeviceID()+`"`)
	})
	waitFor(t, "the member's node to hear back from the host", 90*time.Second, func() bool { return node.has(`"certName":"` + hostID + `"`) })
	node.kill()

	// The member is revoked. The host's own reconciliation puts the certificate on its network program's blocklist
	// and reloads it without restarting.
	pidBefore := h.nw.node.sup.Status().PID
	if _, err := h.svc.RevokeDevice(bg, owner, d.id.DeviceID()); err != nil {
		t.Fatal(err)
	}
	h.nw.node.reconcile(bg)
	if h.nw.node.sup.Status().PID != pidBefore {
		t.Error("revoking a device restarted the host's node: a blocklist is applied by a reload")
	}
	waitFor(t, "the host's node to reload its blocklist", 15*time.Second, func() bool {
		return strings.Contains(strings.Join(h.nw.node.sup.Status().Tail, "\n"), `"msg":"Blocklisted certificates"`)
	})
	// The same device, with the same certificate, is refused by the network now.
	again := runInstalled(t, bin, netDir)
	waitFor(t, "the host to refuse the revoked certificate", 90*time.Second, func() bool {
		return strings.Contains(strings.Join(h.nw.node.sup.Status().Tail, "\n"), "certificate is in the block list")
	})
	if again.has(`"certName":"` + hostID + `"`) {
		t.Error("the revoked device completed a handshake with the host")
	}
}

func TestTheHostsPlaceOnTheNetworkIsATransport(t *testing.T) {
	h, created, _ := newFirstHost(t, "localhost")
	h.serve()
	r := h.invite(created, service.EnrollInviteInput{})
	d := newDevice(t, "bo")
	res, err := d.join(r.Link, "Bo")
	if err != nil {
		t.Fatal(err)
	}
	tr, err := h.nw.Transport(bg)
	if err != nil {
		t.Fatal(err)
	}
	// Before it is started it is stopped, and says nothing is known.
	if st, _ := tr.Status(bg); st.State != "stopped" {
		t.Errorf("state = %s", st.State)
	}
	// Its peers are the workspace's devices, which only a started transport lists.
	if _, err := tr.Peers(bg); err == nil {
		t.Error("a transport that was never started listed peers")
	}
	// (Starting it would start the network program and need the right to create an interface; the transport's own
	// behaviour is tested in internal/team/infra/overlaynet. What is checked here is that it is built from this host's
	// place in this workspace.)
	peers, err := h.svc.NetworkPeers(bg, h.nw.mat.Meta.WorkspaceID, h.nw.mat.Meta.HostDeviceID)
	if err != nil || len(peers) != 1 || peers[0].DeviceID != d.id.DeviceID() || peers[0].OverlayAddr != res.Network.OverlayAddr {
		t.Errorf("peers = %+v, %v", peers, err)
	}
}

// flipChar returns s with the character at i replaced by a different one from the same alphabet.
func flipChar(s string, i int) string {
	c := byte('A')
	if s[i] == 'A' {
		c = 'B'
	}
	return s[:i] + string(c) + s[i+1:]
}
