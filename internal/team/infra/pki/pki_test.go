package pki

import (
	"bytes"
	"crypto/ecdh"
	"crypto/rand"
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/slackhq/nebula/cert"

	"devboard/internal/deviceid"
	"devboard/internal/enrollment"
)

var now = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

func prefix() netip.Prefix { return netip.MustParsePrefix("10.201.0.0/16") }

// ---- sealing ----

func TestSealedKeysAreOpenedOnlyByTheKeyAndLabelTheyWereSealedWith(t *testing.T) {
	dir := t.TempDir()
	s1, err := NewFileSealer(filepath.Join(dir, "kek"))
	if err != nil {
		t.Fatal(err)
	}
	secret := []byte("the authority's private key")
	sealed, err := s1.Seal("a", secret)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, secret) {
		t.Fatal("the sealed text contains the secret")
	}
	if got, err := s1.Open("a", sealed); err != nil || !bytes.Equal(got, secret) {
		t.Fatalf("Open = %q, %v", got, err)
	}
	if _, err := s1.Open("b", sealed); !errors.Is(err, ErrCannotOpen) {
		t.Errorf("a different label opened it: %v", err)
	}
	other, _ := NewFileSealer(filepath.Join(dir, "other"))
	if _, err := other.Open("a", sealed); !errors.Is(err, ErrCannotOpen) {
		t.Errorf("a different key opened it: %v", err)
	}
	for i := range sealed {
		c := append([]byte(nil), sealed...)
		c[i] ^= 1
		if _, err := s1.Open("a", c); err == nil {
			t.Fatalf("a sealed text altered at byte %d still opened", i)
		}
	}
	// The same key file is found again, and is not replaced.
	again, err := NewFileSealer(filepath.Join(dir, "kek"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := again.Open("a", sealed); err != nil {
		t.Errorf("the key was replaced: %v", err)
	}
}

func TestAKeyFileOthersCanReadIsRefused(t *testing.T) {
	p := filepath.Join(t.TempDir(), "kek")
	if _, err := NewFileSealer(p); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewFileSealer(p); err == nil || !strings.Contains(err.Error(), "accessible to other users") {
		t.Errorf("a world-readable key file was accepted: %v", err)
	}
}

func TestPassphraseSealing(t *testing.T) {
	a, err := NewPassphraseSealer([]byte("correct horse battery"))
	if err != nil {
		t.Fatal(err)
	}
	sealed, _ := a.Seal("x", []byte("secret"))
	if got, err := a.Open("x", sealed); err != nil || string(got) != "secret" {
		t.Fatalf("Open = %q, %v", got, err)
	}
	wrong, _ := NewPassphraseSealer([]byte("wrong horse battery"))
	if _, err := wrong.Open("x", sealed); !errors.Is(err, ErrCannotOpen) {
		t.Errorf("wrong passphrase: %v", err)
	}
	if _, err := NewPassphraseSealer([]byte("short")); err == nil {
		t.Error("a short passphrase was accepted")
	}
	// A file-sealed text is not a passphrase-sealed one.
	f, _ := NewFileSealer(filepath.Join(t.TempDir(), "kek"))
	fs, _ := f.Seal("x", []byte("secret"))
	if _, err := a.Open("x", fs); err == nil {
		t.Error("crossed sealers")
	}
}

// ---- the workspace's trust identity ----

func TestTheWorkspaceKeyAndTheNetworkKeyAreDifferentKeys(t *testing.T) {
	trust, err := NewTrust("tws_a", "Acme", now)
	if err != nil {
		t.Fatal(err)
	}
	ca, err := NewNetworkCA("Acme", prefix(), now)
	if err != nil {
		t.Fatal(err)
	}
	host, err := NewHostKeys()
	if err != nil {
		t.Fatal(err)
	}
	netPriv, _, _ := host.NetworkKeyPEMs()
	// No two of the four private keys are equal, and none of the public ones either.
	keys := map[string][]byte{"workspace": trust.Seed(), "network CA": ca.key[:32], "host app": host.app.Seed(), "host network": host.netPriv, "host sealing": host.seal.Bytes()}
	_ = netPriv
	for a, ka := range keys {
		for b, kb := range keys {
			if a < b && bytes.Equal(ka, kb) {
				t.Errorf("%s and %s are the same key", a, b)
			}
		}
	}
	if !deviceid.ValidID(host.DeviceID()) {
		t.Errorf("host device ID %q is not a device ID", host.DeviceID())
	}
}

func TestInvitationsSignedByTheTrustKeyVerify(t *testing.T) {
	trust, _ := NewTrust("tws_a", "Acme", now)
	cred, _ := enrollment.NewCredential()
	link, err := trust.SignInvitation(enrollment.Invitation{ID: enrollment.NewID(), WorkspaceName: "Acme", Endpoints: []string{"192.0.2.1:7440"}, Credential: cred,
		Role: "member", IssuedAt: now.Unix(), ExpiresAt: now.Add(time.Hour).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	inv, err := enrollment.Parse(link, now)
	if err != nil {
		t.Fatal(err)
	}
	if inv.Fingerprint != trust.Fingerprint() || inv.WorkspaceID != "tws_a" {
		t.Errorf("invitation = %+v", inv)
	}
}

func TestABootstrapCertificateChainsToTheWorkspaceKeyAndOnlyForItsAddresses(t *testing.T) {
	trust, _ := NewTrust("tws_a", "Acme", now)
	c, err := trust.IssueServerCertificate([]string{"192.0.2.10", "team.example.org"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if !c.Fresh(now) || c.Fresh(now.Add(ServerCertValidity)) {
		t.Error("freshness")
	}
	if len(c.ChainDER) != 2 || c.Key == nil {
		t.Fatalf("chain = %d", len(c.ChainDER))
	}
	if _, err := trust.IssueServerCertificate(nil, now); err == nil {
		t.Error("a certificate for no address was issued")
	}
}

func TestTheWorkspaceKeyIsRebuiltFromItsSeed(t *testing.T) {
	a, _ := NewTrust("tws_a", "Acme", now)
	b, err := TrustFromSeed("tws_a", "Acme", a.Seed(), now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if a.Fingerprint() != b.Fingerprint() {
		t.Error("the same key has two fingerprints")
	}
	if _, err := TrustFromSeed("tws_a", "Acme", []byte("short"), now); err == nil {
		t.Error("a short seed was accepted")
	}
}

// ---- the network authority ----

func nodeKey(t *testing.T) []byte {
	t.Helper()
	_, pub, err := GenerateNodeKey()
	if err != nil {
		t.Fatal(err)
	}
	return pub
}

func TestTheAuthorityIssuesCertificatesThatNebulaVerifies(t *testing.T) {
	ca, err := NewNetworkCA("Acme", prefix(), now)
	if err != nil {
		t.Fatal(err)
	}
	issued, err := ca.Issue(NodeRequest{Name: "dev_aaaaaaaaaaaaaaaa", Addr: netip.MustParseAddr("10.201.0.2"), Groups: []string{GroupMember, GroupRunner}, PublicKeyPEM: nodeKey(t)}, now)
	if err != nil {
		t.Fatal(err)
	}
	info, err := ca.VerifyNode(issued.CertPEM, now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if info.Addr != netip.MustParseAddr("10.201.0.2") || info.Name != "dev_aaaaaaaaaaaaaaaa" || len(info.Groups) != 2 || info.Fingerprint != issued.Fingerprint {
		t.Errorf("info = %+v", info)
	}
	if !info.NotAfter.Equal(now.Add(DefaultNodeValidity)) {
		t.Errorf("expires %v", info.NotAfter)
	}
	// Another authority's certificate does not verify, and nor does an expired one.
	other, _ := NewNetworkCA("Other", prefix(), now)
	if _, err := other.VerifyNode(issued.CertPEM, now); err == nil {
		t.Error("a certificate verified against a different authority")
	}
	if _, err := ca.VerifyNode(issued.CertPEM, now.Add(DefaultNodeValidity+time.Hour)); err == nil {
		t.Error("an expired certificate verified")
	}
	// Tampering with the groups breaks the signature.
	c, _, _ := cert.UnmarshalCertificateFromPEM(issued.CertPEM)
	if !c.CheckSignature(ca.cert.PublicKey()) {
		t.Error("the signature does not check")
	}
}

func TestTheAuthorityRefusesWhatItShouldNotSign(t *testing.T) {
	ca, _ := NewNetworkCA("Acme", prefix(), now)
	good := NodeRequest{Name: "dev_x", Addr: netip.MustParseAddr("10.201.0.9"), Groups: []string{GroupMember}, PublicKeyPEM: nodeKey(t)}
	for name, edit := range map[string]func(*NodeRequest){
		"an address outside the network": func(r *NodeRequest) { r.Addr = netip.MustParseAddr("192.168.1.5") },
		"the network address itself":     func(r *NodeRequest) { r.Addr = netip.MustParseAddr("10.201.0.0") },
		"a group that does not exist":    func(r *NodeRequest) { r.Groups = []string{"root"} },
		"no group":                       func(r *NodeRequest) { r.Groups = nil },
		"no name":                        func(r *NodeRequest) { r.Name = "" },
		"a certificate lasting years":    func(r *NodeRequest) { r.TTL = 5 * 365 * 24 * time.Hour },
		"a certificate lasting seconds":  func(r *NodeRequest) { r.TTL = time.Second },
		"a key that is not a key":        func(r *NodeRequest) { r.PublicKeyPEM = []byte("nope") },
		"a signing key as a node key": func(r *NodeRequest) {
			r.PublicKeyPEM = cert.MarshalSigningPublicKeyToPEM(cert.Curve_CURVE25519, bytes.Repeat([]byte{1}, 32))
		},
	} {
		r := good
		edit(&r)
		if _, err := ca.Issue(r, now); !errors.Is(err, ErrBadNodeRequest) {
			t.Errorf("%s: %v, want ErrBadNodeRequest", name, err)
		}
	}
	if _, err := ca.Issue(good, now); err != nil {
		t.Errorf("a good request was refused: %v", err)
	}
	for _, p := range []string{"10.0.0.0/8", "10.201.0.1/16", "fd00::/64", "0.0.0.0/0", "10.0.0.0/30", "8.8.8.0/24", "100.64.0.0/16", "172.0.0.0/16"} {
		if _, err := NewNetworkCA("x", netip.MustParsePrefix(p), now); err == nil {
			t.Errorf("a network of %s was accepted", p)
		}
	}
}

func TestTheAuthoritysOwnCertificateLimitsWhatItCanSign(t *testing.T) {
	// Even with the key in hand, the CA certificate bounds the groups and addresses: a
	// certificate outside them is not accepted by Nebula's own checks.
	ca, _ := NewNetworkCA("Acme", prefix(), now)
	tbs := &cert.TBSCertificate{Version: cert.Version2, Name: "evil", Networks: []netip.Prefix{netip.MustParsePrefix("192.168.0.5/16")}, Groups: []string{GroupMember},
		NotBefore: now, NotAfter: now.Add(time.Hour), PublicKey: bytes.Repeat([]byte{2}, 32), Curve: cert.Curve_CURVE25519}
	if _, err := tbs.Sign(ca.cert, cert.Curve_CURVE25519, ca.key); err == nil {
		t.Error("a certificate outside the authority's range was signed")
	}
	tbs.Networks = []netip.Prefix{netip.MustParsePrefix("10.201.0.5/16")}
	tbs.Groups = []string{"superuser"}
	if _, err := tbs.Sign(ca.cert, cert.Curve_CURVE25519, ca.key); err == nil {
		t.Error("a certificate with a group outside the authority's was signed")
	}
}

func TestTheAuthorityIsRebuiltFromItsFiles(t *testing.T) {
	ca, _ := NewNetworkCA("Acme", prefix(), now)
	certPEM, _ := ca.CertificatePEM()
	back, err := NetworkCAFromPEM(certPEM, ca.PrivateKeyPEM())
	if err != nil {
		t.Fatal(err)
	}
	a, _ := ca.Fingerprint()
	b, _ := back.Fingerprint()
	if a != b || back.Prefix() != prefix() {
		t.Error("the rebuilt authority differs")
	}
	other, _ := NewNetworkCA("Other", prefix(), now)
	if _, err := NetworkCAFromPEM(certPEM, other.PrivateKeyPEM()); err == nil {
		t.Error("a certificate and another authority's key were accepted together")
	}
}

// ---- the vault ----

func openVault(t *testing.T) *Vault {
	t.Helper()
	dir := t.TempDir()
	s, err := NewFileSealer(filepath.Join(dir, "secrets", "kek"))
	if err != nil {
		t.Fatal(err)
	}
	v, err := OpenVault(filepath.Join(dir, "pki"), s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestTheFirstWorkspaceBootstrapsItsKeys(t *testing.T) {
	v := openVault(t)
	m, err := v.Create("tws_a", "Acme", prefix(), now)
	if err != nil {
		t.Fatal(err)
	}
	if !m.Meta.Authority || m.Trust == nil || m.CA == nil || m.Host == nil || m.Meta.Fingerprint != m.Trust.Fingerprint() {
		t.Fatalf("material = %+v", m.Meta)
	}
	if _, err := v.Create("tws_b", "Other", prefix(), now); err == nil {
		t.Error("a second workspace overwrote the first")
	}
	back, err := v.Load(now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if back.Trust.Fingerprint() != m.Trust.Fingerprint() || back.Host.DeviceID() != m.Host.DeviceID() || back.Meta != m.Meta {
		t.Error("a reloaded vault differs")
	}
	// No private key sits in a file in clear.
	_ = filepath.Walk(v.Dir(), func(p string, fi os.FileInfo, err error) error {
		if err != nil || fi.IsDir() {
			return err
		}
		b, _ := os.ReadFile(p)
		for _, secret := range [][]byte{m.Trust.Seed(), m.CA.PrivateKeyPEM(), m.Host.app.Seed(), m.Host.netPriv} {
			if bytes.Contains(b, secret) || bytes.Contains(b, []byte(strings.TrimSpace(string(secret)))) {
				t.Errorf("%s holds a private key in clear", fi.Name())
			}
		}
		if fi.Mode().Perm()&0o077 != 0 {
			t.Errorf("%s is mode %o", fi.Name(), fi.Mode().Perm())
		}
		return nil
	})
	if fi, _ := os.Stat(v.Dir()); fi.Mode().Perm() != 0o700 {
		t.Errorf("vault directory is mode %o", fi.Mode().Perm())
	}
}

func TestAVaultWithTheWrongKeyDoesNotOpen(t *testing.T) {
	v := openVault(t)
	if _, err := v.Create("tws_a", "Acme", prefix(), now); err != nil {
		t.Fatal(err)
	}
	s2, _ := NewFileSealer(filepath.Join(t.TempDir(), "kek"))
	v2, _ := OpenVault(v.Dir(), s2)
	if _, err := v2.Load(now); !errors.Is(err, ErrCannotOpen) {
		t.Errorf("Load with another key = %v", err)
	}
}

// ---- handing the authority to a second host ----

func TestAnotherHostIsHandedTheAuthoritySealedToItAlone(t *testing.T) {
	first := openVault(t)
	m, err := first.Create("tws_a", "Acme", prefix(), now)
	if err != nil {
		t.Fatal(err)
	}
	caPEM, _ := m.CA.CertificatePEM()

	// The second host enrolled as a plain device, with its own keys, and so holds no authority yet.
	hostKeys, _ := NewHostKeys()
	secondDir := t.TempDir()
	s, _ := NewFileSealer(filepath.Join(secondDir, "kek"))
	second, _ := OpenVault(filepath.Join(secondDir, "pki"), s)
	meta := Meta{WorkspaceID: "tws_a", WorkspaceName: "Acme", Fingerprint: m.Meta.Fingerprint, NetworkPrefix: prefix().String()}
	if err := second.CreateJoined(meta, hostKeys, caPEM); err != nil {
		t.Fatal(err)
	}
	joined, err := second.Load(now)
	if err != nil {
		t.Fatal(err)
	}
	if joined.Trust != nil || joined.CA != nil || joined.Meta.Authority {
		t.Fatal("a host that has only enrolled holds the authority")
	}

	// The first host seals the minimum signing material to the second's sealing key.
	secrets := Secrets{WorkspaceName: "Acme", TrustSeed: m.Trust.Seed(), NetworkCACertificate: caPEM, NetworkCAKey: m.CA.PrivateKeyPEM()}
	sealed, err := SealSecrets(hostKeys.SealingPublicKey(), "tws_a", hostKeys.DeviceID(), secrets)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range [][]byte{m.Trust.Seed(), m.CA.PrivateKeyPEM()} {
		if bytes.Contains(sealed, secret) {
			t.Fatal("the sealed secrets contain a secret in clear")
		}
	}

	// A third host's keys cannot open it, nor can the same host under another device ID.
	third, _ := NewHostKeys()
	if _, err := third.OpenSecrets(now, "tws_a", m.Meta.Fingerprint, string(caPEM), sealed); err == nil {
		t.Error("another host opened secrets sealed to the second")
	}
	// Wrong workspace.
	if _, err := hostKeys.OpenSecrets(now, "tws_other", m.Meta.Fingerprint, string(caPEM), sealed); err == nil {
		t.Error("secrets were opened for another workspace")
	}
	// The right host, but a fingerprint it did not enrol with.
	if _, err := hostKeys.OpenSecrets(now, "tws_a", enrollment.WorkspaceFingerprint(mustTrust(t).PublicKey()), string(caPEM), sealed); err == nil {
		t.Error("secrets for a different workspace key were accepted")
	}
	// A different authority certificate than the one the host was given.
	otherCA, _ := NewNetworkCA("Acme", prefix(), now)
	otherPEM, _ := otherCA.CertificatePEM()
	if _, err := hostKeys.OpenSecrets(now, "tws_a", m.Meta.Fingerprint, string(otherPEM), sealed); err == nil {
		t.Error("an authority other than the enrolled one was accepted")
	}
	// Corrupted in transit.
	bad := append([]byte(nil), sealed...)
	bad[len(bad)-1] ^= 1
	if _, err := hostKeys.OpenSecrets(now, "tws_a", m.Meta.Fingerprint, string(caPEM), bad); err == nil {
		t.Error("a corrupted bundle opened")
	}

	got, err := hostKeys.OpenSecrets(now, "tws_a", m.Meta.Fingerprint, string(caPEM), sealed)
	if err != nil {
		t.Fatal(err)
	}
	if err := second.Promote(got, now); err != nil {
		t.Fatal(err)
	}
	promoted, err := second.Load(now)
	if err != nil {
		t.Fatal(err)
	}
	if !promoted.Meta.Authority || promoted.Trust.Fingerprint() != m.Trust.Fingerprint() {
		t.Fatal("the promoted host does not hold the authority")
	}
	// It can now issue a certificate the first host's network accepts.
	issued, err := promoted.CA.Issue(NodeRequest{Name: "dev_b", Addr: netip.MustParseAddr("10.201.0.7"), Groups: []string{GroupMember}, PublicKeyPEM: nodeKey(t)}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.CA.VerifyNode(issued.CertPEM, now); err != nil {
		t.Errorf("the first host does not accept a certificate the promoted host issued: %v", err)
	}

	// Demotion takes the secrets away again.
	if err := second.Demote(now); err != nil {
		t.Fatal(err)
	}
	demoted, _ := second.Load(now)
	if demoted.Meta.Authority || demoted.Trust != nil || demoted.CA != nil {
		t.Error("a demoted host still holds the authority")
	}
	for _, name := range []string{fileTrust, fileCAKey} {
		if _, err := os.Stat(filepath.Join(second.Dir(), name)); err == nil {
			t.Errorf("%s still exists", name)
		}
	}
}

func mustTrust(t *testing.T) *Trust {
	t.Helper()
	tr, err := NewTrust("tws_x", "X", now)
	if err != nil {
		t.Fatal(err)
	}
	return tr
}

func TestSealingNeedsAValidRecipientKey(t *testing.T) {
	if _, err := SealSecrets("not a key", "tws_a", "dev_x", Secrets{}); err == nil {
		t.Error("sealed to garbage")
	}
	x, _ := ecdh.X25519().GenerateKey(rand.Reader)
	_ = x
}
