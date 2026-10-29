package teaminstall

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// privilegedMaintain brings an installed Team service that holds a workspace to this app's version. See maintain.go for what it
// promises. It runs as root after the administrator's authorization, from the release-signed app, and takes no input but the
// person's uid. Every step before "stop the service" can fail with the service still running and nothing replaced.
func privilegedMaintain(exe, appRoot string, u *user.User, uid int) (resultErr error) {
	data := filepath.Join(SystemDir, "data")
	owner, err := os.ReadFile(filepath.Join(SystemDir, "owner"))
	if err != nil || strings.TrimSpace(string(owner)) != u.Uid {
		return errors.New("Werkbord Team is installed for another user on this Mac, or not installed")
	}
	if err := CheckMaintenance(data); err != nil {
		return err
	}
	for _, dir := range []string{SystemDir, filepath.Join(SystemDir, "Helpers"), data} {
		fi, err := os.Lstat(dir)
		if err != nil {
			return err
		}
		st, ok := fi.Sys().(*syscall.Stat_t)
		if !ok || st.Uid != 0 || !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 {
			return errors.New("a service directory is not a real root-owned directory")
		}
	}
	// The window's credential is the only way to speak to the running service; it must be the one the service was given.
	userKey, err := os.ReadFile(filepath.Join(u.HomeDir, "Library", "Application Support", "werkbord-team-desktop", "access.key"))
	if err != nil {
		return err
	}
	installedKey, err := os.ReadFile(filepath.Join(SystemDir, "access.key"))
	if err != nil || string(userKey) != string(installedKey) {
		return errors.New("the desktop credential is not the one this Team service was installed with; nothing was changed")
	}
	if _, err := hex.DecodeString(string(userKey)); err != nil || len(userKey) != 64 {
		return errors.New("invalid desktop credential")
	}
	key := string(userKey)

	helpers := filepath.Clean(filepath.Join(filepath.Dir(exe), "..", "Helpers"))
	if err := exec.Command("/usr/bin/codesign", "--verify", "--strict", "--deep", appRoot).Run(); err != nil {
		return errors.New("the Team app's signature could not be verified")
	}
	if err := exec.Command("/usr/bin/codesign", "--verify", "--strict", filepath.Join(helpers, "werkbord-team")).Run(); err != nil {
		return errors.New("the Team service's signature could not be verified")
	}
	if err := VerifyRelease(appRoot); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()

	// A candidate left unfinished by a crashed installation is recovered first, exactly as an installation does.
	x, err := readInstall(SystemDir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err == nil && x.Phase == "prepared" {
		if err := stopInstalledService(); err != nil {
			return err
		}
		if err := recoverInstall(SystemDir, ServicePlist); err != nil {
			return fmt.Errorf("Team installation recovery failed; backups retained: %w", err)
		}
		if err := exec.Command("/bin/launchctl", "bootstrap", "system", ServicePlist).Run(); err != nil {
			return fmt.Errorf("Team installation restored but could not restart: %w", err)
		}
	}

	// Stage the new programs and check them before anything is stopped or backed up.
	stage, err := os.MkdirTemp(SystemDir, ".helpers-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	for _, name := range []string{"werkbord-team", "nebula", "rqlited"} {
		if err := copyFile(filepath.Join(helpers, name), filepath.Join(stage, name), 0700); err != nil {
			return err
		}
	}
	if err := copyFile(filepath.Join(helpers, "..", "Resources", "rqlited.build"), filepath.Join(stage, "rqlited.build"), 0600); err != nil {
		return err
	}
	got, err := programVersion(ctx, filepath.Join(stage, "werkbord-team"))
	if err != nil || strings.TrimPrefix(got, "v") != strings.TrimPrefix(nativeVersion, "v") {
		return fmt.Errorf("the staged Team service reports %q, not this app's version %q; nothing was changed", got, nativeVersion)
	}

	targets, err := maintenanceTargets(ctx, data, BaseURL, key)
	if err != nil {
		return fmt.Errorf("the running Team service could not be asked: %w", err)
	}
	before := targets[0].Before
	if strings.TrimPrefix(before.Version, "v") == strings.TrimPrefix(nativeVersion, "v") {
		return nil // already current
	}

	// A verified backup, before any change to the service.
	backups := filepath.Join(SystemDir, "backups")
	if err := os.MkdirAll(backups, 0700); err != nil {
		return err
	}
	if err := os.Chmod(backups, 0700); err != nil {
		return err
	}
	oldPlist, err := os.ReadFile(ServicePlist)
	if err != nil {
		return err
	}
	newPlist, backupDir, err := withBackupDir(oldPlist, backups)
	if err != nil {
		return err
	}
	if string(newPlist) != string(oldPlist) {
		// The service needs a backup directory to back up into, and has none. Its only other change is that argument.
		if err := restartWith(ctx, newPlist, oldPlist, key, targets); err != nil {
			return err
		}
	}
	for i := range targets {
		started := time.Now().Add(-2 * time.Second)
		if err := requestBackup(ctx, targets[i].Base, key); err != nil {
			return fmt.Errorf("workspace %s: %w", targets[i].Slot, err)
		}
		backup, err := newestBackup(backupDir, started)
		if err != nil {
			return err
		}
		if err := checkBackupFile(backupDir, backup); err != nil {
			return err
		}
		retained, err := retainMaintenanceBackup(backupDir, backups, targets[i].Slot, backup)
		if err != nil {
			return err
		}
		if err := verifyBackupWith(ctx, filepath.Join(stage, "werkbord-team"), retained, backup); err != nil {
			return err
		}
		targets[i].Backup = backup
		targets[i].BackupDir = retained
	}
	if err := writeMaintenanceRecord(SystemDir, map[string]any{"from": before.Version, "to": nativeVersion, "backupDir": backupDir, "workspaces": targets, "verified": true}); err != nil {
		return err
	}

	// Replace the programs, and only the programs.
	if err := beginInstall(SystemDir, ServicePlist); err != nil {
		return err
	}
	rollback := true
	defer func() {
		if !rollback {
			return
		}
		note := fmt.Sprintf("; verified workspace backups are in %s", backupDir)
		if err := stopInstalledService(); err != nil {
			resultErr = errors.Join(resultErr, err)
			return
		}
		if err := recoverInstall(SystemDir, ServicePlist); err != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("automatic rollback failed; backups retained: %w%s", err, note))
			return
		}
		if err := exec.Command("/bin/launchctl", "bootstrap", "system", ServicePlist).Run(); err != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("the previous Team service was restored but could not restart%s: %w", note, err))
			return
		}
		if err := waitAllHealthy(ctx, key, before.Version, targets); err != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("the previous Team service was restored but is not healthy (a newer database schema may need the backup restored)%s: %w", note, err))
		}
	}()
	if err := stopInstalledService(); err != nil {
		return err
	}
	// The workspace may have changed shape while the service was stopping.
	if err := CheckMaintenance(data); err != nil {
		return err
	}
	if err := checkMaintenanceTargets(data, targets); err != nil {
		return err
	}
	installed := filepath.Join(SystemDir, "Helpers")
	previous := filepath.Join(SystemDir, "Helpers.previous")
	if err := os.Rename(installed, previous); err != nil {
		return err
	}
	if err := os.Rename(stage, installed); err != nil {
		return err
	}
	if err := exec.Command("/bin/launchctl", "enable", "system/"+ServiceLabel).Run(); err != nil {
		return err
	}
	if err := exec.Command("/bin/launchctl", "bootstrap", "system", ServicePlist).Run(); err != nil {
		return err
	}
	if err := waitAllHealthy(ctx, key, nativeVersion, targets); err != nil {
		return fmt.Errorf("the updated Team service did not become healthy, so the previous one was put back: %w", err)
	}
	if err := finishInstall(SystemDir); err != nil {
		return err
	}
	rollback = false
	return nil
}

