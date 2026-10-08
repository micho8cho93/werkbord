//go:build !darwin || !cgo

package pki

import "errors"

func secureKey(string) ([]byte, error) {
	return nil, errors.New("OS secure key storage is unavailable in this build; configure a private external passphrase file (WERKBORD_TEAM_PKI_PASSPHRASE_FILE); macOS builds require cgo for Keychain")
}
