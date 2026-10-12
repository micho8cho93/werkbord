//go:build linux

package teaminstall

import (
	"context"
	"crypto/rand"
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

func AuthorizeService(ctx context.Context, action string) error {
	if !ValidAction(action) {
		return errors.New("no such service action")
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	uid := strconv.Itoa(os.Getuid())
	out, err := exec.CommandContext(ctx, "/usr/bin/pkexec", exe, "--team-service", action, uid).CombinedOutput()
	if err != nil {
		detail := strings.TrimSpace(string(out))
		if strings.Contains(strings.ToLower(detail), "dismissed") || strings.Contains(strings.ToLower(detail), "cancel") {
			return errors.New("service setup was cancelled; choose Try again when you are ready")
		}
		return installerRefusal(action, detail)
	}
	return nil
}

func PrivilegedService(action, uid string) error {
	if os.Geteuid() != 0 {
		return errors.New("administrator authorization is required")
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	appRoot := releaseRoot(exe)
	if err := RequireRelease(appRoot); err != nil {
		return err
	}
	n, err := strconv.Atoi(uid)
	if err != nil || n < 1 {
		return errors.New("a normal Linux account is required")
	}
	u, err := user.LookupId(uid)
	if err != nil {
		return err
	}
	if !ValidAction(action) {
		return errors.New("no such service action")
	}
	isolated := action == ActionInstallIsolated
	if isolated {
		action = ActionInstall
	}
	ownerPath := filepath.Join(SystemDir, "owner")
	if b, err := os.ReadFile(ownerPath); err == nil && strings.TrimSpace(string(b)) != uid {
		return errors.New("Werkbord Team is installed for another user on this computer")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	data := filepath.Join(SystemDir, "data")
	if action == ActionUninstall {
		if err := CheckReplacement(data); err != nil {
			return err
		}
		if _, err := os.Lstat(filepath.Join(data, "workspace")); !errors.Is(err, os.ErrNotExist) {
			return errors.New("leave the workspace safely in Team before removing its service; the workspace copy has been preserved")
		}
		_ = exec.Command("/usr/bin/systemctl", "disable", "--now", ServiceLabel).Run()
		if err := stopInstalledService(); err != nil {
			return err
		}
		if _, err := os.Lstat(filepath.Join(data, "workspace")); !errors.Is(err, os.ErrNotExist) {
			return errors.New("workspace creation finished while uninstall was stopping the service; its copy is preserved, start Team and leave safely")
		}
		if err := CheckReplacement(data); err != nil {
			return err
		}
		for _, p := range []string{ServicePlist, filepath.Join(SystemDir, "Helpers")} {
			if err := os.RemoveAll(p); err != nil {
				return err
			}
		}
		_ = exec.Command("/usr/bin/systemctl", "daemon-reload").Run()
		return nil
	}
	if action == ActionStop {
		if err := exec.Command("/usr/bin/systemctl", "disable", ServiceLabel).Run(); err != nil {
			return err
		}
		return exec.Command("/usr/bin/systemctl", "stop", ServiceLabel).Run()
	}
	if action == ActionStart {
		if err := exec.Command("/usr/bin/systemctl", "enable", ServiceLabel).Run(); err != nil {
			return err
		}
		return exec.Command("/usr/bin/systemctl", "start", ServiceLabel).Run()
	}
	if err := CheckReplacement(data); err != nil {
		return err
	}
	access := AccessKeyPath(u.HomeDir)
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
	for _, dir := range []string{SystemDir, filepath.Join(SystemDir, "Helpers"), data, filepath.Join(SystemDir, "secrets")} {
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
	helpers := filepath.Clean(filepath.Join(filepath.Dir(exe), "..", "Helpers"))
	if err := VerifyRelease(appRoot); err != nil {
		return err
	}
	if err := ensurePassphrase(filepath.Join(SystemDir, "secrets", "pki.passphrase")); err != nil {
		return err
	}
	if x, err := readInstall(SystemDir); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	} else if err == nil && x.Phase == "prepared" {
		if err := stopInstalledService(); err != nil {
			return err
		}
		if err := recoverInstall(SystemDir, ServicePlist); err != nil {
			return fmt.Errorf("Team installation recovery failed; backups retained: %w", err)
		}
		_ = exec.Command("/usr/bin/systemctl", "daemon-reload").Run()
		if _, err := os.Stat(ServicePlist); err == nil {
			if err := exec.Command("/usr/bin/systemctl", "start", ServiceLabel).Run(); err != nil {
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
	if err := beginInstall(SystemDir, ServicePlist); err != nil {
		return err
	}
	rollback := true
	defer func() {
		if rollback {
			if stopErr := stopInstalledService(); stopErr != nil {
				return
			}
			if recoverErr := recoverInstall(SystemDir, ServicePlist); recoverErr == nil {
				_ = exec.Command("/usr/bin/systemctl", "daemon-reload").Run()
				if _, statErr := os.Stat(ServicePlist); statErr == nil {
					_ = exec.Command("/usr/bin/systemctl", "start", ServiceLabel).Run()
				}
			}
		}
	}()
	if err := stopInstalledService(); err != nil {
		return err
	}
	if err := CheckReplacement(data); err != nil {
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
	unit, err := LinuxUnit(SystemDir)
	if err != nil {
		return err
	}
	if err := atomicFile(ServicePlist, unit, 0644); err != nil {
		return err
	}
	if err := exec.Command("/usr/bin/systemctl", "daemon-reload").Run(); err != nil {
		return err
	}
	if err := exec.Command("/usr/bin/systemctl", "enable", "--now", ServiceLabel).Run(); err != nil {
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

func stopInstalledService() error {
	_ = exec.Command("/usr/bin/systemctl", "stop", ServiceLabel).Run()
	if out, err := exec.Command("/usr/bin/systemctl", "is-active", ServiceLabel).CombinedOutput(); err == nil || strings.TrimSpace(string(out)) == "active" {
		return errors.New("Team service is still running; installation and data have been preserved")
	}
	return nil
}

func ensurePassphrase(path string) error {
	if fi, err := os.Lstat(path); err == nil {
		st, ok := fi.Sys().(*syscall.Stat_t)
		if !ok || st.Uid != 0 || !fi.Mode().IsRegular() || fi.Mode().Perm()&0077 != 0 || fi.Size() < 32 || fi.Size() > 128 {
			return errors.New("the Team key passphrase is not a private root-owned file")
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	var key [32]byte
	if _, err := rand.Read(key[:]); err != nil {
		return err
	}
	return atomicFile(path, []byte(hex.EncodeToString(key[:])), 0600)
}
