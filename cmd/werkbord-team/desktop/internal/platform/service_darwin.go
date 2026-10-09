package platform

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// shellQuote and appleString encode two different languages. Neither interpolates an invitation or workspace content.
func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
func appleString(s string) string {
	return "\"" + strings.NewReplacer("\\", "\\\\", "\"", "\\\"", "\n", "\\n", "\r", "\\r").Replace(s) + "\""
}

// A failed bootout must never be followed by swapping/removing a live service.
func stopInstalledService() error {
	_, _ = exec.Command("/bin/launchctl", "bootout", "system/"+ServiceLabel).CombinedOutput()
	if exec.Command("/bin/launchctl", "print", "system/"+ServiceLabel).Run() == nil {
		return errors.New("Team service is still running; installation and data have been preserved")
	}
	return nil
}

// AuthorizeService invokes this signed application's fixed installer through macOS's normal administrator dialog.
func AuthorizeService(ctx context.Context, action string) error {
	if !ValidAction(action) {
		return errors.New("no such service action")
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	uid := strconv.Itoa(os.Getuid())
	cmd := shellQuote(exe) + " --team-service " + shellQuote(action) + " " + shellQuote(uid)
	// osascript is only in the native installer, never in the daemon or workspace build. The arguments are fixed operations.
	out, err := exec.CommandContext(ctx, "/usr/bin/osascript", "-e", "do shell script "+appleString(cmd)+" with administrator privileges").CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "-128") {
			return errors.New("service installation was cancelled; choose Try again when you are ready")
		}
		return fmt.Errorf("macOS could not %s the Team service: %s", action, strings.TrimSpace(string(out)))
	}
	return nil
}

