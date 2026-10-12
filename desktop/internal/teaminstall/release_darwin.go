//go:build darwin

package teaminstall

import (
	"errors"
	"path/filepath"
)

func VerifyRelease(appRoot string) error {
	if signingTeam == "" { // local development builds may be ad-hoc signed
		return nil
	}
	return verifySignedBy(signingTeam, appRoot)
}

func RequireRelease(appRoot string) error {
	if signingTeam == "" {
		return errors.New("this development build has no release signing team")
	}
	return verifySignedBy(signingTeam, appRoot)
}

func releaseRoot(exe string) string {
	return filepath.Clean(filepath.Join(filepath.Dir(exe), "..", ".."))
}
