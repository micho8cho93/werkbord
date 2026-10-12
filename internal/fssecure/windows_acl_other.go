//go:build !windows

package fssecure

import "errors"

// VerifyWindowsPrivateACL is available only in a Windows build.
func VerifyWindowsPrivateACL(string, bool) error {
	return errors.New("Windows ACL verification is unavailable")
}
