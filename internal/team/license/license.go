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
	Product   string    `json:"product"`
	ID        string    `json:"id"`
	Customer  string    `json:"customer"`
	Seats     int       `json:"seats"`
	IssuedAt  time.Time `json:"issuedAt"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type document struct {
	Claims    json.RawMessage `json:"claims"`
	Signature string          `json:"signature"`
}

// SigningBytes is the documented format used by an offline license issuer.
func SigningBytes(claims []byte) []byte {
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
	if err := json.Unmarshal(d.Claims, &c); err != nil || c.Product != "werkbord-team" || c.ID == "" || c.Customer == "" || c.Seats < 1 || c.IssuedAt.IsZero() || c.ExpiresAt.IsZero() || !c.ExpiresAt.After(c.IssuedAt) {
		return Claims{}, errors.New("this is not a Werkbord Team license")
	}
	if now.Before(c.IssuedAt.Add(-5 * time.Minute)) {
		return Claims{}, errors.New("this license is not active yet; check this computer's date")
	}
	if !now.Before(c.ExpiresAt) {
		return c, errors.New("this license has expired; import its renewal")
	}
	return c, nil
}
