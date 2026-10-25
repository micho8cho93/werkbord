package api

import (
	"devboard/internal/domain"
	"testing"
)

func TestNativeInteractiveCLIFailsClosedWithoutTerminalTransport(t *testing.T) {
	for _, signIn := range []domain.SignIn{domain.SignedIn, domain.SignedOut, domain.SignInUnknown, ""} {
		status := nativeInteractiveCLIStatus(signIn)
		if status.Supported || status.Authorized {
			t.Fatalf("sign-in %q incorrectly granted native execution: %+v", signIn, status)
		}
		want := "unknown"
		if signIn == domain.SignedIn {
			want = "verified"
		}
		if signIn == domain.SignedOut {
			want = "signed_out"
		}
		if status.Authentication != want {
			t.Fatalf("sign-in %q: got %q, want %q", signIn, status.Authentication, want)
		}
	}
}
