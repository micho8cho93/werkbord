package domain

import (
	"encoding/json"
	"time"
)

// A DeviceMessage is a signed request from one of a person's devices to another, in a Workspace Host's mailbox. The host
// stores and routes it; it does not make it, change it or act on it. The device it is for checks the signature itself.

// MessageState is where a message is.
type MessageState string

const (
	// MessageQueued: waiting for the device it is for to collect it and say what it did.
	MessageQueued MessageState = "queued"
	// MessageDone: the device it was for did it.
	MessageDone MessageState = "done"
	// MessageRefused: the device it was for declined, under its own policy, and said why.
	MessageRefused MessageState = "refused"
	// MessageExpired is not stored: a queued message past its expiry is expired, whatever it says.
	MessageExpired MessageState = "expired"
)

// Valid reports whether s is a state a device may leave a message in.
func (s MessageState) Valid() bool { return s == MessageDone || s == MessageRefused }

// PrefixMessage marks a message's ID. The ID is the envelope's own.
const PrefixMessage = "msg"

// Limits.
const (
	// MaxResultBytes bounds what a device says back.
	MaxResultBytes = 2048
	// MaxQueuedPerDevice bounds how many requests wait for one device.
	MaxQueuedPerDevice = 50
	// MessageKeep is how long a message is kept after it expires or is decided.
	MessageKeep = 7 * 24 * time.Hour
)

// DeviceMessage is a request in the mailbox.
type DeviceMessage struct {
	ID           string `json:"id"`
	WorkspaceID  string `json:"workspaceId"`
	FromDeviceID string `json:"fromDeviceId"`
	ToDeviceID   string `json:"toDeviceId"`
	// MemberID is the person who asked, who owns both devices.
	MemberID string       `json:"memberId"`
	Action   string       `json:"action"`
	State    MessageState `json:"state"`
	// Result is what the device it was for said back: a small JSON object, or empty.
	Result    json.RawMessage `json:"result,omitempty"`
	CreatedAt time.Time       `json:"createdAt"`
	ExpiresAt time.Time       `json:"expiresAt"`
	DecidedAt *time.Time      `json:"decidedAt,omitempty"`
	// Envelope is the signed request exactly as the sender made it. It is returned to the device it is for, and to nobody else.
	Envelope json.RawMessage `json:"envelope,omitempty"`
}

// StateAt is the message's state as of now: a queued message past its expiry is expired.
func (m DeviceMessage) StateAt(now time.Time) MessageState {
	if m.State == MessageQueued && !now.Before(m.ExpiresAt) {
		return MessageExpired
	}
	return m.State
}

// WithoutEnvelope is the message as the person who asked sees it: what happened to it, not the signed bytes.
func (m DeviceMessage) WithoutEnvelope() DeviceMessage {
	m.Envelope = nil
	return m
}
