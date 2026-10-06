package enrollment

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"time"
)

// The protocol, over TLS 1.3 to a bootstrap endpoint of the workspace:
//
//	GET  /enroll/v1/hello   who are you? (the workspace's ID and key, so a reachability check can pin it)
//	POST /enroll/v1/join    present the invitation's credential and my device's public keys
//	POST /enroll/v1/poll    am I approved yet?
//
// Every request is its own connection, and the device signs each one together with
// a value both ends derive from that connection's TLS session (the exporter), so a
// recording of a request is no use on any other connection.

// Paths of the protocol.
const (
	PathHello = "/enroll/v1/hello"
	PathJoin  = "/enroll/v1/join"
	PathPoll  = "/enroll/v1/poll"
)

// State is where an enrollment is.
type State string

const (
	// StatePending: the invitation was good and an administrator has to approve the device.
	StatePending State = "pending"
	// StateApproved: the device is a member's device now; the response carries what it needs.
	StateApproved State = "approved"
	// StateDenied: an administrator refused it.
	StateDenied State = "denied"
)

// Hello is the answer to PathHello. It is not secret: the TLS handshake that carried
// it has already shown the server holds the workspace key.
type Hello struct {
	WorkspaceID  string `json:"workspaceId"`
	WorkspaceKey string `json:"workspaceKey"`
	Fingerprint  string `json:"fingerprint"`
}

// JoinRequest is a device asking to join.
type JoinRequest struct {
	InviteID   string `json:"inviteId"`
	Credential string `json:"credential"`
	// MemberName names the person, when the invitation creates a member.
	MemberName string `json:"memberName,omitempty"`
	DeviceID   string `json:"deviceId"`
	DeviceName string `json:"deviceName"`
	// DevicePublicKey is the device's application key (Ed25519, base64 raw URL). It is
	// not the network key.
	DevicePublicKey string `json:"devicePublicKey"`
	// NetworkPublicKey is the public half of the key the device made for the private
	// network (PEM). The private half never leaves the device: the workspace signs a
	// certificate for this key.
	NetworkPublicKey string `json:"networkPublicKey"`
	// SealingPublicKey is, for a device that will be a Workspace Host, the public key
	// workspace secrets are sealed to when they are handed to it (X25519, base64 raw URL).
	SealingPublicKey string `json:"sealingPublicKey,omitempty"`
	// Proof is the device's signature over JoinStatement.
	Proof string `json:"proof"`
}

// PollRequest asks after a pending enrollment.
type PollRequest struct {
	EnrollmentID string `json:"enrollmentId"`
	DeviceID     string `json:"deviceId"`
	// Proof is the device's signature over PollStatement.
	Proof string `json:"proof"`
}

// Response answers a join or a poll.
type Response struct {
	State        State  `json:"state"`
	EnrollmentID string `json:"enrollmentId,omitempty"`
	// Message says why, in words for the person (pending, denied).
	Message string `json:"message,omitempty"`
	// Set when approved:
	WorkspaceID   string `json:"workspaceId,omitempty"`
	WorkspaceName string `json:"workspaceName,omitempty"`
	WorkspaceKey  string `json:"workspaceKey,omitempty"`
	MemberID      string `json:"memberId,omitempty"`
	MemberName    string `json:"memberName,omitempty"`
	Role          string `json:"role,omitempty"`
	DeviceID      string `json:"deviceId,omitempty"`
	// DeviceToken is the device's credential for the workspace's API. It is shown
	// once, here, and the workspace keeps only its hash.
	DeviceToken  string         `json:"deviceToken,omitempty"`
	Capabilities []string       `json:"capabilities,omitempty"`
	Network      *NetworkBundle `json:"network,omitempty"`
	// Endpoints are the workspace's known bootstrap and API endpoints now, for the
	// device to remember: more than the invitation named, when there are.
	Endpoints []string `json:"endpoints,omitempty"`
}

