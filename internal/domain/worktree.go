package domain

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

// WorktreeState records whether a worktree is still on disk.
type WorktreeState string

const (
	WorktreeActive  WorktreeState = "active"
	WorktreeRemoved WorktreeState = "removed"
)

// Valid reports whether s is a known worktree state.
func (s WorktreeState) Valid() bool { return s == WorktreeActive || s == WorktreeRemoved }

// CanTransitionTo reports whether moving from s to next is allowed. A removed
// worktree never comes back: a new one is a new record.
func (s WorktreeState) CanTransitionTo(next WorktreeState) bool {
	return s == WorktreeActive && next == WorktreeRemoved
}

// Worktree is a Git worktree the controller created for a run, so that
// concurrent runs never share a working directory.
//
// A Worktree row is the controller's only evidence that it owns a directory,
// and removing the directory is irreversible, so rows are validated before
// they are written (Validate, WorktreePlacement) and fixed afterwards: only
// State changes, by compare-and-swap on Version.
type Worktree struct {
	ID        string        `json:"id"`
	ProjectID string        `json:"projectId"`
	Path      string        `json:"path"`
	Branch    string        `json:"branch"`
	BaseRef   string        `json:"baseRef"`
	State     WorktreeState `json:"state"`
	// RemovingSince is set, by compare-and-swap, before the directory is
	// deleted. From then on no run may start on the worktree, and the row cannot
	// become removed without it. See Service.BeginRemoval.
	RemovingSince *time.Time `json:"removingSince,omitempty"`
	Version       int64      `json:"version"`
	CreatedAt     time.Time  `json:"createdAt"`
	UpdatedAt     time.Time  `json:"updatedAt"`
}

// Removing reports whether deletion of the directory has been decided on.
func (w *Worktree) Removing() bool { return w.RemovingSince != nil }

// BeginRemoval is the first step of removal, taken before anything is deleted.
// Persisting it is what proves no run is using the worktree and stops one
// starting; only then is it safe to delete the directory. It updates the
// timestamps but does not persist anything.
func (w *Worktree) BeginRemoval(now time.Time) error {
	if w.State != WorktreeActive || w.Removing() {
		return fmt.Errorf("%w: worktree %s is already %s", ErrTransition, w.ID, w.describeRemoval())
	}
	t := now
	w.RemovingSince, w.UpdatedAt = &t, now
	return nil
}

// FinishRemoval records that the directory is gone. It is only possible after
// BeginRemoval. It updates the timestamp but does not persist anything.
func (w *Worktree) FinishRemoval(now time.Time) error {
	if !w.Removing() || !w.State.CanTransitionTo(WorktreeRemoved) {
		return fmt.Errorf("%w: worktree %s is %s, not being removed", ErrTransition, w.ID, w.describeRemoval())
	}
	w.State, w.UpdatedAt = WorktreeRemoved, now
	return nil
}

func (w *Worktree) describeRemoval() string {
	switch {
	case w.State == WorktreeRemoved:
		return "removed"
	case w.Removing():
		return "being removed"
	}
	return "active"
}

// Validate checks everything about a worktree that can be decided without
// looking at the filesystem or at other records.
func (w *Worktree) Validate() error {
	switch {
	case w.ID == "":
		return fmt.Errorf("%w: worktree id is required", ErrInvalid)
	case w.ProjectID == "":
		return fmt.Errorf("%w: worktree project is required", ErrInvalid)
	case !w.State.Valid():
		return fmt.Errorf("%w: unknown worktree state %q", ErrInvalid, w.State)
	}
	if err := ValidateWorktreePath(w.Path); err != nil {
		return err
	}
	if err := ValidateRefName(w.Branch); err != nil {
		return fmt.Errorf("branch: %w", err)
	}
	if w.Branch == "HEAD" {
		return fmt.Errorf("%w: branch must not be named HEAD", ErrInvalid)
	}
	if err := ValidateRefName(w.BaseRef); err != nil {
		return fmt.Errorf("base ref: %w", err)
	}
	return nil
}

const (
	maxPathLen    = 4096
	maxRefNameLen = 255
)

