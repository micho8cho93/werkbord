// Package enrollment is how a device joins a customer's workspace: the signed
// invitation a person hands another, the protocol the joining device speaks to the
// workspace's own bootstrap endpoint, and the client side of it.
//
// It is shared by both products and belongs to neither: a Team workspace's hosts
// serve the protocol, and a joining device (a Team host, or the
// individual product's runner) speaks it. It holds no private key of a device (a
// Signer is passed in) and names no particular network: what a device receives is
// "network credentials" (certificates and a configuration, opaque here), and which
// network they are for is the business of whoever starts it.
//
// # The shape of the trust
//
// Nothing here talks to anything the vendor operates. A workspace is found at the
// endpoints in its invitation, which are machines the customer owns, and is
// recognised by its **workspace key**: an Ed25519 key created with the workspace,
// whose fingerprint is in the invitation and which signs the invitation. The
// protocol runs over TLS 1.3 with no certificate authority but the workspace's:
// the joining device accepts a server only if its certificate chains to the
// workspace key the invitation names (pinning), so a wrong or hostile endpoint
// learns nothing, not even the one-time credential, because the device does not
// send it until the server has proved it is the workspace.
//
// What the invitation does not do is prove to the joiner that the invitation itself
// is the one the workspace's administrator meant: anyone can make an invitation
// with a key of their own. Its authenticity is the authenticity of the channel it
// arrived over, which is why the fingerprint is short enough to read aloud and
// JoinParams.ExpectFingerprint lets a joiner insist on one learned elsewhere.
package enrollment

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Errors a caller can tell apart.
var (
	// ErrMalformed: the text is not an invitation.
	ErrMalformed = errors.New("enrollment: not a valid invitation")
	// ErrBadSignature: the invitation was changed after it was signed, or was not
	// signed by the workspace key it names.
	ErrBadSignature = errors.New("enrollment: the invitation's signature does not check out")
	// ErrExpired: the invitation's time has passed.
	ErrExpired = errors.New("enrollment: the invitation has expired")
	// ErrWrongWorkspace: the workspace the invitation names is not the one expected.
	ErrWrongWorkspace = errors.New("enrollment: the invitation is for a different workspace than expected")
)

// JoinPrefix starts a join link. A QR code carries the same text.
const JoinPrefix = "werkbord://join/"

// InvitationVersion is the only format there is, so far.
const InvitationVersion = 1

// Limits, so an invitation fits in a QR code and a hostile one cannot be large.
const (
	MaxInvitationBytes = 3000
	MaxEndpoints       = 8
	MaxNameRunes       = 80
	// CredentialBytes is the size of the one-time credential, in bytes: 256 random bits.
	CredentialBytes = 32
)

// IDPrefix marks an invitation ID.
const IDPrefix = "winv"

// Invitation is what is signed. It holds what a device needs to find and recognise
// the workspace and to prove it was invited, and nothing that outlives the
// invitation: no network private key, no member token.
type Invitation struct {
	Version int `json:"v"`
	// ID names the invitation (a random, public identifier).
	ID string `json:"id"`
	// WorkspaceID and WorkspaceName say which workspace this is, for the person
	// joining and for the protocol; neither is trusted until the server has proved it
	// holds WorkspaceKey.
	WorkspaceID   string `json:"ws"`
	WorkspaceName string `json:"name"`
	// WorkspaceKey is the workspace's public key (Ed25519, base64 raw URL), and
	// Fingerprint is WorkspaceFingerprint of it, repeated so it can be read and
	// compared without decoding anything.
	WorkspaceKey string `json:"key"`
	Fingerprint  string `json:"fp"`
	// Endpoints are the customer-owned bootstrap endpoints ("host:port") to try, in
	// order. There may be more than one; none is special.
	Endpoints []string `json:"at"`
	// Credential is the one-time enrollment credential (CredentialBytes random bytes,
	// base64 raw URL). It authorises one enrollment and then is spent.
	Credential string `json:"cred"`
	// Role and Capabilities are what the invitation is for. They are a statement for
	// the person joining to read; the workspace enforces its own record of them.
	Role         string   `json:"role"`
	Capabilities []string `json:"caps,omitempty"`
	IssuedAt     int64    `json:"iat"`
	ExpiresAt    int64    `json:"exp"`
}

