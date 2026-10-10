// Package planning is the part of organizing work that Individual and Team have in common and that
// belongs to neither: what a label is called and coloured, who is expected to do a piece of work
// (a person, an agent, or both), what a planned date range is, and what is wrong with the order
// work depends on.
//
// It is shared plumbing in the sense of docs/STRUCTURE.md: pure functions and plain values, no
// storage, no HTTP, no process, no credential and no knowledge of a workspace, a project or a role.
// Each product keeps its own records and its own permissions and asks this package only whether
// something is well formed or what looks wrong in it. Errors wrap ErrInvalid so that a caller can
// turn them into its own invalid-input error.
package planning

import "errors"

// ErrInvalid is wrapped by every error this package returns for input that is not usable.
var ErrInvalid = errors.New("invalid")
