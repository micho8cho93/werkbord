package teaminstall

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Maintenance is how an installed Team service that holds a workspace is brought to the app's version. An ordinary
// installation refuses such a service (CheckReplacement), because replacing it can strand a workspace. Maintenance is
// allowed only because it takes on that risk explicitly:
//
//   - it needs the same release trust as an installation, and the same administrator authorization;
//   - it takes a backup of the workspace through the running service and will not go on until that backup has been checked
//     against its recorded hash and opened by the NEW service program, which proves that version can read it;
//   - it replaces only the service's programs. The data directory, the workspace, its keys in the System keychain, the access
//     key, the owner record, network configuration and the hosting state are never written, moved or deleted;
//   - it keeps the previous programs (Helpers.previous), and unless the new service comes up healthy, with the workspace still
//     unsealed and enrolled, it puts them back and starts them again.
//
// What it cannot undo is a schema migration the new service has already made. If the previous programs then refuse the database,
// the backup it took is the way back (TEAM_BACKUP_RECOVERY.md); the error names it.

// ActionMaintain is the privileged operation. It is deliberately not in ValidAction: no platform's ordinary installer may fall
// into it, and only the macOS installer implements it.
const ActionMaintain = "maintain"

// ErrNothingToMaintain is returned when the service holds no workspace. The ordinary update applies to it.
var ErrNothingToMaintain = errors.New("Team's service on this computer holds no workspace, so the ordinary update applies; maintenance is only for a service with a workspace")

// ErrMaintenanceBlocked is returned while a workspace on this computer is changing hands or shape.
var ErrMaintenanceBlocked = errors.New("a Team workspace on this computer is being created, joined, left or demoted; let that finish before maintenance. Nothing was changed and the service keeps running")

