package bundleplace

import (
	"strings"
	"testing"
)

func TestAnAppRunningFromADiskImageIsToldWhatToDo(t *testing.T) {
	got := Problem("/Volumes/Werkbord/Werkbord.app/Contents/MacOS/Werkbord", true)
	for _, want := range []string{"disk image", "Applications", "eject"} {
		if !strings.Contains(got, want) {
			t.Errorf("the message does not say %q: %q", want, got)
		}
	}
}

func TestATranslocatedAppIsToldToMoveToApplications(t *testing.T) {
	got := Problem("/private/var/folders/x7/abc/T/AppTranslocation/6B1F/d/Werkbord.app/Contents/MacOS/Werkbord", false)
	if !strings.Contains(got, "temporary location") || !strings.Contains(got, "Applications") {
		t.Errorf("message = %q", got)
	}
}

func TestAnAppThatCanBeReplacedHasNoProblem(t *testing.T) {
	for name, tc := range map[string]struct {
		exe      string
		readOnly bool
	}{
		"in Applications":                  {"/Applications/Werkbord.app/Contents/MacOS/Werkbord", false},
		"in the user's Applications":       {"/Users/a/Applications/Werkbord.app/Contents/MacOS/Werkbord", false},
		"on an external drive that writes": {"/Volumes/Work/Apps/Werkbord.app/Contents/MacOS/Werkbord", false},
		"a development run":                {"/Users/a/src/werkbord/desktop/werkbord", true},
	} {
		if got := Problem(tc.exe, tc.readOnly); got != "" {
			t.Errorf("%s: %q", name, got)
		}
	}
}

// The test is the volume, not the name: a read-only volume called anything is a disk image's situation, and a writable
// volume called Werkbord (a person who copied the app to a disk of that name) is not.
func TestItIsTheVolumeBeingReadOnlyThatMatters(t *testing.T) {
	if Problem("/Volumes/Anything/Werkbord.app/Contents/MacOS/Werkbord", true) == "" {
		t.Error("a read-only volume was accepted")
	}
	if Problem("/Volumes/Werkbord/Werkbord.app/Contents/MacOS/Werkbord", false) != "" {
		t.Error("a writable volume was refused because of its name")
	}
}
