package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"devboard/internal/controller"
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

	_, wasUp := a.healthy(ctx)
	if err := a.checkInterruption(ctx, *force); err != nil {
		return err
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
	a.printf("Installed %s over %s.\n", target, version)
	if !wasUp {
		a.printf("Start it with `devboard start`. A copy of the old version is at %s.\n", prev)
		return a.restartUpdatedRunner(ctx)
	}
	if err := a.start(ctx, false); err != nil {
		// Finish rollback even if the update's caller interrupted or timed out.
		recoveryCtx, recoveryCancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
		defer recoveryCancel()
		// The new controller did not come up: go back to the one that did.
		a.printf("The new version did not start: putting %s back.\n", version)
		if rerr := update.Restore(self, prev); rerr != nil {
			return fmt.Errorf("%w; and the old version could not be put back: %v (it is at %s)", err, rerr, prev)
		}
		if snapshot != "" {
			if stopErr := a.stop(recoveryCtx, false); stopErr != nil {
				return fmt.Errorf("%w; cannot stop failed controller before restoring data: %v", err, stopErr)
			}
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
		return fmt.Errorf("the update to %s failed and %s is running again: %w", target, version, err)
	}
	v, _ := a.healthy(ctx)
	a.printf("Dev Board %s is running again.\n", v)
	_ = os.Remove(prev)
	return a.restartUpdatedRunner(ctx)
}

func (a *app) restartUpdatedRunner(ctx context.Context) error {
	if _, err := os.Stat(runnerDir(a.cfg) + string(os.PathSeparator) + "identity.json"); os.IsNotExist(err) || a.runnerDaemon == nil {
		return nil
	}
	m := a.runnerDaemon(ctx)
	state, err := m.Status(ctx)
	if err != nil {
		return fmt.Errorf("updated successfully, but could not check the runner service: %w; use devboard runner stop, then devboard runner start", err)
	}
	if !state.Running {
		return nil
	}
	if err := m.Restart(ctx); err != nil {
		return fmt.Errorf("updated successfully, but the runner service could not restart: %w; use devboard runner stop, then devboard runner start", err)
	}
	a.printf("Runner service restarted on the updated executable.\n")
	return nil
}
