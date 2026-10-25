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

func TestThePremiumKeyIsSignedOnlyWhenThereIsOne(t *testing.T) {
	r := JoinRequest{InviteID: "i", DeviceID: "d", DeviceName: "n", MemberName: "m", DevicePublicKey: "k", NetworkPublicKey: "net", SealingPublicKey: "s"}
	legacy := lp(nil, joinDomain, "w", r.InviteID, r.DeviceID, r.DeviceName, r.MemberName, r.DevicePublicKey, digest(r.NetworkPublicKey), digest(r.SealingPublicKey), "ex")
	if string(JoinStatement("w", r, []byte("ex"))) != string(legacy) {
		t.Fatal("a request with no key must be signed exactly as before, so an older Workspace Host still accepts it")
	}
	r.PremiumKey = "wbpremium-x"
	with := JoinStatement("w", r, []byte("ex"))
	if string(with) == string(legacy) {
		t.Fatal("the key is not part of what the device signs")
	}
	r.PremiumKey = "wbpremium-y"
	if string(JoinStatement("w", r, []byte("ex"))) == string(with) {
		t.Fatal("a different key signs the same")
	}
}
