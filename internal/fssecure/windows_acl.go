//go:build windows

// Package fssecure contains product-neutral checks for operating-system file
// permissions. It does not create files or decide which product data to protect.
package fssecure

import (
	"errors"
	"unsafe"

	"golang.org/x/sys/windows"
)

// VerifyWindowsPrivateACL accepts only an administrator-controlled DACL. When
// allowCurrentUser is true, the owning account may also read the protected file.
func VerifyWindowsPrivateACL(path string, allowCurrentUser bool) error {
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return err
	}
	owner, _, err := sd.Owner()
	if err != nil || owner == nil {
		return errors.New("cannot determine the file owner")
	}
	acl, _, err := sd.DACL()
	if err != nil || acl == nil {
		return errors.New("the file has no restrictive DACL")
	}
	system, _ := windows.StringToSid("S-1-5-18")
	admins, _ := windows.StringToSid("S-1-5-32-544")
	var current *windows.SID
	if allowCurrentUser {
		tokenUser, err := windows.GetCurrentProcessToken().GetTokenUser()
		if err != nil {
			return err
		}
		current = tokenUser.User.Sid
	}
	trusted := func(sid *windows.SID) bool {
		return sid != nil && (sid.Equals(system) || sid.Equals(admins) || current != nil && sid.Equals(current))
	}
	if !trusted(owner) {
		return errors.New("the file is owned by an untrusted account")
	}
	for i := uint32(0); i < uint32(acl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(acl, i, &ace); err != nil || ace == nil {
			return errors.New("the file ACL could not be read")
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
