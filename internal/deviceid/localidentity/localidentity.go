// Package localidentity is a device's own identity: the private signing key and
// where it is kept. It runs on the device and nowhere else. Team never imports it
// (internal/archtest fails if it does), so no Team code can create, hold or use a
// device's key; Team only ever sees the public half (internal/deviceid.Public).
package localidentity

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base32"
	"errors"
	"fmt"
	"strings"
	"time"

	"devboard/internal/deviceid"
)

var idEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// NewID returns a new random device ID.
func NewID() deviceid.ID {
	var b [10]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("deviceid: crypto/rand failed: " + err.Error())
	}
	return deviceid.ID(deviceid.Prefix + "_" + strings.ToLower(idEncoding.EncodeToString(b[:])))
}

// Identity is this device's identity: its ID, name, creation time and signing key.
type Identity struct {
	id      deviceid.ID
	name    string
	created time.Time
	priv    ed25519.PrivateKey
}

// New creates an identity with a fresh random ID and key pair.
func New(name string, now time.Time) (*Identity, error) {
	name, err := deviceid.CleanName(name)
	if err != nil {
		return nil, err
	}
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("deviceid: generate key: %w", err)
	}
	return &Identity{id: NewID(), name: name, created: now.UTC().Truncate(time.Millisecond), priv: priv}, nil
}

// ID is the device's stable identifier.
func (i *Identity) ID() deviceid.ID { return i.id }

// Name is the device's human-readable name.
func (i *Identity) Name() string { return i.name }

// CreatedAt is when the identity was made.
func (i *Identity) CreatedAt() time.Time { return i.created }

// PublicKey is the verification key.
func (i *Identity) PublicKey() deviceid.PublicKey {
	return deviceid.PublicKey(i.priv.Public().(ed25519.PublicKey))
}

// Public is the shareable half of the identity.
func (i *Identity) Public() deviceid.Public {
	return deviceid.Public{ID: i.id, Name: i.name, PublicKey: deviceid.EncodePublicKey(i.PublicKey()), CreatedAt: i.created}
}

// Sign signs msg. The key is used for nothing but this.
func (i *Identity) Sign(msg []byte) []byte { return ed25519.Sign(i.priv, msg) }

// DeviceID is Sign's companion, so an *Identity can sign envelopes.
func (i *Identity) DeviceID() string { return string(i.id) }

// ProveRegistration signs the statement a workspace checks before it records the
// device's public key (see deviceid.VerifyRegistration).
func (i *Identity) ProveRegistration(workspaceID, memberID string) []byte {
	return i.Sign(deviceid.RegistrationStatement(workspaceID, memberID, i.id, i.name, i.PublicKey()))
}

// Rename changes the display name. The ID and the key stay.
func (i *Identity) Rename(name string) error {
	name, err := deviceid.CleanName(name)
	if err != nil {
		return err
	}
	i.name = name
	return nil
}

// String never prints the key.
func (i *Identity) String() string {
	return fmt.Sprintf("device %s (%s)", i.id, deviceid.Fingerprint(i.PublicKey()))
}

// GoString and Format keep the key out of %v and %#v.
func (i *Identity) GoString() string { return i.String() }

// ErrNotFound: no identity has been stored yet.
var ErrNotFound = errors.New("deviceid: no device identity stored")

// Store keeps an identity between runs. Where is an implementation's business;
// what every one owes is that the private key is readable only by this user, and
// that a stored identity is returned intact or not at all. FileStore is the
// implementation that exists; an operating-system keychain (the macOS Keychain,
// Windows DPAPI, the Secret Service) is a second Store behind the same interface,
// and nothing else changes when one is added.
type Store interface {
	// Load returns the stored identity, or ErrNotFound.
	Load() (*Identity, error)
	// Save stores the identity, replacing any other.
	Save(*Identity) error
}

// LoadOrCreate returns the stored identity, making and storing one named name if
// there is none. A stored identity that cannot be read is an error, never
// replaced: silently minting a new identity would orphan the device's registration.
func LoadOrCreate(s Store, name string, now time.Time) (*Identity, bool, error) {
	id, err := s.Load()
	if err == nil {
		return id, false, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return nil, false, err
	}
	id, err = New(name, now)
	if err != nil {
		return nil, false, err
	}
	if err := s.Save(id); err != nil {
		return nil, false, err
	}
	return id, true, nil
}
