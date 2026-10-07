package enrollment_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"devboard/internal/enrollment"
)

// ---- a workspace, built from the standard library only (this package must not import Team) ----

type workspace struct {
	id   string
	priv ed25519.PrivateKey
	pub  ed25519.PublicKey
	root *x509.Certificate
	der  []byte
}

func newWorkspace(t *testing.T) *workspace {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "root"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, pub, priv)
	if err != nil {
		t.Fatal(err)
	}
	root, _ := x509.ParseCertificate(der)
	return &workspace{id: "tws_test", priv: priv, pub: pub, root: root, der: der}
}

func (w *workspace) serverCert(t *testing.T, ips ...string) enrollment.ServerCert {
	t.Helper()
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "leaf"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, KeyUsage: x509.KeyUsageDigitalSignature}
	for _, ip := range ips {
		tmpl.IPAddresses = append(tmpl.IPAddresses, net.ParseIP(ip))
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, w.root, pub, w.priv)
	if err != nil {
		t.Fatal(err)
	}
	return enrollment.ServerCert{ChainDER: [][]byte{der, w.der}, Key: priv}
}

func (w *workspace) invite(t *testing.T, endpoints []string, ttl time.Duration) (enrollment.Invitation, string) {
	t.Helper()
	cred, _ := enrollment.NewCredential()
	inv := enrollment.Invitation{ID: enrollment.NewID(), WorkspaceID: w.id, WorkspaceName: "Acme", Endpoints: endpoints, Credential: cred, Role: "member",
		IssuedAt: time.Now().Unix(), ExpiresAt: time.Now().Add(ttl).Unix()}
	link, err := enrollment.Sign(inv, w.priv)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := enrollment.Parse(link, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return parsed, link
}

// ---- a device ----

type device struct {
	id   string
	priv ed25519.PrivateKey
}

func newDevice() *device {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	return &device{id: "dev_" + strings.Repeat("a", 16), priv: priv}
}
func (d *device) DeviceID() string             { return d.id }
func (d *device) PublicKey() ed25519.PublicKey { return d.priv.Public().(ed25519.PublicKey) }
func (d *device) Sign(m []byte) []byte         { return ed25519.Sign(d.priv, m) }

// ---- a fake Authority ----

type authority struct {
	mu       sync.Mutex
	w        *workspace
	joins    []enrollment.AuthorityJoin
	polls    int
	approveN int // approve on this poll (0 = immediately)
	pending  map[string]ed25519.PublicKey
	refuse   bool
}

func (a *authority) Identity(context.Context) (enrollment.Hello, error) {
	return enrollment.Hello{WorkspaceID: a.w.id, WorkspaceKey: base64.RawURLEncoding.EncodeToString(a.w.pub), Fingerprint: enrollment.WorkspaceFingerprint(a.w.pub)}, nil
}

func (a *authority) approved(req enrollment.JoinRequest) enrollment.Response {
	return enrollment.Response{State: enrollment.StateApproved, EnrollmentID: "enr_1", WorkspaceID: a.w.id, WorkspaceName: "Acme", WorkspaceKey: base64.RawURLEncoding.EncodeToString(a.w.pub),
		MemberID: "tmb_1", Role: "member", DeviceID: req.DeviceID, DeviceToken: "wbd_secret",
		Network: &enrollment.NetworkBundle{CACertificate: "CA", NodeCertificate: "NODE", Config: "pki:\n  ca: \"" + enrollment.ConfigDirToken + "/ca.crt\"\n", OverlayAddr: "10.9.0.2"}}
}

func (a *authority) Enroll(_ context.Context, req enrollment.AuthorityJoin) (enrollment.Response, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.joins = append(a.joins, req)
	if a.refuse {
		return enrollment.Response{}, enrollment.ErrNotFound
	}
	if a.approveN > 0 {
		a.pending[req.DeviceID] = req.Key
		return enrollment.Response{State: enrollment.StatePending, EnrollmentID: "enr_1", Message: "waiting"}, nil
	}
	return a.approved(req.JoinRequest), nil
}

func (a *authority) Poll(_ context.Context, req enrollment.AuthorityPoll) (enrollment.Response, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.polls++
	if a.polls < a.approveN {
		return enrollment.Response{State: enrollment.StatePending, EnrollmentID: req.EnrollmentID}, nil
	}
	return a.approved(enrollment.JoinRequest{DeviceID: req.DeviceID}), nil
}

func (a *authority) key(_ context.Context, id, dev string) (ed25519.PublicKey, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if k, ok := a.pending[dev]; ok && id != "" {
		return k, nil
	}
	return nil, enrollment.ErrNotFound
}

type env struct {
	w    *workspace
	auth *authority
	addr string
	srv  *http.Server
}

func serve(t *testing.T, w *workspace, auth *authority) *env {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	cert := w.serverCert(t, "127.0.0.1")
	s := enrollment.NewServer(enrollment.ServerOptions{Authority: auth, WorkspaceID: w.id, PollKey: auth.key,
		Cert: func() (enrollment.ServerCert, error) { return cert, nil }, RatePerMinute: 1000})
	srv := s.HTTPServer()
	go func() { _ = srv.Serve(s.Listener(ln)) }()
	t.Cleanup(func() { _ = srv.Close() })
	return &env{w: w, auth: auth, addr: ln.Addr().String(), srv: srv}
}

func newEnv(t *testing.T) *env {
	w := newWorkspace(t)
	return serve(t, w, &authority{w: w, pending: map[string]ed25519.PublicKey{}})
}

func params(d *device) enrollment.JoinParams {
	return enrollment.JoinParams{Signer: d, MemberName: "Sam", DeviceName: "Sam's laptop", NetworkPublicKeyPEM: "-----BEGIN NEBULA X25519 PUBLIC KEY-----\nAAAA\n-----END NEBULA X25519 PUBLIC KEY-----\n"}
}

// ---- invitations ----

func TestAnInvitationRoundTripsAndCarriesOnlyWhatItShould(t *testing.T) {
	w := newWorkspace(t)
	inv, link := w.invite(t, []string{"10.0.0.1:7440", "host.example:7440"}, time.Hour)
	if !strings.HasPrefix(link, enrollment.JoinPrefix) {
		t.Fatalf("link = %q", link)
	}
	if inv.WorkspaceID != w.id || len(inv.Endpoints) != 2 || inv.Fingerprint != enrollment.WorkspaceFingerprint(w.pub) {
		t.Errorf("parsed = %+v", inv)
	}
	// Nothing in it is a network private key or a member token: the payload names only its own fields.
	raw, _ := base64.RawURLEncoding.DecodeString(strings.Split(strings.TrimPrefix(link, enrollment.JoinPrefix), ".")[1])
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	for k := range fields {
		switch k {
		case "v", "id", "ws", "name", "key", "fp", "at", "cred", "role", "caps", "iat", "exp":
		default:
			t.Errorf("the invitation carries a field %q", k)
		}
	}
	if len(link) > 1200 {
		t.Errorf("an invitation of %d bytes is too big for a QR code to be comfortable", len(link))
	}
}

func TestAModifiedInvitationIsRefused(t *testing.T) {
	w := newWorkspace(t)
	_, link := w.invite(t, []string{"10.0.0.1:7440"}, time.Hour)
	parts := strings.Split(strings.TrimPrefix(link, enrollment.JoinPrefix), ".")
	payload, _ := base64.RawURLEncoding.DecodeString(parts[1])
	for name, edit := range map[string]func([]byte) []byte{
		"the role":     func(b []byte) []byte { return bytes.Replace(b, []byte(`"role":"member"`), []byte(`"role":"admin"`), 1) },
		"the endpoint": func(b []byte) []byte { return bytes.Replace(b, []byte("10.0.0.1"), []byte("10.6.6.6"), 1) },
		"the expiry":   func(b []byte) []byte { return bytes.Replace(b, []byte(`"exp":`), []byte(`"exp":9`), 1) },
		"the workspace": func(b []byte) []byte {
			return bytes.Replace(b, []byte(`"ws":"tws_test"`), []byte(`"ws":"tws_evil"`), 1)
		},
	} {
		changed := edit(payload)
		if bytes.Equal(changed, payload) {
			t.Fatalf("%s: the edit changed nothing", name)
		}
		forged := enrollment.JoinPrefix + parts[0] + "." + base64.RawURLEncoding.EncodeToString(changed) + "." + parts[2]
		if _, err := enrollment.Parse(forged, time.Now()); !errors.Is(err, enrollment.ErrBadSignature) {
			t.Errorf("modifying %s: %v, want ErrBadSignature", name, err)
		}
	}
	// A flipped signature byte.
	sig, _ := base64.RawURLEncoding.DecodeString(parts[2])
	sig[0] ^= 1
	if _, err := enrollment.Parse(enrollment.JoinPrefix+parts[0]+"."+parts[1]+"."+base64.RawURLEncoding.EncodeToString(sig), time.Now()); !errors.Is(err, enrollment.ErrBadSignature) {
		t.Errorf("bad signature: %v", err)
	}
	// Someone else's key: the signature is good but the fingerprint field is not that key's.
	other := newWorkspace(t)
	forged := bytes.Replace(payload, []byte(`"key":"`+base64.RawURLEncoding.EncodeToString(w.pub)), []byte(`"key":"`+base64.RawURLEncoding.EncodeToString(other.pub)), 1)
	sig2 := ed25519.Sign(other.priv, append([]byte("werkbord/invitation/v1\x00"), forged...))
	if _, err := enrollment.Parse(enrollment.JoinPrefix+parts[0]+"."+base64.RawURLEncoding.EncodeToString(forged)+"."+base64.RawURLEncoding.EncodeToString(sig2), time.Now()); !errors.Is(err, enrollment.ErrBadSignature) {
		t.Errorf("a key swapped under an old fingerprint: %v, want ErrBadSignature", err)
	}
}

func TestMalformedInvitationsAreRefused(t *testing.T) {
	for _, s := range []string{"", "hello", "werkbord://join/", "werkbord://join/v1.a.b", "werkbord://join/v2.AAAA.AAAA", "https://join/v1.a.b", "werkbord://join/v1." + strings.Repeat("A", 5000) + ".x"} {
		if _, err := enrollment.Parse(s, time.Now()); !errors.Is(err, enrollment.ErrMalformed) {
			t.Errorf("Parse(%.30q) = %v, want ErrMalformed", s, err)
		}
	}
}

func TestAnExpiredInvitationIsRefused(t *testing.T) {
	w := newWorkspace(t)
	_, link := w.invite(t, []string{"10.0.0.1:7440"}, time.Minute)
	if _, err := enrollment.Parse(link, time.Now().Add(2*time.Minute)); !errors.Is(err, enrollment.ErrExpired) {
		t.Errorf("Parse after expiry = %v", err)
	}
	inv, _ := w.invite(t, []string{"10.0.0.1:7440"}, time.Minute)
	p := params(newDevice())
	p.Now = func() time.Time { return time.Now().Add(2 * time.Minute) }
	if _, err := enrollment.Join(context.Background(), inv, p); !errors.Is(err, enrollment.ErrExpired) {
		t.Errorf("Join after expiry = %v", err)
	}
}

func TestFingerprintComparisonIsWhatAPersonWouldDo(t *testing.T) {
	w := newWorkspace(t)
	fp := enrollment.WorkspaceFingerprint(w.pub)
	if len(strings.Split(fp, "-")) != 13 {
		t.Errorf("fingerprint %q is not 13 groups", fp)
	}
	if !enrollment.SameFingerprint(strings.ToUpper(strings.ReplaceAll(fp, "-", " ")), fp) {
		t.Error("case and separators should not matter")
	}
	if enrollment.SameFingerprint(fp, enrollment.WorkspaceFingerprint(newWorkspace(t).pub)) || enrollment.SameFingerprint("", "") {
		t.Error("different (or empty) fingerprints compared equal")
	}
}

// ---- joining ----

func TestADeviceJoinsThroughTheWorkspacesOwnEndpoint(t *testing.T) {
	e := newEnv(t)
	inv, _ := e.w.invite(t, []string{e.addr}, time.Hour)
	d := newDevice()
	res, err := enrollment.Join(context.Background(), inv, params(d))
	if err != nil {
		t.Fatal(err)
	}
	if res.State != enrollment.StateApproved || res.Network == nil || res.Response.DeviceToken != "wbd_secret" {
		t.Fatalf("result = %+v", res)
	}
	if len(e.auth.joins) != 1 {
		t.Fatalf("the authority saw %d joins", len(e.auth.joins))
	}
	j := e.auth.joins[0]
	if j.InviteID != inv.ID || j.Credential != inv.Credential || !j.Key.Equal(d.PublicKey()) || j.DeviceName != "Sam's laptop" {
		t.Errorf("authority saw %+v", j.JoinRequest)
	}
}

func TestADeviceNeverSendsItsCredentialToAnImposter(t *testing.T) {
	real := newEnv(t)
	imposter := newEnv(t) // a different workspace key, so a different fingerprint
	inv, _ := real.w.invite(t, []string{imposter.addr}, time.Hour)
	_, err := enrollment.Join(context.Background(), inv, params(newDevice()))
	if !errors.Is(err, enrollment.ErrNoEndpoint) {
		t.Fatalf("Join = %v, want ErrNoEndpoint", err)
	}
	if n := len(imposter.auth.joins); n != 0 {
		t.Errorf("the imposter's endpoint received %d enrollment requests: the credential was sent before the server proved it was the workspace", n)
	}
}

func TestAWrongExpectedFingerprintIsRefusedBeforeAnythingIsSent(t *testing.T) {
	e := newEnv(t)
	inv, _ := e.w.invite(t, []string{e.addr}, time.Hour)
	p := params(newDevice())
	p.ExpectFingerprint = enrollment.WorkspaceFingerprint(newWorkspace(t).pub)
	if _, err := enrollment.Join(context.Background(), inv, p); !errors.Is(err, enrollment.ErrWrongWorkspace) {
		t.Fatalf("Join = %v, want ErrWrongWorkspace", err)
	}
	if len(e.auth.joins) != 0 {
		t.Error("something was sent")
	}
	p.ExpectFingerprint = inv.Fingerprint
	if _, err := enrollment.Join(context.Background(), inv, p); err != nil {
		t.Errorf("the right fingerprint was refused: %v", err)
	}
}

func TestAnEndpointForTheWrongAddressIsRefused(t *testing.T) {
	// The certificate is for 127.0.0.1; the invitation says to dial it as "localhost".
	e := newEnv(t)
	_, port, _ := net.SplitHostPort(e.addr)
	inv, _ := e.w.invite(t, []string{"localhost:" + port}, time.Hour)
	if _, err := enrollment.Join(context.Background(), inv, params(newDevice())); !errors.Is(err, enrollment.ErrNoEndpoint) {
		t.Errorf("Join = %v, want ErrNoEndpoint", err)
	}
}

func TestSeveralEndpointsAreTriedInOrder(t *testing.T) {
	e := newEnv(t)
	dead, _ := net.Listen("tcp", "127.0.0.1:0")
	deadAddr := dead.Addr().String()
	dead.Close()
	inv, _ := e.w.invite(t, []string{deadAddr, e.addr}, time.Hour)
	res, err := enrollment.Join(context.Background(), inv, params(newDevice()))
	if err != nil || res.State != enrollment.StateApproved {
		t.Fatalf("Join with the first endpoint down = %v, %v", res, err)
	}
	// And a host that answers with a refusal is final: the next one is not asked.
	e2 := newEnv(t)
	e2.auth.refuse = true
	inv2, _ := e2.w.invite(t, []string{e2.addr, e2.addr}, time.Hour)
	if _, err := enrollment.Join(context.Background(), inv2, params(newDevice())); !errors.Is(err, enrollment.ErrRefused) {
		t.Errorf("Join = %v, want ErrRefused", err)
	}
	if len(e2.auth.joins) != 1 {
		t.Errorf("a refusal was retried %d times", len(e2.auth.joins))
	}
}

func TestARefusalDoesNotSayWhy(t *testing.T) {
	e := newEnv(t)
	e.auth.refuse = true
	inv, _ := e.w.invite(t, []string{e.addr}, time.Hour)
	_, err := enrollment.Join(context.Background(), inv, params(newDevice()))
	if !errors.Is(err, enrollment.ErrRefused) {
		t.Fatalf("Join = %v", err)
	}
	if err.Error() != enrollment.ErrRefused.Error() {
		t.Errorf("the refusal carries more than the one fixed message: %q", err)
	}
}

func TestApprovalCanBeWaitedFor(t *testing.T) {
	e := newEnv(t)
	e.auth.approveN = 2 // pending on join, pending on the first poll, approved on the second
	inv, _ := e.w.invite(t, []string{e.addr}, time.Hour)
	p := params(newDevice())
	p.Wait, p.PollEvery = 10*time.Second, 20*time.Millisecond
	res, err := enrollment.Join(context.Background(), inv, p)
	if err != nil || res.State != enrollment.StateApproved {
		t.Fatalf("Join = %+v, %v", res, err)
	}
	if e.auth.polls != 2 {
		t.Errorf("polled %d times", e.auth.polls)
	}
}

func TestNotWaitingReportsPending(t *testing.T) {
	e := newEnv(t)
	e.auth.approveN = 5
	inv, _ := e.w.invite(t, []string{e.addr}, time.Hour)
	res, err := enrollment.Join(context.Background(), inv, params(newDevice()))
	if !errors.Is(err, enrollment.ErrPending) || res == nil || res.EnrollmentID == "" {
		t.Fatalf("Join = %+v, %v; want ErrPending with the enrollment's ID", res, err)
	}
}

// A device that was shut while it waited for an administrator can ask again, and is given what it was issued once it is approved.
func TestAWaitingDeviceCanResumeAfterARestart(t *testing.T) {
	e := newEnv(t)
	e.auth.approveN = 2 // pending on join, pending on the first poll, approved on the second
	inv, _ := e.w.invite(t, []string{e.addr}, time.Hour)
	dev := newDevice()
	res, err := enrollment.Join(context.Background(), inv, params(dev))
	if !errors.Is(err, enrollment.ErrPending) || res == nil {
		t.Fatalf("Join = %+v, %v", res, err)
	}
	// "Restart": only the invitation, the device's own key and the enrollment's ID are left.
	again, err := enrollment.Resume(context.Background(), inv, params(dev), res.EnrollmentID)
	if !errors.Is(err, enrollment.ErrPending) || again == nil || again.EnrollmentID != res.EnrollmentID {
		t.Fatalf("Resume = %+v, %v; want ErrPending", again, err)
	}
	final, err := enrollment.Resume(context.Background(), inv, params(dev), res.EnrollmentID)
	if err != nil || final.State != enrollment.StateApproved || final.Network == nil {
		t.Fatalf("Resume = %+v, %v", final, err)
	}
	// Another device cannot collect what this one was issued (its proof is made with the wrong key).
	if _, err := enrollment.Resume(context.Background(), inv, params(newDevice()), res.EnrollmentID); err == nil {
		t.Fatal("another device resumed this enrollment")
	}
}

// ---- the server's own checks ----

func rawPost(t *testing.T, e *env, pin ed25519.PublicKey, minVersion uint16, path string, body any) (int, []byte) {
	t.Helper()
	conn, err := tls.Dial("tcp", e.addr, &tls.Config{InsecureSkipVerify: true, MinVersion: minVersion, MaxVersion: tls.VersionTLS13}) //nolint:gosec // the test inspects the server, not the chain
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", "https://"+e.addr+path, bytes.NewReader(b))
	req.Close = true
	_ = req.Write(conn)
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(conn)
	text := buf.String()
	code := 0
	if len(text) > 12 {
		_, _ = fmtSscan(text[9:12], &code)
	}
	return code, buf.Bytes()
}

func fmtSscan(s string, n *int) (int, error) {
	v := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, errors.New("nan")
		}
		v = v*10 + int(r-'0')
	}
	*n = v
	return 1, nil
}

