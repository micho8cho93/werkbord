// Package envelope is how one Werkbord device asks another to do something, in a
// form that can be checked by anyone who needs to relay or act on it.
//
// An Envelope names who is asking (a user, on one of their devices), which
// workspace, which device it is for, what is being asked (a semantic Action, never
// a command), when it was issued and when it stops being valid, and carries a
// payload whose hash is signed along with all of that. The signature is made with
// the asking device's own key (internal/deviceid), so a message cannot be forged by
// anyone who is merely on the network, altered in transit, redirected to another
// device or workspace, kept and replayed later, or presented as another person's.
//
// Network membership authenticates a connection; this authenticates a request. A
// relay (Team may route envelopes) can see that one is well formed and signed; it
// cannot change one, and it does not execute one. The device it is for decides
// whether to act, under its own policy.
//
// This package defines the format, signing, verification and the replay and expiry
// checks. It defines the actions and the shape of each one's payload. It does not
// carry an envelope anywhere or act on one: that is not built yet, and when it is,
// the acting side is the individual product's runner, not Team.
package envelope

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"devboard/internal/deviceid"
)

// Protocol and Version identify the format. A verifier refuses any other.
const (
	Protocol = "werkbord.envelope"
	Version  = 1
)

// Limits.
const (
	// NonceSize is the length of the random nonce, in bytes.
	NonceSize = 16
	// MaxPayload is the largest payload an envelope carries, in bytes. An action
	// refers to things by ID; it does not ship content.
	MaxPayload = 16 << 10
	// MaxLifetime is the longest an envelope may be valid for.
	MaxLifetime = 5 * time.Minute
	// DefaultLifetime is what Sign uses when none is asked for.
	DefaultLifetime = time.Minute
	// DefaultSkew is how far a verifier's clock may differ from the signer's.
	DefaultSkew = 30 * time.Second
)

// Envelope is a signed request from one device. Every field before Signature is
// covered by the signature; Payload is covered through PayloadHash.
type Envelope struct {
	Protocol    string `json:"protocol"`
	Version     int    `json:"version"`
	MessageID   string `json:"messageId"`
	WorkspaceID string `json:"workspaceId"`
	// UserID is the member who is asking.
	UserID string `json:"userId"`
	// DeviceID is the device the request is signed by and comes from.
	DeviceID string `json:"deviceId"`
	// TargetDeviceID is the device the request is for. Actions that act on a
	// device require it; it is empty only for a request to the workspace itself.
	TargetDeviceID string `json:"targetDeviceId,omitempty"`
	Action         Action `json:"action"`
	// IssuedAt and ExpiresAt are Unix milliseconds, UTC.
	IssuedAt  int64 `json:"issuedAt"`
	ExpiresAt int64 `json:"expiresAt"`
	// Nonce is 16 random bytes, hex encoded.
	Nonce string `json:"nonce"`
	// PayloadHash is the SHA-256 of Payload, hex encoded.
	PayloadHash string `json:"payloadHash"`
	// Payload is the action's arguments, JSON, of the type its Action defines.
	Payload []byte `json:"payload,omitempty"`
	// Signature is the Ed25519 signature over SigningBytes, base64 (raw URL).
	Signature string `json:"signature"`
}

// Signer signs on behalf of a device. *localidentity.Identity is one.
type Signer interface {
	DeviceID() string
	Sign(msg []byte) []byte
}

// Request is what a caller supplies to make an envelope.
type Request struct {
	WorkspaceID    string
	UserID         string
	TargetDeviceID string
	Action         Action
	// Payload is the action's argument, of the type its Action defines (see
	// PayloadFor); it is encoded to JSON.
	Payload any
	// Lifetime defaults to DefaultLifetime and may not exceed MaxLifetime.
	Lifetime time.Duration
}

// Errors. Each says what failed and nothing about the key or the payload.
var (
	ErrMalformed   = errors.New("envelope: malformed")
	ErrProtocol    = errors.New("envelope: unknown protocol or version")
	ErrAction      = errors.New("envelope: unknown action")
	ErrPayload     = errors.New("envelope: payload does not match its hash or its action")
	ErrExpired     = errors.New("envelope: expired")
	ErrNotYetValid = errors.New("envelope: issued in the future")
	ErrLifetime    = errors.New("envelope: valid for longer than allowed")
	ErrSignature   = errors.New("envelope: bad signature")
	ErrUnknownKey  = errors.New("envelope: the signing device is not known")
	ErrRevoked     = errors.New("envelope: the signing device has been revoked")
	ErrWrongOwner  = errors.New("envelope: the signing device does not belong to the user named")
	ErrWrongTarget = errors.New("envelope: not addressed to this device")
	ErrReplay      = errors.New("envelope: already seen")
)

