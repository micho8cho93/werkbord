// Package runnerwire defines the narrow, versioned runner protocol. It carries
// execution jobs and observations, never controller administration or credentials.
package runnerwire

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/url"
	"strings"
	"time"

	"devboard/internal/agent"
	"devboard/internal/domain"
)

const Lease = 90 * time.Second
const OnlineWindow = 30 * time.Second

type Pairing struct {
	Code       string    `json:"code"`
	ExpiresAt  time.Time `json:"expiresAt"`
	Projects   []string  `json:"projects"`
	AllowClone bool      `json:"allowClone"`
	RunnerID   string    `json:"runnerId,omitempty"`
	Used       bool      `json:"used"`
}
type Join struct {
	Secret    string `json:"secret"`
	PublicKey string `json:"publicKey"`
	Name      string `json:"name"`
	Hostname  string `json:"hostname"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
	Version   string `json:"version"`
}
type Job struct {
	PreviousCommit    string            `json:"previousCommit,omitempty"`
	PreviousPublished bool              `json:"previousPublished,omitempty"`
	PreviousBranch    string            `json:"previousBranch,omitempty"`
	Run               domain.Run        `json:"run"`
	RemoteURL         string            `json:"remoteUrl"`
	TargetBranch      string            `json:"targetBranch"`
	ExpectedCommit    string            `json:"expectedCommit,omitempty"`
	AllowClone        bool              `json:"allowClone"`
	Commands          []Command         `json:"commands"`
	Ack               int64             `json:"ack"`
	OutputBytes       int64             `json:"outputBytes,omitempty"`
	Questions         map[string]string `json:"questions,omitempty"` // adapter ref to controller question ID
	Replies           int               `json:"replies,omitempty"`
	Turn              string            `json:"turn,omitempty"`
}
type Command struct {
	ID         string `json:"id"`
	Kind       string `json:"kind"` // send, respond, finish, stop
	Ref        string `json:"ref,omitempty"`
	Text       string `json:"text,omitempty"`
	QuestionID string `json:"questionId,omitempty"`
}
type Observation struct {
	Seq         int64         `json:"seq"`
	Kind        string        `json:"kind"` // started, event, ended, command, workspace
	Event       *agent.Event  `json:"event,omitempty"`
	Result      *agent.Result `json:"result,omitempty"`
	CommandID   string        `json:"commandId,omitempty"`
	Error       string        `json:"error,omitempty"`
	Branch      string        `json:"branch,omitempty"`
	BaseCommit  string        `json:"baseCommit,omitempty"`
	Uncommitted *bool         `json:"uncommitted,omitempty"`
	HeadCommit  string        `json:"headCommit,omitempty"`
	Usage       *domain.Usage `json:"usage,omitempty"`
}
type Report struct {
	RunID        string        `json:"runId"`
	Observations []Observation `json:"observations"`
}
type Sync struct {
	RunnerID     string                    `json:"runnerId"`
	Sequence     int64                     `json:"sequence"`
	At           time.Time                 `json:"at"`
	Capabilities domain.RunnerCapabilities `json:"capabilities"`
	Reports      []Report                  `json:"reports"`
}
type SyncReply struct {
	Jobs         []Job            `json:"jobs"`
	Acks         map[string]int64 `json:"acks"`
	LeaseSeconds int              `json:"leaseSeconds"`
	Projects     []string         `json:"projects"`
	AllowClone   bool             `json:"allowClone"`
	Capacity     int              `json:"capacity"`
	Disabled     bool             `json:"disabled"`
}

func Signature(key ed25519.PrivateKey, body []byte) string {
	return base64.RawURLEncoding.EncodeToString(ed25519.Sign(key, body))
}
func Verify(publicKey, signature string, body []byte) bool {
	key, e := base64.RawURLEncoding.DecodeString(publicKey)
	if e != nil || len(key) != ed25519.PublicKeySize {
		return false
	}
	sig, e := base64.RawURLEncoding.DecodeString(signature)
	return e == nil && ed25519.Verify(key, body, sig)
}
func SecretHash(secret string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(secret))) }

// The join code includes the controller's private address; no discovery relay.
func EncodeCode(address, secret string) string {
	return "db1." + base64.RawURLEncoding.EncodeToString([]byte(address)) + "." + secret
}
func DecodeCode(code string) (string, string, error) {
	parts := strings.Split(code, ".")
	if len(parts) != 3 || parts[0] != "db1" {
		return "", "", fmt.Errorf("invalid pairing code")
	}
	address, e := base64.RawURLEncoding.DecodeString(parts[1])
	if e != nil {
		return "", "", e
	}
	u, e := url.Parse(string(address))
	if e != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" || (u.Scheme != "https" && u.Scheme != "http") {
		return "", "", fmt.Errorf("invalid controller address")
	}
	secret, e := base64.RawURLEncoding.DecodeString(parts[2])
	if e != nil || len(secret) != 24 {
		return "", "", fmt.Errorf("invalid pairing secret")
	}
	return u.String(), parts[2], nil
}

// SafeRemote excludes credentials, local paths, remote helpers and flags.
func SafeRemote(remote string) bool {
	if strings.HasPrefix(remote, "git@") && !strings.ContainsAny(remote, " \t\n\r\x00") && strings.Contains(remote, ":") && !strings.Contains(remote, "..") {
		return true
	}
	u, e := url.Parse(remote)
	if e != nil || u.Host == "" || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	if u.Scheme == "https" {
		return u.User == nil
	}
	return u.Scheme == "ssh" && (u.User == nil || u.User.String() == "git")
}
