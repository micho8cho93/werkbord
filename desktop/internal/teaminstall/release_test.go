package teaminstall

import (
	"testing"
)

func TestSigningRequirementNamesOnlyAWellFormedTeam(t *testing.T) {
	got, err := signingRequirement("ABCDE12345")
	if err != nil || got != `=anchor apple generic and certificate leaf[subject.OU] = "ABCDE12345"` {
		t.Fatalf("%q, %v", got, err)
	}
	// The team is spliced into requirement text, so nothing but ten letters and digits may get in.
	for _, bad := range []string{"", "abcde12345", "ABCDE1234", "ABCDE123456", `ABCDE1234"`, "ABCDE1234 ", "ABCDE1234\n", `A" or anchor apple generic and "1`} {
		if _, err := signingRequirement(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}
