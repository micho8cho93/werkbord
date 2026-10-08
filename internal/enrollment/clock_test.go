package enrollment

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"testing"
	"time"
)

func linkIssuedAt(t *testing.T, issued time.Time, life time.Duration) string {
	t.Helper()
	_, key, _ := ed25519.GenerateKey(rand.Reader)
	cred, _ := NewCredential()
	link, err := Sign(Invitation{ID: NewID(), WorkspaceID: "ws", WorkspaceName: "Workspace", Endpoints: []string{"127.0.0.1:7440"}, Credential: cred, Role: "member", IssuedAt: issued.Unix(), ExpiresAt: issued.Add(life).Unix()}, key)
	if err != nil {
		t.Fatal(err)
	}
	return link
}

// An invitation made a moment ago must be usable by a computer whose clock is a little behind the one that made it:
// two computers' clocks are never exactly alike, and the workspace decides by its own clock anyway.
func TestAFreshInvitationIsAcceptedByAClockThatIsAFewMinutesBehind(t *testing.T) {
	made := time.Now()
	link := linkIssuedAt(t, made, 24*time.Hour)
	for _, behind := range []time.Duration{0, 45 * time.Second, 2 * time.Minute, ClockSkew - time.Second} {
		if _, err := Parse(link, made.Add(-behind)); err != nil {
			t.Errorf("a clock %v behind refused a fresh invitation: %v", behind, err)
		}
	}
}

// A clock that is plainly wrong is told apart from a link that is not an invitation, and the invitation comes back with the
// error so that the person can be shown when it was made.
func TestAClockFarBehindIsReportedAsTheClockNotAsAnInvalidLink(t *testing.T) {
	made := time.Now()
	link := linkIssuedAt(t, made, 24*time.Hour)
	inv, err := Parse(link, made.Add(-ClockSkew-time.Minute))
	if !errors.Is(err, ErrClockBehind) {
		t.Fatalf("a clock 6 minutes behind = %v, want ErrClockBehind", err)
	}
	if errors.Is(err, ErrMalformed) || errors.Is(err, ErrExpired) {
		t.Error("a wrong clock was reported as a malformed or expired link")
	}
	if inv.Issued().Unix() != made.Unix() || inv.WorkspaceName != "Workspace" {
		t.Errorf("the invitation was not returned with the error: %+v", inv)
	}
}

func TestAnExpiredInvitationIsReturnedWithTheError(t *testing.T) {
	made := time.Now().Add(-48 * time.Hour)
	link := linkIssuedAt(t, made, 24*time.Hour)
	inv, err := Parse(link, time.Now())
	if !errors.Is(err, ErrExpired) {
		t.Fatalf("an invitation two days old = %v, want ErrExpired", err)
	}
	if !inv.Expiry().Equal(time.Unix(made.Add(24*time.Hour).Unix(), 0)) {
		t.Errorf("the expiry was not returned: %v", inv.Expiry())
	}
}
