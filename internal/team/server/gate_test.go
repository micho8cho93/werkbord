package server

import (
	"encoding/json"
	"testing"

	"devboard/internal/team/domain"
	"devboard/internal/team/service"
)

func TestUnsupportedNetworkProtocolAndTrailingDataFailClosed(t *testing.T) {
	for _, raw := range []string{`{"deviceId":"one","protocolVersion":999}`, `{} {}`, `{"deviceId":`, `null`} {
		if _, err := DecodeNodeConfig(json.RawMessage(raw)); err == nil {
			t.Fatalf("unsupported or corrupt network description accepted: %s", raw)
		}
	}
}

func TestEnrollmentCannotAddAMemberPastTheTeamSizeLimit(t *testing.T) {
	h, created, _ := newFirstHost(t, "localhost")
	h.svc.EnforceTeamSize(1)
	h.serve()
	invite := h.invite(created, service.EnrollInviteInput{Label: "No room"})
	device := newDevice(t, "over-limit")
	if _, err := device.join(invite.Link, "Extra member"); err == nil {
		t.Fatal("size-limited enrollment issued a device credential", err)
	}
	ms, err := h.svc.ListMembers(bg, h.owner(created))
	if err != nil || len(ms) != 1 {
		t.Fatalf("enrollment exceeded the team size: %d, %v", len(ms), err)
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
