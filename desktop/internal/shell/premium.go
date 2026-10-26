package shell

import (
	"context"
	"errors"
	"time"

	"werkbord/desktop/internal/premium"
	"werkbord/desktop/internal/teamlink"
	"werkbord/internal/workspace"
)

// Premium is the person's unlock key on this computer (desktop/internal/premium).
type Premium interface {
	Status() premium.Status
	Unlock(text string) (premium.Status, error)
	// Key is the key itself, for giving to a Team workspace's service. It is never returned to a page.
	Key() string
}

var errNoPremium = errors.New("this app has nowhere to keep an unlock key")

// buyURL is where a person gets Premium. It is empty until Premium is on sale: the window then says so instead of offering a
// button that goes nowhere. (There is no payment system yet; docs/PLANS.md.)
const buyURL = ""

// PremiumView is what a page is told about the plan: whether this person is on Premium, which key (by its ID and label, never its
// text), whether this build can check keys at all, and where to get Premium.
type PremiumView struct {
	premium.Status
	BuyURL string `json:"buyUrl"`
}

func (s *Shell) premiumView(st premium.Status) PremiumView {
	return PremiumView{Status: st, BuyURL: buyURL}
}

// Premium tells a page whether this person is on Premium. It is safe to offer a page: it names the key's ID and label and
// nothing that could be used as the key.
func (s *Shell) Premium() PremiumView {
	if s.o.Premium == nil {
		return s.premiumView(premium.Status{})
	}
	return s.premiumView(s.o.Premium.Status())
}

// UnlockPremium checks the key a person pasted into Settings and keeps it, then gives it to each Team workspace they belong to.
// It can only make the person's own plan what a valid key says: the key is checked here against the public key this build was
// stamped with, and again by every workspace it is given to. A key that does not check is refused with a sentence and nothing is
// kept or sent.
func (s *Shell) UnlockPremium(key string) (PremiumView, error) {
	if s.o.Premium == nil {
		return PremiumView{}, errNoPremium
	}
	st, err := s.o.Premium.Unlock(key)
	if err != nil {
		return PremiumView{}, err
	}
	ctx, cancel := s.timeout(20 * time.Second)
	defer cancel()
	for _, it := range s.o.Workspaces.Refresh(ctx).Items {
		if it.Kind == workspace.KindTeam {
			s.givePremium(ctx, it.ID)
		}
	}
	return s.premiumView(st), nil
}

// givePremium gives a Team workspace's service the person's key, when they have one. It is best effort: a service that is not
// running, or is older than this app, simply does not receive it, and the key is given again the next time the workspace is
// opened. Nothing is told to a workspace that is not a Team's.
func (s *Shell) givePremium(ctx context.Context, id string) {
	if s.o.Premium == nil || s.o.Team == nil {
		return
	}
	if _, ok := teamlink.SlotOf(id); !ok {
		return
	}
	key := s.o.Premium.Key()
	if key == "" {
		return
	}
	if err := s.o.Team.DeliverPremium(ctx, id, key); err != nil {
		s.o.Log.Debug("the unlock key was not given to a workspace", "workspace", id, "err", err)
	}
}