func TestTheServerRefusesTLS12(t *testing.T) {
	e := newEnv(t)
	_, err := tls.Dial("tcp", e.addr, &tls.Config{InsecureSkipVerify: true, MaxVersion: tls.VersionTLS12}) //nolint:gosec // downgrade attempt
	if err == nil {
		t.Fatal("a TLS 1.2 handshake succeeded")
	}
}

func TestAProofForOneConnectionIsNotAProofForAnother(t *testing.T) {
	e := newEnv(t)
	d := newDevice()
	inv, _ := e.w.invite(t, []string{e.addr}, time.Hour)
	req := enrollment.JoinRequest{InviteID: inv.ID, Credential: inv.Credential, DeviceID: d.id, DeviceName: "x", MemberName: "Sam",
		DevicePublicKey: base64.RawURLEncoding.EncodeToString(d.PublicKey()), NetworkPublicKey: "-----BEGIN X-----\nAAAA\n-----END X-----\n"}
	// Sign for an exporter value that is not this connection's (a recording of someone else's request).
	req.Proof = base64.RawURLEncoding.EncodeToString(d.Sign(enrollment.JoinStatement(e.w.id, req, bytes.Repeat([]byte{7}, enrollment.ExporterBytes))))
	code, _ := rawPost(t, e, e.w.pub, tls.VersionTLS13, enrollment.PathJoin, req)
	if code != http.StatusForbidden {
		t.Errorf("a proof bound to another connection got status %d, want 403", code)
	}
	if len(e.auth.joins) != 0 {
		t.Error("the authority was asked")
	}
}

