package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

// The assistant (internal/assistant) is a conversation with the person's own coding agent runtime that can look at
// and change the board through application operations (internal/appops). What it keeps is deliberately small: a
// handle to resume the provider's own conversation, the changes it proposed and what became of each, and an
// append-only audit of every operation. It does not keep a transcript.

// Prefixes of the assistant's identifiers.
const (
	PrefixAssistantSession = "ast"
	PrefixAssistantAction  = "act"
)

// AssistantSessionState says what an assistant session is doing.
type AssistantSessionState string

const (
	// AssistantIdle: nothing is running; a message can be sent.
	AssistantIdle AssistantSessionState = "idle"
	// AssistantRunning: a turn is in progress.
	AssistantRunning AssistantSessionState = "running"
	// AssistantAwaiting: a turn ended with a change waiting for the person to confirm or decline it.
	AssistantAwaiting AssistantSessionState = "awaiting_confirmation"
)

// Valid reports whether s is a known state.
func (s AssistantSessionState) Valid() bool {
	switch s {
	case AssistantIdle, AssistantRunning, AssistantAwaiting:
		return true
	}
	return false
}

// AssistantSession is the minimum needed to continue a conversation and to recover after a restart.
type AssistantSession struct {
	ID       string `json:"id"`
	Provider string `json:"provider"`
	// Model and Reasoning are what the person chose. Empty means the provider's own default.
	Model     string `json:"model,omitempty"`
	Reasoning string `json:"reasoning,omitempty"`
	// ProviderRef is the provider's own handle for the conversation (a session or thread id). It is what makes the
	// next turn a continuation. The conversation itself stays with the provider; Werkbord keeps no copy.
	ProviderRef string                `json:"providerRef,omitempty"`
	State       AssistantSessionState `json:"state"`
	// Turns counts the turns that ended, however they ended.
	Turns int `json:"turns"`
	// LastError is the code of the last turn that failed, cleared by the next one that succeeds.
	LastError string `json:"lastError,omitempty"`
	// ReportedAt is the time up to which the assistant has been told what became of the changes it proposed (confirmed,
	// declined, expired), so that each outcome is told once, with the next message.
	ReportedAt time.Time `json:"reportedAt"`
	Version    int64     `json:"version"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

// AssistantActionState says what became of a change the assistant proposed.
//
//	pending ──▶ executing ──▶ executed   the person confirmed it and it was carried out
//	   │            └───────▶ failed     the person confirmed it and it could not be carried out
//	   ├──────────▶ rejected             the person declined it
//	   ├──────────▶ expired              nobody answered in time
//	   └──────────▶ withdrawn            the session was deleted or restarted before anyone answered
//
// Executing is the claim: exactly one confirmation can move a pending action there, so a change is never carried
// out twice. An action found executing after a restart was cut off: whether it was applied is unknown, so it is
// marked failed and the person is told to look.
type AssistantActionState string

const (
	ActionPending   AssistantActionState = "pending"
	ActionExecuting AssistantActionState = "executing"
	ActionExecuted  AssistantActionState = "executed"
	ActionFailed    AssistantActionState = "failed"
	ActionRejected  AssistantActionState = "rejected"
	ActionExpired   AssistantActionState = "expired"
	ActionWithdrawn AssistantActionState = "withdrawn"
)

// Valid reports whether s is a known state.
func (s AssistantActionState) Valid() bool {
	switch s {
	case ActionPending, ActionExecuting, ActionExecuted, ActionFailed, ActionRejected, ActionExpired, ActionWithdrawn:
		return true
	}
	return false
}

// Settled reports whether the action has reached an end.
func (s AssistantActionState) Settled() bool {
	return s.Valid() && s != ActionPending && s != ActionExecuting
}

// Open reports whether the action may still be carried out or decided.
func (s AssistantActionState) Open() bool { return s == ActionPending || s == ActionExecuting }

// AssistantAction is one change an operation wants to make, held until the person confirms it. Args are exactly
// what will be carried out: confirming is approving these bytes (ArgsHash is their digest), and nothing the model
// says afterwards can change them.
type AssistantAction struct {
	ID        string `json:"id"`
	SessionID string `json:"sessionId"`
	// Principal is who proposed it (appops.Principal.ID): the session's own principal, so one session's
	// confirmation can never be used by another.
	Principal string               `json:"principal"`
	Operation string               `json:"operation"`
	Args      json.RawMessage      `json:"args"`
	ArgsHash  string               `json:"argsHash"`
	Summary   string               `json:"summary"`
	State     AssistantActionState `json:"state"`
	// Outcome is a short account of the result or of the error, once settled.
	Outcome    string     `json:"outcome,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
	ExpiresAt  time.Time  `json:"expiresAt"`
	ResolvedAt *time.Time `json:"resolvedAt,omitempty"`
}

