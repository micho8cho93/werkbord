//go:build windows

package teaminstall

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

var windowsThumbprint = regexp.MustCompile(`(?i)^[A-F0-9]{40}$`)

var verifyAuthenticode = func(thumbprint, path string) error {
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
	script := "$s=Get-AuthenticodeSignature -LiteralPath " + quote(path) + "; if ($s.Status -ne 'Valid' -or $s.SignerCertificate.Thumbprint -ne " + quote(strings.ToUpper(thumbprint)) + ") { exit 1 }"
	return exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script).Run()
}

func VerifyRelease(appRoot string) error {
	if signingThumbprint == "" { // development builds are not accepted by privileged setup
		return nil
	}
	return verifyWindowsRelease(appRoot)
}

func RequireRelease(appRoot string) error {
	if !windowsThumbprint.MatchString(signingThumbprint) {
		return errors.New("this development build has no valid Windows release signing certificate")
	}
	return verifyWindowsRelease(appRoot)
}

func verifyWindowsRelease(appRoot string) error {
	if strings.TrimPrefix(releaseBuild, "v") != strings.TrimPrefix(nativeVersion, "v") {
		return errors.New("the app and Team installer versions do not match")
	}
	if !windowsThumbprint.MatchString(signingThumbprint) {
		return errors.New("invalid Windows release signing certificate thumbprint")
	}
	for _, path := range []string{
		filepath.Join(appRoot, "bin", "Werkbord.exe"),
		filepath.Join(appRoot, "Helpers", "werkbord.exe"),
		filepath.Join(appRoot, "Helpers", "werkbord-team.exe"),
	} {
		if err := verifyAuthenticode(signingThumbprint, path); err != nil {
			return fmt.Errorf("%s was not signed by the Werkbord release certificate", filepath.Base(path))
		}
	}
	return nil
}

func releaseRoot(exe string) string {
	return filepath.Clean(filepath.Join(filepath.Dir(exe), ".."))
}