// NetworkBundle is what a device needs to join the workspace's private network. It
// holds nothing secret that was not the device's own to begin with: the private key
// stayed on the device.
type NetworkBundle struct {
	// CACertificate and NodeCertificate are PEM: the network's authority (to be
	// trusted) and this device's own certificate, signed by it.
	CACertificate   string `json:"caCertificate"`
	NodeCertificate string `json:"nodeCertificate"`
	// Config is the node's configuration, as a template: ConfigDirToken stands where
	// the device's own directory goes (Install fills it in). It names no private key
	// but the device's own.
	Config string `json:"config"`
	// OverlayAddr is the device's address on the private network.
	OverlayAddr string `json:"overlayAddr"`
	// ExpiresAt is when the node certificate stops working (Unix seconds); the device
	// asks for a new one before then.
	ExpiresAt int64 `json:"expiresAt"`
	// Discovery lists the machines that help devices find each other (more than one,
	// when there is more than one), and Relays the ones that carry traffic for devices
	// that cannot reach each other directly.
	Discovery []Peer   `json:"discovery"`
	Relays    []string `json:"relays,omitempty"`
	// APIAddrs are the private-network addresses of the machines that serve the
	// workspace's API.
	APIAddrs []string `json:"apiAddrs,omitempty"`
	// Node describes the same thing in the workspace's own terms (opaque here), for a
	// program that renders its own configuration, as a Workspace Host does, and not only
	// for one that runs Config as it is.
	Node json.RawMessage `json:"node,omitempty"`
}

// Peer is a machine of the workspace the network can be reached through.
type Peer struct {
	// OverlayAddr is its address on the private network.
	OverlayAddr string `json:"overlayAddr"`
	// Endpoints are where it can be reached from outside ("host:port").
	Endpoints []string `json:"endpoints"`
}

// ConfigDirToken stands in NetworkBundle.Config for the directory the device keeps
// its network files in.
const ConfigDirToken = "@DIR@"

// ---- what is signed ----

const (
	joinDomain = "werkbord/enroll-join/v1"
	pollDomain = "werkbord/enroll-poll/v1"
)

func lp(b []byte, fields ...string) []byte {
	for _, f := range fields {
		b = binary.BigEndian.AppendUint32(b, uint32(len(f)))
		b = append(b, f...)
	}
	return b
}

func digest(s string) string {
	h := sha256.Sum256([]byte(s))
	return string(h[:])
}

// JoinStatement is what the device signs to join: it binds the invitation, the
// workspace, the device's identity, both of its public keys and what it asked for to
// the TLS connection it is sent over (exporter), so a proof for one enrollment is not
// a proof for another. Fields are length-prefixed, so no two different requests have
// the same bytes.
func JoinStatement(workspaceID string, r JoinRequest, exporter []byte) []byte {
	return lp(nil, joinDomain, workspaceID, r.InviteID, r.DeviceID, r.DeviceName, r.MemberName,
		r.DevicePublicKey, digest(r.NetworkPublicKey), digest(r.SealingPublicKey), string(exporter))
}

// PollStatement is what the device signs to ask after an enrollment.
func PollStatement(workspaceID string, r PollRequest, exporter []byte) []byte {
	return lp(nil, pollDomain, workspaceID, r.EnrollmentID, r.DeviceID, string(exporter))
}

// ExporterLabel and ExporterBytes are the TLS exporter both ends use for channel binding.
const (
	ExporterLabel = "EXPORTER-werkbord-enrollment-v1"
	ExporterBytes = 32
)

// Errors of the protocol, as the client reports them.
var (
	// ErrRefused: the workspace did not accept the request. It does not say whether
	// the invitation never existed, was used, or has expired, on purpose.
	ErrRefused = errors.New("enrollment: the workspace refused: the invitation is not valid (it may have been used, withdrawn or expired)")
	// ErrDenied: an administrator refused the device.
	ErrDenied = errors.New("enrollment: an administrator denied this device")
	// ErrPending: the device is waiting for approval and the caller chose not to wait.
	ErrPending = errors.New("enrollment: waiting for an administrator to approve this device")
	// ErrNoEndpoint: none of the invitation's endpoints could be reached and verified.
	ErrNoEndpoint = errors.New("enrollment: none of the invitation's endpoints could be reached as the workspace")
)

// RejectedError is the workspace saying the request is fine but something the joiner
// chose cannot be used (a name already taken, say). It is for the one person holding a
// valid credential, so it may say what to change; it never says anything about the
// invitation itself.
type RejectedError struct{ Reason string }

func (e *RejectedError) Error() string {
	return "enrollment: the workspace cannot accept this: " + e.Reason
}

// Reject is what an Authority returns for such a request.
func Reject(reason string) error { return &RejectedError{Reason: reason} }

// DefaultPoll is how often a waiting device asks.
const DefaultPoll = 3 * time.Second
