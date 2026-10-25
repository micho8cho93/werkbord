package entitlement

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"
)

// An unlock key is how a person's Werkbord knows it is Premium. It is one string, signed offline by the person who sells
// Premium and checked here against a public key that is stamped into the build. It never expires, names no seats, no team and
// no customer account, and needs no server: pasting it into the app once is all there is.
//
//	wbpremium-<payload>.<signature>        both unpadded URL base64
//
// The payload is the claims as JSON in a fixed field order; the signature covers a domain-separation prefix, a NUL byte and
// those exact bytes.

// KeyPrefix starts every unlock key, so a pasted key can be told from anything else at a glance.
const KeyPrefix = "wbpremium-"

const (
	// Schema is the only claims layout there is.
	Schema = 1
	// Product names what the key unlocks.
	Product = "werkbord"

	signingDomain = "werkbord/premium/v1"
	maxKeyLength  = 1024
	maxIDLength   = 64
	maxHolder     = 80
)

// Claims are what a key says. Holder is a label for the person to recognise their own key by (a name or an email address
// they gave when buying); it grants nothing and is shown only to them.
type Claims struct {
	Schema   int       `json:"schema"`
	Product  string    `json:"product"`
	ID       string    `json:"id"`
	Holder   string    `json:"holder,omitempty"`
	IssuedAt time.Time `json:"issuedAt"`
}

// Errors a key can fail with. They are for the person pasting it, so they say what is wrong in words.
var (
	// ErrNoIssuer: this build has no public key to check a key against, so no key can unlock it.
	ErrNoIssuer = errors.New("this build of Werkbord cannot check unlock keys")
	// ErrMalformed: the text is not an unlock key.
	ErrMalformed = errors.New("that is not a Werkbord unlock key")
	// ErrBadSignature: the key is the right shape but was not issued for Werkbord, or was changed.
	ErrBadSignature = errors.New("this unlock key is not valid: it was not issued for Werkbord or it was changed")
)

func validate(c Claims) error {
	if c.Schema != Schema || c.Product != Product || !validID(c.ID) || len(c.Holder) > maxHolder || c.IssuedAt.IsZero() {
		return ErrMalformed
	}
	for _, r := range c.Holder {
		if r < ' ' || r == 0x7f {
			return ErrMalformed
		}
	}
	return nil
}

func validID(id string) bool {
	if id == "" || len(id) > maxIDLength {
		return false
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

func canonical(c Claims) ([]byte, error) {
	c.IssuedAt = c.IssuedAt.UTC().Truncate(time.Second)
	if err := validate(c); err != nil {
		return nil, err
	}
	return json.Marshal(c)
}

func signedBytes(payload []byte) []byte {
	return append([]byte(signingDomain+"\x00"), payload...)
}

// Issue makes an unlock key. It is for the separate offline tool that sells Premium (cmd/werkbord-team/vendor); nothing that
// ships calls it. Claims are fixed up (Schema and Product are set) so a caller names only the key's ID, its holder and a date.
func Issue(c Claims, key ed25519.PrivateKey) (string, error) {
	if len(key) != ed25519.PrivateKeySize {
		return "", errors.New("an Ed25519 signing key is required")
	}
	c.Schema, c.Product = Schema, Product
	payload, err := canonical(c)
	if err != nil {
		return "", err
	}
	sig := ed25519.Sign(key, signedBytes(payload))
	return KeyPrefix + base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

// Verify checks a pasted key against the issuer's public key and returns what it says. Spaces and line breaks around the key,
// which a paste often brings, are ignored.
func Verify(text string, issuer ed25519.PublicKey) (Claims, error) {
	if len(issuer) != ed25519.PublicKeySize {
		return Claims{}, ErrNoIssuer
	}
	text = strings.TrimSpace(text)
	body, ok := strings.CutPrefix(text, KeyPrefix)
	if !ok || len(text) > maxKeyLength {
		return Claims{}, ErrMalformed
	}
	p, s, ok := strings.Cut(body, ".")
	if !ok {
		return Claims{}, ErrMalformed
	}
	payload, err := base64.RawURLEncoding.DecodeString(p)
	if err != nil {
		return Claims{}, ErrMalformed
	}
	sig, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return Claims{}, ErrMalformed
	}
	if !ed25519.Verify(issuer, signedBytes(payload), sig) {
		return Claims{}, ErrBadSignature
	}
	// Only now is the payload read, strictly: no unknown or repeated fields, and exactly the bytes a signer would have made.
	var c Claims
	dec := json.NewDecoder(bytes.NewReader(payload))
	dec.DisallowUnknownFields()
	if dec.Decode(&c) != nil || dec.Decode(new(any)) != io.EOF {
		return Claims{}, ErrMalformed
	}
	again, err := canonical(c)
	if err != nil || !bytes.Equal(again, payload) {
		return Claims{}, ErrMalformed
	}
	return c, nil
}
