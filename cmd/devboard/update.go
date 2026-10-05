package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"devboard/internal/controller"
	"devboard/internal/daemon"
	"devboard/internal/remote"
	"devboard/internal/runnerwire"
	"devboard/internal/store/sqlite"
	"devboard/internal/update"
)

// cmdUpdate installs a newer release over this executable, restarts the
// controller on it, and puts the old one back if the new one does not come up.
// Nothing is installed unless its checksum matches, and the new executable is run
// once before it replaces anything.
func (a *app) cmdUpdate(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	fs.SetOutput(a.errOut)
	check := fs.Bool("check", false, "only say whether a newer version exists")
	want := fs.String("version", "", "install this version (default: the latest)")
	force := fs.Bool("force", false, "install even if it is not newer, or this build is not a release")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: devboard update [--check] [--version vX.Y.Z] [--force]")
	}
	src := update.Source{}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()

	if !update.Release(version) && !*force && !*check {
		return fmt.Errorf("this devboard (%s) was built from source, not installed from a release: update it from the source tree (git pull && make build), or use --force to install the latest release over it", version)
	}
	var tag string // how the release is found: werkbord-v1.2.3
	if *want == "" {
		var err error
		if tag, err = src.Latest(ctx); err != nil {
			return err
		}
	} else if !update.ValidTag(*want) {
		return fmt.Errorf("%q is not a version: want something like v1.2.3", *want)
	} else {
		tag = update.TagFor(*want)
	}
	target := update.VersionOf(tag) // what the program reports: v1.2.3

	cmp := update.Compare(version, target)
	switch {
	case *check:
		if cmp < 0 || !update.Release(version) {
			a.printf("Update available: %s → %s. Run `devboard update` to install it.\n", version, target)
		} else {
			a.printf("Up to date (%s).\n", version)
		}
		return nil
	case cmp == 0 && update.Release(version) && !*force:
		a.printf("Already up to date (%s).\n", version)
		return nil
	case cmp > 0 && *want != "" && !*force:
		return fmt.Errorf("%s is older than the installed %s: a downgrade can fail on a database the newer version has already upgraded (a copy from before the upgrade is in %s); use --force if you mean it",
			target, version, a.cfg.DataDir+string(os.PathSeparator)+"backups")
	case cmp > 0 && *want == "" && !*force:
		a.printf("This devboard (%s) is newer than the latest release (%s): nothing to do.\n", version, target)
		return nil
	}

	self, err := a.executable()
	if err != nil {
		return err
	}
	tmp, err := os.MkdirTemp("", "devboard-update-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	a.printf("Downloading %s…\n", target)
	archive, err := src.Download(ctx, tag, runtime.GOOS, runtime.GOARCH, tmp)
	if err != nil {
		return err
	}
	fresh, err := update.ExtractBinary(archive, runtime.GOOS, tmp)
	if err != nil {
		return err
	}
	// The checksum says the download is what was published. Running it says it is a
	// devboard for this computer, of the version it claims, before it replaces anything.
	out, err := exec.CommandContext(ctx, fresh, "version").Output()
	if err != nil || strings.TrimSpace(string(out)) != target {
		return fmt.Errorf("the downloaded executable did not report %s (it said %q): not installing it", target, strings.TrimSpace(string(out)))
	}

	return a.installRelease(ctx, self, fresh, target, version, *force)
}

