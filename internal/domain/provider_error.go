package domain

import "strings"

// ProviderErrorKind classifies provider failures, never tool output. Unknown errors
// require intervention. Classification does not establish that replay is safe.
type ProviderErrorKind string

const (
	ProviderRateLimit ProviderErrorKind = "rate_limit"
	ProviderQuota     ProviderErrorKind = "quota_exhausted"
	ProviderContext   ProviderErrorKind = "context_exhausted"
	ProviderAuth      ProviderErrorKind = "authentication"
	ProviderOther     ProviderErrorKind = "other"
)

// ClassifyProviderError is a conservative fallback for CLI protocols that supply
// only a message. Quota takes precedence over a generic HTTP 429/rate-limit label.
func ClassifyProviderError(message string) ProviderErrorKind {
	s := strings.ToLower(message)
	has := func(words ...string) bool {
		for _, w := range words {
			if strings.Contains(s, w) {
				return true
			}
		}
		return false
	}
	switch {
	case has("insufficient_quota", "quota", "usage limit", "usage_limit", "credit balance", "billing", "spending limit"):
		return ProviderQuota
	case has("context_length_exceeded", "context window", "context length", "too many tokens", "prompt is too long"):
		return ProviderContext
	case has("unauthorized", "unauthenticated", "authentication", "invalid api key", "invalid_api_key", "not signed in", "token expired"):
		return ProviderAuth
	case has("rate_limit", "rate limit", "too many requests", "429"):
		return ProviderRateLimit
	default:
		return ProviderOther
	}
}

func ProviderRecoveryMessage(kind ProviderErrorKind) string {
	switch kind {
	case ProviderRateLimit:
		return "Temporary provider rate limit. Review the session before continuing."
	case ProviderQuota:
		return "Provider quota exhausted. Restore usage allowance, then review and continue the session."
	case ProviderContext:
		return "Provider context window exhausted. Prepare a session handoff before continuing."
	case ProviderAuth:
		return "Provider authentication failed. Sign in on the runner, then continue."
	default:
		return "Provider error. Inspect the recorded session before continuing."
	}
}

// ProviderBlocker retains the failure category with the session and its worktree.
// A failed turn may have performed side effects; it is never treated as success.
func ProviderBlocker(message string) Blocker {
	kind := ClassifyProviderError(message)
	return Blocker{Summary: ProviderRecoveryMessage(kind), Detail: message, Source: BlockerReport, ProviderKind: kind}
}
