//go:build !windows

package nebula

import (
	"net"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"devboard/internal/team/infra/overlay"
	"devboard/internal/team/infra/pki"
)

// The pinned program is needed by the tests that run a real node. Tests do not
// download it on their own: `make test-nebula` (scripts/fetch-nebula.sh) puts it in
// .cache/nebula, checked against the pin, and WERKBORD_REQUIRE_NEBULA=1 turns a
// missing one into a failure (CI sets it) instead of a skip.
func realBinaryDir(t *testing.T) string {
	t.Helper()
	art, err := ThisPlatform()
	if err != nil {
		t.Skip(err)
	}
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(file), "..", "..", "..", "..")
	dirs := []string{os.Getenv("WERKBORD_TEST_NEBULA_DIR"), filepath.Join(root, ".cache", "nebula", Version, runtime.GOOS+"_"+runtime.GOARCH)}
	for _, d := range dirs {
		if d == "" {
			continue
		}
		d, _ = filepath.Abs(d)
		if sum, err := sha256File(filepath.Join(d, binaryFile())); err == nil && sameHash(sum, art.BinarySHA256) {
			return d
		}
	}
	if os.Getenv("WERKBORD_REQUIRE_NEBULA") == "1" {
		t.Fatalf("the pinned Nebula %s is required (WERKBORD_REQUIRE_NEBULA=1) and is not in .cache/nebula: run scripts/fetch-nebula.sh", Version)
	}
	t.Skipf("the pinned Nebula %s is not in .cache/nebula: run scripts/fetch-nebula.sh to run this test", Version)
	return ""
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

// network is a throwaway authority and the nodes issued under it.
type network struct {
	t      *testing.T
	ca     *pki.NetworkCA
	caPEM  []byte
	prefix netip.Prefix
	now    time.Time
	next   int
}

func newNetwork(t *testing.T) *network { return newNetworkAt(t, time.Now()) }

func newNetworkAt(t *testing.T, now time.Time) *network {
	t.Helper()
	prefix := netip.MustParsePrefix("10.77.0.0/16")
	ca, err := pki.NewNetworkCA("Test", prefix, now)
	if err != nil {
		t.Fatal(err)
	}
	pemBytes, _ := ca.CertificatePEM()
	return &network{t: t, ca: ca, caPEM: pemBytes, prefix: prefix, now: now, next: 1}
}

type node struct {
	name            string
	addr            netip.Addr
	groups          []string
	certPEM, keyPEM []byte
	fingerprint     string
	port            int
}

func (n *network) issue(name string, groups ...string) node {
	n.t.Helper()
	priv, pub, err := pki.GenerateNodeKey()
	if err != nil {
		n.t.Fatal(err)
	}
	n.next++
	addr := netip.AddrFrom4([4]byte{10, 77, 0, byte(n.next)})
	is, err := n.ca.Issue(pki.NodeRequest{Name: name, Addr: addr, Groups: groups, PublicKeyPEM: pub}, n.now)
	if err != nil {
		n.t.Fatal(err)
	}
	return node{name: name, addr: addr, groups: groups, certPEM: is.CertPEM, keyPEM: priv, fingerprint: is.Fingerprint, port: freeUDPPort(n.t)}
}

func (n *network) config(nd node, mutate func(*overlay.NodeSpec)) NebulaConfig {
	spec := overlay.NodeSpec{Name: nd.name, Addr: nd.addr, Bits: n.prefix.Bits(), Port: nd.port, UseRelays: true, TunDisabled: true,
		Policy: overlay.PolicyFor(nd.groups, overlay.DefaultPorts()), LogLevel: "info"}
	if mutate != nil {
		mutate(&spec)
	}
	return NebulaConfig{DataDir: privateDir(n.t), Node: spec, CACertPEM: n.caPEM, NodeCertPEM: nd.certPEM, NodeKeyPEM: nd.keyPEM}
}

func (n *network) peer(nd node) overlay.Peer {
	return overlay.Peer{Addr: nd.addr, Endpoints: []string{net.JoinHostPort("127.0.0.1", itoa(nd.port))}}
}

func itoa(i int) string { return strconv.Itoa(i) }

func privateDir(t *testing.T) string {
	t.Helper()
	d, err := os.MkdirTemp("", "wbn")
	if err != nil {
		t.Fatal(err)
	}
	// macOS temp paths are long and symlinked; use the resolved path and keep it private.
	d, _ = filepath.EvalSymlinks(d)
	_ = os.Chmod(d, 0o700)
	t.Cleanup(func() { _ = os.RemoveAll(d) })
	return d
}

func startReal(t *testing.T, dir string, cfg NebulaConfig) *Supervisor {
	t.Helper()
	s, err := New(Options{BinaryDirs: []string{dir}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Stop(t.Context()) })
	if err := s.StartNebula(t.Context(), cfg); err != nil {
		t.Fatalf("StartNebula: %v\n%s", err, strings.Join(s.Status().Tail, "\n"))
	}
	return s
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

func tailHas(s *Supervisor, substr string) bool {
	for _, l := range s.Status().Tail {
		if strings.Contains(l, substr) {
			return true
		}
	}
	return false
}

// nebulaTest runs `nebula -test -config`, the program's own check of a configuration.
func nebulaTest(t *testing.T, dir string, cfg NebulaConfig) (string, error) {
	t.Helper()
	p, err := prepare(cfg, time.Now(), true)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(filepath.Join(dir, binaryFile()), "-test", "-config", p.configPath)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

type netipAddr = netip.Addr

func execTest(dir, configPath string) (string, error) {
	out, err := exec.Command(filepath.Join(dir, binaryFile()), "-test", "-config", configPath).CombinedOutput()
	return string(out), err
}
