package doctor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"devboard/internal/agent"
	"devboard/internal/config"
	"devboard/internal/domain"
	"devboard/internal/gitrepo"
	"devboard/internal/netprivate"
	"devboard/internal/service"
	"devboard/internal/store/sqlite"
)

// Env is what the checks look at. A field that is nil is a thing that cannot be
// checked from here, and its check says so rather than guessing.
type Env struct {
	Config config.Config
	// Agents are the registered agents.
	Agents *agent.Registry
	// Network reports the private network; nil, or ok=false, means there is no
	// running controller to ask.
	Network func() (netprivate.Status, bool)
	GitHub  *service.GitHubSetup
	// Settings and Projects need the controller's database.
	Settings *service.Settings
	Projects *service.Projects
	Git      gitrepo.Inspector
	// GitBinary is the git executable (default "git").
	GitBinary string
}

// Standard returns the checks that need no running controller, in the order a
// report lists them, plus the ones that use its live parts when Env has them.
func (e Env) Standard() []Func {
	return []Func{e.database, e.configCheck, e.git, e.agents, e.network, e.github, e.runner, e.projects}
}

func skip(id, name, why string) Check { return Check{ID: id, Name: name, Status: Skip, Summary: why} }

// ---- database ----

func (e Env) database(ctx context.Context) []Check {
	info, err := sqlite.Inspect(ctx, e.Config.DBPath())
	switch {
	case err != nil:
		return []Check{{ID: "database", Name: "database", Status: Fail, Summary: "cannot read it: " + err.Error(),
			Fix: "Check that " + e.Config.DBPath() + " is readable. Copies made before upgrades are in " + sqlite.BackupDir(e.Config.DBPath()) + "."}}
	case !info.Exists:
		return []Check{{ID: "database", Name: "database", Status: Warn, Summary: "not created yet (" + e.Config.DBPath() + ")",
			Fix: "It is created the first time the controller starts: run `werkbord start`."}}
	case info.NewerThanBuild:
		return []Check{{ID: "database", Name: "database", Status: Fail,
			Summary: fmt.Sprintf("schema v%d is newer than this build understands (v%d)", info.Version, info.Latest),
			Fix:     "This werkbord is older than the one that wrote the database. Run `werkbord update`."}}
	case info.Integrity != "ok":
		return []Check{{ID: "database", Name: "database", Status: Fail, Summary: "SQLite reports damage: " + clip(info.Integrity, 120),
			Fix: "Stop the controller and restore the newest copy in " + sqlite.BackupDir(e.Config.DBPath()) + "."}}
	case info.Version < info.Latest:
		return []Check{{ID: "database", Name: "database", Status: Warn,
			Summary: fmt.Sprintf("schema v%d, behind this build's v%d; it is upgraded (after a backup) when the controller starts", info.Version, info.Latest),
			Fix:     "Run `werkbord restart`."}}
	}
	return []Check{{ID: "database", Name: "database", Status: OK,
		Summary: fmt.Sprintf("schema v%d (current), %s, integrity ok", info.Version, size(info.Size))}}
}

// ---- configuration ----

func (e Env) configCheck(context.Context) []Check {
	var warns []string
	warns = append(warns, e.Config.RiskyAgentSettings()...)
	if runtime.GOOS != "windows" {
		for _, p := range []struct{ path, what string }{{e.Config.TokenPath(), "the access token file"}, {e.Config.DataDir, "the data directory"}} {
			if fi, err := os.Stat(p.path); err == nil && fi.Mode().Perm()&0o077 != 0 {
				warns = append(warns, fmt.Sprintf("%s (%s) can be read by other users: mode %o", p.what, p.path, fi.Mode().Perm()))
			}
		}
	}
	if !e.Config.AuthRequired() {
		warns = append(warns, "the access token is switched off for this computer (requireToken=false): any program here can use the API")
	}
	if !e.Config.IsLoopback() {
		warns = append(warns, "the controller listens on "+e.Config.Addr+", not only on this computer; the access token is what protects it")
	}
	if err := e.Config.Validate(); err != nil {
		return []Check{{ID: "config", Name: "config", Status: Fail, Summary: err.Error(), Fix: "Fix " + filepath.Join(e.Config.DataDir, "config.json") + "."}}
	}
	if len(warns) > 0 {
		return []Check{{ID: "config", Name: "config", Status: Warn, Summary: strings.Join(warns, "; ")}}
	}
	return []Check{{ID: "config", Name: "config", Status: OK, Summary: "valid; data in " + e.Config.DataDir}}
}

