//go:build !unix

package agent

import (
	"os/exec"
	"syscall"
	"time"
)

const runtimeUnsupported = true

func setProcAttr(*exec.Cmd)           {}
func signalGroup(int, syscall.Signal) {}
func isClosedPipe(error) bool         { return false }
func processIdentity(int) string      { return "" }

// ReapResult says what Reap found.
type ReapResult int

const (
	ReapGone ReapResult = iota
	ReapKilled
	ReapForeign
)

// Reap does nothing on this platform.
func Reap(int, string, time.Duration) (ReapResult, error) { return ReapForeign, errNotUnix }
