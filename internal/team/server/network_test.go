package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"devboard/internal/deviceid/localidentity"
	"devboard/internal/enrollment"
	"devboard/internal/team/config"
	"devboard/internal/team/domain"
	"devboard/internal/team/infra/pki"
	"devboard/internal/team/service"
	"devboard/internal/team/store"
)

// These are the end-to-end tests of the customer-owned network: a real workspace on a
// real database, real keys, the real enrollment protocol over real TLS on loopback, and,
// where the pinned program is available, real network nodes joined from what the
// workspace issued. Nothing is faked except where two machines would be needed: every
// "machine" is a data directory and a port on this one.

var bg = context.Background()

func freeTCPPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func freeUDPPort(t *testing.T) int {
	t.Helper()
	c, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	return c.LocalAddr().(*net.UDPAddr).Port
}

// host is one workspace host: a data directory, its configuration and what runs on it.
type host struct {
	t    *testing.T
	cfg  config.Config
	db   store.Store
	svc  *service.Service
	nw   *network
	stop context.CancelFunc
	done chan struct{}
}

func quiet() *slog.Logger { return slog.New(slog.DiscardHandler) }

// newFirstHost makes a workspace with its private network on a new "machine". endpoints are
// the hosts it says it can be reached at ("localhost" can, as far as a test can tell).
func newFirstHost(t *testing.T, endpoints ...string) (*host, service.Created, NetworkCreated) {
	t.Helper()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.Addr = "127.0.0.1:" + strconv.Itoa(freeTCPPort(t))
	cfg.BootstrapAddr = "127.0.0.1:" + strconv.Itoa(freeTCPPort(t))
	cfg.NetworkPort = freeUDPPort(t)
	cfg.Endpoints = endpoints
	cfg.RunNode = false // a test has no right to create a network interface
	db, svc, err := Open(bg, cfg, quiet())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	created, err := svc.CreateWorkspace(bg, "Acme", "Ada", "")
	if err != nil {
		t.Fatal(err)
	}
	nc, err := CreateNetwork(bg, cfg, quiet(), svc, created, NetworkOptions{Connectivity: "auto"})
	if err != nil {
		t.Fatal(err)
	}
	nw, err := openNetwork(cfg, quiet(), svc)
	if err != nil || nw == nil {
		t.Fatalf("openNetwork = %v, %v", nw, err)
	}
	return &host{t: t, cfg: cfg, db: db, svc: svc, nw: nw}, created, nc
}