// ---- git ----

var gitVersionRE = regexp.MustCompile(`(\d+)\.(\d+)`)

func (e Env) git(ctx context.Context) []Check {
	bin := e.GitBinary
	if bin == "" {
		bin = "git"
	}
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(cctx, bin, "--version").Output()
	if err != nil {
		return []Check{{ID: "git", Name: "git", Status: Fail, Summary: "not found on PATH", Fix: "Install Git (https://git-scm.com/downloads): Werkbord works through it."}}
	}
	v := strings.TrimSpace(string(out))
	m := gitVersionRE.FindStringSubmatch(v)
	if m != nil {
		major, _ := strconv.Atoi(m[1])
		minor, _ := strconv.Atoi(m[2])
		if major < 2 || (major == 2 && minor < 25) {
			return []Check{{ID: "git", Name: "git", Status: Fail, Summary: v + ": too old for worktrees", Fix: "Update Git to 2.25 or newer."}}
		}
		if major == 2 && minor < 38 {
			return []Check{{ID: "git", Name: "git", Status: Warn, Summary: v + ": works, but cannot simulate merges", Fix: "Git 2.38 or newer lets repository health predict merge conflicts without touching anything."}}
		}
	}
	return []Check{{ID: "git", Name: "git", Status: OK, Summary: strings.TrimPrefix(v, "git version ")}}
}

// ---- agents ----

func (e Env) agents(ctx context.Context) []Check {
	if e.Agents == nil {
		return []Check{skip("agents", "agents", "not available")}
	}
	var out []Check
	usable := 0
	for _, a := range e.Agents.Detect(ctx) {
		c := Check{ID: a.ID, Name: a.ID}
		switch {
		case a.Available && a.SignIn == domain.SignedIn:
			c.Status, c.Summary = OK, fmt.Sprintf("%s %s, signed in", a.Name, a.Version)
			usable++
		case a.Available:
			c.Status, c.Summary = OK, fmt.Sprintf("%s %s (sign-in status not reported by this version)", a.Name, a.Version)
			usable++
		case a.Installed:
			c.Status, c.Summary, c.Fix = Warn, fmt.Sprintf("%s %s is installed but %s", a.Name, a.Version, trimPrefix(a.Detail)), a.Guidance
		default:
			c.Status, c.Summary, c.Fix = Warn, a.Name+" is not installed (optional if another agent works)", a.Guidance
		}
		out = append(out, c)
	}
	if usable == 0 {
		out = append(out, Check{ID: "agent", Name: "agent", Status: Fail, Summary: "no coding agent can be used",
			Fix: "Install and sign in to Claude Code or Codex (see above). Werkbord uses your own account and never asks for an API key."})
	}
	return out
}

func trimPrefix(detail string) string {
	if detail == "" {
		return "cannot be used"
	}
	return detail
}

// ---- private network ----

func (e Env) network(ctx context.Context) []Check {
	if e.Network == nil {
		return []Check{skip("network", "network", "needs the controller running")}
	}
	st, ok := e.Network()
	if !ok {
		return []Check{skip("network", "network", "needs the controller running")}
	}
	c := Check{ID: "network", Name: "network"}
	switch st.State {
	case netprivate.StateOff:
		c.Status, c.Summary = Warn, "private network is off: Werkbord is reachable on this computer only"
		c.Fix = "To use it from your phone, run `werkbord open --phone` or turn on phone access in Settings."
	case netprivate.StateStarting:
		c.Status, c.Summary = Warn, "private network is starting"
		c.Fix = "Give it a few seconds and run doctor again."
	case netprivate.StateNeedsLogin:
		// The sign-in link is a credential for the user, so it is not printed here.
		c.Status, c.Summary = Warn, "private network is waiting for you to sign in"
		c.Fix = "Run `werkbord open --phone`, or open Werkbord and use \"Sign in\" under Settings → Phone access."
	case netprivate.StateNeedsApproval:
		c.Status, c.Summary = Warn, "private network is waiting for your tailnet's admin to approve this device"
		c.Fix = "Approve it at https://login.tailscale.com/admin/machines."
	case netprivate.StateError:
		c.Status, c.Summary = Fail, "private network failed: "+clip(st.Error, 160)
		c.Fix = "Run `werkbord restart`. If it persists, turn phone access off and on in Settings."
	case netprivate.StateConnected:
		c.Status, c.Summary = OK, "connected at "+st.URL
		if !st.HTTPS {
			c.Status = Warn
			c.Summary += " (http: encrypted by the tailnet, but a phone cannot install it as an app)"
			c.Fix = st.HTTPSHint
		}
		if len(st.Health) > 0 {
			c.Status, c.Summary = Warn, c.Summary+"; Tailscale reports: "+clip(strings.Join(st.Health, "; "), 160)
		}
	}
	return []Check{c}
}

