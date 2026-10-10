package teaminstall

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
)

// signingTeam is the Apple Developer team that signs a Werkbord release, stamped into a release build (-X
// devboard/desktop/internal/teaminstall.signingTeam=ABCDE12345). It is public. The Team service is installed
// into a root-owned directory, so before it replaces anything it checks that the app it is running from was signed by that
// team under Apple's own certificate chain: a modified copy, or one signed by anyone else, is refused. The app is built,
// signed and notarized by the same release as everything else in Werkbord, so this is the one trust anchor a release has.
// A development build has no team and is ad hoc signed, and is not checked.
var signingTeam string

var teamIdentifier = regexp.MustCompile(`^[A-Z0-9]{10}$`)

// signingRequirement is the code-signing requirement a release's executables must satisfy for the team.
func signingRequirement(team string) (string, error) {
	if !teamIdentifier.MatchString(team) {
		return "", errors.New("invalid release signing team")
	}
	return `=anchor apple generic and certificate leaf[subject.OU] = "` + team + `"`, nil
}

// codesignRequirement asks the system whether path satisfies requirement. It is a variable so tests do not need a certificate.
var codesignRequirement = func(requirement, path string) error {
	return exec.Command("/usr/bin/codesign", "--verify", "--strict", "-R", requirement, path).Run()
}

// VerifyRelease checks that the app and the Team service in it were signed by the release's team. It allows the
// established ad hoc workflow only when no team is configured.
func VerifyRelease(appRoot string) error {
	if signingTeam == "" {
		return nil
	}
	return verifySignedBy(signingTeam, appRoot)
}

// RequireRelease is VerifyRelease for a caller that must not accept a development build.
func RequireRelease(appRoot string) error {
	if signingTeam == "" {
		return errors.New("this development build has no release signing team")
	}
	return verifySignedBy(signingTeam, appRoot)
}

func verifySignedBy(team, appRoot string) error {
	requirement, err := signingRequirement(team)
	if err != nil {
		return err
	}
	// Nebula keeps its upstream signature and is checked against its pin, so only the code Werkbord signs is tested here.
	for _, path := range []string{appRoot, filepath.Join(appRoot, "Contents", "Helpers", "werkbord-team")} {
		if err := codesignRequirement(requirement, path); err != nil {
			return fmt.Errorf("%s was not signed by the Werkbord release team", filepath.Base(path))
		}
	}
	return nil
}
