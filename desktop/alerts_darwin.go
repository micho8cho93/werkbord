//go:build darwin

package main

/*
#cgo CFLAGS: -fobjc-arc -mmacosx-version-min=13.0
#cgo LDFLAGS: -framework Cocoa -framework UserNotifications
#include <stdlib.h>
#include "alerts_darwin.h"
*/
import "C"

import (
	"unsafe"

	"werkbord/desktop/internal/shell"
)

// nativeAlerter shows alerts as the Mac does (alerts_darwin.m): a notification and a bounce of the Dock icon.
type nativeAlerter struct{}

var _ shell.Alerter = nativeAlerter{}

func newAlerter() shell.Alerter { return nativeAlerter{} }

func (nativeAlerter) Status() string {
	switch C.wbAlertStatus() {
	case C.WBAlertGranted:
		return "granted"
	case C.WBAlertDenied:
		return "denied"
	case C.WBAlertUnknown:
		return "unknown"
	}
	return "unsupported"
}

func (nativeAlerter) Show(title, body string, urgent, force bool) bool {
	t, b := C.CString(title), C.CString(body)
	defer C.free(unsafe.Pointer(t))
	defer C.free(unsafe.Pointer(b))
	var u, f C.int
	if urgent {
		u = 1
	}
	if force {
		f = 1
	}
	return C.wbAlertShow(t, b, u, f) == 1
}

func (nativeAlerter) OpenSettings() { C.wbAlertOpenSettings() }