// ---- GitHub ----

func (e Env) github(ctx context.Context) []Check {
	if e.GitHub == nil {
		return []Check{skip("github", "github", "not available")}
	}
	st := e.GitHub.Status(ctx)
	c := Check{ID: "github", Name: "github"}
	switch st.State {
	case service.GitHubDisabled:
		c.Status, c.Summary = Skip, "turned off in config.json"
	case service.GitHubMissing:
		c.Status, c.Summary, c.Fix = Warn, "optional: the GitHub CLI (gh) is not installed", st.Guidance
	case service.GitHubSignedOut:
		c.Status, c.Summary, c.Fix = Warn, "optional: gh is installed ("+st.Version+") but not signed in", "Connect GitHub from Settings, or run `gh auth login`."
	case service.GitHubSigningIn:
		c.Status, c.Summary = Warn, "waiting for you to finish signing in on github.com"
	case service.GitHubError:
		c.Status, c.Summary, c.Fix = Warn, "GitHub could not be checked: "+clip(st.Message, 160), "Pull requests and repository discovery wait until it can; everything local works."
	default:
		who := ""
		if st.Account != nil {
			who = " as " + st.Account.Login
		}
		c.Status, c.Summary = OK, "connected"+who+" (gh "+st.Version+")"
	}
	return []Check{c}
}

// ---- runner and projects ----

func (e Env) runner(ctx context.Context) []Check {
	if e.Settings == nil {
		return []Check{skip("runner", "runner", "needs the controller running")}
	}
	rs, err := e.Settings.Runners(ctx)
	if err != nil {
		return []Check{{ID: "runner", Name: "runner", Status: Fail, Summary: "cannot list runners: " + err.Error()}}
	}
	for _, r := range rs {
		if r.Kind == domain.RunnerLocal {
			return []Check{{ID: "runner", Name: "runner", Status: OK, Summary: fmt.Sprintf("this computer (%s, %s/%s) is registered and online", r.Name, r.OS, r.Arch)}}
		}
	}
	return []Check{{ID: "runner", Name: "runner", Status: Fail, Summary: "this computer is not registered as a runner", Fix: "Run `werkbord restart`: the controller registers it when it starts."}}
}

func (e Env) projects(ctx context.Context) []Check {
	if e.Projects == nil {
		return []Check{skip("projects", "projects", "needs the controller running")}
	}
	ps, err := e.Projects.List(ctx)
	if err != nil {
		return []Check{{ID: "projects", Name: "projects", Status: Fail, Summary: "cannot list projects: " + err.Error()}}
	}
	if len(ps) == 0 {
		return []Check{{ID: "projects", Name: "projects", Status: Warn, Summary: "none configured yet", Fix: "Add one from the app, or run `werkbord project add <path>`."}}
	}
	var bad []string
	for _, p := range ps {
		if _, err := os.Stat(p.RepoPath); err != nil {
			bad = append(bad, fmt.Sprintf("%s: %s no longer exists", p.Name, p.RepoPath))
			continue
		}
		if e.Git != nil {
			if repo, err := e.Git.Inspect(ctx, p.RepoPath); err != nil {
				bad = append(bad, fmt.Sprintf("%s: %s is no longer a Git repository", p.Name, p.RepoPath))
			} else if repo.RootPath != p.RepoPath {
				bad = append(bad, fmt.Sprintf("%s: %s now belongs to the repository at %s", p.Name, p.RepoPath, repo.RootPath))
			}
		}
	}
	if len(bad) > 0 {
		return []Check{{ID: "projects", Name: "projects", Status: Warn, Summary: fmt.Sprintf("%d of %d have a problem: %s", len(bad), len(ps), strings.Join(bad, "; ")),
			Fix: "Restore the folder, or add it again at its new location."}}
	}
	return []Check{{ID: "projects", Name: "projects", Status: OK, Summary: fmt.Sprintf("%d configured, all reachable", len(ps))}}
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func size(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}
