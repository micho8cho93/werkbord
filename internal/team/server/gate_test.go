package server

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"werkbord/internal/enrollment"
	"werkbord/internal/team/domain"
	"werkbord/internal/team/service"
)

func TestUnsupportedNetworkProtocolAndTrailingDataFailClosed(t *testing.T) {
	for _, raw := range []string{`{"deviceId":"one","protocolVersion":999}`, `{} {}`, `{"deviceId":`, `null`} {
		if _, err := DecodeNodeConfig(json.RawMessage(raw)); err == nil {
			t.Fatalf("unsupported or corrupt network description accepted: %s", raw)
		}
	}
}

// fakePlan accepts a key that starts with "good-" and names it by the rest; the key format has its own tests.
func fakePlan(key string) (string, error) {
	if id, ok := strings.CutPrefix(key, "good-"); ok && id != "" {
		return id, nil
	}
	return "", errors.New("not an unlock key")
}

func TestEnrollmentIsHeldToThePlan(t *testing.T) {
	h, created, _ := newFirstHost(t, "localhost")
	h.svc.EnforcePlan(fakePlan)
	h.serve()
	owner := h.owner(created)
	members := func() int {
		ms, err := h.svc.ListMembers(bg, owner)
		if err != nil {
			t.Fatal(err)
		}
		return len(ms)
	}
	withKey := func(key string) func(*enrollment.JoinParams) {
		return func(p *enrollment.JoinParams) { p.PremiumKey = key }
	}
	join := func(name, key string) error {
		invite := h.invite(created, service.EnrollInviteInput{Label: name})
		_, err := newDevice(t, name).join(invite.Link, name, withKey(key))
		return err
	}

	if err := join("Partner", ""); err != nil {
		t.Fatal("the second person is the Free plan's partner, and brings no key", err)
	}
	for name, key := range map[string]string{"No key": "", "Bad key": "nonsense", "A key, owner on Free": "good-b"} {
		err := join(name, key)
		if err == nil {
			t.Fatalf("%s: a third person joined a Free team", name)
		}
		if name != "Bad key" && !strings.Contains(err.Error(), "Premium") {
			t.Fatalf("%s: the person is not told what to do: %v", name, err)
		}
	}
	if members() != 2 {
		t.Fatalf("a refused enrollment added a member: %d", members())
	}

	if _, err := h.svc.PresentPremium(bg, owner, "good-owner"); err != nil {
		t.Fatal(err)
	}
	if err := join("Still no key", ""); err == nil {
		t.Fatal("a third person joined without a key of their own")
	}
	if err := join("Third", "good-third"); err != nil {
		t.Fatal("a third person with a key joins a team whose owner is on Premium", err)
	}
	if members() != 3 {
		t.Fatalf("members: %d", members())
	}
	ms, _ := h.svc.ListMembers(bg, owner)
	for _, m := range ms {
		if m.Name == "Third" && !m.Premium {
			t.Fatal("the key the person joined with was not recorded")
		}
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
