//go:build !darwin && !linux && !windows

package teaminstall

import "errors"

func VerifyRelease(string) error {
	return errors.New("Team service installation is not supported on this platform")
}
func RequireRelease(string) error {
	return errors.New("Team service installation is not supported on this platform")
}
func releaseRoot(string) string { return "" }