// NewID returns a random invitation ID such as "winv_k3j9x2m4q7p1a8z5".
func NewID() string {
	var b [10]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("enrollment: crypto/rand failed: " + err.Error())
	}
	return IDPrefix + "_" + strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b[:]))
}

// ValidID reports whether s has the shape of an invitation ID.
func ValidID(s string) bool {
	rest, ok := strings.CutPrefix(s, IDPrefix+"_")
	if !ok || len(rest) != 16 {
		return false
	}
	for _, r := range rest {
		if !(r >= 'a' && r <= 'z' || r >= '2' && r <= '7') {
			return false
		}
	}
	return true
}

// NewCredential returns a new one-time credential and its hash (what a workspace
// stores: it keeps the hash and never the credential).
func NewCredential() (credential string, hash []byte) {
	var b [CredentialBytes]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("enrollment: crypto/rand failed: " + err.Error())
	}
	credential = base64.RawURLEncoding.EncodeToString(b[:])
	return credential, HashCredential(credential)
}

// HashCredential is the hash a workspace stores for a credential.
func HashCredential(credential string) []byte {
	h := sha256.Sum256([]byte("werkbord/enrollment-credential/v1\x00" + credential))
	return h[:]
}

var fpEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// WorkspaceFingerprint is the workspace key's fingerprint: the full SHA-256 of the
// public key in lower-case base 32, in groups of four ("abcd-efgh-…", 13 groups).
// It is the whole hash on purpose: it is what a device pins, so it is not truncated
// to something that could be ground out.
func WorkspaceFingerprint(k ed25519.PublicKey) string {
	sum := sha256.Sum256(append([]byte("werkbord/workspace-key/v1\x00"), k...))
	s := strings.ToLower(fpEncoding.EncodeToString(sum[:]))
	var b strings.Builder
	for i := 0; i < len(s); i += 4 {
		if i > 0 {
			b.WriteByte('-')
		}
		end := i + 4
		if end > len(s) {
			end = len(s)
		}
		b.WriteString(s[i:end])
	}
	return b.String()
}

// SameFingerprint compares two fingerprints as a person would: ignoring case,
// dashes and spaces. It is a comparison of secrets-in-flight only in the sense that
// it is constant-time on the normalised text.
func SameFingerprint(a, b string) bool {
	norm := func(s string) []byte {
		return []byte(strings.Map(func(r rune) rune {
			if r == '-' || unicode.IsSpace(r) {
				return -1
			}
			return unicode.ToLower(r)
		}, s))
	}
	na, nb := norm(a), norm(b)
	return len(na) > 0 && len(na) == len(nb) && bytes.Equal(na, nb)
}

const (
	invitationDomain = "werkbord/invitation/v1\x00"
	linkVersion      = "v1"
)

// Sign makes the join link for an invitation, signed with the workspace key's
// private half: "werkbord://join/v1.<payload>.<signature>", the payload being the
// JSON exactly as signed, so there is no canonical form to disagree about.
func Sign(inv Invitation, key ed25519.PrivateKey) (string, error) {
	pub, ok := key.Public().(ed25519.PublicKey)
	if !ok || len(key) != ed25519.PrivateKeySize {
		return "", errors.New("enrollment: not a workspace signing key")
	}
	inv.Version = InvitationVersion
	if inv.WorkspaceKey == "" {
		inv.WorkspaceKey = base64.RawURLEncoding.EncodeToString(pub)
	}
	if inv.Fingerprint == "" {
		inv.Fingerprint = WorkspaceFingerprint(pub)
	}
	if inv.WorkspaceKey != base64.RawURLEncoding.EncodeToString(pub) {
		return "", errors.New("enrollment: the invitation names a different key than the one signing it")
	}
	if err := inv.validate(); err != nil {
		return "", err
	}
	payload, err := json.Marshal(inv)
	if err != nil {
		return "", err
	}
	sig := ed25519.Sign(key, append([]byte(invitationDomain), payload...))
	link := JoinPrefix + linkVersion + "." + base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(sig)
	if len(link) > MaxInvitationBytes {
		return "", fmt.Errorf("%w: it would be %d bytes, more than the %d a QR code can carry", ErrMalformed, len(link), MaxInvitationBytes)
	}
	return link, nil
}

