package server

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"testing"
	"time"

	"devboard/internal/team/domain"
	"devboard/internal/team/license"
	"devboard/internal/team/service"
)

func TestUnsupportedNetworkProtocolAndTrailingDataFailClosed(t *testing.T) {
	for _, raw := range []string{`{"deviceId":"one","protocolVersion":999}`, `{} {}`, `{"deviceId":`, `null`} {
		if _, err := DecodeNodeConfig(json.RawMessage(raw)); err == nil {
			t.Fatalf("unsupported or corrupt network description accepted: %s", raw)
		}
	}
}

func TestEnrollmentCannotAddAMemberPastTheLicenseSeatLimit(t *testing.T) {
	h, created, _ := newFirstHost(t, "localhost")
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := license.Issue(license.Claims{Schema: license.Schema, Product: "werkbord-team", Edition: license.EditionTeam, ID: "lic_enrollment", Customer: "org_test", Seats: 1, IssuedAt: time.Now().Add(-time.Hour)}, key)
	if err != nil {
		t.Fatal(err)
	}
	h.svc.EnforceLicense(pub, raw)
	h.serve()
	invite := h.invite(created, service.EnrollInviteInput{Label: "No spare seat"})
	device := newDevice(t, "over-limit")
	if _, err := device.join(invite.Link, "Extra member"); err == nil {
		t.Fatal("seat-limited enrollment issued a device credential", err)
	}
	ms, err := h.svc.ListMembers(bg, h.owner(created))
	if err != nil || len(ms) != 1 {
		t.Fatalf("enrollment exceeded the seat count: %d, %v", len(ms), err)
	}
}

func FuzzNodeConfigParser(f *testing.F) {
	seed, _ := json.Marshal(domain.NodeConfig{DeviceID: "device", OverlayAddr: "10.88.0.2"})
	f.Add(seed)
	f.Add([]byte(`{"protocolVersion":999}`))
	f.Fuzz(func(t *testing.T, raw []byte) {
		_, _ = DecodeNodeConfig(raw)
	})
}
