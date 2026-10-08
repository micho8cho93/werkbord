// Package license verifies offline Team licenses. Only the issuer's public key ships with Team.
package license

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"time"
)

// Claims are signed verbatim, under a domain separated signature.
type Claims struct {
	Schema        int       `json:"schema,omitempty"`
	Product       string    `json:"product"`
	ID            string    `json:"id"`
	Customer      string    `json:"customer"`
	Edition       string    `json:"edition,omitempty"`
	Seats         int       `json:"seats"`
	IssuedAt      time.Time `json:"issuedAt"`
	ExpiresAt     time.Time `json:"expiresAt,omitzero"`
	SupportEndsAt time.Time `json:"supportEndsAt,omitzero"`
}

const Schema = 2
const EditionTeam = "team"

// CanonicalClaims is the only payload new issuers sign: a fixed field order,
// UTC whole-second dates, no unknown fields, and optional dates omitted when absent.
func CanonicalClaims(c Claims) ([]byte, error) {
	if c.Schema != Schema || c.Edition != EditionTeam {
		return nil, errors.New("unsupported license schema or edition")
	}
	if err := validate(c); err != nil {
		return nil, err
	}
	c.IssuedAt = c.IssuedAt.UTC().Truncate(time.Second)
	if !c.ExpiresAt.IsZero() {
		c.ExpiresAt = c.ExpiresAt.UTC().Truncate(time.Second)
	}
	if !c.SupportEndsAt.IsZero() {
		c.SupportEndsAt = c.SupportEndsAt.UTC().Truncate(time.Second)
	}
	if err := validate(c); err != nil {
		return nil, err
	}
	return json.Marshal(c)
}

// Issue is for the separate offline vendor tool. Customer applications carry only a public key.
func Issue(c Claims, key ed25519.PrivateKey) ([]byte, error) {
	if len(key) != ed25519.PrivateKeySize {
		return nil, errors.New("an Ed25519 signing key is required")
	}
	raw, err := CanonicalClaims(c)
	if err != nil {
		return nil, err
	}
	return json.Marshal(document{Claims: raw, Signature: base64.RawURLEncoding.EncodeToString(ed25519.Sign(key, SigningBytes(raw)))})
}

type document struct {
	Claims    json.RawMessage `json:"claims"`
	Signature string          `json:"signature"`
}

// SigningBytes is the documented format used by an offline license issuer.
func SigningBytes(claims []byte) []byte {
	var c struct {
		Schema int `json:"schema"`
	}
	if json.Unmarshal(claims, &c) == nil && c.Schema == Schema {
		return append([]byte("werkbord-team/license/v2\x00"), claims...)
	}
	return append([]byte("werkbord-team/license/v1\x00"), claims...)
}

// Verify checks a signed license without contacting an account or sending customer data anywhere.
func Verify(raw []byte, key ed25519.PublicKey, now time.Time) (Claims, error) {
	var c Claims
	if len(key) != ed25519.PublicKeySize {
		return c, errors.New("this build has no license issuer configured; contact the person who supplied Werkbord Team")
	}
	if len(raw) > 16384 {
		return c, errors.New("the license file is too large")
	}
	var d document
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&d); err != nil {
		return c, errors.New("choose a Werkbord Team license file")
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return c, errors.New("the license contains extra data")
	}
	sig, err := base64.RawURLEncoding.DecodeString(d.Signature)
	if err != nil || !ed25519.Verify(key, SigningBytes(d.Claims), sig) {
		return c, errors.New("the license signature is not valid")
	}
	claimsDecoder := json.NewDecoder(bytes.NewReader(d.Claims))
	claimsDecoder.DisallowUnknownFields()
	if err := claimsDecoder.Decode(&c); err != nil || validate(c) != nil {
		return Claims{}, errors.New("this is not a Werkbord Team license")
	}
	if c.Schema == Schema {
		canonical, err := CanonicalClaims(c)
		if err != nil || !bytes.Equal(canonical, d.Claims) {
			return Claims{}, errors.New("the license payload is not canonical")
		}
	} else if c.Schema != 0 || c.Edition != "" || c.ExpiresAt.IsZero() || !c.SupportEndsAt.IsZero() {
		return Claims{}, errors.New("unsupported license schema or edition")
	}
	if now.Before(c.IssuedAt.Add(-5 * time.Minute)) {
		return Claims{}, errors.New("this license is not active yet; check this computer's date")
	}
	if !c.ExpiresAt.IsZero() && !now.Before(c.ExpiresAt) {
		return c, errors.New("this license has expired; import its renewal")
	}
	return c, nil
}

func validate(c Claims) error {
	if c.Product != "werkbord-team" || c.ID == "" || len(c.ID) > 128 || c.Customer == "" || len(c.Customer) > 256 || c.Seats < 1 || c.Seats > 1000000 || c.IssuedAt.IsZero() || (!c.ExpiresAt.IsZero() && !c.ExpiresAt.After(c.IssuedAt)) || (!c.SupportEndsAt.IsZero() && c.SupportEndsAt.Before(c.IssuedAt)) {
		return errors.New("invalid Team license claims")
	}
	return nil
}
