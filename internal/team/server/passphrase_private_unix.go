//go:build !windows

package server

import "os"

func securePassphraseFile(_ string, fi os.FileInfo) error {
	if fi.Mode().Perm()&0o077 != 0 {
		return os.ErrPermission
	}
	return nil
}