func TestAProofFromAnotherKeyIsRefused(t *testing.T) {
	e := newEnv(t)
	d, thief := newDevice(), newDevice()
	inv, _ := e.w.invite(t, []string{e.addr}, time.Hour)
	// The request claims d's key but is signed by someone who does not hold it.
	conn, err := tls.Dial("tcp", e.addr, &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS13}) //nolint:gosec // inspecting the server
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	cs := conn.ConnectionState()
	ex, _ := cs.ExportKeyingMaterial(enrollment.ExporterLabel, nil, enrollment.ExporterBytes)
	req := enrollment.JoinRequest{InviteID: inv.ID, Credential: inv.Credential, DeviceID: d.id, DeviceName: "x",
		DevicePublicKey: base64.RawURLEncoding.EncodeToString(d.PublicKey()), NetworkPublicKey: "-----BEGIN X-----\nAAAA\n-----END X-----\n"}
	req.Proof = base64.RawURLEncoding.EncodeToString(thief.Sign(enrollment.JoinStatement(e.w.id, req, ex)))
	b, _ := json.Marshal(req)
	hreq, _ := http.NewRequest("POST", "https://"+e.addr+enrollment.PathJoin, bytes.NewReader(b))
	hreq.Close = true
	_ = hreq.Write(conn)
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(conn)
	if !strings.Contains(buf.String()[:20], "403") || len(e.auth.joins) != 0 {
		t.Errorf("response %q, joins %d", buf.String()[:20], len(e.auth.joins))
	}
}