// cmdInstallRelease is the installer's private entry point. The installer has
// already verified the archive checksum and the downloaded binary's version.
// Running that binary gives upgrades the current recovery implementation even
// when the installed version predates it.
func (a *app) cmdInstallRelease(ctx context.Context, args []string) error {
	if len(args) != 1 || !filepath.IsAbs(args[0]) || !update.Release(version) {
		return fmt.Errorf("installer upgrade requires an absolute existing executable path and a release build")
	}
	self, err := filepath.EvalSymlinks(args[0])
	if err != nil {
		return fmt.Errorf("cannot locate installed executable: %w", err)
	}
	fresh, err := a.executable()
	if err != nil {
		return err
	}
	out, err := exec.CommandContext(ctx, self, "version").Output()
	if err != nil {
		return fmt.Errorf("cannot read the installed version: %w; original executable preserved", err)
	}
	current := strings.TrimSpace(string(out))
	if update.Compare(current, version) > 0 {
		return fmt.Errorf("installer release %s is older than installed %s; use devboard update --version %s --force for an intentional downgrade", version, current, version)
	}
	if current == version {
		a.printf("Already installed (%s). Run `devboard setup` to change startup options.\n", version)
		return nil
	}
	a.executable = func() (string, error) { return self, nil }
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	return a.installRelease(ctx, self, fresh, version, current, false)
}

// installRelease is shared by CLI updates and installer upgrades. The source
// binary's version is separate from the running installer's version.
func (a *app) installRelease(ctx context.Context, self, fresh, target, current string, force bool) error {
	_, wasUp := a.healthy(ctx)
	if err := a.checkInterruption(ctx, force); err != nil {
		return err
	}
	runner, err := a.runnerForUpdate(ctx)
	if err != nil {
		return err
	}
	// Remember required services before the executable or controller changes.
	// A service that crashes during the upgrade is still required to recover.
	if runner != nil {
		if err := runner.Stop(ctx); err != nil {
			return fmt.Errorf("stop the runner before updating: %w", err)
		}
		defer func() {
			// Restart also on a pre-install failure or controller rollback. A new
			// sync is verified separately before an upgrade is declared complete.
			recoveryCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), startTimeout)
			defer cancel()
			if st, err := runner.Status(recoveryCtx); err == nil && !st.Running {
				if err := runner.Start(recoveryCtx); err != nil {
					a.printf("Runner recovery failed: %v. Use `devboard runner start`; its identity and journals were preserved.\n", err)
				}
			}
		}()
	}
	if wasUp {
		if err := a.stop(ctx, false); err != nil {
			return err
		}
	}
	unlock, err := controller.LockController(a.cfg.LockPath())
	if err != nil {
		return err
	}
	snapshot, err := a.snapshotDatabase(ctx)
	if err != nil {
		unlock()
		if wasUp {
			_ = a.start(ctx, false)
		}
		return fmt.Errorf("cannot back up the database before updating: %w", err)
	}
	unlock()
	prev, err := update.Replace(self, fresh)
	if err != nil {
		if wasUp {
			_ = a.start(ctx, false)
		}
		return err
	}
	a.printf("Installed %s over %s.\n", target, current)
	if !wasUp {
		if err := a.restartUpdatedRunner(ctx, runner, target); err != nil {
			return fmt.Errorf("update recovery incomplete: %w; old executable: %s; database backup: %s", err, prev, snapshot)
		}
		a.printf("Controller verification is pending: start it with `devboard start`. The old executable is retained at %s; database backup: %s.\n", prev, snapshot)
		return nil
	}
	if err := a.startUpdatedController(ctx, target); err != nil {
		// Finish rollback even if the update's caller interrupted or timed out.
		recoveryCtx, recoveryCancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
		defer recoveryCancel()
		// The new controller did not come up: go back to the one that did.
		a.printf("The new version did not start: putting %s back.\n", current)
		if stopErr := a.stop(recoveryCtx, false); stopErr != nil {
			return fmt.Errorf("%w; cannot stop failed controller: %v (old executable: %s; database backup: %s)", err, stopErr, prev, snapshot)
		}
		if rerr := update.Restore(self, prev); rerr != nil {
			return fmt.Errorf("%w; and the old version could not be put back: %v (it is at %s)", err, rerr, prev)
		}
		if snapshot != "" {
			unlock, lockErr := controller.LockController(a.cfg.LockPath())
			if lockErr != nil {
				return lockErr
			}
			restoreErr := a.restoreDatabase(recoveryCtx, snapshot)
			unlock()
			if restoreErr != nil {
				return fmt.Errorf("%w; database restore failed: %v (backup: %s)", err, restoreErr, snapshot)
			}
		}
		if serr := a.start(recoveryCtx, false); serr != nil {
			return fmt.Errorf("%w; the old version was put back but does not start either (%v): your data was backed up before the upgrade, in %s", err, serr, a.cfg.DataDir+string(os.PathSeparator)+"backups")
		}
		return fmt.Errorf("the update to %s failed and %s is running again: %w", target, current, err)
	}
	if err := a.restartUpdatedRunner(ctx, runner, target); err != nil {
		return fmt.Errorf("update recovery incomplete: %w; a copy of the old executable is retained at %s; database backup: %s", err, prev, snapshot)
	}
	if err := os.Remove(prev); err != nil {
		return fmt.Errorf("services recovered, but could not remove rollback executable %s: %w", prev, err)
	}
	a.printf("Dev Board %s is running again; database and required runner verified.\n", target)
	return nil
}

