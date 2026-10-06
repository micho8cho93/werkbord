package pki

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"time"

	"github.com/slackhq/nebula/cert"
	"golang.org/x/crypto/curve25519"
)

// Groups are what a certificate says its holder is. The network's policy is written
// in them (internal/team/infra/overlay): a device is allowed to reach what its group
// allows and nothing else. They are baked into the certificate by the authority, so a
// device cannot give itself another.
const (
	GroupWorkspaceHost    = "workspace-host"
	GroupConnectivityHost = "connectivity-host"
	GroupMember           = "member"
	GroupRunner           = "runner"
)

// Groups lists the groups the authority may put in a certificate. The authority's own
// certificate is limited to these, so even a stolen signing key cannot mint a group
// that policy does not know.
func Groups() []string {
	return []string{GroupWorkspaceHost, GroupConnectivityHost, GroupMember, GroupRunner}
}

const (
	// CAValidity is how long the network authority lasts. Replacing it is a planned
	// migration (both authorities trusted for a while), documented in TEAM_NETWORK.md.
	CAValidity = 10 * 365 * 24 * time.Hour
	// DefaultNodeValidity is how long a device's certificate lasts. It is short, so that
	// a device whose membership has ended falls off the network by itself even if
	// nothing else tells the network, and a device that is working renews quietly.
	DefaultNodeValidity = 30 * 24 * time.Hour
	// MaxNodeValidity bounds what a caller may ask for.
	MaxNodeValidity = 90 * 24 * time.Hour
)

// ErrOutsideNetwork etc. are the refusals of Issue.
var (
	ErrBadNodeRequest = errors.New("pki: the certificate request is not valid")
)

// NetworkCA is the private network's certificate authority: it signs the certificates
// that let a device onto the network. Its private key is the most sensitive secret a
// workspace has, and only Workspace Hosts hold it.
type NetworkCA struct {
	cert   cert.Certificate
	key    []byte // Ed25519 private key, 64 bytes
	prefix netip.Prefix
}

