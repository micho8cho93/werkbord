package shell

import (
	"errors"
	"strings"
	"testing"

	"devboard/desktop/internal/premium"
)

type fakePremium struct {
	key    string
	st     premium.Status
	refuse error
}

func (p *fakePremium) Status() premium.Status { return p.st }
func (p *fakePremium) Key() string            { return p.key }
func (p *fakePremium) Unlock(text string) (premium.Status, error) {
	if p.refuse != nil {
		return premium.Status{}, p.refuse
	}
	p.key = strings.TrimSpace(text)
	p.st = premium.Status{Active: true, ID: "pk_test", Holder: "Ada", CanCheck: true}
	return p.st, nil
}

func newPremiumRig(t *testing.T) (*rig, *fakePremium) {
	t.Helper()
	r := newRig(t)
	p := &fakePremium{st: premium.Status{CanCheck: true}}
	r.Shell.o.Premium = p
	return r, p
}

func TestAPageIsToldThePlanButNeverTheKey(t *testing.T) {
	r, p := newPremiumRig(t)
	if v := r.Premium(); v.Active || !v.CanCheck || v.BuyURL != "" {
		t.Fatalf("before a key: %+v", v)
	}
	if _, err := r.UnlockPremium("wbpremium-secret-text"); err != nil {
		t.Fatal(err)
	}
	v := r.Premium()
	if !v.Active || v.ID != "pk_test" || v.Holder != "Ada" {
		t.Fatalf("after a key: %+v", v)
	}
	if strings.Contains(strings.Join([]string{v.ID, v.Holder, v.BuyURL}, " "), p.key) {
		t.Fatal("the key's text reached a page")
	}
	// A shell with no key store says there is no Premium rather than failing.
	none := newRig(t)
	if v := none.Premium(); v.Active {
		t.Fatalf("no store: %+v", v)
	}
	if _, err := none.UnlockPremium("x"); err == nil {
		t.Fatal("a shell with nowhere to keep a key unlocked")
	}
}

func TestUnlockingGivesTheKeyToEveryTeamAndOnlyTeams(t *testing.T) {
	r, _ := newPremiumRig(t)
	if _, err := r.UnlockPremium("  wbpremium-k\n"); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(r.tm.premiums, "|")
	if got != "team:main wbpremium-k|team:ws_2 wbpremium-k" {
		t.Fatalf("delivered to %q: every Team workspace gets it, and the person's own Werkbord does not", got)
	}
}

func TestAKeyThatDoesNotCheckIsRefusedAndSentNowhere(t *testing.T) {
	r, p := newPremiumRig(t)
	p.refuse = errors.New("this unlock key is not valid")
	if _, err := r.UnlockPremium("nonsense"); err == nil || !strings.Contains(err.Error(), "not valid") {
		t.Fatalf("err = %v", err)
	}
	if len(r.tm.premiums) != 0 {
		t.Fatalf("a refused key was given to a workspace: %v", r.tm.premiums)
	}
}

func TestOpeningATeamGivesItTheKeyAndTheOwnWerkbordNothing(t *testing.T) {
	r, p := newPremiumRig(t)
	if _, err := r.OpenWorkspace("team:main"); err != nil {
		t.Fatal(err)
	}
	if len(r.tm.premiums) != 0 {
		t.Fatalf("a person with no key gave something away: %v", r.tm.premiums)
	}
	p.key = "wbpremium-k"
	for _, id := range []string{"personal", "team:main"} {
		if _, err := r.OpenWorkspace(id); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := r.LoadWorkspace("team:ws_2"); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(r.tm.premiums, "|"); got != "team:main wbpremium-k|team:ws_2 wbpremium-k" {
		t.Fatalf("delivered to %q", got)
	}
	if _, err := r.AddTeam(); err != nil {
		t.Fatal(err)
	}
	if last := r.tm.premiums[len(r.tm.premiums)-1]; last != "team:ws_3 wbpremium-k" {
		t.Fatalf("a team about to be created or joined gets the key first: %q", last)
	}
}