// ValidateWorktreePath requires an absolute, lexically clean path that is not
// the filesystem root and has no control characters (a newline would corrupt
// any line-oriented Git output the path is later matched against).
func ValidateWorktreePath(path string) error {
	switch {
	case path == "":
		return fmt.Errorf("%w: worktree path is required", ErrInvalid)
	case len(path) > maxPathLen:
		return fmt.Errorf("%w: worktree path is longer than %d bytes", ErrInvalid, maxPathLen)
	case strings.IndexFunc(path, isControl) >= 0:
		return fmt.Errorf("%w: worktree path %q contains a control character", ErrInvalid, path)
	case !filepath.IsAbs(path):
		return fmt.Errorf("%w: worktree path %q must be absolute", ErrInvalid, path)
	case filepath.Clean(path) != path:
		return fmt.Errorf("%w: worktree path %q must be clean: no '.', '..', repeated or trailing '/'", ErrInvalid, path)
	case filepath.Dir(path) == path:
		return fmt.Errorf("%w: worktree path must not be the filesystem root", ErrInvalid)
	}
	return nil
}

func isControl(r rune) bool { return r < 0x20 || r == 0x7f }

// ValidateRefName checks a branch or ref name. It accepts only names that
// `git check-ref-format --allow-onelevel` accepts (a test compares the two) and
// additionally refuses a leading '-', so a name can never be taken for an
// option when it is passed to Git on a command line.
func ValidateRefName(name string) error {
	bad := func(why string) error { return fmt.Errorf("%w: ref name %q %s", ErrInvalid, name, why) }
	switch {
	case name == "":
		return fmt.Errorf("%w: ref name is required", ErrInvalid)
	case len(name) > maxRefNameLen:
		return bad(fmt.Sprintf("is longer than %d bytes", maxRefNameLen))
	case !utf8.ValidString(name):
		return bad("is not valid UTF-8")
	case name[0] == '-':
		return bad("must not start with '-'")
	case name == "@":
		return bad("must not be '@'")
	case strings.HasPrefix(name, "/") || strings.HasSuffix(name, "/") || strings.Contains(name, "//"):
		return bad("must not start or end with '/' or contain '//'")
	case strings.HasSuffix(name, "."):
		return bad("must not end with '.'")
	case strings.Contains(name, "..") || strings.Contains(name, "@{"):
		return bad("must not contain '..' or '@{'")
	case strings.IndexFunc(name, func(r rune) bool { return isControl(r) || r == ' ' || strings.ContainsRune("~^:?*[\\", r) }) >= 0:
		return bad("contains a control character, space or one of ~ ^ : ? * [ \\")
	}
	for _, part := range strings.Split(name, "/") {
		if strings.HasPrefix(part, ".") || strings.HasSuffix(part, ".lock") {
			return bad("has a component starting with '.' or ending with '.lock'")
		}
	}
	return nil
}

// WorktreePlacement is where worktrees may be created, and what already
// occupies the disk. Paths are expected to be canonical (symlinks resolved).
type WorktreePlacement struct {
	Root      string   // all worktrees live strictly inside this directory
	RepoRoots []string // working trees of registered repositories
	GitDirs   []string // their shared .git directories
	Worktrees []string // paths of active worktrees
}

// Check reports whether a worktree may be created at path. It exists to make
// one mistake impossible: a "worktree" whose later removal deletes something
// that was never the controller's to delete. Nothing is placeable when Root is
// empty, so a missing configuration fails closed.
func (pl WorktreePlacement) Check(path string) error {
	deny := func(format string, args ...any) error {
		return fmt.Errorf("%w: worktree path %s", ErrInvalid, fmt.Sprintf(format, args...))
	}
	if pl.Root == "" {
		return deny("%q cannot be used: no worktree directory is configured", path)
	}
	if !pathWithin(pl.Root, path) || path == pl.Root {
		return deny("%q must be inside %s", path, pl.Root)
	}
	for _, r := range pl.RepoRoots {
		if pathWithin(path, r) {
			return deny("%q is, or contains, the registered repository %s", path, r)
		}
	}
	for _, g := range pl.GitDirs {
		if pathWithin(path, g) || pathWithin(g, path) {
			return deny("%q overlaps the Git directory %s", path, g)
		}
	}
	for _, w := range pl.Worktrees {
		if pathWithin(path, w) || pathWithin(w, path) {
			return deny("%q overlaps the active worktree %s", path, w)
		}
	}
	return nil
}

// pathWithin reports whether child is parent or lies below it. Both must be
// clean; comparison is by whole path components, so /a/bc is not within /a/b.
func pathWithin(parent, child string) bool {
	return child == parent || strings.HasPrefix(child, strings.TrimSuffix(parent, "/")+"/")
}