// restartWith swaps the service definition and restarts the same programs. If the service does not come back with its
// workspace open, the original definition is restored and started again.
func restartWith(ctx context.Context, next, original []byte, key string, targets []maintenanceTarget) (err error) {
	if err := stopInstalledService(); err != nil {
		return err
	}
	if err := atomicFile(ServicePlist, next, 0644); err != nil {
		return err
	}
	defer func() {
		if err == nil {
			return
		}
		_ = exec.Command("/bin/launchctl", "bootout", "system/"+ServiceLabel).Run()
		if rerr := atomicFile(ServicePlist, original, 0644); rerr != nil {
			err = errors.Join(err, rerr)
			return
		}
		if rerr := exec.Command("/bin/launchctl", "bootstrap", "system", ServicePlist).Run(); rerr != nil {
			err = errors.Join(err, rerr)
		}
	}()
	if err := exec.Command("/bin/launchctl", "bootstrap", "system", ServicePlist).Run(); err != nil {
		return err
	}
	if err := waitAllHealthy(ctx, key, targets[0].Before.Version, targets); err != nil {
		return fmt.Errorf("the service did not come back with its backup directory: %w", err)
	}
	return nil
}

// waitHealthy waits for the service to report the version with its workspace open, and returns the last answer.
func waitHealthyAt(ctx context.Context, base, key, version string, wasConnected bool) (maintenanceState, error) {
	ctx, cancel := context.WithTimeout(ctx, 150*time.Second)
	defer cancel()
	var last error = errors.New("no answer")
	for {
		s, err := readMaintenanceState(ctx, base, key)
		if err == nil {
			if err = afterMaintenance(s, version, wasConnected); err == nil {
				return s, nil
			}
		}
		last = err
		select {
		case <-ctx.Done():
			return maintenanceState{}, last
		case <-time.After(time.Second):
		}
	}
}

func waitAllHealthy(ctx context.Context, key, version string, targets []maintenanceTarget) error {
	for _, target := range targets {
		after, err := waitHealthyAt(ctx, target.Base, key, version, target.Before.Connected)
		if err != nil {
			return fmt.Errorf("workspace %s: %w", target.Slot, err)
		}
		var oldID, newID struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(target.Before.Workspace, &oldID) != nil || json.Unmarshal(after.Workspace, &newID) != nil || oldID.ID == "" || oldID.ID != newID.ID {
			return fmt.Errorf("workspace %s identity changed after update", target.Slot)
		}
	}
	return nil
}