func TestTheServerHasNoPlaintextMode(t *testing.T) {
	e := newEnv(t)
	conn, err := net.Dial("tcp", e.addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_, _ = conn.Write([]byte("POST /enroll/v1/join HTTP/1.1\r\nHost: x\r\nContent-Length: 2\r\n\r\n{}"))
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 256)
	n, _ := conn.Read(buf)
	if strings.Contains(string(buf[:n]), "200") || strings.Contains(string(buf[:n]), "403 Forbidden") && strings.Contains(string(buf[:n]), "refused") {
		t.Errorf("plaintext request was served: %q", buf[:n])
	}
}

func TestProbeVerifiesTheWorkspaceWithoutSendingAnything(t *testing.T) {
	e := newEnv(t)
	h, err := enrollment.Probe(context.Background(), e.addr, e.w.pub, nil, nil)
	if err != nil || h.WorkspaceID != e.w.id {
		t.Fatalf("Probe = %+v, %v", h, err)
	}
	if _, err := enrollment.Probe(context.Background(), e.addr, newWorkspace(t).pub, nil, nil); err == nil {
		t.Error("Probe accepted a server that is not the pinned workspace")
	}
	if len(e.auth.joins) != 0 {
		t.Error("Probe enrolled something")
	}
}

func TestTheRateLimitSlowsGuessing(t *testing.T) {
	w := newWorkspace(t)
	auth := &authority{w: w, pending: map[string]ed25519.PublicKey{}}
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	cert := w.serverCert(t, "127.0.0.1")
	s := enrollment.NewServer(enrollment.ServerOptions{Authority: auth, WorkspaceID: w.id, PollKey: auth.key, RatePerMinute: 3,
		Cert: func() (enrollment.ServerCert, error) { return cert, nil }})
	srv := s.HTTPServer()
	go func() { _ = srv.Serve(s.Listener(ln)) }()
	defer srv.Close()
	busy := 0
	for i := 0; i < 8; i++ {
		if _, err := enrollment.Probe(context.Background(), ln.Addr().String(), w.pub, nil, nil); err != nil {
			busy++
		}
	}
	if busy < 4 {
		t.Errorf("only %d of 8 rapid requests were slowed", busy)
	}
}

