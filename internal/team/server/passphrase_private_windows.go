//go:build windows

package server

import (
	"errors"
	"os"

	"werkbord/internal/fssecure"
)

func securePassphraseFile(path string, _ os.FileInfo) error {
	if err := fssecure.VerifyWindowsPrivateACL(path, false); err != nil {
		return errors.New("the passphrase file permissions are not private: " + err.Error())
	}
	return nil
}