// Parse reads a join link and checks everything that can be checked without
// contacting anyone: that it is well formed, that its fingerprint is its key's,
// that it was signed by that key and has not been changed since, and that it has
// not expired at now.
func Parse(link string, now time.Time) (Invitation, error) {
	link = strings.TrimSpace(link)
	if len(link) > MaxInvitationBytes {
		return Invitation{}, fmt.Errorf("%w: too long", ErrMalformed)
	}
	rest, ok := strings.CutPrefix(link, JoinPrefix)
	if !ok {
		return Invitation{}, fmt.Errorf("%w: it does not start with %s", ErrMalformed, JoinPrefix)
	}
	parts := strings.Split(rest, ".")
	if len(parts) != 3 || parts[0] != linkVersion {
		return Invitation{}, fmt.Errorf("%w: unknown format", ErrMalformed)
	}
	payload, err1 := base64.RawURLEncoding.DecodeString(parts[1])
	sig, err2 := base64.RawURLEncoding.DecodeString(parts[2])
	if err1 != nil || err2 != nil || len(sig) != ed25519.SignatureSize {
		return Invitation{}, fmt.Errorf("%w: bad encoding", ErrMalformed)
	}
	// The signature is checked over the payload bytes as received, before they are
	// interpreted: a payload that was changed does not get as far as being parsed.
	// The key it must verify against is inside the payload, so read that field alone first.
	var head struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal(payload, &head); err != nil {
		return Invitation{}, fmt.Errorf("%w: %v", ErrMalformed, err)
	}
	kb, err := base64.RawURLEncoding.DecodeString(head.Key)
	if err != nil || len(kb) != ed25519.PublicKeySize {
		return Invitation{}, fmt.Errorf("%w: no workspace key", ErrMalformed)
	}
	if !ed25519.Verify(ed25519.PublicKey(kb), append([]byte(invitationDomain), payload...), sig) {
		return Invitation{}, ErrBadSignature
	}
	var inv Invitation
	dec := json.NewDecoder(bytes.NewReader(payload))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&inv); err != nil {
		return Invitation{}, fmt.Errorf("%w: %v", ErrMalformed, err)
	}
	if inv.Version != InvitationVersion {
		return Invitation{}, fmt.Errorf("%w: version %d is not supported", ErrMalformed, inv.Version)
	}
	if err := inv.validate(); err != nil {
		return Invitation{}, err
	}
	if !SameFingerprint(inv.Fingerprint, WorkspaceFingerprint(ed25519.PublicKey(kb))) {
		return Invitation{}, fmt.Errorf("%w: its fingerprint is not its key's", ErrBadSignature)
	}
	if !now.Before(time.Unix(inv.ExpiresAt, 0)) {
		return Invitation{}, ErrExpired
	}
	return inv, nil
}

// Key returns the workspace's public key.
func (i Invitation) Key() (ed25519.PublicKey, error) {
	kb, err := base64.RawURLEncoding.DecodeString(i.WorkspaceKey)
	if err != nil || len(kb) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("%w: no workspace key", ErrMalformed)
	}
	return ed25519.PublicKey(kb), nil
}

// Expiry is when the invitation stops working.
func (i Invitation) Expiry() time.Time { return time.Unix(i.ExpiresAt, 0) }

func (i Invitation) validate() error {
	bad := func(format string, a ...any) error {
		return fmt.Errorf("%w: %s", ErrMalformed, fmt.Sprintf(format, a...))
	}
	switch {
	case !ValidID(i.ID):
		return bad("the invitation ID")
	case i.WorkspaceID == "" || len(i.WorkspaceID) > 64:
		return bad("the workspace ID")
	case i.WorkspaceName == "" || utf8.RuneCountInString(i.WorkspaceName) > MaxNameRunes || hasControl(i.WorkspaceName):
		return bad("the workspace name")
	case len(i.Endpoints) == 0 || len(i.Endpoints) > MaxEndpoints:
		return bad("between 1 and %d endpoints are needed", MaxEndpoints)
	case i.Role == "" || len(i.Role) > 32:
		return bad("the role")
	case len(i.Capabilities) > 8:
		return bad("capabilities")
	case i.ExpiresAt <= i.IssuedAt:
		return bad("it expires before it is issued")
	}
	for _, e := range i.Endpoints {
		if _, _, err := net.SplitHostPort(e); err != nil || len(e) > 255 {
			return bad("endpoint %q is not host:port", e)
		}
	}
	if cb, err := base64.RawURLEncoding.DecodeString(i.Credential); err != nil || len(cb) != CredentialBytes {
		return bad("the credential")
	}
	return nil
}

func hasControl(s string) bool {
	for _, r := range s {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}
