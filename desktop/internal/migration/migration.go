// Package migration adopts existing installations in place. It never rewrites a product
// database, moves a repository, stops a service, or deletes an old installation.
package migration

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	_ "modernc.org/sqlite"
)

type Installation struct {
	Kind string `json:"kind"`
	Path string `json:"path"`
}

type Status struct {
	Phase         string         `json:"phase"`
	Installations []Installation `json:"installations"`
	Backup        string         `json:"backup,omitempty"`
}

type marker struct {
	Status
	Schema int               `json:"schema"`
	Hashes map[string]string `json:"hashes"`
}

type Manager struct {
	mu  sync.Mutex
	Dir string
	// Roots come from native discovery only, never a page-supplied path.
	Roots []Installation
	// Personal is the launcher's resolved data directory (including service/env overrides).
	Personal string
}

func (m *Manager) Status() (Status, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.status()
}

func (m *Manager) status() (Status, error) {
	x, err := m.read()
	if err == nil {
		return x.Status, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return Status{}, err
	}
	out := Status{Phase: "not_needed", Installations: []Installation{}}
	seen := map[string]bool{}
	for _, r := range m.Roots {
		if seen[r.Path] {
			continue
		}
		seen[r.Path] = true
		fi, err := os.Lstat(r.Path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return out, err
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			return out, fmt.Errorf("installation path is a symlink: %s; inspect it before migration", r.Path)
		}
		out.Installations = append(out.Installations, r)
	}
	if len(out.Installations) > 0 {
		out.Phase = "detected"
	}
	return out, nil
}

func (m *Manager) read() (marker, error) {
	var x marker
	b, err := os.ReadFile(filepath.Join(m.Dir, "migration.json"))
	if err != nil {
		return x, err
	}
	if len(b) > 1<<20 || json.Unmarshal(b, &x) != nil || x.Schema != 1 {
		return x, errors.New("migration marker is unreadable; preserve it for recovery")
	}
	switch x.Phase {
	case "prepared", "verified", "rolled_back":
	default:
		return x, errors.New("unknown migration phase")
	}
	if !strings.HasPrefix(x.Backup, "backup-") || !safeRelative(x.Backup) || filepath.Base(x.Backup) != x.Backup {
		return x, errors.New("invalid migration backup path")
	}
	for p := range x.Hashes {
		if !safeRelative(p) {
			return x, errors.New("invalid migration backup entry")
		}
	}
	return x, nil
}

func safeRelative(p string) bool {
	return p != "." && !filepath.IsAbs(p) && filepath.Clean(p) == p && p != ".." && !strings.HasPrefix(p, ".."+string(filepath.Separator))
}

// Prepare publishes the backup directory first, then the durable marker. Retry after a
// crash before marker publication rebuilds the snapshot; retry afterwards verifies it.
func (m *Manager) Prepare(ctx context.Context) (Status, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := os.MkdirAll(m.Dir, 0700); err != nil {
		return Status{}, err
	}
	fi, err := os.Lstat(m.Dir)
	if err != nil || !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 || fi.Mode().Perm()&0077 != 0 {
		return Status{}, errors.New("migration directory must be private")
	}
	unlock, err := lockDirectory(m.Dir)
	if err != nil {
		return Status{}, err
	}
	defer unlock()
	x, err := m.read()
	if err == nil && x.Phase != "rolled_back" {
		return x.Status, m.verifyBackup(x)
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return Status{}, err
	}
	st, err := m.status()
	if err != nil {
		return st, err
	}
	stage, err := os.MkdirTemp(m.Dir, ".backup-")
	if err != nil {
		return st, err
	}
	defer os.RemoveAll(stage)
	// Snapshot only managed state. Repositories/worktrees and credentials held by agents,
	// Git, Keychain or external stores remain in their original locations and are untouched.
	seen := map[string]bool{}
	for i, r := range m.Roots {
		if r.Path == "" || seen[r.Path] {
			continue
		}
		seen[r.Path] = true
		if r.Kind != "personal_data" && r.Kind != "devboard_data" && r.Kind != "team_user_data" && r.Kind != "shell_data" && r.Kind != "personal_resolved" {
			continue
		}
		if _, err := os.Stat(r.Path); errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err := snapshot(ctx, r.Path, filepath.Join(stage, fmt.Sprint(i)), r.Kind); err != nil {
			return st, err
		}
	}
	hashes, err := inventory(stage)
	if err != nil {
		return st, err
	}
	backupName := strings.TrimPrefix(filepath.Base(stage), ".")
	backup := filepath.Join(m.Dir, backupName)
	if err := os.Rename(stage, backup); err != nil {
		return st, err
	}
	if err := syncDir(m.Dir); err != nil {
		return st, err
	}
	x = marker{Status: Status{Phase: "prepared", Installations: st.Installations, Backup: backupName}, Schema: 1, Hashes: hashes}
	return x.Status, m.write(x)
}

