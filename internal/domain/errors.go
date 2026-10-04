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
)
