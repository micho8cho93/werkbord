package gitrepo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"werkbord/internal/domain"
)

// WorkingFingerprint binds confirmation to HEAD, index, selected bytes and mode.
// Oversized files, submodules and paths outside this checkout are refused.
func (c *CLI) WorkingFingerprint(ctx context.Context, root string, files []string) (string, error) {
	status, err := c.Status(ctx, root)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	raw, _ := json.Marshal(status)
	h.Write(raw)
	for _, p := range files {
		if p == "" || filepath.IsAbs(p) || strings.ContainsAny(p, "\x00\r\n") || filepath.ToSlash(filepath.Clean(p)) != p {
			return "", domain.ErrInvalid
		}
		for _, part := range strings.Split(p, "/") {
			if part == ".." || part == ".git" {
				return "", domain.ErrInvalid
			}
		}
		full := filepath.Join(root, filepath.FromSlash(p))
		parent, err := filepath.EvalSymlinks(filepath.Dir(full))
		if err != nil {
			return "", err
		}
		rel, err := filepath.Rel(root, parent)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return "", domain.ErrForbidden
		}
		h.Write([]byte(p))
		info, err := os.Lstat(full)
		if os.IsNotExist(err) {
			h.Write([]byte("deleted"))
			continue
		}
		if err != nil {
			return "", err
		}
		h.Write([]byte(info.Mode().String()))
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(full)
			if err != nil {
				return "", err
			}
			h.Write([]byte(target))
			continue
		}
		if !info.Mode().IsRegular() || info.Size() > 16<<20 {
			return "", fmt.Errorf("%w: selected path is not a bounded regular file", domain.ErrInvalid)
		}
		f, err := os.Open(full)
		if err != nil {
			return "", err
		}
		n, err := io.Copy(h, io.LimitReader(f, (16<<20)+1))
		f.Close()
		if err != nil {
			return "", err
		}
		if n > 16<<20 {
			return "", domain.ErrInvalid
		}
	}
	args := append([]string{"--literal-pathspecs", "diff", "--cached", "--binary", "--no-ext-diff", "--no-textconv", "--"}, files...)
	raw, truncated, err := c.run(ctx, root, runOpts{}, args...)
	if err != nil {
		return "", err
	}
	if truncated {
		return "", domain.ErrInvalid
	}
	h.Write(raw)
	return hex.EncodeToString(h.Sum(nil)), nil
}

// CommitFiles commits only the explicitly reviewed working-tree paths and keeps
// unrelated staged changes intact. Repository hooks and signing programs never run.
func (c *CLI) CommitFiles(ctx context.Context, root, head, message string, files []string) (string, error) {
	if !IsCommitID(head) || strings.TrimSpace(message) == "" || len(message) > 4096 || len(files) == 0 || len(files) > 200 {
		return "", domain.ErrInvalid
	}
	actual, err := c.HeadCommit(ctx, root)
	if err != nil {
		return "", err
	}
	if actual != head {
		return "", fmt.Errorf("%w: HEAD changed", domain.ErrConflict)
	}
	// Clean filters can execute repository-configured commands or change bytes.
	// Refuse them rather than silently bypassing LFS or running arbitrary filters.
	args := append([]string{"--literal-pathspecs", "check-attr", "-z", "filter", "--"}, files...)
	raw, truncated, err := c.run(ctx, root, runOpts{}, args...)
	if err != nil {
		return "", err
	}
	if truncated {
		return "", domain.ErrInvalid
	}
	fields := strings.Split(strings.TrimSuffix(string(raw), "\x00"), "\x00")
	if len(fields)%3 != 0 {
		return "", domain.ErrInvalid
	}
	for i := 2; i < len(fields); i += 3 {
		if fields[i] != "unspecified" && fields[i] != "unset" {
			return "", fmt.Errorf("%w: commit filtered files locally", domain.ErrInvalid)
		}
	}
	status, err := c.Status(ctx, root)
	if err != nil {
		return "", err
	}
	selected := map[string]bool{}
	for _, path := range files {
		selected[path] = true
	}
	var added []string
	for _, f := range status.Untracked {
		if selected[f.Path] {
			added = append(added, f.Path)
		}
	}
	if len(added) > 0 {
		args = append([]string{"--literal-pathspecs", "add", "--intent-to-add", "--"}, added...)
		if _, err = c.runWrite(ctx, root, false, args...); err != nil {
			return "", err
		}
	}
	args = append([]string{"-c", "commit.gpgsign=false", "--literal-pathspecs", "commit", "--only", "--no-verify", "--no-gpg-sign", "-m", message, "--"}, files...)
	if _, err = c.runWrite(ctx, root, false, args...); err != nil {
		return "", err
	}
	return c.HeadCommit(ctx, root)
}
