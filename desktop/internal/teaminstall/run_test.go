package teaminstall

import (
	"bytes"
	"strings"
	"testing"
)

func TestOnlyTheFixedRequestsAreTheInstallers(t *testing.T) {
	for _, args := range [][]string{{"--activate"}, {"--service", "stop"}, {"--verify-release"}, {"--team-service", "install", "501"}} {
		if !Handles(args) {
			t.Errorf("%v is the installer's", args)
		}
	}
	// Anything else, including an invitation link the system hands the app, is for the window.
	for _, args := range [][]string{nil, {}, {"werkbord://join/abc"}, {"-psn_0_12345"}, {"--activate-everything"}} {
		if Handles(args) {
			t.Errorf("%v is not the installer's", args)
		}
	}
}

func TestUnknownOrMalformedRequestsDoNothing(t *testing.T) {
	for _, args := range [][]string{{"--service"}, {"--service", "format-disk"}, {"--activate", "extra"}, {"--verify-release", "x"}} {
		var out, errOut bytes.Buffer
		if code := Run("v1.2.3", args, &out, &errOut); code == 0 || !strings.Contains(out.String(), `"ok":false`) {
			t.Errorf("%v: code %d, out %q", args, code, out.String())
		}
	}
	// The privileged step needs exactly an action and a user, and as an ordinary user it refuses before doing anything.
	var out, errOut bytes.Buffer
	if code := Run("v1.2.3", []string{"--team-service", "install", "501"}, &out, &errOut); code == 0 || errOut.Len() == 0 {
		t.Errorf("the privileged step ran without privilege: code %d %q", code, errOut.String())
	}
	if code := Run("v1.2.3", []string{"--team-service", "install"}, &out, &errOut); code == 0 {
		t.Error("the privileged step accepted a request with no user")
	}
}
