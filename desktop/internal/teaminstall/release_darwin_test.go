//go:build darwin

package teaminstall

import (
	"errors"
	"strings"
	"testing"
)

func TestReleaseVerificationChecksTheAppAndTheService(t *testing.T) {
	old := codesignRequirement
	t.Cleanup(func() { codesignRequirement = old; signingTeam = "" })
	var checked []string
	codesignRequirement = func(requirement, path string) error {
		if !strings.Contains(requirement, `"ABCDE12345"`) {
			t.Errorf("wrong requirement %q", requirement)
		}
		checked = append(checked, path)
		return nil
	}
	signingTeam = ""
	if err := VerifyRelease("/Applications/Werkbord Team.app"); err != nil || len(checked) != 0 {
		t.Fatalf("a development build is not checked: %v %v", err, checked)
	}
	if err := RequireRelease("/Applications/Werkbord Team.app"); err == nil {
		t.Fatal("a development build satisfied a required release check")
	}
	signingTeam = "ABCDE12345"
	if err := VerifyRelease("/Applications/Werkbord Team.app"); err != nil {
		t.Fatal(err)
	}
	if len(checked) != 2 || checked[0] != "/Applications/Werkbord Team.app" || checked[1] != "/Applications/Werkbord Team.app/Contents/Helpers/werkbord-team" {
		t.Fatalf("checked %v", checked)
	}
	codesignRequirement = func(string, string) error { return errors.New("not signed by them") }
	if err := VerifyRelease("/x.app"); err == nil || RequireRelease("/x.app") == nil {
		t.Fatal("a copy signed by someone else was accepted")
	}
}