func snapshot(ctx context.Context, src, dst, kind string) error {
	return filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		if d.IsDir() && rel != "." {
			switch d.Name() {
			case "worktrees", "logs", "backups", "storage", "tsnet", "tailscale", ".git":
				return filepath.SkipDir
			}
		}
		to := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(to, 0700)
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		if !fi.Mode().IsRegular() {
			return fmt.Errorf("managed state contains an unsupported file: %s", p)
		}
		if strings.HasSuffix(p, "-wal") || strings.HasSuffix(p, "-shm") || strings.HasSuffix(p, ".lock") || strings.HasSuffix(p, ".pid") {
			return nil
		}
		if strings.HasSuffix(p, ".db") || strings.HasSuffix(p, ".sqlite") {
			return snapshotDB(ctx, p, to)
		}
		in, err := os.Open(p)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(to, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		_, err = io.Copy(out, in)
		if err == nil {
			err = out.Sync()
		}
		cerr := out.Close()
		if err != nil {
			return err
		}
		return cerr
	})
}

func snapshotDB(ctx context.Context, src, dst string) error {
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(src)+"?mode=ro&_pragma=busy_timeout(5000)")
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err = db.ExecContext(ctx, "VACUUM INTO ?", dst); err != nil {
		return fmt.Errorf("back up %s: %w", src, err)
	}
	if err = os.Chmod(dst, 0600); err != nil {
		return err
	}
	check, err := sql.Open("sqlite", "file:"+filepath.ToSlash(dst)+"?mode=ro")
	if err != nil {
		return err
	}
	defer check.Close()
	var integrity string
	if err = check.QueryRowContext(ctx, "PRAGMA quick_check").Scan(&integrity); err != nil {
		return err
	}
	if integrity != "ok" {
		return errors.New("migration database backup failed integrity verification")
	}
	return nil
}

func inventory(dir string) (map[string]string, error) {
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		fi, err := d.Info()
		if err != nil || !fi.Mode().IsRegular() {
			return errors.New("invalid backup file")
		}
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		h := sha256.New()
		_, err = io.Copy(h, f)
		f.Close()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		out[rel] = hex.EncodeToString(h.Sum(nil))
		return nil
	})
	return out, err
}

func (m *Manager) verifyBackup(x marker) error {
	h, err := inventory(filepath.Join(m.Dir, x.Backup))
	if err != nil {
		return err
	}
	if len(h) != len(x.Hashes) {
		return errors.New("migration backup has changed")
	}
	for k, v := range x.Hashes {
		if h[k] != v {
			return errors.New("migration backup checksum mismatch")
		}
	}
	return nil
}

// Verify checks the snapshot and the real services through a native callback. The
// marker is committed only after the services confirm that existing state is usable.
func (m *Manager) Verify(ctx context.Context, probe func(context.Context) error) (Status, error) {
	m.mu.Lock()
	x, err := m.read()
	if err == nil && x.Phase == "rolled_back" {
		err = errors.New("prepare migration again before verifying")
	}
	if err == nil {
		err = m.verifyBackup(x)
	}
	m.mu.Unlock()
	if err != nil {
		return x.Status, err
	}
	if probe == nil {
		return x.Status, errors.New("migration requires service verification")
	}
	// The probe may consult migration status while connecting an old controller.
	// Never hold the manager lock while invoking native/service callbacks.
	if err = probe(ctx); err != nil {
		return x.Status, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	unlock, err := lockDirectory(m.Dir)
	if err != nil {
		return x.Status, err
	}
	defer unlock()
	current, err := m.read()
	if err != nil {
		return x.Status, err
	}
	if current.Backup != x.Backup || current.Phase == "rolled_back" {
		return current.Status, errors.New("migration changed during verification; retry")
	}
	if err = m.verifyBackup(current); err != nil {
		return current.Status, err
	}
	current.Phase = "verified"
	return current.Status, m.write(current)
}

// Rollback removes only adoption authority. Original data was never moved or
// rewritten, so restoring a snapshot over new work would be destructive and is avoided.
func (m *Manager) Rollback() (Status, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	unlock, err := lockDirectory(m.Dir)
	if err != nil {
		return Status{}, err
	}
	defer unlock()
	x, err := m.read()
	if err != nil {
		return Status{}, err
	}
	if err = m.verifyBackup(x); err != nil {
		return x.Status, err
	}
	x.Phase = "rolled_back"
	return x.Status, m.write(x)
}

func (m *Manager) write(x marker) error {
	b, err := json.MarshalIndent(x, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(m.Dir, ".marker-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	cerr := f.Close()
	if err != nil {
		return err
	}
	if cerr != nil {
		return cerr
	}
	if err = os.Rename(f.Name(), filepath.Join(m.Dir, "migration.json")); err != nil {
		return err
	}
	return syncDir(m.Dir)
}

func syncDir(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
