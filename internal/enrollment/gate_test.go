package enrollment

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"
)

func FuzzInvitationParser(f *testing.F) {
	_, key, _ := ed25519.GenerateKey(rand.Reader)
	at := time.Unix(1800000000, 0)
	cred, _ := NewCredential()
	link, _ := Sign(Invitation{ID: NewID(), WorkspaceID: "ws", WorkspaceName: "Workspace", Endpoints: []string{"127.0.0.1:7440"}, Credential: cred, Role: "member", IssuedAt: at.Unix(), ExpiresAt: at.Add(time.Hour).Unix()}, key)
	f.Add(link)
	f.Add("")
	f.Add(JoinPrefix + "v99.x.y")
	f.Fuzz(func(t *testing.T, s string) {
		inv, err := Parse(s, at)
		if err == nil {
			if inv.validate() != nil || inv.Version != InvitationVersion || !at.Before(inv.Expiry()) {
				t.Fatal("invalid invitation accepted")
			}
			if _, err := ParseSaved(s); err != nil {
				t.Fatal("saved parser lost verification")
			}
		}
	})
}

func TestSavedEnrollmentRetainsSignatureAndShapeChecks(t *testing.T) {
	_, key, _ := ed25519.GenerateKey(rand.Reader)
	at := time.Unix(1800000000, 0)
	cred, _ := NewCredential()
	link, err := Sign(Invitation{ID: NewID(), WorkspaceID: "ws", WorkspaceName: "Workspace", Endpoints: []string{"127.0.0.1:7440"}, Credential: cred, Role: "member", IssuedAt: at.Unix(), ExpiresAt: at.Add(time.Hour).Unix()}, key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(link, at.Add(2*time.Hour)); err != ErrExpired {
		t.Fatal("new invitation expiry bypassed", err)
	}
	if _, err := ParseSaved(link); err != nil {
		t.Fatal(err)
	}
	tampered := link[:len(link)-4] + "AAAA"
	if _, err := ParseSaved(tampered); err == nil {
		t.Fatal("saved invitation forgery accepted")
	}
	if _, err := Sign(Invitation{}, nil); err == nil {
		t.Fatal("nil key accepted")
	}
}
