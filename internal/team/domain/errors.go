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
)
