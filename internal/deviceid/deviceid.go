// Package deviceid is how Werkbord names a device and checks that something came
// from it. It holds only what a verifier needs: the device's ID, its public
// signing key, and the checks on them. A device's private key is not here and
// never leaves its own computer: it lives in internal/deviceid/localidentity,
// which Team does not import (internal/archtest keeps it that way), so there is no
// code in Team that could hold or use one.
//
// The device identity is the *application* identity. It is a different key from
// the one a private network gives a node, on purpose: the network authenticates
// which machine a connection comes from; this identity authenticates which Werkbord
// device a request comes from, and is what internal/envelope signs with. A node
// that reaches the network without a registered device identity cannot get a
// request accepted, and a device identity does not let a request onto the network.
//
// The algorithm is Ed25519 from the standard library.
package deviceid

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Prefix marks a device ID, so one is recognisable in logs and URLs.
const Prefix = "dev"

// Limits on what people type.
const MaxNameLen = 80

// Errors.
var (
	ErrInvalid   = errors.New("deviceid: invalid")
	ErrBadSigner = errors.New("deviceid: signature does not match")
)

// ID is a device's stable, random identifier: "dev_" and 80 random bits in
// lower-case base 32.
type ID string

// ValidID reports whether s has the shape of a device ID.
func ValidID(s string) bool {
	rest, ok := strings.CutPrefix(s, Prefix+"_")
	if !ok || len(rest) != 16 {
		return false
	}
	for _, r := range rest {
		if !(r >= 'a' && r <= 'z' || r >= '2' && r <= '7') {
			return false
		}
	}
	return true
}

// PublicKey is a device's public signing key.
type PublicKey = ed25519.PublicKey

// EncodePublicKey is the text form of a key, as stored and sent: base64 (raw URL alphabet).
func EncodePublicKey(k PublicKey) string { return base64.RawURLEncoding.EncodeToString(k) }

// ParsePublicKey reads the text form of a public key.
func ParsePublicKey(s string) (PublicKey, error) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil || len(b) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("%w: not a public signing key", ErrInvalid)
	}
	return PublicKey(b), nil
}

// Fingerprint is a short, stable summary of a public key for people to compare:
// the first 16 hex digits of its SHA-256, in groups of four.
func Fingerprint(k PublicKey) string {
	sum := sha256.Sum256(k)
	h := hex.EncodeToString(sum[:8])
	return h[0:4] + "-" + h[4:8] + "-" + h[8:12] + "-" + h[12:16]
}

// CleanName checks a human-readable device name.
func CleanName(s string) (string, error) {
	s = strings.TrimSpace(s)
	switch {
	case s == "":
		return "", fmt.Errorf("%w: a device needs a name", ErrInvalid)
	case utf8.RuneCountInString(s) > MaxNameLen:
		return "", fmt.Errorf("%w: a device name is at most %d characters", ErrInvalid, MaxNameLen)
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return "", fmt.Errorf("%w: a device name has a control character", ErrInvalid)
		}
	}
	return s, nil
}

// Public is everything about a device that is safe to share: what a workspace
// records and what a verifier needs. It has no private key, and there is no way
// to put one in it.
type Public struct {
	ID        ID        `json:"id"`
	Name      string    `json:"name"`
	PublicKey string    `json:"publicKey"` // EncodePublicKey
	CreatedAt time.Time `json:"createdAt"`
}

// Key returns the parsed public key.
func (p Public) Key() (PublicKey, error) { return ParsePublicKey(p.PublicKey) }

// Validate checks the record is well formed.
func (p Public) Validate() error {
	if !ValidID(string(p.ID)) {
		return fmt.Errorf("%w: %q is not a device ID", ErrInvalid, p.ID)
	}
	if _, err := CleanName(p.Name); err != nil {
		return err
	}
	_, err := p.Key()
	return err
}

// Verify checks sig over msg against the device's public key.
func Verify(k PublicKey, msg, sig []byte) bool {
	return len(k) == ed25519.PublicKeySize && len(sig) == ed25519.SignatureSize && ed25519.Verify(k, msg, sig)
}

// ---- proof of possession ----

// registrationDomain separates a registration proof from every other signature.
const registrationDomain = "werkbord/device-registration/v1"

// RegistrationStatement is what a device signs to prove it holds the private key
// for the public key it is registering, bound to the workspace, the member and
// the name it is registering under, so a proof cannot be replayed to enrol the
// same key somewhere else. Fields are length-prefixed, so no two different
// registrations have the same bytes.
func RegistrationStatement(workspaceID, memberID string, id ID, name string, k PublicKey) []byte {
	var b []byte
	for _, f := range []string{registrationDomain, workspaceID, memberID, string(id), name, EncodePublicKey(k)} {
		b = append(b, byte(len(f)>>8), byte(len(f)))
		b = append(b, f...)
	}
	return b
}

// VerifyRegistration checks a proof of possession.
func VerifyRegistration(workspaceID, memberID string, p Public, proof []byte) error {
	k, err := p.Key()
	if err != nil {
		return err
	}
	if !Verify(k, RegistrationStatement(workspaceID, memberID, p.ID, p.Name, k), proof) {
		return fmt.Errorf("%w: the device did not prove it holds the key", ErrBadSigner)
	}
	return nil
}
