package server

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"werkbord/internal/sqlitekit"
	"werkbord/internal/team/config"
	"werkbord/internal/team/store"
	"werkbord/internal/team/store/replicated"
)

// The service protects its own data when it meets a new version.
//
// Whoever starts the service (the installer's maintenance, macOS's own launch of the app's service, a person running
// `werkbord-team serve`), the first time a version opens data that an older version last opened, the service
//
//  1. takes a backup of the workspace into the backup directory,
//  2. verifies it the way `werkbord-team storage verify-backup` does (it reads it back, checks its hash and integrity, and
//     brings a scratch copy to this version's schema, which proves this version can open it),
//  3. records the two versions in data-guard.json beside the data,
//  4. and only then opens the database, which is what migrates it.
//
// If the backup or its verification fails, the database is not opened and nothing in it is touched. The reason is written to
// data-guard.json and shown to the window in the device state (dataProtection, and error), and the next start tries again.
//
// The installer's maintenance (desktop/internal/teaminstall) already takes and verifies such a backup, before the old service
// is stopped. It leaves a hand-off note in the backup directory (upgrade-handoff-<slot>.json) naming it; the service accepts
// that backup if the note matches this upgrade and the file is intact, and does not make a second one.
//
// Nothing here starts a process or holds a credential: it reads and copies the workspace's own database.

const (
	guardFileName   = "data-guard.json"
	handoffPrefix   = "upgrade-handoff-"
	guardRetryAfter = 5 * time.Minute
)

// guardRecord is data-guard.json.
type guardRecord struct {
	// Version is the version that last opened the data.
	Version  string    `json:"version"`
	OpenedAt time.Time `json:"openedAt"`
	// From is the version that opened it before, when this one found it older; "unknown" when nothing recorded which.
	From string `json:"from,omitempty"`
	// Backup and BackupDir name the verified backup taken (or accepted) before this version opened the data.
	Backup    string `json:"backup,omitempty"`
	BackupDir string `json:"backupDir,omitempty"`
	// By says who took the backup: "service", or "maintenance" for the installer's hand-off.
	By string `json:"by,omitempty"`
	// Created marks data made by CreateStorage that no version has started yet: nothing older has opened it.
	Created bool `json:"created,omitempty"`
	// Refused is set while a version has been kept from opening the data because it could not be protected.
	Refused *guardRefusal `json:"refused,omitempty"`
}

type guardRefusal struct {
	Version string    `json:"version"`
	Reason  string    `json:"reason"`
	At      time.Time `json:"at"`
}

// handoff is what the installer's maintenance leaves for the service it is about to start.
type handoff struct {
	Slot    string `json:"slot"`
	From    string `json:"from"`
	To      string `json:"to"`
	Backup  string `json:"backup"`
	SHA256  string `json:"sha256"`
	Created string `json:"created,omitempty"`
}

// dataGuard protects one workspace's data for one version of the service.
type dataGuard struct {
	cfg     config.Config
	slot    string
	version string
	log     *slog.Logger
	now     func() time.Time
}

// newDataGuard returns nil, which guards nothing, for a build that does not know its version (tests, and `go run`).
func newDataGuard(cfg config.Config, slot, version string, log *slog.Logger) *dataGuard {
	if strings.TrimSpace(version) == "" {
		return nil
	}
	if slot == "" {
		slot = MainSlot
	}
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &dataGuard{cfg: cfg, slot: slot, version: strings.TrimSpace(version), log: log.With("component", "dataguard"), now: time.Now}
}

// pending says whether this version still has to protect the data: it has not been recorded as the one that opened it.
func (g *dataGuard) pending() bool {
	if g == nil {
		return false
	}
	rec, have := g.read()
	return !(have && rec.Refused == nil && sameVersion(rec.Version, g.version))
}

func (g *dataGuard) path() string { return filepath.Join(g.cfg.DataDir, guardFileName) }

// backupDir is where the service's backups go: the configured directory (the desktop service always has one), or backups
// inside the data directory for a `serve` that was given none.
func (g *dataGuard) backupDir() string {
	if g.cfg.BackupDir != "" {
		return g.cfg.BackupDir
	}
	return filepath.Join(g.cfg.DataDir, "backups")
}

func (g *dataGuard) read() (guardRecord, bool) {
	return readGuardRecord(g.cfg.DataDir)
}

func readGuardRecord(dataDir string) (guardRecord, bool) {
	b, err := os.ReadFile(filepath.Join(dataDir, guardFileName))
	if err != nil {
		return guardRecord{}, false
	}
	var rec guardRecord
	if json.Unmarshal(b, &rec) != nil {
		return guardRecord{}, false // unreadable counts as unknown, which is backed up
	}
	return rec, true
}

