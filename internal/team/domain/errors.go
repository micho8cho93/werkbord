package domain

import "errors"

// Sentinel errors. The API maps each to a status; the message is for people.
var (
	ErrNotFound        = errors.New("not found")
	ErrInvalid         = errors.New("invalid")
	ErrConflict        = errors.New("conflict")
	ErrForbidden       = errors.New("forbidden")
	ErrUnauthenticated = errors.New("unauthenticated")
	ErrBusy            = errors.New("busy") // too many open requests; try again shortly
	// ErrPremiumRequired is returned when a team would pass the Free plan's size. The API maps it to 402 "premium_required".
	ErrPremiumRequired = errors.New("premium required")
)

// ErrReadOnly is returned for a write the workspace cannot accept now because its storage has no quorum:
// reading still works, from the host's last copy of the data, and no write is accepted anywhere until a
// quorum of the Workspace Hosts is back. It is never "accepted on this host and merged later".
var ErrReadOnly = errors.New("read-only")
