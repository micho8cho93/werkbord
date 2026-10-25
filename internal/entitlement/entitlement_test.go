package entitlement

import "testing"

func TestTheFreePlanIsAPair(t *testing.T) {
	if FreeTeamLimit != 2 {
		t.Errorf("the Free plan is you and a partner, so 2; got %d", FreeTeamLimit)
	}
}
