//go:build darwin

package main

/*
#cgo CFLAGS: -fobjc-arc -mmacosx-version-min=13.0
#cgo LDFLAGS: -framework Cocoa
#include "updater_darwin.h"
*/
import "C"

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"
	"unsafe"

	"devboard/desktop/internal/shell"
)

// nativeUpdater is Sparkle, as the shell sees it (shell.Updater). The Objective-C half is updater_darwin.m.
// It makes no request of its own: Probe and Show are what a person's click on "Update now" or on
// "Check for Updates…" ends up as, after the shell has checked that looking for updates is allowed.
type nativeUpdater struct {
	log   *slog.Logger
	allow func() bool

	mu      sync.Mutex
	active  bool
	token   int
	waiting map[int]chan probeResult
	busy    func() bool
}

type probeResult struct {
	shell.Probe
	err error
}

// current is who the native callbacks, which carry no context, talk to. There is one updater in the app.
var (
	current     *nativeUpdater
	menuHandler func()
)

var _ shell.Updater = (*nativeUpdater)(nil)

func newNativeUpdater(log *slog.Logger, allow func() bool) *nativeUpdater {
	u := &nativeUpdater{log: log, allow: allow, waiting: map[int]chan probeResult{}}
	current = u
	return u
}

// start loads and starts Sparkle, if this build has it. A build that does not (a development build, or one without
// the signing key) is a normal build: the log says why, and the app updates its program as it always did.
func (u *nativeUpdater) start() {
	buf := make([]byte, 512)
	rc := C.wbUpdaterStart((*C.char)(unsafe.Pointer(&buf[0])), C.int(len(buf)))
	why := C.GoString((*C.char)(unsafe.Pointer(&buf[0])))
	if rc == C.WBUpdaterStarted {
		u.mu.Lock()
		u.active = true
		u.mu.Unlock()
		u.log.Info("the app can update itself", "test_build", updaterTestBuild)
		return
	}
	level := slog.LevelInfo
	if rc != C.WBUpdaterNotInThisBuild {
		level = slog.LevelWarn // it is there and failed: worth knowing
	}
	u.log.Log(context.Background(), level, "the app cannot update itself in this build", "why", why)
}

func (u *nativeUpdater) Active() bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.active
}

func (u *nativeUpdater) GuardRelaunch(busy func() bool) {
	u.mu.Lock()
	u.busy = busy
	u.mu.Unlock()
}

func (u *nativeUpdater) Probe(ctx context.Context) (shell.Probe, error) {
	if !u.Active() {
		return shell.Probe{}, errors.New("the updater is not running")
	}
	if !u.allow() {
		return shell.Probe{}, errors.New("looking for updates is turned off")
	}
	u.mu.Lock()
	u.token++
	token := u.token
	ch := make(chan probeResult, 1)
	u.waiting[token] = ch
	u.mu.Unlock()
	defer func() {
		u.mu.Lock()
		delete(u.waiting, token)
		u.mu.Unlock()
	}()
	C.wbUpdaterProbe(C.int(token))
	select {
	case r := <-ch:
		return r.Probe, r.err
	case <-ctx.Done():
		return shell.Probe{}, ctx.Err()
	case <-time.After(45 * time.Second):
		// Sparkle does nothing while one of its windows is already open, and says nothing.
		return shell.Probe{}, errors.New("the update feed did not answer (or an update window is already open)")
	}
}

func (u *nativeUpdater) Show() { C.wbUpdaterShow() }

// event is the native side telling Go what happened. It only ever logs, and answers a probe.
func (u *nativeUpdater) event(kind, token int, text string) {
	names := map[int]string{
		C.EventFound: "found an update", C.EventDownloaded: "downloaded the update", C.EventExtracting: "starting to unpack the update (Sparkle checks its signature first)",
		C.EventInstalling: "installing the update", C.EventRelaunching: "relaunching", C.EventPostponed: "waiting for the program's update to finish before replacing the app",
		C.EventAborted: "refused or could not finish an update", C.EventMenuAdded: "added Check for Updates… to the application menu",
		C.EventMenuFailed: "could not add Check for Updates… to the application menu", C.EventTestInstall: "TEST BUILD: installing without asking",
		C.EventTestMenu: "TEST BUILD: the menu bar",
	}
	switch kind {
	case C.EventProbeFound, C.EventProbeNone, C.EventProbeError:
		r := probeResult{}
		switch kind {
		case C.EventProbeFound:
			r.Probe = shell.Probe{Found: true, Version: text}
		case C.EventProbeError:
			r.err = errors.New(text)
		}
		u.mu.Lock()
		ch := u.waiting[token]
		u.mu.Unlock()
		u.log.Info("update feed", "found", r.Found, "version", r.Version, "err", text)
		if ch != nil {
			select {
			case ch <- r:
			default:
			}
		}
	case C.EventAborted, C.EventMenuFailed:
		u.log.Warn(names[kind], "text", text)
	default:
		u.log.Info(names[kind], "text", text)
	}
}

//export wbUpdaterEvent
func wbUpdaterEvent(kind, token C.int, text *C.char) {
	if u := current; u != nil {
		u.event(int(kind), int(token), C.GoString(text))
	}
}

// wbUpdaterAllowed: may Sparkle look at the feed? Only if the person has not turned looking for updates off.
//
//export wbUpdaterAllowed
func wbUpdaterAllowed() C.int {
	if u := current; u != nil && u.allow() {
		return 1
	}
	return 0
}

// wbUpdaterMayRelaunch: may the app quit and be replaced now? Not while the program is being updated.
//
//export wbUpdaterMayRelaunch
func wbUpdaterMayRelaunch() C.int {
	u := current
	if u == nil {
		return 1
	}
	u.mu.Lock()
	busy := u.busy
	u.mu.Unlock()
	if busy != nil && busy() {
		return 0
	}
	return 1
}

//export wbMenuCheckForUpdates
func wbMenuCheckForUpdates() {
	if h := menuHandler; h != nil {
		go h()
	}
}

// startNative starts what is native and optional: Sparkle, and the menu item that asks for an update (which is there
// whether or not Sparkle is: it asks the shell, which knows what can be done).
func startNative(u *nativeUpdater, sh *shell.Shell) {
	menuHandler = sh.CheckForUpdates
	C.wbInstallCheckForUpdatesMenuItem()
	if u != nil {
		u.start()
	}
}
