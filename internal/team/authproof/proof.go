// Package authproof authenticates an API request in addition to its network and bearer credential.
// It holds no keys: a device signer is passed in, and verification uses public material.
package authproof

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"devboard/internal/envelope"
)

const Header = "Werkbord-Device-Proof"
const Lifetime = time.Minute
const Skew = 30 * time.Second
const domain = "werkbord-team/api-proof/v1\x00"

var ErrInvalid = errors.New("invalid or expired device request proof")

// ClockError is a proof that is valid in every way except that the two computers' clocks disagree by more than the
// allowance: it was made too far in the future of the host's clock, or so long ago by it that it has run out. It is an
// ErrInvalid, and says which way the device's clock is off so that the person can be told.
type ClockError struct {
	// Offset is the device's clock minus the host's: positive when the device is ahead.
	Offset time.Duration
}

func (e *ClockError) Error() string {
	return "the device's clock and the workspace host's clock disagree by " + e.Offset.Abs().Round(time.Second).String()
}
func (e *ClockError) Unwrap() error { return ErrInvalid }

type Proof struct {
	Version   int    `json:"v"`
	Workspace string `json:"workspace"`
	User      string `json:"user"`
	Device    string `json:"device"`
	Method    string `json:"method"`
	Target    string `json:"target"`
	Path      string `json:"path"`
	BodyHash  string `json:"bodyHash"`
	Issued    int64  `json:"issued"`
	Expires   int64  `json:"expires"`
	Nonce     string `json:"nonce"`
	Signature string `json:"signature,omitempty"`
}

func (p Proof) signingBytes() []byte {
	p.Signature = ""
	b, _ := json.Marshal(p)
	return append([]byte(domain), b...)
}

func bodyHash(body []byte) string { h := sha256.Sum256(body); return hex.EncodeToString(h[:]) }

func Sign(r *http.Request, body []byte, signer envelope.Signer, ws, user string, now time.Time) error {
	if signer == nil || ws == "" || user == "" {
		return ErrInvalid
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	p := Proof{Version: 1, Workspace: ws, User: user, Device: signer.DeviceID(), Method: r.Method, Target: r.URL.Host, Path: r.URL.RequestURI(), BodyHash: bodyHash(body), Issued: now.UnixMilli(), Expires: now.Add(Lifetime).UnixMilli(), Nonce: hex.EncodeToString(nonce[:])}
	p.Signature = base64.RawURLEncoding.EncodeToString(signer.Sign(p.signingBytes()))
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	r.Header.Set(Header, base64.RawURLEncoding.EncodeToString(b))
	return nil
}

func Verify(r *http.Request, body []byte, key ed25519.PublicKey, ws, user, device string, now time.Time) (Proof, error) {
	var p Proof
	values := r.Header.Values(Header)
	if len(values) != 1 || len(values[0]) > 4096 || len(key) != ed25519.PublicKeySize {
		return p, ErrInvalid
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(values[0])
	if err != nil {
		return p, ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&p); err != nil {
		return p, ErrInvalid
	}
	canonical, _ := json.Marshal(p)
	if !bytes.Equal(raw, canonical) {
		return p, ErrInvalid
	}
	if p.Version != 1 || p.Workspace != ws || p.User != user || p.Device != device || p.Method != r.Method || p.Target != r.Host || p.Path != r.URL.RequestURI() || p.BodyHash != bodyHash(body) || p.Issued <= 0 || p.Expires <= p.Issued || p.Expires-p.Issued > Lifetime.Milliseconds() {
		return p, ErrInvalid
	}
	nonce, err := hex.DecodeString(p.Nonce)
	if err != nil || len(nonce) != 16 || hex.EncodeToString(nonce) != p.Nonce {
		return p, ErrInvalid
	}
	sig, err := base64.RawURLEncoding.Strict().DecodeString(p.Signature)
	if err != nil || !ed25519.Verify(key, p.signingBytes(), sig) {
		return p, ErrInvalid
	}
	// The time is judged last, so that only a request the device really signed is told that its clock is wrong.
	issued := time.UnixMilli(p.Issued)
	if issued.After(now.Add(Skew)) || !now.Before(time.UnixMilli(p.Expires).Add(Skew)) {
		return p, &ClockError{Offset: issued.Sub(now)}
	}
	return p, nil
}