// PrivilegedService runs before any web view exists. It accepts only a real OS user and three fixed lifecycle operations.
func PrivilegedService(action, uid string) (resultErr error) {
	if os.Geteuid() != 0 {
		return errors.New("macOS administrator authorization is required")
	}
	n, err := strconv.Atoi(uid)
	if err != nil || n < 501 {
		return errors.New("a normal macOS account is required")
	}
	u, err := user.LookupId(uid)
	if err != nil {
		return err
	}
	if !ValidAction(action) {
		return errors.New("no such service action")
	}
	// An isolated installation is an installation whose service is told nothing about the person's own Werkbord.
	isolated := action == ActionInstallIsolated
	if isolated {
		action = ActionInstall
	}
	ownerPath := filepath.Join(SystemDir, "owner")
	if b, err := os.ReadFile(ownerPath); err == nil && strings.TrimSpace(string(b)) != uid {
		return errors.New("Werkbord Team is installed for another user on this Mac")
	}
	if action == "uninstall" {
		if err := CheckReplacement(filepath.Join(SystemDir, "data")); err != nil {
			return err
		}
		// A live or unaccounted workspace is never removed by an OS installer. The daemon must first complete a checked leave.
		if _, err := os.Lstat(filepath.Join(SystemDir, "data", "workspace")); !errors.Is(err, os.ErrNotExist) {
			return errors.New("leave the workspace safely in Team before removing its service; the workspace copy has been preserved")
		}
		_ = exec.Command("/bin/launchctl", "disable", "system/"+ServiceLabel).Run()
		if err := stopInstalledService(); err != nil {
			return err
		}
		if _, err := os.Lstat(filepath.Join(SystemDir, "data", "workspace")); !errors.Is(err, os.ErrNotExist) {
			return errors.New("workspace creation finished while uninstall was stopping the service; its copy is preserved, start Team and leave safely")
		}
		if err := CheckReplacement(filepath.Join(SystemDir, "data")); err != nil {
			return err
		}
		// Explicitly retained archives/backups stay on disk; uninstall never silently destroys them.
		return removeEmptyService(SystemDir, ServicePlist)
	}
	if action == "stop" {
		if err := exec.Command("/bin/launchctl", "disable", "system/"+ServiceLabel).Run(); err != nil {
			return err
		}
		if exec.Command("/bin/launchctl", "print", "system/"+ServiceLabel).Run() != nil {
			return nil
		}
		_, err := exec.Command("/bin/launchctl", "bootout", "system/"+ServiceLabel).CombinedOutput()
		return err
	}
	if action == "start" {
		if err := exec.Command("/bin/launchctl", "enable", "system/"+ServiceLabel).Run(); err != nil {
			return err
		}
		return exec.Command("/bin/launchctl", "bootstrap", "system", ServicePlist).Run()
	}
	if err := CheckReplacement(filepath.Join(SystemDir, "data")); err != nil {
		return err
	}
	// Only the authenticated owner's key can seed the local connection. Nothing from the GUI supplies a root data path.
	access := filepath.Join(u.HomeDir, "Library", "Application Support", "werkbord-team-desktop", "access.key")
	fi, err := os.Lstat(access)
	if err != nil {
		return err
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok || int(st.Uid) != n || !fi.Mode().IsRegular() || fi.Mode().Perm()&0077 != 0 {
		return errors.New("the desktop credential must be a private file owned by this user")
	}
	b, err := os.ReadFile(access)
	if err != nil {
		return err
	}
	if _, err := hex.DecodeString(string(b)); err != nil || len(b) != 64 {
		return errors.New("invalid desktop credential")
	}
	// Refuse symlinks in the installation. All installed executable and data directories are root owned.
	for _, dir := range []string{SystemDir, filepath.Join(SystemDir, "Helpers"), filepath.Join(SystemDir, "data")} {
		if fi, err := os.Lstat(dir); err == nil {
			st, ok := fi.Sys().(*syscall.Stat_t)
			if !ok || st.Uid != 0 || !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 {
				return errors.New("a service directory is not a real root-owned directory")
			}
		}
		if err := os.MkdirAll(dir, 0700); err != nil {
			return err
		}
		if err := os.Chmod(dir, 0700); err != nil {
			return err
		}
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	helpers := filepath.Clean(filepath.Join(filepath.Dir(exe), "..", "Helpers"))
	// A release signs the service and the window. Verification happens before replacing any installed executable.
	appRoot := filepath.Clean(filepath.Join(filepath.Dir(exe), "..", ".."))
	if err := exec.Command("/usr/bin/codesign", "--verify", "--strict", "--deep", appRoot).Run(); err != nil {
		return errors.New("the Team app's signature could not be verified")
	}
	if err := exec.Command("/usr/bin/codesign", "--verify", "--strict", filepath.Join(helpers, "werkbord-team")).Run(); err != nil {
		return errors.New("the Team service's signature could not be verified")
	}
	if err := VerifyRelease(filepath.Join(appRoot, "Contents"), nativeVersion); err != nil {
		return err
	}
	// Stop the candidate before recovering a previous generation. A healthy but
	// uncommitted candidate still advertises maintenance through its local API.
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
		if _, err := os.Stat(ServicePlist); err == nil {
			if err := exec.Command("/bin/launchctl", "bootstrap", "system", ServicePlist).Run(); err != nil {
				return fmt.Errorf("Team installation restored but could not restart: %w", err)
			}
		}
	}
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
	// Fence device mutations before stopping; the Hub honors the prepared marker.
	if err := beginInstall(SystemDir, ServicePlist); err != nil {
		return err
	}
	rollback := true
	defer func() {
		if rollback {
			if err := stopInstalledService(); err != nil {
				resultErr = errors.Join(resultErr, err)
				return
			}
			if err := recoverInstall(SystemDir, ServicePlist); err != nil {
				resultErr = errors.Join(resultErr, fmt.Errorf("automatic rollback failed; backups retained: %w", err))
				return
			}
			if _, err := os.Stat(ServicePlist); err == nil {
				if err := exec.Command("/bin/launchctl", "bootstrap", "system", ServicePlist).Run(); err != nil {
					resultErr = errors.Join(resultErr, fmt.Errorf("prior Team installation restored but could not restart: %w", err))
				}
			}
		}
	}()
	if err := stopInstalledService(); err != nil {
		return err
	}
	if err := CheckReplacement(filepath.Join(SystemDir, "data")); err != nil {
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
	if err := atomicFile(filepath.Join(SystemDir, "access.key"), b, 0600); err != nil {
		return err
	}
	if err := atomicFile(ownerPath, []byte(uid), 0600); err != nil {
		return err
	}
	runner := filepath.Join(u.HomeDir, "Library", "Application Support", "werkbord", "config.json")
	if _, err := os.Stat(filepath.Dir(runner)); errors.Is(err, os.ErrNotExist) {
		legacy := filepath.Join(u.HomeDir, "Library", "Application Support", "devboard", "config.json")
		if _, err := os.Stat(filepath.Dir(legacy)); err == nil {
			runner = legacy
		}
	}
	if isolated {
		runner = ""
	}
	plist, err := LaunchDaemon(SystemDir, runner)
	if err != nil {
		return err
	}
	if err := atomicFile(ServicePlist, plist, 0644); err != nil {
		return err
	}
	if err := exec.Command("/bin/launchctl", "enable", "system/"+ServiceLabel).Run(); err != nil {
		return err
	}
	if err := exec.Command("/bin/launchctl", "bootstrap", "system", ServicePlist).Run(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for {
		if err := probe(ctx, string(b), nativeVersion, true); err == nil {
			break
		}
		if ctx.Err() != nil {
			return errors.New("Team did not become healthy; the prior installation is restored")
		}
		time.Sleep(250 * time.Millisecond)
	}
	if err := finishInstall(SystemDir); err != nil {
		return err
	}
	rollback = false
	return nil
}

// SetupRunner opens an installed free Werkbord app, or uses its bundled signed CLI to do the normal supported setup.
// This is a local native action. The Team daemon and Hosts have no entry point to it.
func SetupRunner(ctx context.Context) error {
	if _, err := os.Stat("/Applications/Werkbord.app"); err == nil {
		return exec.CommandContext(ctx, "/usr/bin/open", "/Applications/Werkbord.app").Run()
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cli := filepath.Clean(filepath.Join(filepath.Dir(exe), "..", "Helpers", "werkbord"))
	if _, err := os.Stat(cli); err != nil {
		return errors.New("this app has no runner installer; download the supported Werkbord app from its release page")
	}
	if err := exec.CommandContext(ctx, "/usr/bin/codesign", "--verify", "--strict", cli).Run(); err != nil {
		return errors.New("the bundled runner's signature could not be verified")
	}
	// Werkbord's own installer preserves DEVBOARD aliases and credentials, installs its login service and applies its own policies.
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	bin := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(bin, 0755); err != nil {
		return err
	}
	installed := filepath.Join(bin, "werkbord")
	if _, err := os.Stat(installed); errors.Is(err, os.ErrNotExist) {
		if err := copyFile(cli, installed, 0755); err != nil {
			return err
		}
	}
	alias := filepath.Join(bin, "devboard")
	if _, err := os.Lstat(alias); errors.Is(err, os.ErrNotExist) {
		if err := os.Symlink("werkbord", alias); err != nil {
			return err
		}
	}
	cmd := exec.CommandContext(ctx, installed, "setup", "--no-open", "--no-network")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("Werkbord setup could not finish: %s", strings.TrimSpace(string(out)))
	}
	return nil
}
