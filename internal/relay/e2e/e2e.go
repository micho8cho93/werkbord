// Package e2e is the end-to-end encryption between a phone and a runner, carried over a
// relay that sees only ciphertext. The wire format is specified in docs/RELAY.md and pinned
// by relay/conformance/e2e-vectors.json, which the browser implementation (relay/e2e) must
// reproduce byte for byte.
//
// The runner has a long-term X25519 key (its public half travels in the QR code). The phone
// proves it holds a pre-shared secret from the QR; the runner proves it holds the private key.
// Both contribute a fresh ephemeral key, so a later leak of either long-term secret does not
// open recorded traffic.
package e2e

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
)

// Frame types.
const (
	TypeHello    byte = 0x01 // phone -> runner
	TypeAccept   byte = 0x02 // runner -> phone
	TypeData     byte = 0x03 // both ways
	version      byte = 1
	PSKIDSize         = 8
	KeySize           = 32
	MaxPlaintext      = 60 * 1024 // a sealed frame must fit the relay's 64 KiB frame with its headers
)

const (
	labelKeys    = "werkbord-relay-e2e-v1"
	labelInfo    = "werkbord-relay-keys"
	labelPSKID   = "werkbord-relay-psk-id"
	acceptSecret = "wb-ok"
)

var (
	ErrMalformed    = errors.New("e2e: malformed frame")
	ErrUnknownPSK   = errors.New("e2e: unknown pre-shared secret")
	ErrAuth         = errors.New("e2e: authentication failed")
	ErrReplay       = errors.New("e2e: replayed or reordered frame")
	ErrTooLarge     = errors.New("e2e: message too large")
	ErrNonceExhaust = errors.New("e2e: session exhausted")
)

// PSKID names a pre-shared secret without revealing it.
type PSKID [PSKIDSize]byte

// IDOf returns the identifier the phone sends so the runner can find the secret.
func IDOf(psk []byte) PSKID {
	h := sha256.New()
	h.Write([]byte(labelPSKID))
	h.Write(psk)
	var id PSKID
	copy(id[:], h.Sum(nil))
	return id
}

// PSKStore is how the runner finds the secret for a hello. Confirm is called once the phone
// has proved it holds the secret, so a single-use enrollment secret can be spent.
type PSKStore interface {
	Lookup(PSKID) (psk []byte, ok bool)
	Confirm(PSKID)
}

// Identity is the runner's long-term key.
type Identity struct{ priv *ecdh.PrivateKey }

func NewIdentity() (*Identity, error) {
	k, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	return &Identity{k}, nil
}

// IdentityFromSeed restores an identity from the 32 bytes returned by Seed.
func IdentityFromSeed(seed []byte) (*Identity, error) {
	k, err := ecdh.X25519().NewPrivateKey(seed)
	if err != nil {
		return nil, err
	}
	return &Identity{k}, nil
}

func (i *Identity) Seed() []byte   { return i.priv.Bytes() }
func (i *Identity) Public() []byte { return i.priv.PublicKey().Bytes() }

func random32() []byte {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return b
}

// NewSecret returns 32 random bytes for a pre-shared secret.
func NewSecret() []byte { return random32() }

// Session encrypts and decrypts one direction pair. It is not safe for concurrent use.
type Session struct {
	send, recv   cipher.AEAD
	sendN, recvN uint64
	recvAny      bool
}

func newSession(okm []byte, initiator bool) (*Session, error) {
	p2r, r2p := okm[:32], okm[32:]
	if !initiator {
		p2r, r2p = r2p, p2r // after this swap, "send" below is the runner's r2p
	}
	mk := func(k []byte) (cipher.AEAD, error) {
		b, err := aes.NewCipher(k)
		if err != nil {
			return nil, err
		}
		return cipher.NewGCM(b)
	}
	s, err := mk(p2r)
	if err != nil {
		return nil, err
	}
	r, err := mk(r2p)
	if err != nil {
		return nil, err
	}
	return &Session{send: s, recv: r}, nil
}

func nonce(counter uint64) []byte {
	n := make([]byte, 12)
	binary.BigEndian.PutUint64(n[4:], counter)
	return n
}

func derive(dhStatic, dhEph, psk, msg1, re, rs []byte) ([]byte, error) {
	h := sha256.New()
	h.Write([]byte(labelKeys))
	h.Write(msg1)
	h.Write(re)
	h.Write(rs)
	ikm := append(append(append([]byte{}, dhStatic...), dhEph...), psk...)
	return hkdf.Key(sha256.New, ikm, h.Sum(nil), labelInfo, 64)
}

// Initiator is the phone's half of a handshake in progress.
type Initiator struct {
	eph        *ecdh.PrivateKey
	rs         *ecdh.PublicKey
	rsRaw, psk []byte
	msg1       []byte
}

