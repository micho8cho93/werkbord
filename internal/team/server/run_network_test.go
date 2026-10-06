package server

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"devboard/internal/enrollment"
	"devboard/internal/team/domain"
	"devboard/internal/team/infra/overlay"
	"devboard/internal/team/service"
)

// The server, as `werkbord-team serve` runs it, on a host that has a private network: the API on its address, the
// endpoint for joining devices beside it, and the health report that says what is missing.
func TestServeRunsTheWorkspacesEnrollmentEndpointBesideItsAPI(t *testing.T) {
	h, created, nc := newFirstHost(t, "localhost")
	_ = h.db.Close()

	ctx, cancel := context.WithCancel(context.Background())
	ran := make(chan error, 1)
	go func() { ran <- Run(ctx, h.cfg, quiet(), "test") }()
	defer func() {
		cancel()
		select {
		case err := <-ran:
			if err != nil {
				t.Errorf("Run: %v", err)
			}
		case <-time.After(10 * time.Second):
			t.Error("shutdown did not finish")
		}
	}()
	base := "http://" + h.cfg.Addr
	var up bool
	for i := 0; i < 150 && !up; i++ {
		if res, err := http.Get(base + "/api/team/v1/health"); err == nil {
			res.Body.Close()
			up = true
		} else {
			time.Sleep(20 * time.Millisecond)
		}
	}
	if !up {
		t.Fatal("the server did not start")
	}
	// The endpoint for joining devices answers, as the workspace, to a device that pinned its key.
	key, _ := base64.RawURLEncoding.DecodeString(h.nw.mat.Trust.PublicKeyText())
	var probed enrollment.Hello
	var err error
	for i := 0; i < 100; i++ {
		probed, err = enrollment.Probe(ctx, h.cfg.BootstrapAddr, ed25519.PublicKey(key), nil, nil)
		if err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil || probed.WorkspaceID != h.nw.mat.Meta.WorkspaceID || probed.Fingerprint != nc.Fingerprint {
		t.Fatalf("Probe = %+v, %v", probed, err)
	}
	// The health report is available, and does not pretend.
	req, _ := http.NewRequest("GET", base+"/api/team/v1/network", nil)
	req.Header.Set("Authorization", "Bearer "+created.Token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var health struct {
		RemoteAccess string
		Warnings     []string
		Hosts        []domain.HostNetworkStatus
		Node         map[string]any
	}
	if err := json.NewDecoder(res.Body).Decode(&health); err != nil || res.StatusCode != 200 {
		t.Fatalf("health: %d %v", res.StatusCode, err)
	}
	if health.RemoteAccess != domain.RemoteAccessNotGuaranteed || len(health.Warnings) == 0 || len(health.Hosts) != 1 {
		t.Errorf("health = %+v", health)
	}
	if health.Node["state"] != "not_running" {
		t.Errorf("the node reported as %v although this host was told not to run one", health.Node)
	}
}

// A host that checks its own endpoint learns that it answers; that is not evidence that anyone elsewhere can reach it.
func TestAHostsCheckOfItselfProvesNothingAboutTheOutside(t *testing.T) {
	h, created, _ := newFirstHost(t, "localhost")
	h.serve()
	h.nw.probeOnce(bg)
	health, err := h.svc.NetworkHealth(bg, h.owner(created))
	if err != nil {
		t.Fatal(err)
	}
	host := health.Hosts[0]
	if host.LastCheckAt == nil {
		t.Fatal("the host's check of itself was not recorded")
	}
	if host.LastExternalOKAt != nil || health.RemoteAccess != domain.RemoteAccessNotGuaranteed {
		t.Errorf("a host's check of itself counted as an external one: %+v / %s", host, health.RemoteAccess)
	}
	// A second host's check of the first, from its own machine, is the external kind.
	r := h.invite(created, service.EnrollInviteInput{ForMemberID: h.owner(created).Member.ID, Capabilities: []domain.Capability{domain.CapabilityWorkspaceHost}})
	other := joinAsHost(t, h, r.Link, "other")
	a, err := h.svc.Authenticate(bg, other.token)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.svc.RecordReachability(bg, a, h.nw.mat.Meta.HostDeviceID, service.ReachReport{Endpoint: host.Endpoints[0].Endpoint, OK: true}); err != nil {
		t.Fatal(err)
	}
	health, _ = h.svc.NetworkHealth(bg, h.owner(created))
	for _, x := range health.Hosts {
		if x.DeviceID == h.nw.mat.Meta.HostDeviceID && (x.LastExternalOKAt == nil || health.RemoteAccess != domain.RemoteAccessAvailable) {
			t.Errorf("another device's check was not counted: %+v / %s", x, health.RemoteAccess)
		}
	}
}

func get2(t *testing.T, url, token string) (string, int) {
	t.Helper()
	s, c := get(t, url, token)
	return c, s
}

// ---- nothing Werkbord operates is needed ----

var hostnameRE = regexp.MustCompile(`(?i)\b[a-z0-9][a-z0-9-]*(\.[a-z0-9-]+)*\.(com|net|org|io|dev|app|cloud|co|ai|sh|xyz|info)\b`)

func TestNothingThatIsGeneratedNamesAServiceOutsideTheCustomersMachines(t *testing.T) {
	h, created, _ := newFirstHost(t, "localhost")
	h.serve()
	var generated []string

	// An invitation, decoded.
	r := h.invite(created, service.EnrollInviteInput{Label: "Bo"})
	inv, err := enrollment.Parse(r.Link, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(inv)
	generated = append(generated, string(b), r.Link)

	// What a joining device is sent, and the configuration it will run.
	d := newDevice(t, "bo")
	res, err := d.join(r.Link, "Bo")
	if err != nil {
		t.Fatal(err)
	}
	rb, _ := json.Marshal(res.Response)
	generated = append(generated, string(rb), res.Network.Config)

	// The host's own configuration, every role's policy, the health report.
	cfg, _ := h.svc.NodeConfigOf(bg, h.nw.mat.Meta.WorkspaceID, h.nw.mat.Meta.HostDeviceID)
	spec, err := nodeSpec(cfg, hostPorts(h), "/c", "/n", "/k")
	if err != nil {
		t.Fatal(err)
	}
	text, _ := overlay.Render(spec)
	generated = append(generated, string(text))
	health, _ := h.svc.NetworkHealth(bg, h.owner(created))
	hb, _ := json.Marshal(health)
	generated = append(generated, string(hb))

	for i, text := range generated {
		for _, m := range hostnameRE.FindAllString(text, -1) {
			t.Errorf("generated text %d names %q: nothing the workspace produces may depend on a name that is not the customer's own", i, m)
		}
		for _, banned := range []string{"werkbord.", "tailscale", "headscale", "defined.net", "github.com", "railway", "vercel", "supabase", "http://", "https://"} {
			if strings.Contains(strings.ToLower(text), banned) {
				t.Errorf("generated text %d mentions %q", i, banned)
			}
		}
	}
	// And the only addresses in a configuration are the network's own and the ones the customer gave.
	for _, ip := range regexp.MustCompile(`\b\d{1,3}(\.\d{1,3}){3}\b`).FindAllString(res.Network.Config, -1) {
		if !strings.HasPrefix(ip, strings.TrimSuffix(strings.Join(strings.Split(h.nw.mat.Meta.NetworkPrefix, ".")[:2], "."), ".")+".") && ip != "0.0.0.0" && ip != "127.0.0.1" {
			t.Errorf("the configuration holds the address %s, which is neither the network's nor one the customer gave", ip)
		}
	}
	_ = net.IPv4len
}

func TestTheRuntimeSettingsHaveNoServiceInThem(t *testing.T) {
	h, _, _ := newFirstHost(t, "localhost")
	if err := h.cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	// The settings that matter to the network are addresses and files of the customer's own.
	for _, bad := range []string{"http://", "https://"} {
		for _, v := range append([]string{h.cfg.Addr, h.cfg.BootstrapAddr, h.cfg.PassphraseFile}, h.cfg.Endpoints...) {
			if strings.Contains(v, bad) {
				t.Errorf("a setting holds a URL: %s", v)
			}
		}
	}
	bad := h.cfg
	bad.Endpoints = []string{"https://relay.example.com"}
	if bad.Validate() == nil {
		t.Error("an endpoint that is a URL was accepted")
	}
	bad.Endpoints = []string{"team.example.org:7440"}
	if bad.Validate() == nil {
		t.Error("an endpoint with a port was accepted: ports are the configuration's own")
	}
}
