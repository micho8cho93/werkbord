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
	"sync"
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
// inspection never writes to the repository. Lazy fetching and all transports
// are disabled (see gitEnv) because a repository's own config could otherwise
// make even a read run a command.
type CLI struct {
	Binary    string        // defaults to "git" on PATH
	Timeout   time.Duration // per command; defaults to 10s
	MaxOutput int64         // bytes kept per output stream; defaults to 1 MiB
	MaxProcs  int           // git processes running at once; defaults to 4

	slotsOnce sync.Once
	slots     chan struct{}
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

	// The common directory is shared by a repository's main checkout and all of
	// its linked worktrees, which have different top levels. It is relative to
	// the working directory (root) when the repository is not a linked worktree.
	common, err := c.git(ctx, root, "rev-parse", "--git-common-dir")
	if err != nil {
		return nil, fmt.Errorf("%s: %w: %v", abs, ErrUninspectable, err)
	}
	if !filepath.IsAbs(common) {
		common = filepath.Join(root, common)
	}
	if common, err = filepath.EvalSymlinks(common); err != nil {
		return nil, fmt.Errorf("%s: %w: %v", common, ErrUninspectable, err)
	}

	repo := &domain.GitRepository{RootPath: root, CommonDir: common, Remotes: []domain.GitRemote{}, InspectedAt: time.Now().UTC().Truncate(time.Millisecond)}

	// Each of these exits 1 for a legitimate "none": no commits yet, detached
	// HEAD, no remotes, unknown origin/HEAD. Only other failures are errors.
	if repo.HeadCommit, err = c.optional(ctx, root, "rev-parse", "-q", "--verify", "HEAD^{commit}"); err != nil {
		return nil, err
	}
	if repo.HeadCommit == "" {
		// An unborn branch has no HEAD at all. A HEAD that resolves to an object
		// we cannot read as a commit is damage, and must not look like "empty".
		named, err := c.optional(ctx, root, "rev-parse", "-q", "--verify", "HEAD")
		if err != nil {
			return nil, err
		}
		if named != "" {
			return nil, fmt.Errorf("%s: %w: HEAD points at %s, which is missing or not a commit", root, ErrUninspectable, named)
		}
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

// redactURL removes credentials embedded in a remote URL, which some users
// have from `https://user:token@host/...` clones.
//
// The userinfo ends at the last '@' of the authority, not the first: user
// names that are email addresses, and passwords containing '@', are common, and
// cutting early left the secret in the output. For http(s) and ftp nothing of
// the userinfo is kept, since tokens are often sent as the user name. For other
// schemes the user name is kept (ssh://git@host is not a secret) but never a
// password.
func redactURL(u string) string {
	scheme, rest, ok := strings.Cut(u, "://")
	if !ok {
		return u // scp-like git@host:path
	}
	end := strings.IndexAny(rest, "/?#")
	if end < 0 {
		end = len(rest)
	}
	authority, tail := rest[:end], rest[end:]
	at := strings.LastIndex(authority, "@")
	if at < 0 {
		return u
	}
	userinfo, host := authority[:at], authority[at+1:]
	switch strings.ToLower(scheme) {
	case "http", "https", "ftp", "ftps":
		return scheme + "://" + host + tail
	}
	user, _, hasPassword := strings.Cut(userinfo, ":")
	if !hasPassword {
		return u
	}
	return scheme + "://" + user + "@" + host + tail
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

// repoLocalEnv are the variables that select or reconfigure a repository
// without any path being given. They mirror `git rev-parse --local-env-vars`
// (which Git itself clears before running in another repository) plus
// GIT_NAMESPACE, which changes which refs HEAD resolves to; a test keeps the
// list in step with the installed Git. Left in place they make `git -C dir`
// describe whatever repository the controller's environment points at, as
// happens when it is started from a Git hook or `git rebase --exec`.
var repoLocalEnv = map[string]bool{
	"GIT_ALTERNATE_OBJECT_DIRECTORIES": true,
	"GIT_COMMON_DIR":                   true,
	"GIT_CONFIG":                       true,
	"GIT_CONFIG_COUNT":                 true,
	"GIT_CONFIG_PARAMETERS":            true,
	"GIT_DIR":                          true,
	"GIT_GRAFT_FILE":                   true,
	"GIT_IMPLICIT_WORK_TREE":           true,
	"GIT_INDEX_FILE":                   true,
	"GIT_INTERNAL_SUPER_PREFIX":        true,
	"GIT_NAMESPACE":                    true,
	"GIT_NO_REPLACE_OBJECTS":           true,
	"GIT_OBJECT_DIRECTORY":             true,
	"GIT_PREFIX":                       true,
	"GIT_QUARANTINE_PATH":              true,
	"GIT_REPLACE_REF_BASE":             true,
	"GIT_SHALLOW_FILE":                 true,
	"GIT_WORK_TREE":                    true,
}

func isRepoLocalEnv(kv string) bool {
	name, _, _ := strings.Cut(kv, "=")
	return repoLocalEnv[name] || strings.HasPrefix(name, "GIT_CONFIG_KEY_") || strings.HasPrefix(name, "GIT_CONFIG_VALUE_")
}

// gitEnv returns base, minus variables that select or reconfigure a repository,
// plus the settings every git process started here runs with. Those come last
// so they win over values inherited from the controller.
//
// Reading a repository can run code chosen by whoever wrote its config: when
// it is configured as a partial clone and an object is missing, Git lazily
// fetches the object from the "promisor" remote, which runs core.sshCommand or
// remote.<name>.uploadpack. Inspection never needs the network, so both lazy
// fetching and every transport are switched off. Two independent settings are
// used because GIT_NO_LAZY_FETCH only exists in Git 2.44 and later, while
// GIT_ALLOW_PROTOCOL is honoured by every transport in older versions.
//
// The user's own setup (GIT_CONFIG_GLOBAL, GIT_EXEC_PATH, PATH, ...) is kept.
func gitEnv(base []string) []string {
	env := make([]string, 0, len(base)+5)
	for _, kv := range base {
		if !isRepoLocalEnv(kv) {
			env = append(env, kv)
		}
	}
	return append(env,
		"GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0", "LC_ALL=C",
		"GIT_NO_LAZY_FETCH=1", "GIT_ALLOW_PROTOCOL=none")
}

const (
	defaultMaxOutput = 1 << 20
	defaultMaxProcs  = 4

	// waitDelay is how long after a timeout git's pipes are held open for. On
	// timeout only git is killed; a process it started (a stuck ssh, credential
	// helper or pager) survives and keeps stdout/stderr open, and exec.Cmd
	// otherwise waits for those pipes without limit.
	waitDelay = time.Second
)

var errOutputTooLarge = errors.New("output too large")

// capWriter keeps at most max bytes. Past that it stops the command, because
// a process blocked writing to a pipe nobody reads would never exit.
type capWriter struct {
	buf      bytes.Buffer
	max      int64
	cancel   context.CancelFunc
	exceeded bool
}

func (w *capWriter) Write(p []byte) (int, error) {
	if int64(w.buf.Len())+int64(len(p)) > w.max {
		w.exceeded = true
		w.cancel()
		return 0, errOutputTooLarge
	}
	return w.buf.Write(p)
}

// acquire takes one of the MaxProcs slots, or gives up when ctx is done.
// Every request that reaches git comes through here, so a burst of requests
// queues instead of becoming a burst of processes.
func (c *CLI) acquire(ctx context.Context) (release func(), err error) {
	c.slotsOnce.Do(func() {
		n := c.MaxProcs
		if n <= 0 {
			n = defaultMaxProcs
		}
		c.slots = make(chan struct{}, n)
	})
	select {
	case c.slots <- struct{}{}:
		return func() { <-c.slots }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
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
	maxOut := c.MaxOutput
	if maxOut <= 0 {
		maxOut = defaultMaxOutput
	}
	cmdline := "git " + strings.Join(args, " ")

	release, err := c.acquire(ctx)
	if err != nil {
		return "", fmt.Errorf("%s: waiting for a free slot: %w", cmdline, err)
	}
	defer release()

	// The timeout starts once the command may run, not while it is queued.
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	full := append([]string{"-C", dir}, args...)
	cmd := exec.CommandContext(ctx, bin, full...)
	cmd.Env = gitEnv(os.Environ())
	cmd.WaitDelay = waitDelay
	stdout, stderr := &capWriter{max: maxOut, cancel: cancel}, &capWriter{max: maxOut, cancel: cancel}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	err = cmd.Run()

	switch {
	case stdout.exceeded || stderr.exceeded:
		return "", fmt.Errorf("%s: %w (more than %d bytes)", cmdline, errOutputTooLarge, maxOut)
	case ctx.Err() != nil:
		return "", fmt.Errorf("%s: %w", cmdline, ctx.Err())
	case errors.Is(err, exec.ErrWaitDelay):
		return "", fmt.Errorf("%s: a process it started kept its output open", cmdline)
	}
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return "", &gitError{args: args, code: ee.ExitCode(), stderr: strings.TrimSpace(stderr.buf.String())}
		}
		return "", fmt.Errorf("run git: %w", err)
	}
	return strings.TrimSpace(stdout.buf.String()), nil
}