// Sign makes and signs an envelope.
func Sign(s Signer, r Request, now time.Time) (Envelope, error) {
	if r.Lifetime <= 0 {
		r.Lifetime = DefaultLifetime
	}
	if r.Lifetime > MaxLifetime {
		return Envelope{}, fmt.Errorf("%w: at most %s", ErrLifetime, MaxLifetime)
	}
	payload, err := EncodePayload(r.Action, r.Payload)
	if err != nil {
		return Envelope{}, err
	}
	var id, nonce [NonceSize]byte
	if _, err := rand.Read(id[:]); err != nil {
		return Envelope{}, err
	}
	if _, err := rand.Read(nonce[:]); err != nil {
		return Envelope{}, err
	}
	sum := sha256.Sum256(payload)
	e := Envelope{
		Protocol: Protocol, Version: Version,
		MessageID:   "msg_" + hex.EncodeToString(id[:]),
		WorkspaceID: r.WorkspaceID, UserID: r.UserID, DeviceID: s.DeviceID(), TargetDeviceID: r.TargetDeviceID,
		Action:      r.Action,
		IssuedAt:    now.UTC().UnixMilli(),
		ExpiresAt:   now.UTC().Add(r.Lifetime).UnixMilli(),
		Nonce:       hex.EncodeToString(nonce[:]),
		PayloadHash: hex.EncodeToString(sum[:]),
		Payload:     payload,
	}
	if err := e.checkShape(); err != nil {
		return Envelope{}, err
	}
	e.Signature = encodeSig(s.Sign(e.SigningBytes()))
	return e, nil
}

// signingDomain separates an envelope signature from every other signature the
// same key makes (a registration proof, a future message type).
const signingDomain = "werkbord/envelope/v1\x00"

// SigningBytes is exactly what is signed: a fixed domain prefix, then every
// covered field in a fixed order, strings length-prefixed and numbers fixed width,
// so no two different envelopes have the same bytes and nothing depends on how
// JSON happens to be written.
func (e Envelope) SigningBytes() []byte {
	var b bytes.Buffer
	b.WriteString(signingDomain)
	str := func(s string) {
		var n [4]byte
		binary.BigEndian.PutUint32(n[:], uint32(len(s)))
		b.Write(n[:])
		b.WriteString(s)
	}
	num := func(v int64) {
		var n [8]byte
		binary.BigEndian.PutUint64(n[:], uint64(v))
		b.Write(n[:])
	}
	str(e.Protocol)
	num(int64(e.Version))
	str(e.MessageID)
	str(e.WorkspaceID)
	str(e.UserID)
	str(e.DeviceID)
	str(e.TargetDeviceID)
	str(string(e.Action))
	num(e.IssuedAt)
	num(e.ExpiresAt)
	str(e.Nonce)
	str(e.PayloadHash)
	return b.Bytes()
}

func (e Envelope) checkShape() error {
	switch {
	case e.Protocol != Protocol || e.Version != Version:
		return ErrProtocol
	case e.MessageID == "" || e.WorkspaceID == "" || e.UserID == "" || e.DeviceID == "":
		return fmt.Errorf("%w: message, workspace, user and device are all required", ErrMalformed)
	case len(e.MessageID) > 128 || len(e.WorkspaceID) > 128 || len(e.UserID) > 128 || len(e.DeviceID) > 128 || len(e.TargetDeviceID) > 128:
		return fmt.Errorf("%w: an identifier is too long", ErrMalformed)
	case !deviceid.ValidID(e.DeviceID):
		return fmt.Errorf("%w: %q is not a device ID", ErrMalformed, e.DeviceID)
	case e.TargetDeviceID != "" && !deviceid.ValidID(e.TargetDeviceID):
		return fmt.Errorf("%w: %q is not a device ID", ErrMalformed, e.TargetDeviceID)
	}
	spec, ok := specs[e.Action]
	if !ok {
		return fmt.Errorf("%w: %q", ErrAction, e.Action)
	}
	if spec.NeedsTarget && e.TargetDeviceID == "" {
		return fmt.Errorf("%w: %s must name the device it is for", ErrMalformed, e.Action)
	}
	if !spec.NeedsTarget && e.TargetDeviceID != "" {
		return fmt.Errorf("%w: %s is not addressed to a device", ErrMalformed, e.Action)
	}
	if e.IssuedAt <= 0 || e.ExpiresAt <= e.IssuedAt {
		return fmt.Errorf("%w: it expires before it is issued", ErrMalformed)
	}
	// Compare milliseconds before conversion to time.Duration; an attacker can otherwise overflow it.
	if e.ExpiresAt-e.IssuedAt > MaxLifetime.Milliseconds() {
		return ErrLifetime
	}
	if len(e.Nonce) != 2*NonceSize || !isHex(e.Nonce) {
		return fmt.Errorf("%w: bad nonce", ErrMalformed)
	}
	if len(e.PayloadHash) != 2*sha256.Size || !isHex(e.PayloadHash) {
		return fmt.Errorf("%w: bad payload hash", ErrMalformed)
	}
	if len(e.Payload) > MaxPayload {
		return fmt.Errorf("%w: payload is larger than %d bytes", ErrMalformed, MaxPayload)
	}
	return nil
}

func isHex(s string) bool {
	_, err := hex.DecodeString(s)
	return err == nil
}

// IssuedTime and ExpiryTime are the envelope's times.
func (e Envelope) IssuedTime() time.Time { return time.UnixMilli(e.IssuedAt).UTC() }
func (e Envelope) ExpiryTime() time.Time { return time.UnixMilli(e.ExpiresAt).UTC() }