// CheckMaintenance says whether a service holding a workspace may be maintained. It is the counterpart of CheckReplacement: it
// admits a settled workspace and still refuses one in transition, a symlinked slot, or no workspace at all. Like CheckReplacement
// it is repeated under administrator authority, after the service has stopped, and nothing a frontend says can waive it.
func CheckMaintenance(data string) error {
	if fi, err := os.Lstat(filepath.Join(data, "slots")); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		return errors.New("Team slots directory is a symlink")
	}
	dirs := []string{data}
	slots, err := os.ReadDir(filepath.Join(data, "slots"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for _, s := range slots {
		if s.Type()&os.ModeSymlink != 0 {
			return errors.New("a Team workspace slot is a symlink; inspect it before maintenance")
		}
		if s.IsDir() {
			dirs = append(dirs, filepath.Join(data, "slots", s.Name()))
		}
	}
	held := false
	for _, dir := range dirs {
		for _, name := range []string{"pending", "leaving", "demoting"} {
			if _, err := os.Lstat(filepath.Join(dir, name)); !errors.Is(err, os.ErrNotExist) {
				return ErrMaintenanceBlocked
			}
		}
		if fi, err := os.Lstat(filepath.Join(dir, "workspace")); err == nil {
			if fi.Mode()&os.ModeSymlink != 0 {
				return errors.New("a Team workspace is a symlink; inspect it before maintenance")
			}
			held = true
		}
	}
	if !held {
		return ErrNothingToMaintain
	}
	return nil
}

var backupDirArg = regexp.MustCompile(`<string>--backup-dir</string>\s*<string>([^<]*)</string>`)

// withBackupDir returns the service definition with --backup-dir added to the daemon's arguments, and the backup directory the
// result names. A definition that already names one is returned as it is. Nothing else in it changes, including a runner
// configuration the installation was made with, so a definition is never regenerated for maintenance.
func withBackupDir(plist []byte, dir string) ([]byte, string, error) {
	if m := backupDirArg.FindSubmatch(plist); m != nil {
		have := html.UnescapeString(string(m[1]))
		if !filepath.IsAbs(have) {
			return nil, "", errors.New("the service's backup directory is not an absolute path")
		}
		return plist, have, nil
	}
	if !filepath.IsAbs(dir) || strings.ContainsAny(dir, "<>&\"'\x00") {
		return nil, "", errors.New("the backup directory must be a plain absolute path")
	}
	const (
		label = "<key>Label</key><string>" + ServiceLabel + "</string>"
		args  = "<key>ProgramArguments</key><array>"
	)
	if !bytes.Contains(plist, []byte(label)) {
		return nil, "", errors.New("the installed service definition is not Team's; it was not changed")
	}
	i := bytes.Index(plist, []byte(args))
	if i < 0 {
		return nil, "", errors.New("the installed service definition has no program arguments; it was not changed")
	}
	rest := plist[i+len(args):]
	j := bytes.Index(rest, []byte("</array>"))
	if j < 0 {
		return nil, "", errors.New("the installed service definition is malformed; it was not changed")
	}
	at := i + len(args) + j
	out := make([]byte, 0, len(plist)+len(dir)+64)
	out = append(out, plist[:at]...)
	out = append(out, "<string>--backup-dir</string><string>"+dir+"</string>"...)
	out = append(out, plist[at:]...)
	return out, dir, nil
}

// maintenanceState is what the running service says about itself.
type maintenanceState struct {
	Daemon    bool            `json:"daemon"`
	Version   string          `json:"version"`
	Enrolled  bool            `json:"enrolled"`
	Pending   bool            `json:"pending"`
	Leaving   bool            `json:"leaving"`
	Connected bool            `json:"connected"`
	Operation string          `json:"operation"`
	Workspace json.RawMessage `json:"workspace"`
}

// unsealed reports whether the service has opened the workspace's sealed material: it only reports a workspace once it has.
func (s maintenanceState) unsealed() bool {
	return len(s.Workspace) > 0 && string(s.Workspace) != "null"
}

func readMaintenanceState(ctx context.Context, base, key string) (maintenanceState, error) {
	var s maintenanceState
	if err := askLocal(ctx, base+"/api/device/v1/state", key, &s); err != nil {
		return s, err
	}
	if !s.Daemon {
		return s, errors.New("the Team service could not be verified")
	}
	return s, nil
}

// beforeMaintenance says whether a service is fit to be maintained: running, with its workspace open and settled. A service
// that cannot show its workspace cannot be backed up through, and replacing it would hide whether the update harmed it.
func beforeMaintenance(s maintenanceState) error {
	switch {
	case !s.Enrolled || !s.unsealed():
		return errors.New("the running Team service does not show an open workspace, so a backup cannot be taken through it; nothing was changed")
	case s.Pending || s.Leaving || s.Operation != "":
		return ErrMaintenanceBlocked
	}
	return nil
}

// afterMaintenance says whether the new service is healthy: the right version, the workspace still enrolled and unsealed by the
// new program (a keychain item is tied to the program that made it, so an unsealing failure shows up here), and, when the
// workspace was connected before, connected again.
func afterMaintenance(s maintenanceState, version string, wasConnected bool) error {
	switch {
	case strings.TrimPrefix(s.Version, "v") != strings.TrimPrefix(version, "v"):
		return fmt.Errorf("the service reports version %q, not %q", s.Version, version)
	case !s.Enrolled || !s.unsealed():
		return errors.New("the new service could not open the workspace")
	case wasConnected && !s.Connected:
		return errors.New("the new service has not reconnected to the workspace")
	}
	return nil
}

// requestBackup asks the running service, as this person's device, to back the workspace up now into its backup directory.
func requestBackup(ctx context.Context, base, key string) error {
	cl := &http.Client{Timeout: 3 * time.Minute, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/team/v1/storage/backup", strings.NewReader("{}"))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	res, err := cl.Do(req)
	if err != nil {
		return fmt.Errorf("the backup could not be requested: %w", err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("the workspace refused the backup (%d): %s; this device must be an administrator and a Workspace Host", res.StatusCode, bytes.TrimSpace(body))
	}
	var st struct {
		Configured bool   `json:"configured"`
		LastOK     bool   `json:"lastOk"`
		LastError  string `json:"lastError"`
	}
	if err := json.Unmarshal(body, &st); err != nil {
		return errors.New("the backup's answer could not be read")
	}
	if !st.Configured || !st.LastOK || st.LastError != "" {
		return fmt.Errorf("the backup did not complete: %s", st.LastError)
	}
	return nil
}

// newestBackup names the newest workspace backup in dir made at or after notBefore.
func newestBackup(dir string, notBefore time.Time) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	var best string
	var bestAt time.Time
	for _, e := range entries {
		n := e.Name()
		if !strings.HasPrefix(n, "workspace-") || !strings.HasSuffix(n, ".sqlite") || !e.Type().IsRegular() {
			continue
		}
		fi, err := e.Info()
		if err != nil || fi.ModTime().Before(notBefore) {
			continue
		}
		if best == "" || fi.ModTime().After(bestAt) {
			best, bestAt = n, fi.ModTime()
		}
	}
	if best == "" {
		return "", errors.New("no new backup appeared in the backup directory")
	}
	return best, nil
}

// checkBackupFile recomputes the backup's hash and compares it with the record written beside it. It detects a damaged or
// truncated copy; it is not a signature (TEAM_BACKUP_RECOVERY.md says the same of the record).
func checkBackupFile(dir, name string) error {
	if name != filepath.Base(name) || !strings.HasSuffix(name, ".sqlite") {
		return errors.New("not a backup name")
	}
	raw, err := os.ReadFile(filepath.Join(dir, name+".json"))
	if err != nil {
		return fmt.Errorf("the backup has no record beside it: %w", err)
	}
	var rec struct {
		SHA256 string `json:"sha256"`
		Size   int64  `json:"size"`
	}
	if err := json.Unmarshal(raw, &rec); err != nil || len(rec.SHA256) != 64 {
		return errors.New("the backup's record is unreadable")
	}
	f, err := os.Open(filepath.Join(dir, name))
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return err
	}
	if hex.EncodeToString(h.Sum(nil)) != rec.SHA256 || (rec.Size != 0 && rec.Size != n) {
		return errors.New("the backup does not match its recorded hash")
	}
	if n == 0 {
		return errors.New("the backup is empty")
	}
	return nil
}

// runProgram runs one fixed program by absolute path with a minimal environment. A test replaces it.
var runProgram = func(ctx context.Context, prog string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, prog, args...)
	cmd.Env = []string{"PATH=/usr/bin:/bin:/usr/sbin:/sbin"}
	return cmd.CombinedOutput()
}

// verifyBackupWith asks a service program to open the backup the way a restore relies on it.
func verifyBackupWith(ctx context.Context, prog, dir, name string) error {
	if out, err := runProgram(ctx, prog, "storage", "verify-backup", name, "--dir", dir); err != nil {
		return fmt.Errorf("the new service program cannot read the backup: %w: %s", err, bytes.TrimSpace(out))
	}
	return nil
}

// programVersion is what a service program says it is.
func programVersion(ctx context.Context, prog string) (string, error) {
	out, err := runProgram(ctx, prog, "version")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// writeMaintenanceRecord leaves a root-owned note of what was backed up and replaced, for the person and for support.
func writeMaintenanceRecord(root string, rec map[string]any) error {
	dir := filepath.Join(root, "maintenance")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	return atomicFile(filepath.Join(dir, time.Now().UTC().Format("20060102T150405Z")+".json"), b, 0600)
}
