package pki

import (
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base32"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/slackhq/nebula/cert"
	"golang.org/x/crypto/curve25519"

	"devboard/internal/deviceid"
)

// HostKeys are the keys a host machine has of its own: three, for three purposes,
// and no two the same.
//
//   - the application key (Ed25519) is how the host is a registered device: it proves
//     possession at enrollment and is what the registry knows it by;
//   - the network key (X25519) is the private half of the key the network authority
//     certifies for the host's node;
//   - the sealing key (X25519) is what secrets are encrypted to when another Workspace
//     Host hands this one the workspace's authority.
//
// They belong to a host, which Team registers as a device. They are not a runner's
// identity: a person's own Werkbord has its own, in internal/deviceid/localidentity,
// which Team never links.
type HostKeys struct {
	id      deviceid.ID
	app     ed25519.PrivateKey
	netPriv []byte
	seal    *ecdh.PrivateKey
}

// NewDeviceID makes a device ID of the shape internal/deviceid defines.
func NewDeviceID() deviceid.ID {
	var b [10]byte
	if _, err := io.ReadFull(rand.Reader, b[:]); err != nil {
		panic("pki: crypto/rand failed: " + err.Error())
	}
	return deviceid.ID(deviceid.Prefix + "_" + strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b[:])))
}

// NewHostKeys makes a host's keys.
func NewHostKeys() (*HostKeys, error) {
	_, app, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	net := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, net); err != nil {
		return nil, err
	}
	seal, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	return &HostKeys{id: NewDeviceID(), app: app, netPriv: net, seal: seal}, nil
}

// DeviceID, PublicKey and Sign make HostKeys an enrollment.Signer.
func (k *HostKeys) DeviceID() string { return string(k.id) }

// PublicKey is the host's application public key.
func (k *HostKeys) PublicKey() ed25519.PublicKey { return k.app.Public().(ed25519.PublicKey) }

// Sign signs with the host's application key.
func (k *HostKeys) Sign(msg []byte) []byte { return ed25519.Sign(k.app, msg) }

// NetworkKeyPEMs are the host's network key pair, in the form the network program reads.
func (k *HostKeys) NetworkKeyPEMs() (privatePEM, publicPEM []byte, err error) {
	pub, err := curve25519.X25519(k.netPriv, curve25519.Basepoint)
	if err != nil {
		return nil, nil, err
	}
	return cert.MarshalPrivateKeyToPEM(cert.Curve_CURVE25519, k.netPriv), cert.MarshalPublicKeyToPEM(cert.Curve_CURVE25519, pub), nil
}

// SealingPublicKey is the key secrets are sealed to, as it travels in an enrollment request.
func (k *HostKeys) SealingPublicKey() string {
	return base64.RawURLEncoding.EncodeToString(k.seal.PublicKey().Bytes())
}

type hostKeysFile struct {
	ID      string `json:"id"`
	App     []byte `json:"app"` // the Ed25519 seed
	NetPriv []byte `json:"net"`
	Seal    []byte `json:"seal"`
}

func (k *HostKeys) marshal() ([]byte, error) {
	return json.Marshal(hostKeysFile{ID: string(k.id), App: k.app.Seed(), NetPriv: k.netPriv, Seal: k.seal.Bytes()})
}

func unmarshalHostKeys(b []byte) (*HostKeys, error) {
	var f hostKeysFile
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, err
	}
	if !deviceid.ValidID(f.ID) || len(f.App) != ed25519.SeedSize || len(f.NetPriv) != 32 {
		return nil, errors.New("pki: the host's key file is not valid")
	}
	seal, err := ecdh.X25519().NewPrivateKey(f.Seal)
	if err != nil {
		return nil, fmt.Errorf("pki: the host's sealing key: %w", err)
	}
	return &HostKeys{id: deviceid.ID(f.ID), app: ed25519.NewKeyFromSeed(f.App), netPriv: f.NetPriv, seal: seal}, nil
}