// NewNetworkCA creates a network authority for a private network of the given
// address range (e.g. 10.201.0.0/16). The range is part of the authority's
// certificate, so it can only ever sign addresses inside it.
func NewNetworkCA(workspaceName string, prefix netip.Prefix, now time.Time) (*NetworkCA, error) {
	if err := CheckNetworkRange(prefix); err != nil {
		return nil, err
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	tbs := &cert.TBSCertificate{
		Version:   cert.Version2,
		Name:      "Werkbord " + workspaceName,
		Networks:  []netip.Prefix{prefix},
		Groups:    Groups(),
		IsCA:      true,
		NotBefore: now.Add(-clockSkew),
		NotAfter:  now.Add(CAValidity),
		PublicKey: pub,
		Curve:     cert.Curve_CURVE25519,
	}
	c, err := tbs.Sign(nil, cert.Curve_CURVE25519, priv)
	if err != nil {
		return nil, err
	}
	return &NetworkCA{cert: c, key: priv, prefix: prefix}, nil
}

var privateRanges = []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8"), netip.MustParsePrefix("172.16.0.0/12"), netip.MustParsePrefix("192.168.0.0/16")}

// CheckNetworkRange says whether prefix can be a workspace's private network: an
// IPv4 range inside the private address blocks, between a /16 and a /24. Not larger,
// because every node routes its whole range into the network: a /8 would take every
// 10.x address on the customer's own LAN with it. Not outside the private blocks,
// because it would shadow real Internet addresses.
func CheckNetworkRange(prefix netip.Prefix) error {
	ok := false
	for _, p := range privateRanges {
		ok = ok || (prefix.IsValid() && prefix.Addr().Is4() && p.Contains(prefix.Addr()) && prefix.Bits() >= p.Bits())
	}
	if !ok || prefix.Bits() < 16 || prefix.Bits() > 24 || prefix.Masked() != prefix {
		return fmt.Errorf("pki: %v is not a usable network range (a private IPv4 range between a /16 and a /24, such as 10.201.0.0/16)", prefix)
	}
	return nil
}

// NetworkCAFromPEM rebuilds an authority from its certificate and its private key,
// checking they belong together.
func NetworkCAFromPEM(certPEM, keyPEM []byte) (*NetworkCA, error) {
	c, _, err := cert.UnmarshalCertificateFromPEM(certPEM)
	if err != nil {
		return nil, fmt.Errorf("pki: the authority's certificate: %w", err)
	}
	if !c.IsCA() || len(c.Networks()) != 1 {
		return nil, errors.New("pki: not a network authority's certificate")
	}
	key, _, curve, err := cert.UnmarshalSigningPrivateKeyFromPEM(keyPEM)
	if err != nil {
		return nil, fmt.Errorf("pki: the authority's key: %w", err)
	}
	if err := c.VerifyPrivateKey(curve, key); err != nil {
		return nil, errors.New("pki: the authority's key is not the one for its certificate")
	}
	return &NetworkCA{cert: c, key: key, prefix: c.Networks()[0]}, nil
}

// CertificatePEM is the authority's certificate: public, and what every node trusts.
func (ca *NetworkCA) CertificatePEM() ([]byte, error) { return ca.cert.MarshalPEM() }

// PrivateKeyPEM is the authority's private key. It is the secret; callers seal it.
func (ca *NetworkCA) PrivateKeyPEM() []byte {
	return cert.MarshalSigningPrivateKeyToPEM(cert.Curve_CURVE25519, ca.key)
}

// Prefix is the network's address range.
func (ca *NetworkCA) Prefix() netip.Prefix { return ca.prefix }

// Fingerprint identifies the authority.
func (ca *NetworkCA) Fingerprint() (string, error) { return ca.cert.Fingerprint() }

// NodeRequest is what the authority is asked to certify.
type NodeRequest struct {
	// Name is the certificate's name: the device's ID.
	Name string
	// Addr is the device's address on the network, inside the authority's range.
	Addr netip.Addr
	// Groups are drawn from Groups().
	Groups []string
	// PublicKeyPEM is the device's network public key (it made the pair; the private
	// half never reaches the authority).
	PublicKeyPEM []byte
	// TTL is how long the certificate lasts (DefaultNodeValidity if zero).
	TTL time.Duration
}

// Issued is a signed node certificate.
type Issued struct {
	CertPEM     []byte
	Fingerprint string
	NotAfter    time.Time
	Addr        netip.Addr
}

// Issue signs a certificate for a device's network key. The authority decides the
// name, address and groups; the device decides only its key.
func (ca *NetworkCA) Issue(req NodeRequest, now time.Time) (Issued, error) {
	bad := func(format string, a ...any) (Issued, error) {
		return Issued{}, fmt.Errorf("%w: %s", ErrBadNodeRequest, fmt.Sprintf(format, a...))
	}
	if req.Name == "" || len(req.Name) > 80 {
		return bad("a name is needed")
	}
	if !req.Addr.IsValid() || !ca.prefix.Contains(req.Addr) || req.Addr == ca.prefix.Addr() {
		return bad("address %v is not inside the network %v", req.Addr, ca.prefix)
	}
	if len(req.Groups) == 0 {
		return bad("a certificate needs at least one group")
	}
	allowed := map[string]bool{}
	for _, g := range Groups() {
		allowed[g] = true
	}
	for _, g := range req.Groups {
		if !allowed[g] {
			return bad("group %q does not exist", g)
		}
	}
	pub, _, curve, err := cert.UnmarshalPublicKeyFromPEM(req.PublicKeyPEM)
	if err != nil || curve != cert.Curve_CURVE25519 || len(pub) != 32 {
		return bad("the device's network public key")
	}
	ttl := req.TTL
	if ttl == 0 {
		ttl = DefaultNodeValidity
	}
	if ttl < time.Hour || ttl > MaxNodeValidity {
		return bad("a certificate lasts between an hour and %d days", int(MaxNodeValidity/(24*time.Hour)))
	}
	notAfter := now.Add(ttl)
	if notAfter.After(ca.cert.NotAfter()) {
		notAfter = ca.cert.NotAfter()
	}
	tbs := &cert.TBSCertificate{
		Version:   cert.Version2,
		Name:      req.Name,
		Networks:  []netip.Prefix{netip.PrefixFrom(req.Addr, ca.prefix.Bits())},
		Groups:    req.Groups,
		NotBefore: now.Add(-clockSkew),
		NotAfter:  notAfter,
		PublicKey: pub,
		Curve:     cert.Curve_CURVE25519,
	}
	c, err := tbs.Sign(ca.cert, cert.Curve_CURVE25519, ca.key)
	if err != nil {
		return Issued{}, err
	}
	pemBytes, err := c.MarshalPEM()
	if err != nil {
		return Issued{}, err
	}
	fp, err := c.Fingerprint()
	if err != nil {
		return Issued{}, err
	}
	return Issued{CertPEM: pemBytes, Fingerprint: fp, NotAfter: notAfter, Addr: req.Addr}, nil
}

// CertInfo is what can be read from a node certificate.
type CertInfo struct {
	Name        string
	Addr        netip.Addr
	Groups      []string
	Fingerprint string
	NotBefore   time.Time
	NotAfter    time.Time
}

// VerifyNode checks that a node certificate was signed by the authority and is valid
// at now, and returns what it says. A certificate from another authority, a changed
// one, an expired one and a CA certificate are all refused.
func (ca *NetworkCA) VerifyNode(nodePEM []byte, now time.Time) (CertInfo, error) {
	c, _, err := cert.UnmarshalCertificateFromPEM(nodePEM)
	if err != nil {
		return CertInfo{}, err
	}
	pool := cert.NewCAPool()
	if err := pool.AddCA(ca.cert); err != nil {
		return CertInfo{}, err
	}
	if _, err := pool.VerifyCertificate(now, c); err != nil {
		return CertInfo{}, err
	}
	return infoOf(c)
}

// ReadNodeCert reads a node certificate without judging it.
func ReadNodeCert(nodePEM []byte) (CertInfo, error) {
	c, _, err := cert.UnmarshalCertificateFromPEM(nodePEM)
	if err != nil {
		return CertInfo{}, err
	}
	return infoOf(c)
}

func infoOf(c cert.Certificate) (CertInfo, error) {
	fp, err := c.Fingerprint()
	if err != nil {
		return CertInfo{}, err
	}
	info := CertInfo{Name: c.Name(), Groups: c.Groups(), Fingerprint: fp, NotBefore: c.NotBefore(), NotAfter: c.NotAfter()}
	if n := c.Networks(); len(n) > 0 {
		info.Addr = n[0].Addr()
	}
	return info, nil
}

// GenerateNodeKey makes a device's network key pair (X25519, in Nebula's PEM form).
// The device keeps the private half; the public half goes in the enrollment request.
func GenerateNodeKey() (privatePEM, publicPEM []byte, err error) {
	priv := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, priv); err != nil {
		return nil, nil, err
	}
	pub, err := curve25519.X25519(priv, curve25519.Basepoint)
	if err != nil {
		return nil, nil, err
	}
	return cert.MarshalPrivateKeyToPEM(cert.Curve_CURVE25519, priv), cert.MarshalPublicKeyToPEM(cert.Curve_CURVE25519, pub), nil
}
