// Package tunnel carries HTTP between a phone and the controller over the relay's end-to-end channel, so the
// phone's browser can use the app as if it were connected to the controller directly. The phone's service worker
// (relay/connector) turns each fetch into a request here; the runner answers it with the controller's own handler,
// the one the private network serves, so a phone gets exactly the access a phone has anywhere else (internal/api/phone.go).
//
// Everything in this package travels inside e2e-sealed messages: the relay sees none of it.
//
// Wire format of one message (the plaintext of an e2e data frame, at most e2e.MaxPlaintext bytes):
//
//	type (1) ‖ request id (4, big-endian, chosen by the phone) ‖ payload
//
// A request is ReqHead (JSON), any number of ReqBody, then ReqEnd. A response is ResHead (JSON), any number of
// ResBody, then ResEnd. Either side may end a request early: the phone with Cancel, the runner with Err.
package tunnel

import (
	"encoding/binary"
	"errors"
)

// Message types.
const (
	ReqHead byte = 0x01 // phone → runner: JSON Head
	ReqBody byte = 0x02 // phone → runner: bytes
	ReqEnd  byte = 0x03 // phone → runner: the request is complete
	ResHead byte = 0x04 // runner → phone: JSON Head
	ResBody byte = 0x05 // runner → phone: bytes
	ResEnd  byte = 0x06 // runner → phone: the response is complete
	Err     byte = 0x07 // runner → phone: JSON Failure; the request is over
	Cancel  byte = 0x08 // phone → runner: stop this request (the browser gave up on it)
)

// Limits. The first three protect the controller from a phone that misbehaves; the chunk size keeps every message well
// inside one relay frame.
const (
	MaxHeadBytes     = 16 * 1024
	MaxBodyBytes     = 16 << 20
	MaxOpenRequests  = 24
	ChunkBytes       = 32 * 1024
	headerSize       = 5
	failureBusy      = "busy"
	failureBadReq    = "bad_request"
	failureTooLarge  = "too_large"
	failureInternal  = "internal"
	failureNoHandler = "unavailable"
)

// Head is a request's or a response's first message.
type Head struct {
	// Request: Method and Path (with its query, starting with a single "/").
	Method string `json:"m,omitempty"`
	Path   string `json:"p,omitempty"`
	// Response: Status.
	Status int `json:"s,omitempty"`
	// Header holds the headers, lower-cased, one value each (a repeated header is joined with ", ").
	Header map[string]string `json:"h,omitempty"`
}

// Failure is why the runner ended a request without a response.
type Failure struct {
	Code    string `json:"c"`
	Message string `json:"m"`
}

var errShort = errors.New("tunnel: short message")

// Pack builds a message.
func Pack(t byte, id uint32, payload []byte) []byte {
	out := make([]byte, headerSize+len(payload))
	out[0] = t
	binary.BigEndian.PutUint32(out[1:5], id)
	copy(out[headerSize:], payload)
	return out
}

// Unpack splits a message.
func Unpack(msg []byte) (t byte, id uint32, payload []byte, err error) {
	if len(msg) < headerSize {
		return 0, 0, nil, errShort
	}
	return msg[0], binary.BigEndian.Uint32(msg[1:5]), msg[headerSize:], nil
}
