package domain

import "testing"

func TestProviderFailureClassification(t *testing.T) {
	for _, tc := range []struct {
		message string
		kind    ProviderErrorKind
	}{
		{"429 too many requests", ProviderRateLimit},
		{"429 rate_limit: insufficient_quota", ProviderQuota},
		{"usage limit reached", ProviderQuota},
		{"context_length_exceeded", ProviderContext},
		{"prompt is too long", ProviderContext},
		{"invalid_api_key", ProviderAuth},
		{"connection reset", ProviderOther},
	} {
		if got := ClassifyProviderError(tc.message); got != tc.kind {
			t.Errorf("%q: %s != %s", tc.message, got, tc.kind)
		}
	}
}
