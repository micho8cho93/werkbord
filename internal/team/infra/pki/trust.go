package pki

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"errors"
	"fmt"
	"math/big"
	"net"
	"strings"
	"time"

	"devboard/internal/enrollment"
)

// Lifetimes. The workspace's root certificate is long-lived because its identity is
// the key, not the certificate (a new certificate for the same key is the same
// workspace). A bootstrap endpoint's certificate is short-lived: whoever holds a
// leaf and its key can pass for that endpoint until it expires, so a host that is
// removed stops being able to for days, not years.
const (
	rootValidity = 20 * 365 * 24 * time.Hour
	// ServerCertValidity is how long a bootstrap endpoint's certificate lasts; a host
	// reissues it when under a third of that remains.
	ServerCertValidity = 72 * time.Hour
	clockSkew          = 5 * time.Minute
)

// Trust is the workspace's identity: an Ed25519 key, and the root certificate for it
// that bootstrap endpoints' certificates chain to. Whoever holds the private key can
// make invitations and endpoints that devices will believe, so it exists only on
// Workspace Hosts.
type Trust struct {
	WorkspaceID string
	priv        ed25519.PrivateKey
	rootDER     []byte
	root        *x509.Certificate
}

// NewTrust creates the identity of a new workspace.
func NewTrust(workspaceID, workspaceName string, now time.Time) (*Trust, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	t := &Trust{WorkspaceID: workspaceID, priv: priv}
	if err := t.issueRoot(pub, workspaceName, now); err != nil {
		return nil, err
	}
	return t, nil
}

// TrustFromSeed rebuilds the identity from its private key seed (what a Vault stores,
// and what is handed to a new Workspace Host), reissuing the root certificate.
func TrustFromSeed(workspaceID, workspaceName string, seed []byte, now time.Time) (*Trust, error) {
	if len(seed) != ed25519.SeedSize {
		return nil, errors.New("pki: not a workspace key seed")
	}
	priv := ed25519.NewKeyFromSeed(seed)
	t := &Trust{WorkspaceID: workspaceID, priv: priv}
	if err := t.issueRoot(priv.Public().(ed25519.PublicKey), workspaceName, now); err != nil {
		return nil, err
	}
	return t, nil
}

func (t *Trust) issueRoot(pub ed25519.PublicKey, name string, now time.Time) error {
	tmpl := &x509.Certificate{
		SerialNumber:          serial(),
		Subject:               pkix.Name{CommonName: "Werkbord workspace " + t.WorkspaceID, Organization: []string{name}},
		NotBefore:             now.Add(-clockSkew),
		NotAfter:              now.Add(rootValidity),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            0,
		MaxPathLenZero:        true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, pub, t.priv)
	if err != nil {
		return err
	}
	root, err := x509.ParseCertificate(der)
	if err != nil {
		return err
	}
	t.rootDER, t.root = der, root
	return nil
}

func serial() *big.Int {
	n, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	if err != nil {
		panic("pki: crypto/rand failed: " + err.Error())
	}
	return n.Add(n, big.NewInt(1))
}

// PublicKey is the workspace's public key.
func (t *Trust) PublicKey() ed25519.PublicKey { return t.priv.Public().(ed25519.PublicKey) }

// PublicKeyText is the key as it appears in invitations.
func (t *Trust) PublicKeyText() string { return base64.RawURLEncoding.EncodeToString(t.PublicKey()) }

// Fingerprint is the workspace's public fingerprint, what a device pins.
func (t *Trust) Fingerprint() string { return enrollment.WorkspaceFingerprint(t.PublicKey()) }

// Seed is the private key's seed. It is the secret; callers seal it before it touches disk.
func (t *Trust) Seed() []byte { return t.priv.Seed() }

// RootDER is the root certificate.
func (t *Trust) RootDER() []byte { return t.rootDER }

// SignInvitation signs an invitation with the workspace key.
func (t *Trust) SignInvitation(inv enrollment.Invitation) (string, error) {
	inv.WorkspaceID = t.WorkspaceID
	inv.WorkspaceKey = t.PublicKeyText()
	inv.Fingerprint = t.Fingerprint()
	return enrollment.Sign(inv, t.priv)
}

// ServerCertificate is a bootstrap endpoint's certificate, with the key that goes with it.
type ServerCertificate struct {
	enrollment.ServerCert
	NotAfter time.Time
}

// Fresh reports whether the certificate has more than a third of its life left at
// now (and so does not need replacing yet).
func (c ServerCertificate) Fresh(now time.Time) bool {
	return now.Add(ServerCertValidity / 3).Before(c.NotAfter)
}

// IssueServerCertificate makes a certificate for a bootstrap endpoint reachable at
// hosts (IP addresses and names, as the invitation names them), with a new key of its
// own. The certificate is signed by the workspace key and so chains to the root a
// device pins.
func (t *Trust) IssueServerCertificate(hosts []string, now time.Time) (ServerCertificate, error) {
	if len(hosts) == 0 {
		return ServerCertificate{}, errors.New("pki: a bootstrap certificate needs the addresses it is reached at")
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return ServerCertificate{}, err
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial(),
		Subject:      pkix.Name{CommonName: "Werkbord workspace bootstrap", Organization: []string{t.WorkspaceID}},
		NotBefore:    now.Add(-clockSkew),
		NotAfter:     now.Add(ServerCertValidity),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	seen := map[string]bool{}
	for _, h := range hosts {
		h = strings.Trim(strings.TrimSpace(h), "[]")
		if h == "" || seen[h] {
			continue
		}
		seen[h] = true
		if ip := net.ParseIP(h); ip != nil {
			tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
		} else {
			tmpl.DNSNames = append(tmpl.DNSNames, h)
		}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, t.root, pub, t.priv)
	if err != nil {
		return ServerCertificate{}, err
	}
	return ServerCertificate{
		ServerCert: enrollment.ServerCert{ChainDER: [][]byte{der, t.rootDER}, Key: priv},
		NotAfter:   tmpl.NotAfter,
	}, nil
}

// HostsOf returns the host parts of "host:port" endpoints, for IssueServerCertificate.
func HostsOf(endpoints []string) ([]string, error) {
	var out []string
	for _, e := range endpoints {
		h, _, err := net.SplitHostPort(e)
		if err != nil {
			return nil, fmt.Errorf("pki: endpoint %q is not host:port", e)
		}
		out = append(out, h)
	}
	return out, nil
}
