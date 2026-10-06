package enrollment

import (
	"crypto"
	"crypto/ed25519"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"time"
)

// ServerCert is what a workspace's bootstrap endpoint presents: its certificate
// chain (leaf first, then the workspace's root certificate) in DER, and the leaf's
// private key. The leaf is signed by the workspace key, so possessing one is
// possessing the workspace's authority to speak for it.
type ServerCert struct {
	ChainDER [][]byte
	Key      crypto.Signer
}

func (c ServerCert) tls() (*tls.Certificate, error) {
	if len(c.ChainDER) == 0 || c.Key == nil {
		return nil, errors.New("enrollment: no server certificate")
	}
	return &tls.Certificate{Certificate: c.ChainDER, PrivateKey: c.Key}, nil
}

// serverTLS is the server's TLS configuration: 1.3 only, the workspace's chain, no
// client certificates (a device has none yet; it proves itself with a signature).
func serverTLS(source func() (ServerCert, error)) *tls.Config {
	return &tls.Config{
		MinVersion: tls.VersionTLS13,
		NextProtos: []string{"http/1.1"},
		GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
			c, err := source()
			if err != nil {
				return nil, err
			}
			return c.tls()
		},
	}
}

// pinnedTLS is the client's TLS configuration for dialling host: 1.3 only, and the
// server is accepted only if its chain leads to the workspace key in pin.
//
// Go's own verification is switched off because it can only anchor on a certificate
// the client already holds, and the client holds only the workspace's public key; the
// check below is the whole of it, done with crypto/x509 and not by hand: it finds the
// root the server presented, requires that root to be the pinned key and to be a
// valid self-signed authority, then asks x509 to verify the leaf against it for TLS
// server authentication at now, and for the host that was dialled.
func pinnedTLS(pin ed25519.PublicKey, host string, now func() time.Time) *tls.Config {
	return &tls.Config{
		MinVersion:         tls.VersionTLS13,
		NextProtos:         []string{"http/1.1"},
		InsecureSkipVerify: true, //nolint:gosec // replaced by VerifyPeerCertificate below, which is stricter
		VerifyPeerCertificate: func(raw [][]byte, _ [][]*x509.Certificate) error {
			return verifyPinned(raw, pin, host, now())
		},
	}
}

// errNotWorkspace is deliberately uninformative: a client that dialled the wrong
// machine learns that it is the wrong machine and nothing else.
var errNotWorkspace = errors.New("enrollment: the server is not the workspace the invitation names")

func verifyPinned(raw [][]byte, pin ed25519.PublicKey, host string, now time.Time) error {
	if len(raw) == 0 || len(raw) > 4 {
		return errNotWorkspace
	}
	certs := make([]*x509.Certificate, len(raw))
	for i, der := range raw {
		c, err := x509.ParseCertificate(der)
		if err != nil {
			return errNotWorkspace
		}
		certs[i] = c
	}
	leaf := certs[0]
	var root *x509.Certificate
	inter := x509.NewCertPool()
	for _, c := range certs[1:] {
		if k, ok := c.PublicKey.(ed25519.PublicKey); ok && k.Equal(pin) {
			root = c
		} else {
			inter.AddCert(c)
		}
	}
	if root == nil || !root.IsCA || root.CheckSignatureFrom(root) != nil {
		return errNotWorkspace
	}
	roots := x509.NewCertPool()
	roots.AddCert(root)
	if _, err := leaf.Verify(x509.VerifyOptions{Roots: roots, Intermediates: inter, CurrentTime: now, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}); err != nil {
		return errNotWorkspace
	}
	if err := leaf.VerifyHostname(host); err != nil {
		return errNotWorkspace
	}
	return nil
}

// exporter is the channel-binding value of a TLS 1.3 connection, equal at both ends.
func exporter(cs tls.ConnectionState) ([]byte, error) {
	if cs.Version != tls.VersionTLS13 {
		return nil, fmt.Errorf("enrollment: TLS 1.3 is required")
	}
	return cs.ExportKeyingMaterial(ExporterLabel, nil, ExporterBytes)
}
