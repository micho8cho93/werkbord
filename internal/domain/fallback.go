package domain

import (
	"fmt"
	"strings"
	"time"
)

// Automatic provider fallback is opt-in per project, bounded, and at most once per failed run. It continues work with
// another agent from a handoff the controller verified, through the same continuation a person uses; it never replays a
// command and never stops a session that is still open.

// SettingFallbackPrefix is the settings key prefix: the project ID follows.
const SettingFallbackPrefix = "fallback:"

// SettingFallbackLedgerPrefix keys the record of what fallback claimed and started for a project.
const SettingFallbackLedgerPrefix = "fallback-ledger:"

// FallbackSettings are a project's choices. The zero value is off.
type FallbackSettings struct {
	Enabled bool `json:"enabled"`
	// Providers are the agent IDs that may take over, in order of preference.
	Providers []string `json:"providers"`
	// MaxSwitches caps automatic continuations of one task. DailyLimit caps them for the project in any 24 hours. They count
	// starts, not money: what a provider bills is not known to Werkbord.
	MaxSwitches int `json:"maxSwitches"`
	DailyLimit  int `json:"dailyLimit"`
}

// Normalize fills the defaults of an enabled configuration.
func (f *FallbackSettings) Normalize() {
	if f.Providers == nil {
		f.Providers = []string{}
	}
	if f.MaxSwitches == 0 {
		f.MaxSwitches = 1
	}
	if f.DailyLimit == 0 {
		f.DailyLimit = 3
	}
}

// Validate checks the choices. An enabled configuration must name at least one provider.
func (f FallbackSettings) Validate() error {
	if f.MaxSwitches < 1 || f.MaxSwitches > 3 {
		return fmt.Errorf("%w: automatic switches per task must be 1 to 3", ErrInvalid)
	}
	if f.DailyLimit < 1 || f.DailyLimit > 20 {
		return fmt.Errorf("%w: automatic switches per day must be 1 to 20", ErrInvalid)
	}
	if len(f.Providers) > 8 {
		return fmt.Errorf("%w: name at most 8 fallback agents", ErrInvalid)
	}
	seen := map[string]bool{}
	for _, id := range f.Providers {
		if err := (ExecutionConfig{Agent: id}).Validate(); err != nil || id == "" || seen[id] {
			return fmt.Errorf("%w: %q is not a distinct agent ID", ErrInvalid, id)
		}
		seen[id] = true
	}
	if f.Enabled && len(f.Providers) == 0 {
		return fmt.Errorf("%w: choose at least one agent that may take over", ErrInvalid)
	}
	return nil
}

// FallbackKind reports whether a provider failure is one another provider can plausibly get past. Authentication, a full
// context window and unknown errors need a person.
func FallbackKind(k ProviderErrorKind) bool { return k == ProviderRateLimit || k == ProviderQuota }

// Ledger results.
const (
	FallbackClaimed  = "claimed"  // reserved; nothing known yet. Blocks a second attempt, even after a crash
	FallbackStarted  = "started"  // a continuation run exists
	FallbackDeclined = "declined" // looked at and not started; the reason says why. Never retried automatically
	FallbackRetry    = "retry"    // the start was refused for a passing reason (such as capacity); eligible again
)

// FallbackEntry is one attempt, keyed by the failed run it would continue: that run ID is the idempotency key.
type FallbackEntry struct {
	RunID  string    `json:"runId"`
	TaskID string    `json:"taskId"`
	Agent  string    `json:"agent"`
	Result string    `json:"result"`
	Reason string    `json:"reason,omitempty"`
	NewRun string    `json:"newRunId,omitempty"`
	At     time.Time `json:"at"`
}

// FallbackLedger is a project's attempts, newest last, bounded.
type FallbackLedger struct {
	Entries []FallbackEntry `json:"entries"`
}

// FallbackOrigin is the origin recorded on a run an automatic fallback started; the failed run follows the colon.
func FallbackOrigin(parentRunID string) string { return "fallback:" + parentRunID }

// FallbackParent is the failed run an origin names, or "" for any other origin.
func FallbackParent(origin string) string {
	id, ok := strings.CutPrefix(origin, "fallback:")
	if !ok {
		return ""
	}
	return id
}

// HandoffUnverified says why a handoff cannot be trusted as the controller's own reading of the work, or "" when it is. The
// controller writes GitState from the worktree when it captures a handoff; the placeholders it writes when it could not
// are not evidence, and neither is a tree mid-operation or in conflict.
func HandoffUnverified(h *Handoff) string {
	switch {
	case h == nil:
		return "no handoff was captured"
	case h.GeneratedAt.IsZero():
		return "the handoff has no capture time"
	case !strings.HasPrefix(h.GitState, "Branch: ") || !strings.Contains(h.GitState, "; HEAD: "):
		return "the controller could not read the work's Git state"
	case !strings.Contains(h.GitState, "; conflicts: 0;") || !strings.HasSuffix(h.GitState, "operation: ") && !strings.Contains(h.GitState, "operation: ;"):
		return "the work is in conflict or in the middle of a Git operation"
	case len(h.FilesChanged) == 0 && (strings.TrimSpace(h.Summary) == "" || strings.HasPrefix(h.Summary, "Implementation details have not been reported")):
		return "there is no recorded work for another agent to continue from"
	}
	return ""
}