func (g *dataGuard) write(rec guardRecord) error {
	b, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	return writeDaemonFile(g.path(), append(b, '\n'))
}

// holdsDatabase says whether the workspace in dir keeps a database copy on this computer. A device that only joined a team
// keeps its identity and nothing else: the Workspace Hosts hold the data. It errs towards yes, like the installer's check.
func holdsDatabase(dir string) bool {
	for _, name := range []string{"storage.json", "storage", "team.db", "team.db-wal", "team.db-shm"} {
		if _, err := os.Lstat(filepath.Join(dir, name)); !errors.Is(err, os.ErrNotExist) {
			return true
		}
	}
	return false
}

// GuardView is what the window may know of the guard: absent when nothing was ever recorded for this workspace.
func guardView(dataDir string) map[string]any {
	rec, ok := readGuardRecord(dataDir)
	if !ok || (rec.Version == "" && rec.Refused == nil) {
		return nil // nothing recorded yet, or a workspace made and not yet started
	}
	out := map[string]any{"version": rec.Version, "openedAt": rec.OpenedAt.UnixMilli()}
	switch {
	case rec.Refused != nil:
		out["state"] = "refused"
		out["refusedVersion"] = rec.Refused.Version
		out["reason"] = rec.Refused.Reason
		out["at"] = rec.Refused.At.UnixMilli()
		out["dataUntouched"] = true
	default:
		out["state"] = "protected"
	}
	if rec.From != "" {
		out["from"] = rec.From
	}
	if rec.Backup != "" {
		out["backup"] = rec.Backup
		out["backupDir"] = rec.BackupDir
		out["backupBy"] = rec.By
	}
	return out
}

// guardRefusals remembers, for this process, a refusal and when it was made, so a service that is started again every few
// seconds does not take (and throw away) a backup every few seconds. A new process always tries again.
var guardRefusals sync.Map // data directory -> refusalNote

type refusalNote struct {
	version string
	err     error
	at      time.Time
}

// ErrDataNotProtected is what opening the workspace returns when its data could not be backed up first.
var ErrDataNotProtected = errors.New("the workspace's data was not opened")

// take makes a verified backup in dir and returns its name. It is the part that differs between a file and a cluster.
type takeBackup func(ctx context.Context, dir string) (name string, err error)

