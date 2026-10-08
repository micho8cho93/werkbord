package envelope

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	"devboard/internal/deviceid"
)

func encodeSig(sig []byte) string { return base64.RawURLEncoding.EncodeToString(sig) }

// Device is what a verifier needs to know about the device that signed.
type Device struct {
	ID          string
	WorkspaceID string
	// OwnerID is the member the device belongs to.
	OwnerID   string
	PublicKey deviceid.PublicKey
	Revoked   bool
}

// Directory resolves a device by workspace and ID. It returns ErrUnknownKey when
// there is no such device in that workspace. Team's device registry is one.
type Directory interface {
	Device(ctx context.Context, workspaceID, deviceID string) (Device, error)
}

// ReplayCache remembers envelopes that have been accepted. Seen records the
// envelope and reports whether it had been recorded before. The cache is keyed by
// device, so one device's identifiers cannot shadow another's.
type ReplayCache interface {
	// Seen records (deviceID, messageID, nonce) until expires and reports whether
	// they were already recorded. It returns ErrReplayCacheFull rather than
	// forgetting an entry that has not expired: an attacker must not be able to
	// push a live envelope out of the cache.
	Seen(deviceID, messageID, nonce string, expires time.Time) (bool, error)
}

// ErrReplayCacheFull is returned by a cache that cannot take another live entry.
var ErrReplayCacheFull = errors.New("envelope: replay cache is full")

// MemoryReplayCache is an in-memory ReplayCache with a bounded size.
type MemoryReplayCache struct {
	// Max is the most live entries kept; zero means 100000.
	Max int
	Now func() time.Time

	mu      sync.Mutex
	entries map[string]time.Time
	nonces  map[string]time.Time
}

// Seen implements ReplayCache.
func (c *MemoryReplayCache) Seen(deviceID, messageID, nonce string, expires time.Time) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	if c.Now != nil {
		now = c.Now()
	}
	if c.entries == nil {
		c.entries = map[string]time.Time{}
		c.nonces = map[string]time.Time{}
	}
	max := c.Max
	if max <= 0 {
		max = 100000
	}
	key := deviceID + "\x00" + messageID
	nonceKey := deviceID + "\x00" + nonce
	if exp, ok := c.entries[key]; ok && exp.After(now) {
		return true, nil
	}
	if exp, ok := c.nonces[nonceKey]; ok && exp.After(now) {
		return true, nil
	}
	if len(c.entries) >= max || len(c.nonces) >= max {
		for k, exp := range c.entries { // drop what has expired
			if !exp.After(now) {
				delete(c.entries, k)
			}
		}
		for k, exp := range c.nonces {
			if !exp.After(now) {
				delete(c.nonces, k)
			}
		}
		if len(c.entries) >= max || len(c.nonces) >= max {
			return false, ErrReplayCacheFull
		}
	}
	c.entries[key] = expires
	c.nonces[nonceKey] = expires
	return false, nil
}

// Len is the number of entries held (some may have expired).
func (c *MemoryReplayCache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}

// Verifier checks envelopes.
type Verifier struct {
	// Directory resolves signing devices. Required.
	Directory Directory
	// Replay remembers accepted envelopes. Required: a verifier without one is
	// not constructed (NewVerifier), because expiry alone leaves a replay window.
	Replay ReplayCache
	// Self is the device doing the verifying, when it is one. An envelope that
	// names a different target is refused: an envelope for another device is not
	// this device's to act on, and cannot be redirected to it.
	Self string
	// Now defaults to time.Now.
	Now func() time.Time
	// Skew is the tolerated difference between clocks; default DefaultSkew.
	Skew time.Duration
}

// NewVerifier returns a Verifier. self is the verifying device's ID, or empty for
// a relay that is not the target (it still checks everything else).
func NewVerifier(dir Directory, replay ReplayCache, self string) (*Verifier, error) {
	if dir == nil || replay == nil {
		return nil, errors.New("envelope: a verifier needs a directory and a replay cache")
	}
	return &Verifier{Directory: dir, Replay: replay, Self: self}, nil
}

// Verified is an envelope that passed every check, with its decoded payload.
type Verified struct {
	Envelope Envelope
	Device   Device
	Payload  any
}

// Verify checks an envelope completely and records it as seen. It returns the
// decoded payload only for an envelope that is well formed, within its lifetime,
// from a known, unrevoked device of the user it names, in the workspace it names,
// correctly signed over every field, addressed to this device if the verifier is
// one, carrying the payload it hashes to, and not seen before.
//
// The order matters: nothing is recorded in the replay cache until the signature
// has been checked, so an unauthenticated sender cannot fill it.
func (v *Verifier) Verify(ctx context.Context, e Envelope) (Verified, error) {
	now := time.Now()
	if v.Now != nil {
		now = v.Now()
	}
	skew := v.Skew
	if skew <= 0 {
		skew = DefaultSkew
	}
	if err := e.checkShape(); err != nil {
		return Verified{}, err
	}
	switch {
	case now.After(e.ExpiryTime().Add(skew)):
		return Verified{}, ErrExpired
	case e.IssuedTime().After(now.Add(skew)):
		return Verified{}, ErrNotYetValid
	}

	dev, err := v.Directory.Device(ctx, e.WorkspaceID, e.DeviceID)
	if err != nil {
		return Verified{}, err
	}
	// A directory that answers for another workspace or device is a bug, not a
	// reason to accept: check what it said against what the envelope says.
	if dev.ID != e.DeviceID || dev.WorkspaceID != e.WorkspaceID {
		return Verified{}, ErrUnknownKey
	}
	if dev.Revoked {
		return Verified{}, ErrRevoked
	}
	sig, err := base64.RawURLEncoding.DecodeString(e.Signature)
	if err != nil || !deviceid.Verify(dev.PublicKey, e.SigningBytes(), sig) {
		return Verified{}, ErrSignature
	}
	// From here the sender is who the envelope says; what remains is whether the
	// envelope is acceptable.
	if subtle.ConstantTimeCompare([]byte(dev.OwnerID), []byte(e.UserID)) != 1 {
		return Verified{}, ErrWrongOwner
	}
	if v.Self != "" && e.TargetDeviceID != "" && e.TargetDeviceID != v.Self {
		return Verified{}, ErrWrongTarget
	}
	sum := sha256.Sum256(e.Payload)
	if subtle.ConstantTimeCompare([]byte(hex.EncodeToString(sum[:])), []byte(e.PayloadHash)) != 1 {
		return Verified{}, ErrPayload
	}
	payload, err := DecodePayload(e)
	if err != nil {
		return Verified{}, err
	}
	replayed, err := v.Replay.Seen(e.DeviceID, e.MessageID, e.Nonce, e.ExpiryTime().Add(skew))
	if err != nil {
		return Verified{}, err
	}
	if replayed {
		return Verified{}, ErrReplay
	}
	return Verified{Envelope: e, Device: dev, Payload: payload}, nil
}
