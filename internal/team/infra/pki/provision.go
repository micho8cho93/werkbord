package pki

import (
	"crypto/ecdh"
	"crypto/hpke"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"devboard/internal/enrollment"
)

// Secrets are what a Workspace Host holds that no other device does: the workspace's
// private key and the network authority's private key and certificate. They are the
// "minimum host signing material": exactly what a second host needs to take over
// issuing invitations, bootstrap certificates and node certificates if the first is
// gone, and nothing it does not (not the first host's own keys, not any member's
// token, not any device's private key: those were never here).
//
// They exist in this form only in transit between two hosts. At rest they are sealed
// (Vault).
type Secrets struct {
	Version       int    `json:"v"`
	WorkspaceID   string `json:"workspaceId"`
	WorkspaceName string `json:"workspaceName"`
	// TrustSeed is the workspace key's private seed.
	TrustSeed []byte `json:"trustSeed"`
	// NetworkCACertificate and NetworkCAKey are the authority's, in PEM.
	NetworkCACertificate []byte `json:"networkCaCertificate"`
	NetworkCAKey         []byte `json:"networkCaKey"`
}

const secretsVersion = 1

// hpke suite: DHKEM(X25519, HKDF-SHA256), HKDF-SHA256, ChaCha20-Poly1305. One-shot
// base mode, with the recipient and workspace bound in as info.
func secretsInfo(workspaceID, deviceID string) []byte {
	return []byte("werkbord/host-secrets/v1\x00" + workspaceID + "\x00" + deviceID)
}

// SealSecrets encrypts secrets to a host's sealing public key (as it sent it in its
// enrollment request, and as the registry holds it), for that device only: the
// ciphertext is useless to anyone else, and to the same host under another device ID.
// Whoever carries it, an API response included, learns nothing.
func SealSecrets(recipientSealingKey, workspaceID, recipientDeviceID string, s Secrets) ([]byte, error) {
	raw, err := base64.RawURLEncoding.DecodeString(recipientSealingKey)
	if err != nil {
		return nil, errors.New("pki: the recipient's sealing key is not valid")
	}
	pub, err := ecdh.X25519().NewPublicKey(raw)
	if err != nil {
		return nil, errors.New("pki: the recipient's sealing key is not valid")
	}
	hk, err := hpke.NewDHKEMPublicKey(pub)
	if err != nil {
		return nil, err
	}
	s.Version = secretsVersion
	s.WorkspaceID = workspaceID
	pt, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	return hpke.Seal(hk, hpke.HKDFSHA256(), hpke.ChaCha20Poly1305(), secretsInfo(workspaceID, recipientDeviceID), pt)
}

// OpenSecrets decrypts what SealSecrets sealed to this host, and checks it is what
// the host expects before returning it: that the workspace key is the one whose
// fingerprint the host pinned when it enrolled, and that the authority's key matches
// the authority certificate the host was given. A host handed secrets for another
// workspace, or another authority, refuses them.
func (k *HostKeys) OpenSecrets(now time.Time, workspaceID, pinnedFingerprint, expectedCACert string, sealed []byte) (Secrets, error) {
	hk, err := hpke.NewDHKEMPrivateKey(k.seal)
	if err != nil {
		return Secrets{}, err
	}
	pt, err := hpke.Open(hk, hpke.HKDFSHA256(), hpke.ChaCha20Poly1305(), secretsInfo(workspaceID, k.DeviceID()), sealed)
	if err != nil {
		return Secrets{}, ErrCannotOpen
	}
	var s Secrets
	if err := json.Unmarshal(pt, &s); err != nil || s.Version != secretsVersion || s.WorkspaceID != workspaceID {
		return Secrets{}, errors.New("pki: the secrets are not for this workspace")
	}
	t, err := TrustFromSeed(s.WorkspaceID, s.WorkspaceName, s.TrustSeed, now)
	if err != nil {
		return Secrets{}, err
	}
	if !enrollment.SameFingerprint(t.Fingerprint(), pinnedFingerprint) {
		return Secrets{}, errors.New("pki: the workspace key in the secrets is not the one this host enrolled with")
	}
	ca, err := NetworkCAFromPEM(s.NetworkCACertificate, s.NetworkCAKey)
	if err != nil {
		return Secrets{}, err
	}
	have, _ := ca.CertificatePEM()
	if expectedCACert != "" && string(have) != expectedCACert {
		return Secrets{}, fmt.Errorf("pki: the network authority in the secrets is not the one this host enrolled with")
	}
	return s, nil
}