var versionPattern = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)(?:-preview\.(\d+))?$`)

// versionOrder compares two release versions: -1, 0 or 1. ok is false when either is not a plain release or preview, such
// as a build made from source between releases.
func versionOrder(a, b string) (order int, ok bool) {
	pa, pb := versionPattern.FindStringSubmatch(strings.TrimSpace(a)), versionPattern.FindStringSubmatch(strings.TrimSpace(b))
	if pa == nil || pb == nil {
		return 0, false
	}
	num := func(s string) int { n, _ := strconv.Atoi(s); return n }
	for i := 1; i <= 3; i++ {
		if x, y := num(pa[i]), num(pb[i]); x != y {
			if x < y {
				return -1, true
			}
			return 1, true
		}
	}
	// A preview comes before the release it leads to.
	switch {
	case pa[4] == pb[4]:
		return 0, true
	case pa[4] == "":
		return 1, true
	case pb[4] == "":
		return -1, true
	}
	if num(pa[4]) < num(pb[4]) {
		return -1, true
	}
	return 1, true
}

func sameVersion(a, b string) bool {
	return strings.TrimPrefix(strings.TrimSpace(a), "v") == strings.TrimPrefix(strings.TrimSpace(b), "v")
}

// protect is the guard: it returns nil when the data may be opened. first says the data did not exist before this start, so
// there is nothing older to protect it from. take makes the backup when one is needed.
func (g *dataGuard) protect(ctx context.Context, first bool, take takeBackup) error {
	rec, have := g.read()
	if have && rec.Refused == nil && rec.Version != "" && sameVersion(rec.Version, g.version) {
		return nil
	}
	if have && rec.Version != "" {
		if order, ok := versionOrder(rec.Version, g.version); ok && order > 0 {
			// Data last opened by a newer version: nothing older opened it. The database itself refuses a schema it does not
			// know; this only records who has it now.
			return g.record(guardRecord{Version: g.version, OpenedAt: g.now().UTC(), From: rec.Version})
		}
	}
	if have && rec.Created && rec.Version == "" {
		first, have = true, false // made by this build lineage and not started yet
	}
	if first && !have {
		return g.record(guardRecord{Version: g.version, OpenedAt: g.now().UTC()})
	}
	from := "unknown"
	if have && rec.Version != "" {
		from = rec.Version
	}
	key := g.cfg.DataDir
	if v, ok := guardRefusals.Load(key); ok {
		if n := v.(refusalNote); n.version == g.version && g.now().Sub(n.at) < guardRetryAfter {
			return n.err
		}
	}

	name, dir, by, err := g.acceptHandoff(from, have)
	if err != nil || name == "" {
		name, dir, by = "", g.backupDir(), "service"
		if name, err = g.makeBackup(ctx, dir, take); err != nil {
			return g.refuse(rec, have, from, err)
		}
	}
	g.log.Info("the workspace's data was backed up before this version opened it", "from", from, "to", g.version, "backup", name, "dir", dir, "by", by)
	guardRefusals.Delete(key)
	return g.record(guardRecord{Version: g.version, OpenedAt: g.now().UTC(), From: from, Backup: name, BackupDir: dir, By: by})
}

func (g *dataGuard) record(rec guardRecord) error {
	if err := g.write(rec); err != nil {
		// The backup exists, but the record that lets this version open the data cannot be kept: opening it again on every start
		// would back it up again, so say so rather than go on.
		return g.refuseWith(fmt.Errorf("the versions could not be recorded in %s: %w", g.path(), err))
	}
	return nil
}

func (g *dataGuard) makeBackup(ctx context.Context, dir string, take takeBackup) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("the backup directory %s cannot be used: %w", dir, err)
	}
	name, err := take(ctx, dir)
	if err != nil {
		return "", err
	}
	if name == "" {
		return "", errors.New("no backup was made")
	}
	return name, nil
}

func (g *dataGuard) refuse(prev guardRecord, have bool, from string, cause error) error {
	err := g.refuseWith(fmt.Errorf("Werkbord Team %s found this workspace's data last opened by %s and could not make a verified backup first (%w); it did not open the data and changed nothing in it. Free space or fix the backup directory %s and start the service again", g.version, from, cause, g.backupDir()))
	rec := prev
	if !have {
		rec = guardRecord{}
	}
	rec.Refused = &guardRefusal{Version: g.version, Reason: err.Error(), At: g.now().UTC()}
	if werr := g.write(rec); werr != nil {
		g.log.Error("the refusal could not be recorded", "err", werr)
	}
	return err
}

// refuseWith remembers the refusal for this process and wraps it so callers can recognise it.
func (g *dataGuard) refuseWith(err error) error {
	wrapped := fmt.Errorf("%w: %w", ErrDataNotProtected, err)
	guardRefusals.Store(g.cfg.DataDir, refusalNote{version: g.version, err: wrapped, at: g.now()})
	g.log.Error("the workspace's data was not opened", "err", err)
	return wrapped
}

// acceptHandoff takes the installer's backup for this upgrade instead of making another, if the note matches and the file is
// intact. It returns an empty name when there is no such backup.
func (g *dataGuard) acceptHandoff(from string, haveRecord bool) (name, dir, by string, err error) {
	dir = g.backupDir()
	path := filepath.Join(dir, handoffPrefix+g.slot+".json")
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", "", "", nil
	}
	var h handoff
	if json.Unmarshal(raw, &h) != nil || h.Slot != g.slot || !sameVersion(h.To, g.version) || h.Backup != filepath.Base(h.Backup) || !strings.HasSuffix(h.Backup, ".sqlite") {
		return "", "", "", nil
	}
	if haveRecord && !sameVersion(h.From, from) {
		return "", "", "", nil // it was made for another upgrade
	}
	sum, err := fileSHA256(filepath.Join(dir, h.Backup))
	if err != nil || h.SHA256 == "" || sum != h.SHA256 {
		return "", "", "", nil
	}
	_ = os.Remove(path)
	return h.Backup, dir, "maintenance", nil
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// ---- a workspace in one file ----

// protectFile guards a workspace kept in one SQLite file, before the file is opened (which migrates it).
func (g *dataGuard) protectFile(ctx context.Context) error {
	if g == nil {
		return nil
	}
	if _, err := os.Stat(g.cfg.DBPath()); errors.Is(err, os.ErrNotExist) {
		return nil // no database here: a first start, or a computer that only joined a team
	}
	return g.protect(ctx, false, func(ctx context.Context, dir string) (string, error) {
		return backupDatabaseFile(ctx, g.cfg.DBPath(), dir, g.now(), g.log)
	})
}

// backupDatabaseFile copies the database with VACUUM INTO (a consistent copy), writes a record beside it, and verifies it.
func backupDatabaseFile(ctx context.Context, dbPath, dir string, now time.Time, log *slog.Logger) (string, error) {
	src, err := sql.Open("sqlite", "file:"+filepath.ToSlash(dbPath)+"?mode=ro&_pragma=busy_timeout(5000)")
	if err != nil {
		return "", err
	}
	defer src.Close()
	src.SetMaxOpenConns(1)
	schema := 0
	if v, err := sqlitekit.SchemaVersion(ctx, src); err == nil {
		schema = v
	}
	name := fmt.Sprintf("workspace-%s-file.sqlite", now.UTC().Format("20060102T150405Z"))
	final := filepath.Join(dir, name)
	tmp := filepath.Join(dir, ".partial-"+name)
	_ = os.Remove(tmp)
	if _, err := src.ExecContext(ctx, `VACUUM INTO ?`, tmp); err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("copying the database: %w", err)
	}
	defer os.Remove(tmp)
	if err := os.Chmod(tmp, 0o600); err != nil {
		return "", err
	}
	sum, err := fileSHA256(tmp)
	if err != nil {
		return "", err
	}
	fi, err := os.Stat(tmp)
	if err != nil {
		return "", err
	}
	if err := os.Rename(tmp, final); err != nil {
		return "", err
	}
	info := replicated.BackupInfo{Name: name, CreatedAt: now.UTC(), SchemaVersion: schema, SHA256: sum, Size: fi.Size(), Host: "single-file"}
	meta, _ := json.MarshalIndent(info, "", "  ")
	if err := writeDaemonFile(final+".json", meta); err != nil {
		_ = os.Remove(final)
		return "", err
	}
	if err := verifyDatabaseFileBackup(ctx, dir, name, log); err != nil {
		_ = os.Remove(final)
		_ = os.Remove(final + ".json")
		return "", fmt.Errorf("the backup was written but did not verify, and was removed: %w", err)
	}
	return name, nil
}

// verifyDatabaseFileBackup is verify-backup for a single-file workspace: the copy matches its record, passes SQLite's own
// checks, and a scratch copy of it opens with this version, which migrates it to this version's schema.
func verifyDatabaseFileBackup(ctx context.Context, dir, name string, log *slog.Logger) error {
	raw, err := os.ReadFile(filepath.Join(dir, name+".json"))
	if err != nil {
		return fmt.Errorf("the backup has no record beside it: %w", err)
	}
	var rec replicated.BackupInfo
	if json.Unmarshal(raw, &rec) != nil || len(rec.SHA256) != 64 {
		return errors.New("the backup's record is unreadable")
	}
	sum, err := fileSHA256(filepath.Join(dir, name))
	if err != nil {
		return err
	}
	if sum != rec.SHA256 {
		return errors.New("the backup does not match its recorded hash")
	}
	scratch, err := os.MkdirTemp(dir, ".verify-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(scratch)
	work := filepath.Join(scratch, "team.db")
	in, err := os.Open(filepath.Join(dir, name))
	if err != nil {
		return err
	}
	out, err := os.OpenFile(work, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		in.Close()
		return err
	}
	_, cerr := io.Copy(out, in)
	in.Close()
	if err := errors.Join(cerr, out.Close()); err != nil {
		return err
	}
	ms, err := store.Migrations()
	if err != nil {
		return err
	}
	info, err := sqlitekit.Inspect(ctx, work, len(ms))
	if err != nil {
		return err
	}
	if info.Integrity != "ok" {
		return fmt.Errorf("SQLite's integrity check failed: %s", info.Integrity)
	}
	if info.NewerThanBuild {
		return fmt.Errorf("the database is at schema version %d, newer than the %d this version knows", info.Version, len(ms))
	}
	db, err := store.Open(ctx, work, log)
	if err != nil {
		return fmt.Errorf("this version cannot open the backup: %w", err)
	}
	if err := db.Ping(ctx); err != nil {
		_ = db.Close()
		return err
	}
	if err := db.Close(); err != nil {
		return err
	}
	chk, err := sql.Open("sqlite", "file:"+filepath.ToSlash(work)+"?mode=ro")
	if err != nil {
		return err
	}
	defer chk.Close()
	rows, err := chk.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return err
	}
	broken := rows.Next()
	_ = rows.Close()
	if broken {
		return errors.New("the backup has rows that point at rows that do not exist")
	}
	return nil
}

// ---- a workspace in a cluster ----

// beforeMigrate is the hook the replicated store calls before it changes the cluster's schema. first is true when this host
// holds no copy of the data yet and has no record: there is nothing older to protect it from.
func (g *dataGuard) beforeMigrate(first bool) func(context.Context, *replicated.Store) error {
	if g == nil {
		return nil
	}
	return func(ctx context.Context, s *replicated.Store) error {
		return g.protect(ctx, first, func(ctx context.Context, dir string) (string, error) {
			info, err := s.Backup(ctx, replicated.DirDestination{Dir: dir})
			return info.Name, err
		})
	}
}

// nonEmptyDir says whether dir exists and holds anything.
func nonEmptyDir(dir string) bool {
	es, err := os.ReadDir(dir)
	return err == nil && len(es) > 0
}
