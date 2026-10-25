package entitlement

import "testing"

func TestFreeAllowsAPairAndNoMore(t *testing.T) {
	for size, want := range map[int]bool{0: true, 1: true, 2: true, 3: false, 10: false} {
		if got := AllowsTeamSize(false, size); got != want {
			t.Errorf("free, %d people: got %v, want %v", size, got, want)
		}
	}
}

func TestPremiumHasNoLimit(t *testing.T) {
	for _, size := range []int{1, 2, 3, 50, 1_000_000} {
		if !AllowsTeamSize(true, size) {
			t.Errorf("premium refused a team of %d", size)
		}
	}
	if FreeTeamLimit != 2 {
		t.Errorf("the Free plan is you and a partner, so 2; got %d", FreeTeamLimit)
	}
}
