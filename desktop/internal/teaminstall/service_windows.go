//go:build windows

package teaminstall

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
	"golang.org/x/sys/windows/svc/mgr"
)

type windowsServiceDefinition struct {
	BinaryPath string `json:"binaryPath"`
	StartType  uint32 `json:"startType"`
	Running    bool   `json:"running"`
}

func AuthorizeService(ctx context.Context, action string) error {
	if !ValidAction(action) {
		return errors.New("no such service action")
	}
	token := windows.GetCurrentProcessToken()
	user, err := token.GetTokenUser()
	if err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	psQuote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
	args := "--team-service " + action + " " + user.User.Sid.String()
	script := "$p=Start-Process -FilePath " + psQuote(exe) + " -ArgumentList " + psQuote(args) + " -Verb RunAs -Wait -PassThru; exit $p.ExitCode"
	cmd, err := windowsCommand(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)
	if err != nil {
		return err
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		detail := strings.TrimSpace(string(out))
		if strings.Contains(strings.ToLower(detail), "cancel") || strings.Contains(strings.ToLower(detail), "canceled") {
			return errors.New("service setup was cancelled; choose Try again when you are ready")
		}
		return installerRefusal(action, detail)
	}
	return nil
}

func PrivilegedService(action, sidText string) error {
	if !windows.GetCurrentProcessToken().IsElevated() {
		return errors.New("Windows administrator authorization is required")
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	appRoot := releaseRoot(exe)
	if err := RequireRelease(appRoot); err != nil {
		return err
	}
	if !ValidAction(action) {
		return errors.New("no such service action")
	}
	sid, err := windows.StringToSid(sidText)
	if err != nil || sidText == "" {
		return errors.New("a valid Windows user account is required")
	}
	home, err := profileForSID(sidText)
	if err != nil {
		return err
	}
	ownerPath := filepath.Join(SystemDir, "owner")
	if b, err := os.ReadFile(ownerPath); err == nil && strings.TrimSpace(string(b)) != sidText {
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
		if err := stopWindowsService(); err != nil {
			return err
		}
		if err := sc("delete", ServiceLabel); err != nil && windowsServiceInstalled() {
			return err
		}
		if err := waitForWindowsService(false, 30*time.Second); err != nil {
			return err
		}
		if _, err := os.Lstat(filepath.Join(data, "workspace")); !errors.Is(err, os.ErrNotExist) {
			return errors.New("workspace creation finished while uninstall was stopping the service; its copy is preserved, start Team and leave safely")
		}
		if err := CheckReplacement(data); err != nil {
			return err
		}
		for _, p := range []string{filepath.Join(SystemDir, "Helpers"), ServicePlist} {
			if err := os.RemoveAll(p); err != nil {
				return err
			}
		}
		return nil
	}
	if action == ActionStop {
		if windowsServiceInstalled() {
			if err := sc("config", ServiceLabel, "start=", "disabled"); err != nil {
				return err
			}
		}
		return stopWindowsService()
	}
	if action == ActionStart {
		if err := sc("config", ServiceLabel, "start=", "auto"); err != nil {
			return err
		}
		if err := sc("start", ServiceLabel); err != nil {
			return err
		}
		return waitForWindowsService(true, 30*time.Second)
	}
	if err := CheckReplacement(data); err != nil {
		return err
	}
	access := accessKeyPath(home)
	if err := verifyWindowsOwner(access, sid); err != nil {
		return fmt.Errorf("the desktop credential must be a private file owned by this user: %w", err)
	}
	b, err := os.ReadFile(access)
	if err != nil {
		return err
	}
	if _, err := hex.DecodeString(string(b)); err != nil || len(b) != 64 {
		return errors.New("invalid desktop credential")
	}
	for _, dir := range []string{SystemDir, filepath.Join(SystemDir, "Helpers"), data, filepath.Join(SystemDir, "secrets")} {
		if err := rejectReparsePoint(dir); err != nil {
			return err
		}
		if err := os.MkdirAll(dir, 0700); err != nil {
			return err
		}
	}
	if err := setWindowsACL(SystemDir, sid, true); err != nil {
		return err
	}
	if err := setWindowsACL(filepath.Join(SystemDir, "Helpers"), sid, true); err != nil {
		return err
	}
	if err := setWindowsACL(data, nil, false); err != nil {
		return err
	}
	if err := setWindowsACL(filepath.Join(SystemDir, "secrets"), nil, false); err != nil {
		return err
	}
	if err := VerifyRelease(appRoot); err != nil {
		return err
	}
	if x, err := readInstall(SystemDir); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	} else if err == nil && x.Phase == "prepared" {
		if err := stopWindowsService(); err != nil {
			return err
		}
		if err := recoverInstall(SystemDir, ServicePlist); err != nil {
			return fmt.Errorf("Team installation recovery failed; backups retained: %w", err)
		}
		if err := restoreWindowsService(); err != nil {
			return fmt.Errorf("Team installation files were restored but its service could not be restored: %w", err)
		}
	}
	if current, err := captureServiceDefinition(); err == nil {
		if err := atomicFile(ServicePlist, current, 0600); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	} else {
		_ = os.Remove(ServicePlist)
	}
	if err := ensureWindowsPassphrase(filepath.Join(SystemDir, "secrets", "pki.passphrase")); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(SystemDir, ".helpers-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	for _, name := range []string{"werkbord-team.exe", "nebula.exe", "rqlited.exe", "wintun.dll"} {
		if err := copyFile(filepath.Join(filepath.Dir(exe), "..", "Helpers", name), filepath.Join(stage, name), 0700); err != nil {
			return err
		}
	}
	if err := copyFile(filepath.Join(filepath.Dir(exe), "..", "Resources", "rqlited.build"), filepath.Join(stage, "rqlited.build"), 0600); err != nil {
		return err
	}
	if err := beginInstall(SystemDir, ServicePlist); err != nil {
		return err
	}
	rollback := true
	defer func() {
		if rollback {
			_ = stopWindowsService()
			if recoverInstall(SystemDir, ServicePlist) == nil {
				_ = restoreWindowsService()
			}
		}
	}()
	if err := stopWindowsService(); err != nil {
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
	if err := setWindowsACL(filepath.Join(SystemDir, "access.key"), sid, true); err != nil {
		return err
	}
	if err := atomicFile(ownerPath, []byte(sidText), 0600); err != nil {
		return err
	}
	if err := installWindowsService(); err != nil {
		return err
	}
	if def, err := captureServiceDefinition(); err == nil {
		if err := atomicFile(ServicePlist, def, 0600); err != nil {
			return err
		}
	} else {
		return err
	}
	if err := sc("start", ServiceLabel); err != nil {
		return err
	}
	if err := waitForWindowsService(true, 30*time.Second); err != nil {
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

func profileForSID(sidText string) (string, error) {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows NT\CurrentVersion\ProfileList\`+sidText, registry.QUERY_VALUE)
	if err != nil {
		return "", errors.New("the current Windows user's profile could not be located")
	}
	defer k.Close()
	home, _, err := k.GetStringValue("ProfileImagePath")
	if err != nil || home == "" || strings.Contains(home, "%") {
		return "", errors.New("the current Windows user's profile path is invalid")
	}
	return filepath.Clean(home), nil
}

func verifyWindowsOwner(path string, expected *windows.SID) error {
	if err := rejectReparsePoint(path); err != nil {
		return err
	}
	fi, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !fi.Mode().IsRegular() {
		return errors.New("credential is not a regular file")
	}
	system, _ := windows.StringToSid("S-1-5-18")
	admins, _ := windows.StringToSid("S-1-5-32-544")
	return verifyWindowsPrivateACL(path, expected, system, admins)
}

func rejectReparsePoint(path string) error {
	attrs, err := windows.GetFileAttributes(windows.StringToUTF16Ptr(path))
	if err != nil && !errors.Is(err, windows.ERROR_FILE_NOT_FOUND) && !errors.Is(err, windows.ERROR_PATH_NOT_FOUND) {
		return err
	}
	if err == nil && attrs&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return errors.New("a service path is a reparse point; installation stopped")
	}
	return nil
}

func setWindowsACL(path string, userSID *windows.SID, allowUser bool) error {
	args := []string{path, "/inheritance:r", "/grant:r", "*S-1-5-18:(OI)(CI)F", "*S-1-5-32-544:(OI)(CI)F"}
	if allowUser && userSID != nil {
		args = append(args, "*"+userSID.String()+":(OI)(CI)RX")
	}
	args = append(args, "/remove:g", "*S-1-1-0", "*S-1-5-11", "*S-1-5-32-545")
	cmd, err := windowsCommand(context.Background(), "icacls.exe", args...)
	if err != nil {
		return err
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("protect Team service files: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func windowsSystemPath(name string) (string, error) {
	dir, err := windows.GetSystemDirectory()
	if err != nil {
		return "", err
	}
	switch name {
	case "sc.exe", "icacls.exe":
		return filepath.Join(dir, name), nil
	case "powershell.exe":
		return filepath.Join(dir, "WindowsPowerShell", "v1.0", name), nil
	default:
		return "", errors.New("unsupported Windows system program")
	}
}

func windowsCommand(ctx context.Context, name string, args ...string) (*exec.Cmd, error) {
	path, err := windowsSystemPath(name)
	if err != nil {
		return nil, err
	}
	return exec.CommandContext(ctx, path, args...), nil
}

func ensureWindowsPassphrase(path string) error {
	if err := rejectReparsePoint(path); err != nil {
		return err
	}
	if fi, err := os.Lstat(path); err == nil {
		if !fi.Mode().IsRegular() || fi.Size() < 32 || fi.Size() > 128 {
			return errors.New("the Team key passphrase is not a private regular file")
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

func captureServiceDefinition() ([]byte, error) {
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, `SYSTEM\CurrentControlSet\Services\`+ServiceLabel, registry.QUERY_VALUE)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return nil, os.ErrNotExist
		}
		return nil, err
	}
	defer key.Close()
	image, _, err := key.GetStringValue("ImagePath")
	if err != nil || image == "" {
		return nil, errors.New("the installed Team service definition is unreadable")
	}
	start, _, err := key.GetIntegerValue("Start")
	if err != nil {
		return nil, err
	}
	def := windowsServiceDefinition{BinaryPath: image, StartType: uint32(start)}
	if m, err := mgr.Connect(); err == nil {
		if s, err := m.OpenService(ServiceLabel); err == nil {
			if status, err := s.Query(); err == nil {
				def.Running = status.State == 4 // SERVICE_RUNNING
			}
			_ = s.Close()
		}
		_ = m.Disconnect()
	}
	return json.Marshal(def)
}

func restoreWindowsService() error {
	b, err := os.ReadFile(ServicePlist)
	if errors.Is(err, os.ErrNotExist) {
		if windowsServiceInstalled() {
			_ = stopWindowsService()
			if err := sc("delete", ServiceLabel); err != nil && windowsServiceInstalled() {
				return err
			}
			return waitForWindowsService(false, 30*time.Second)
		}
		return nil
	}
	if err != nil {
		return err
	}
	var def windowsServiceDefinition
	if json.Unmarshal(b, &def) != nil || def.BinaryPath == "" {
		return errors.New("saved Windows service definition is unreadable")
	}
	start := "demand"
	switch def.StartType {
	case mgr.StartAutomatic:
		start = "auto"
	case mgr.StartDisabled:
		start = "disabled"
	}
	if windowsServiceInstalled() {
		if err := sc("config", ServiceLabel, "binPath=", def.BinaryPath, "start=", start, "obj=", "LocalSystem"); err != nil {
			return err
		}
	} else {
		if err := sc("create", ServiceLabel, "binPath=", def.BinaryPath, "start=", start, "obj=", "LocalSystem", "DisplayName=", "Werkbord Team"); err != nil {
			return err
		}
	}
	if def.Running {
		if err := sc("start", ServiceLabel); err != nil {
			return err
		}
		return waitForWindowsService(true, 30*time.Second)
	}
	return nil
}

func installWindowsService() error {
	exe := filepath.Join(SystemDir, "Helpers", "werkbord-team.exe")
	args := []string{"daemon", "--data-dir", filepath.Join(SystemDir, "data"), "--local-key-file", filepath.Join(SystemDir, "access.key"), "--passphrase-file", filepath.Join(SystemDir, "secrets", "pki.passphrase")}
	var command strings.Builder
	command.WriteString(windows.EscapeArg(exe))
	for _, arg := range args {
		command.WriteByte(' ')
		command.WriteString(windows.EscapeArg(arg))
	}
	if windowsServiceInstalled() {
		if err := sc("config", ServiceLabel, "binPath=", command.String(), "start=", "auto", "obj=", "LocalSystem"); err != nil {
			return err
		}
		return nil
	}
	return sc("create", ServiceLabel, "binPath=", command.String(), "start=", "auto", "obj=", "LocalSystem", "DisplayName=", "Werkbord Team")
}

func sc(args ...string) error {
	cmd, err := windowsCommand(context.Background(), "sc.exe", args...)
	if err != nil {
		return err
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("Windows service manager: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func stopWindowsService() error {
	if !windowsServiceInstalled() {
		return nil
	}
	_ = sc("stop", ServiceLabel)
	return waitForWindowsService(false, 60*time.Second)
}

func waitForWindowsService(running bool, within time.Duration) error {
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		m, err := mgr.Connect()
		if err != nil {
			return err
		}
		s, err := m.OpenService(ServiceLabel)
		if err != nil {
			m.Disconnect()
			if !running && errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
				return nil
			}
			return err
		}
		status, queryErr := s.Query()
		s.Close()
		m.Disconnect()
		if queryErr == nil && (running && status.State == 4 || !running && status.State == 1) {
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	if running {
		return errors.New("Team service did not start before the timeout")
	}
	return errors.New("Team service is still running; installation and data have been preserved")
}