// ---- installing what came back ----

func TestInstallWritesTheNetworkFilesPrivately(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "net")
	n := &enrollment.NetworkBundle{CACertificate: "CA\n", NodeCertificate: "NODE\n", Config: "pki:\n  ca: \"" + enrollment.ConfigDirToken + "/ca.crt\"\n  key: \"" + enrollment.ConfigDirToken + "/node.key\"\n"}
	if err := enrollment.Install(dir, n, []byte("PRIVATE\n")); err != nil {
		t.Fatal(err)
	}
	cfg, _ := os.ReadFile(filepath.Join(dir, enrollment.FileConfig))
	abs, _ := filepath.Abs(dir)
	if strings.Contains(string(cfg), enrollment.ConfigDirToken) || !strings.Contains(string(cfg), filepath.ToSlash(abs)+"/node.key") {
		t.Errorf("config = %q", cfg)
	}
	for name, want := range map[string]os.FileMode{enrollment.FileKey: 0o600, enrollment.FileConfig: 0o600, enrollment.FileCA: 0o644, enrollment.FileCert: 0o644} {
		fi, err := os.Stat(filepath.Join(dir, name))
		if err != nil || fi.Mode().Perm() != want {
			t.Errorf("%s: %v %v, want %o", name, fi, err, want)
		}
	}
	if fi, _ := os.Stat(dir); fi.Mode().Perm() != 0o700 {
		t.Errorf("directory mode %o", fi.Mode().Perm())
	}
	if err := enrollment.Install(filepath.Join(t.TempDir(), `a"b`), n, nil); err == nil {
		t.Error("a directory with a quote in it was accepted (it would break the YAML)")
	}
}
