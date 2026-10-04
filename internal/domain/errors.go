// Package domain holds the core types of the control plane and the rules that
// govern them. It has no dependencies on storage, transport, Git or agents.
package domain

import "errors"

// Sentinel errors shared across layers. Adapters wrap these so callers can use
// errors.Is regardless of where the failure originated.
var (
	ErrNotFound   = errors.New("not found")
	ErrConflict   = errors.New("conflict")
	ErrInvalid    = errors.New("invalid")
	ErrDuplicate  = errors.New("already exists")
	ErrTransition = errors.New("invalid state transition")
	// ErrAgent means an agent could not be started or failed to answer. Unlike
	// an unexpected error its message is meant for the user.
	ErrAgent = errors.New("agent failed")
	// ErrGit means the git executable failed. Its message says what it ran and
	// what it reported, which is what the user needs to see; it is only ever
	// produced for a repository on this computer.
	ErrGit = errors.New("git failed")
)
