// Package relay holds the shared vocabulary of Werkbord's relay protocol (docs/RELAY.md): the
// relay-facing frame format, close codes and the pairing a QR code carries. The Worker (relay/worker)
// and Go relay (server) forward opaque bytes and never hold an encryption key.
package relay

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// Subprotocol names the protocol on the WebSocket; the join key rides in a second
// subprotocol token because a browser cannot set headers on a WebSocket.
const (
	Subprotocol = "werkbord.relay.v1"
	AuthPrefix  = "wb-auth."
	PingText    = "ping" // answered with "pong" by the relay without waking it (free of charge)
	PongText    = "pong"
	MaxFrame    = 64 * 1024
	MaxPhones   = 8
)

// Frame types between the relay and the runner. Phone connections carry the raw payload.
const (
	TData  byte = 0x01 // [type][peer 8][payload]
	TEvent byte = 0x02 // relay -> runner: [type][peer 8][event]
	TKick  byte = 0x03 // runner -> relay: [type][peer 8]
)

// Events carried in a TEvent frame.
const (
	EvConnected    byte = 1
	EvDisconnected byte = 2
)

// WebSocket close codes the relay uses.
const (
	CloseReplaced    = 4001 // another runner connection took this room
	CloseKicked      = 4403 // the runner removed this phone
	CloseRunnerAway  = 4404 // no runner connected
	CloseRunnerGone  = 4410 // runner disconnected or reconnected
	CloseRateLimited = 4429
)

// PeerID names one phone connection for the life of that connection.
type PeerID [8]byte

func (p PeerID) String() string { return hex.EncodeToString(p[:]) }

// Frame builds a runner-side frame.
func Frame(t byte, peer PeerID, payload []byte) []byte {
	out := make([]byte, 0, 9+len(payload))
	out = append(out, t)
	out = append(out, peer[:]...)
	return append(out, payload...)
}

// ParseFrame splits a runner-side frame.
func ParseFrame(b []byte) (t byte, peer PeerID, payload []byte, err error) {
	if len(b) < 9 {
		return 0, peer, nil, errors.New("relay: short frame")
	}
	copy(peer[:], b[1:9])
	return b[0], peer, b[9:], nil
}

// Room is a room's public address and the key that gates the relay (not the traffic).
type Room struct {
	ID      string // 32 hex characters
	JoinKey string // 43 base64url characters
}

// NewRoom makes a fresh random room.
func NewRoom() Room {
	id, key := make([]byte, 16), make([]byte, 32)
	if _, err := rand.Read(id); err != nil {
		panic(err)
	}
	if _, err := rand.Read(key); err != nil {
		panic(err)
	}
	return Room{ID: hex.EncodeToString(id), JoinKey: base64.RawURLEncoding.EncodeToString(key)}
}

// JoinKeyHash is what the relay stores in place of the key.
func JoinKeyHash(joinKey string) string {
	h := sha256.Sum256([]byte(joinKey))
	return hex.EncodeToString(h[:])
}

// WebSocketURL is the address a runner or phone dials.
func WebSocketURL(base, roomID, role string) (string, error) {
	u, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	case "http":
		u.Scheme = "ws"
	case "wss", "ws":
	default:
		return "", fmt.Errorf("relay: unsupported scheme %q", u.Scheme)
	}
	u.Path = "/v1/room/" + roomID
	u.RawQuery = "role=" + role
	return u.String(), nil
}

// Pairing is everything a phone needs, carried in the QR code's URL fragment so no server
// ever receives it. The runner key lets the phone reject an impostor; the secret lets the
// runner reject a stranger.
type Pairing struct {
	Lane      string `json:"lane,omitempty"` // optional connector lane; no authority
	V         int    `json:"v"`
	Relay     string `json:"u"` // https://relay.example.workers.dev
	Room      string `json:"r"`
	JoinKey   string `json:"j"`
	RunnerKey string `json:"k"` // base64url X25519 public key
	Secret    string `json:"s"` // base64url pre-shared secret
	// Code is the phone enrollment code (internal/phoneaccess) this pairing stands for. The connector opens the app
	// with it, and the app spends it as it does for any phone. Optional: a pairing without it only reconnects.
	Code string `json:"c,omitempty"`
}

const pairingPrefix = "wbr1."

func (p Pairing) Encode() string {
	b, _ := json.Marshal(p)
	return pairingPrefix + base64.RawURLEncoding.EncodeToString(b)
}

func DecodePairing(s string) (Pairing, error) {
	var p Pairing
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	if !strings.HasPrefix(s, pairingPrefix) {
		return p, errors.New("relay: not a pairing code")
	}
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(s, pairingPrefix))
	if err != nil {
		return p, fmt.Errorf("relay: pairing: %w", err)
	}
	if err := json.Unmarshal(b, &p); err != nil || p.V != 1 || p.Relay == "" || p.Room == "" || p.JoinKey == "" || p.RunnerKey == "" || p.Secret == "" {
		return Pairing{}, errors.New("relay: pairing is incomplete")
	}
	return p, nil
}
