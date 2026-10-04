// Package gitrepo is the Git boundary. Today it only inspects repositories;
// worktree management, diffs and pushes will be added behind the same kind of
// interface when runs need them.
package gitrepo

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"devboard/internal/domain"
)

// Inspector reads metadata from a local repository without modifying it.
type Inspector interface {
	// Inspect validates that path is (inside) a non-bare Git repository and
	// returns its metadata. RootPath is the absolute, symlink-resolved top
	// level of the working tree. ProjectID is left empty.
	Inspect(ctx context.Context, path string) (*domain.GitRepository, error)
}

// Validation failures. All wrap domain.ErrInvalid.
var (
	ErrPathNotFound  = fmt.Errorf("%w: path does not exist", domain.ErrInvalid)
	ErrNotDirectory  = fmt.Errorf("%w: path is not a directory", domain.ErrInvalid)
	ErrNotRepository = fmt.Errorf("%w: path is not a Git repository", domain.ErrInvalid)
	ErrBare          = fmt.Errorf("%w: bare repositories are not supported", domain.ErrInvalid)
	ErrUninspectable = fmt.Errorf("%w: repository could not be inspected", domain.ErrInvalid)
)

// CLI implements Inspector by running the git executable.
//
// It only runs plumbing commands (rev-parse, symbolic-ref, config) that do
// not execute hooks or fsmonitor, and it sets GIT_OPTIONAL_LOCKS=0 so
// inspection never writes to the repository.
type CLI struct {
	Binary  string        // defaults to "git" on PATH
	Timeout time.Duration // per command; defaults to 10s
}

var _ Inspector = (*CLI)(nil)

// Inspect implements Inspector.
func (c *CLI) Inspect(ctx context.Context, path string) (*domain.GitRepository, error) {
	abs, err := resolveDir(path)
	if err != nil {
		return nil, err
	}

	bare, err := c.git(ctx, abs, "rev-parse", "--is-bare-repository")
	if err != nil {
		if isNotRepo(err) {
			return nil, fmt.Errorf("%s: %w", abs, ErrNotRepository)
		}
		return nil, fmt.Errorf("%s: %w: %v", abs, ErrUninspectable, err)
	}
	if bare == "true" {
		return nil, fmt.Errorf("%s: %w", abs, ErrBare)
	}

	top, err := c.git(ctx, abs, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, fmt.Errorf("%s: %w: %v", abs, ErrUninspectable, err)
	}
	root, err := filepath.EvalSymlinks(top)
	if err != nil {
		return nil, fmt.Errorf("%s: %w: %v", top, ErrUninspectable, err)
	}

	repo := &domain.GitRepository{RootPath: root, Remotes: []domain.GitRemote{}, InspectedAt: time.Now().UTC().Truncate(time.Millisecond)}

	// Each of these exits 1 for a legitimate "none": no commits yet, detached
	// HEAD, no remotes, unknown origin/HEAD. Only other failures are errors.
	if repo.HeadCommit, err = c.optional(ctx, root, "rev-parse", "-q", "--verify", "HEAD^{commit}"); err != nil {
		return nil, err
	}
	if repo.CurrentBranch, err = c.optional(ctx, root, "symbolic-ref", "-q", "--short", "HEAD"); err != nil {
		return nil, err
	}
	originHead, err := c.optional(ctx, root, "symbolic-ref", "-q", "--short", "refs/remotes/origin/HEAD")
	if err != nil {
		return nil, err
	}
	repo.DefaultBranch = strings.TrimPrefix(originHead, "origin/")

	urls, err := c.optional(ctx, root, "config", "--get-regexp", `^remote\..*\.url$`)
	if err != nil {
		return nil, err
	}
	repo.Remotes = parseRemotes(urls)
	return repo, nil
}

func resolveDir(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("%w: path is required", domain.ErrInvalid)
	}
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			path = filepath.Join(home, path[2:])
		}
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("%w: %v", domain.ErrInvalid, err)
	}
	info, err := os.Stat(abs)
	if errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("%s: %w", abs, ErrPathNotFound)
	}
	if err != nil {
		return "", fmt.Errorf("%s: %w: %v", abs, ErrUninspectable, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s: %w", abs, ErrNotDirectory)
	}
	return filepath.EvalSymlinks(abs)
}

func parseRemotes(out string) []domain.GitRemote {
	remotes := []domain.GitRemote{}
	for _, line := range strings.Split(out, "\n") {
		key, url, ok := strings.Cut(strings.TrimSpace(line), " ")
		if !ok {
			continue
		}
		name := strings.TrimSuffix(strings.TrimPrefix(key, "remote."), ".url")
		remotes = append(remotes, domain.GitRemote{Name: name, URL: redactURL(url)})
	}
	return remotes
}

// redactURL removes credentials embedded in an https remote URL, which some
// users have from `https://user:token@host/...` clones.
func redactURL(u string) string {
	scheme, rest, ok := strings.Cut(u, "://")
	if !ok || (scheme != "https" && scheme != "http") {
		return u // ssh user names such as git@ are not secrets
	}
	if at := strings.Index(rest, "@"); at >= 0 && at < strings.IndexAny(rest+"/", "/") {
		return scheme + "://" + rest[at+1:]
	}
	return u
}

type gitError struct {
	args   []string
	code   int
	stderr string
}

func (e *gitError) Error() string {
	return fmt.Sprintf("git %s: exit %d: %s", strings.Join(e.args, " "), e.code, e.stderr)
}

func isNotRepo(err error) bool {
	var ge *gitError
	return errors.As(err, &ge) && strings.Contains(strings.ToLower(ge.stderr), "not a git repository")
}

// optional runs git and maps exit status 1 (with no stderr) to "".
func (c *CLI) optional(ctx context.Context, dir string, args ...string) (string, error) {
	out, err := c.git(ctx, dir, args...)
	var ge *gitError
	if errors.As(err, &ge) && ge.code == 1 && ge.stderr == "" {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrUninspectable, err)
	}
	return out, nil
}

func (c *CLI) git(ctx context.Context, dir string, args ...string) (string, error) {
	bin := c.Binary
	if bin == "" {
		bin = "git"
	}
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	full := append([]string{"-C", dir}, args...)
	cmd := exec.CommandContext(ctx, bin, full...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0", "LC_ALL=C")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return "", &gitError{args: args, code: ee.ExitCode(), stderr: strings.TrimSpace(stderr.String())}
		}
		return "", fmt.Errorf("run git: %w", err)
	}
	return strings.TrimSpace(stdout.String()), nil
}