// Initiate starts a handshake from the phone. Send the returned hello to the runner.
func Initiate(runnerPub, psk []byte) (hello []byte, in *Initiator, err error) {
	eph, err := ecdh.X25519().NewPrivateKey(random32())
	if err != nil {
		return nil, nil, err
	}
	return initiate(eph, runnerPub, psk)
}

func initiate(eph *ecdh.PrivateKey, runnerPub, psk []byte) ([]byte, *Initiator, error) {
	rs, err := ecdh.X25519().NewPublicKey(runnerPub)
	if err != nil {
		return nil, nil, fmt.Errorf("e2e: runner key: %w", err)
	}
	id := IDOf(psk)
	msg := append([]byte{TypeHello, version}, id[:]...)
	msg = append(msg, eph.PublicKey().Bytes()...)
	return msg, &Initiator{eph: eph, rs: rs, rsRaw: runnerPub, psk: psk, msg1: msg}, nil
}

// Finish consumes the runner's accept and returns the session.
func (in *Initiator) Finish(accept []byte) (*Session, error) {
	if len(accept) < 1+32+16 || accept[0] != TypeAccept {
		return nil, ErrMalformed
	}
	re, err := ecdh.X25519().NewPublicKey(accept[1:33])
	if err != nil {
		return nil, ErrMalformed
	}
	d1, err := in.eph.ECDH(in.rs)
	if err != nil {
		return nil, ErrAuth
	}
	d2, err := in.eph.ECDH(re)
	if err != nil {
		return nil, ErrAuth
	}
	okm, err := derive(d1, d2, in.psk, in.msg1, accept[1:33], in.rsRaw)
	if err != nil {
		return nil, err
	}
	s, err := newSession(okm, true)
	if err != nil {
		return nil, err
	}
	// The accept carries the runner's proof as its first sealed frame (counter 0).
	pt, err := s.recv.Open(nil, nonce(0), accept[33:], accept[:33])
	if err != nil || string(pt) != acceptSecret {
		return nil, ErrAuth
	}
	s.recvN, s.recvAny = 0, true
	return s, nil
}

// Respond handles a hello on the runner. It returns the accept to send back, the session, and
// the id of the secret the phone used. The session is not trusted until the phone's first data
// frame opens; then call store.Confirm(id).
func Respond(id *Identity, store PSKStore, hello []byte) (accept []byte, s *Session, pid PSKID, err error) {
	eph, err := ecdh.X25519().NewPrivateKey(random32())
	if err != nil {
		return nil, nil, pid, err
	}
	return respond(eph, id, store, hello)
}

func respond(eph *ecdh.PrivateKey, id *Identity, store PSKStore, hello []byte) ([]byte, *Session, PSKID, error) {
	var pid PSKID
	if len(hello) != 2+PSKIDSize+32 || hello[0] != TypeHello || hello[1] != version {
		return nil, nil, pid, ErrMalformed
	}
	copy(pid[:], hello[2:2+PSKIDSize])
	psk, ok := store.Lookup(pid)
	if !ok {
		return nil, nil, pid, ErrUnknownPSK
	}
	pe, err := ecdh.X25519().NewPublicKey(hello[2+PSKIDSize:])
	if err != nil {
		return nil, nil, pid, ErrMalformed
	}
	d1, err := id.priv.ECDH(pe)
	if err != nil {
		return nil, nil, pid, ErrAuth
	}
	d2, err := eph.ECDH(pe)
	if err != nil {
		return nil, nil, pid, ErrAuth
	}
	re := eph.PublicKey().Bytes()
	okm, err := derive(d1, d2, psk, hello, re, id.Public())
	if err != nil {
		return nil, nil, pid, err
	}
	s, err := newSession(okm, false)
	if err != nil {
		return nil, nil, pid, err
	}
	head := append([]byte{TypeAccept}, re...)
	accept := s.send.Seal(head, nonce(0), []byte(acceptSecret), head)
	s.sendN = 1
	return accept, s, pid, nil
}

// Seal encrypts one message.
func (s *Session) Seal(plaintext []byte) ([]byte, error) {
	if len(plaintext) > MaxPlaintext {
		return nil, ErrTooLarge
	}
	if s.sendN == ^uint64(0) {
		return nil, ErrNonceExhaust
	}
	head := make([]byte, 9)
	head[0] = TypeData
	binary.BigEndian.PutUint64(head[1:], s.sendN)
	out := s.send.Seal(head, nonce(s.sendN), plaintext, head)
	s.sendN++
	return out, nil
}

// Open decrypts one message. Counters must strictly increase.
func (s *Session) Open(frame []byte) ([]byte, error) {
	if len(frame) < 9+16 || frame[0] != TypeData {
		return nil, ErrMalformed
	}
	c := binary.BigEndian.Uint64(frame[1:9])
	if s.recvAny && c <= s.recvN {
		return nil, ErrReplay
	}
	pt, err := s.recv.Open(nil, nonce(c), frame[9:], frame[:9])
	if err != nil {
		return nil, ErrAuth
	}
	s.recvN, s.recvAny = c, true
	return pt, nil
}
