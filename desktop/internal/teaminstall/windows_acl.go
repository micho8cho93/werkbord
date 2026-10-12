//go:build windows

package teaminstall

import (
	"errors"
	"unsafe"

	"golang.org/x/sys/windows"
)

// verifyWindowsPrivateACL accepts a file only when its owner and every allow
// ACE belong to one of the supplied trusted principals. It fails closed for
// inherited Users/Everyone access and unrecognized ACE types.
func verifyWindowsPrivateACL(path string, owners ...*windows.SID) error {
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return err
	}
	owner, _, err := sd.Owner()
	if err != nil || owner == nil {
		return errors.New("the file owner could not be verified")
	}
	acl, _, err := sd.DACL()
	if err != nil || acl == nil {
		return errors.New("the file has no restrictive access list")
	}
	trusted := func(sid *windows.SID) bool {
		if sid == nil {
			return false
		}
		for _, candidate := range owners {
			if candidate != nil && sid.Equals(candidate) {
				return true
			}
		}
		return false
	}
	if !trusted(owner) {
		return errors.New("file ownership does not match a trusted account")
	}
	for i := uint32(0); i < uint32(acl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(acl, i, &ace); err != nil || ace == nil {
			return errors.New("the file access list could not be read")
		}
		switch ace.Header.AceType {
		case windows.ACCESS_DENIED_ACE_TYPE:
			continue
		case windows.ACCESS_ALLOWED_ACE_TYPE:
		default:
			return errors.New("the file has an unrecognized access rule")
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if !trusted(sid) {
			return errors.New("the file is accessible to an untrusted account")
		}
	}
	return nil
}