// ArgsDigest is the digest of an action's arguments.
func ArgsDigest(args []byte) string {
	sum := sha256.Sum256(args)
	return hex.EncodeToString(sum[:])
}

// AuditKind says whether an audited operation only looked or changed something.
type AuditKind string

const (
	AuditRead     AuditKind = "read"
	AuditMutation AuditKind = "mutation"
	// AuditSystem: something about the assistant itself, such as a session starting or recovering.
	AuditSystem AuditKind = "system"
)

// Audit outcomes.
const (
	AuditOutcomeOK        = "ok"        // a read was answered, or a change was carried out
	AuditOutcomeProposed  = "proposed"  // a change was put to the person
	AuditOutcomeConfirmed = "confirmed" // the person approved it; written before it is carried out, so the intent is on record
	AuditOutcomeRejected  = "rejected"  // the person declined it
	AuditOutcomeExpired   = "expired"
	AuditOutcomeDenied    = "denied" // not permitted: no grant, outside the principal's projects, or refused by a rule
	AuditOutcomeInvalid   = "invalid"
	AuditOutcomeFailed    = "failed" // permitted and confirmed, but the domain service refused or failed
)

// AssistantAuditEntry is one line of the audit trail. The trail is append-only and chained: each entry carries the
// hash of the one before, so removing or editing a line is detectable (appops.VerifyAudit).
type AssistantAuditEntry struct {
	Seq       int64     `json:"seq"`
	At        time.Time `json:"at"`
	SessionID string    `json:"sessionId,omitempty"`
	ActionID  string    `json:"actionId,omitempty"`
	// Actor is who did it, such as "assistant:claude-code", and Via where it came in, such as "assistant" or "mcp".
	Actor     string    `json:"actor"`
	Operation string    `json:"operation"`
	Kind      AuditKind `json:"kind"`
	Outcome   string    `json:"outcome"`
	ArgsHash  string    `json:"argsHash,omitempty"`
	// Detail is a short human account: what was asked, what was refused and why. Never a credential or a
	// transcript; operations put only identifiers and the words of the change in it.
	Detail   string `json:"detail,omitempty"`
	PrevHash string `json:"prevHash"`
	Hash     string `json:"hash"`
}

// AuditHash is the hash that chains e to the entry before it. It covers everything but Seq (which the database
// assigns) and Hash itself.
func AuditHash(e AssistantAuditEntry) string {
	h := sha256.New()
	for _, part := range []string{
		e.PrevHash, strconv.FormatInt(e.At.UTC().UnixMilli(), 10), e.SessionID, e.ActionID, e.Actor, e.Operation,
		string(e.Kind), e.Outcome, e.ArgsHash, e.Detail,
	} {
		// Length-prefixed, so no two different entries can feed the hash the same bytes.
		fmt.Fprintf(h, "%d:%s|", len(part), part)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// AuditCheckpoint remembers the end of the audit that was removed under the retention period: everything up to ThroughSeq is
// gone, and Hash is the hash of the last entry removed, which the first entry that remains must follow.
type AuditCheckpoint struct {
	ThroughSeq int64     `json:"throughSeq"`
	Hash       string    `json:"hash"`
	Pruned     int64     `json:"pruned"` // how many entries have been removed in all
	UpdatedAt  time.Time `json:"updatedAt"`
}
