package teaminstall

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
)

// signingTeam, signingThumbprint and releaseBuild are public build stamps. They
// let the privileged Team installer verify that it is running from the same
// release app that shipped the service. The trust check is platform-specific.
var signingTeam string
var signingThumbprint string
var releaseBuild string

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

func verifySignedBy(team, appRoot string) error {
	requirement, err := signingRequirement(team)
	if err != nil {
		return err
	}
	// The app and service establish publisher identity; the service embeds the exact hashes of its rebuilt helpers.
	for _, path := range []string{appRoot, filepath.Join(appRoot, "Contents", "Helpers", "werkbord-team")} {
		if err := codesignRequirement(requirement, path); err != nil {
			return fmt.Errorf("%s was not signed by the Werkbord release team", filepath.Base(path))
		}
	}
	return nil
}