func (a *app) runnerForUpdate(ctx context.Context) (daemon.Manager, error) {
	if a.runnerDaemon == nil {
		return nil, nil
	}
	if _, err := os.Stat(filepath.Join(runnerDir(a.cfg), "identity.json")); os.IsNotExist(err) {
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("cannot inspect runner identity before updating: %w", err)
	}
	m := a.runnerDaemon(ctx)
	state, err := m.Status(ctx)
	if err != nil {
		return nil, fmt.Errorf("cannot check runner service before updating: %w; use devboard runner status", err)
	}
	if !state.Running {
		return nil, nil
	}
	if _, err := loadIdentity(a.cfg); err != nil {
		return nil, fmt.Errorf("cannot read the running runner's identity: %w", err)
	}
	return m, nil
}

func (a *app) startUpdatedController(ctx context.Context, target string) error {
	if err := a.start(ctx, false); err != nil {
		return err
	}
	if v, ok := a.healthy(ctx); !ok || v != target {
		return fmt.Errorf("updated controller did not report %s (reported %q)", target, v)
	}
	c, err := a.client()
	if err != nil {
		return err
	}
	if _, err := c.listProjects(ctx); err != nil {
		return fmt.Errorf("updated controller cannot read projects: %w", err)
	}
	info, err := sqlite.Inspect(ctx, a.cfg.DBPath())
	if err != nil || !info.Exists || info.Integrity != "ok" {
		return fmt.Errorf("updated database is unusable (integrity %q): %v", info.Integrity, err)
	}
	return nil
}

func (a *app) restartUpdatedRunner(ctx context.Context, m daemon.Manager, target string) error {
	if m == nil {
		return nil
	}
	id, err := loadIdentity(a.cfg)
	if err != nil {
		return err
	}
	notBefore := time.Now()
	if err := m.Start(ctx); err != nil {
		return fmt.Errorf("runner service could not restart: %w; use devboard runner start", err)
	}
	waitCtx, cancel := context.WithTimeout(ctx, startTimeout)
	defer cancel()
	for {
		st, err := m.Status(waitCtx)
		if err != nil {
			return fmt.Errorf("cannot verify runner service: %w; use devboard runner status", err)
		}
		if !st.Running {
			return fmt.Errorf("runner service did not stay running; use devboard runner status and inspect its logs")
		}
		var health remote.Health
		data, err := os.ReadFile(filepath.Join(runnerDir(a.cfg), "health.json"))
		if err == nil && json.Unmarshal(data, &health) == nil && health.Version == target &&
			health.RunnerID == id.RunnerID && health.Controller == id.Controller &&
			!health.SyncedAt.Before(notBefore) && health.Sequence > id.ControllerSequence &&
			!health.SyncedAt.After(time.Now()) && time.Since(health.SyncedAt) < runnerwire.OnlineWindow {
			a.printf("Runner %s reconnected on %s.\n", id.RunnerID, target)
			return nil
		}
		select {
		case <-waitCtx.Done():
			return fmt.Errorf("runner did not complete a fresh controller sync on %s: %w; use devboard runner status and check its controller connection", target, waitCtx.Err())
		case <-time.After(a.poll):
		}
	}
}