// serve starts this host's enrollment endpoint.
func (h *host) serve() {
	h.t.Helper()
	ctx, cancel := context.WithCancel(bg)
	h.stop, h.done = cancel, make(chan struct{})
	go func() { defer close(h.done); h.nw.serveEnrollment(ctx) }()
	for i := 0; i < 100; i++ {
		if c, err := net.Dial("tcp", h.cfg.BootstrapAddr); err == nil {
			c.Close()
			h.t.Cleanup(func() { cancel(); <-h.done })
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	h.t.Fatal("the enrollment endpoint did not come up")
}

func (h *host) owner(created service.Created) service.Actor {
	h.t.Helper()
	a, err := h.svc.Authenticate(bg, created.Token)
	if err != nil {
		h.t.Fatal(err)
	}
	return a
}

// device is a member's device, with the identity the individual product gives it.
type device struct {
	id      *localidentity.Identity
	netPriv []byte
	netPub  string
}

func newDevice(t *testing.T, name string) *device {
	t.Helper()
	id, err := localidentity.New(name, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	priv, pub, err := pki.GenerateNodeKey()
	if err != nil {
		t.Fatal(err)
	}
	return &device{id: id, netPriv: priv, netPub: string(pub)}
}

func (d *device) join(link, memberName string, mutate ...func(*enrollment.JoinParams)) (*enrollment.Result, error) {
	inv, err := enrollment.Parse(link, time.Now())
	if err != nil {
		return nil, err
	}
	p := enrollment.JoinParams{Signer: d.id, MemberName: memberName, DeviceName: d.id.Name(), NetworkPublicKeyPEM: d.netPub}
	for _, m := range mutate {
		m(&p)
	}
	res, err := enrollment.Join(bg, inv, p)
	if err == nil && res.Response.DeviceToken != "" {
		proofIdentities.Store(res.Response.DeviceToken, proofIdentity{d.id, res.Response.WorkspaceID, res.Response.MemberID})
	}
	return res, err
}

// ---- the first workspace ----

func TestTheFirstWorkspaceGetsItsKeysItsNetworkAndItsFirstHost(t *testing.T) {
	h, created, nc := newFirstHost(t, "localhost")
	if !nc.ConnectivityHost || nc.HostAddress == "" || !strings.HasPrefix(nc.Range, "10.") || !strings.HasSuffix(nc.Range, ".0.0/16") {
		t.Fatalf("created = %+v", nc)
	}
	owner := h.owner(created)
	// The host is a Workspace Host and a Connectivity Host, and the owner's device.
	devs, err := h.svc.ListDevices(bg, owner)
	if err != nil || len(devs) != 1 {
		t.Fatalf("devices = %+v, %v", devs, err)
	}
	d := devs[0]
	if d.MemberID != owner.Member.ID || !d.Has(domain.CapabilityWorkspaceHost) || !d.Has(domain.CapabilityConnectivityHost) || d.HostStatus != domain.HostActive {
		t.Errorf("host device = %+v", d)
	}
	// Three families of keys, in a vault that holds nothing in clear.
	mat := h.nw.mat
	if !mat.Meta.Authority || mat.Meta.Fingerprint != nc.Fingerprint || mat.Meta.HostDeviceID != d.ID {
		t.Fatalf("vault meta = %+v", mat.Meta)
	}
	caPEM, _ := mat.CA.CertificatePEM()
	secrets := [][]byte{mat.Trust.Seed(), mat.CA.PrivateKeyPEM()}
	hostNet, _, _ := mat.Host.NetworkKeyPEMs()
	secrets = append(secrets, hostNet)
	_ = filepath.Walk(h.cfg.DataDir, func(p string, fi os.FileInfo, err error) error {
		if err != nil || fi.IsDir() {
			return err
		}
		b, _ := os.ReadFile(p)
		for _, s := range secrets {
			if bytes.Contains(b, s) || bytes.Contains(b, bytes.TrimSpace(s)) {
				t.Errorf("a private key is in clear in %s", p)
			}
		}
		if bytes.Contains(b, []byte(base64.RawURLEncoding.EncodeToString(mat.Trust.Seed()))) {
			t.Errorf("the workspace key's seed is in clear in %s", p)
		}
		return nil
	})
	for _, f := range []string{"workspace.key.sealed", "network-ca.key.sealed", "host.keys.sealed", "workspace.json"} {
		fi, err := os.Stat(filepath.Join(h.cfg.PKIDir(), f))
		if err != nil || fi.Mode().Perm()&0o077 != 0 {
			t.Errorf("%s: %v %v", f, fi, err)
		}
	}
	// None of it is in the database or in anything the API returns.
	db, _ := os.ReadFile(h.cfg.DBPath())
	wal, _ := os.ReadFile(h.cfg.DBPath() + "-wal")
	for _, b := range [][]byte{db, wal} {
		for _, s := range secrets {
			if bytes.Contains(b, bytes.TrimSpace(s)) {
				t.Error("a private key is in the database")
			}
		}
	}
	ts := httptest.NewServer(Handler(h.db, h.svc, quiet(), "test"))
	t.Cleanup(ts.Close)
	for _, path := range []string{"/api/team/v1/network", "/api/team/v1/devices", "/api/team/v1/enrollments", "/api/team/v1/enrollment-invitations", "/api/team/v1/me", "/api/team/v1/members"} {
		req, _ := http.NewRequest("GET", ts.URL+path, nil)
		req.Header.Set("Authorization", "Bearer "+created.Token)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		for _, s := range secrets {
			if bytes.Contains(body, bytes.TrimSpace(s)) || bytes.Contains(body, []byte(base64.RawURLEncoding.EncodeToString(mat.Trust.Seed()))) {
				t.Errorf("GET %s returns a private key", path)
			}
		}
		if strings.Contains(string(body), "PRIVATE KEY") || strings.Contains(string(body), "SIGNING PRIVATE") {
			t.Errorf("GET %s returns private key material", path)
		}
		if path == "/api/team/v1/network" {
			var got struct {
				Settings domain.NetworkSettings
				Hosts    []domain.HostNetworkStatus
				Warnings []string
			}
			if err := json.Unmarshal(body, &got); err != nil || got.Settings.Fingerprint != nc.Fingerprint || len(got.Hosts) != 1 || got.Settings.CACertificate != string(caPEM) {
				t.Errorf("health = %s (%v)", body, err)
			}
		}
	}
	// A second network cannot be made over the first.
	if _, err := CreateNetwork(bg, h.cfg, quiet(), h.svc, created, NetworkOptions{}); err == nil {
		t.Error("a second set of keys replaced the first")
	}
}
