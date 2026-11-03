package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"werkbord/internal/team/config"
	"werkbord/internal/team/store"
)

// guardCfg is a data directory with a backup directory beside it.
func guardCfg(t *testing.T) config.Config {
	t.Helper()
	root := t.TempDir()
	cfg := config.Default()
	cfg.DataDir = filepath.Join(root, "workspace")
	cfg.BackupDir = filepath.Join(root, "backups")
	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	return cfg
}

// singleFileWorkspace makes a real SQLite workspace file at the current schema, with one row to look for in a backup.
func singleFileWorkspace(t *testing.T, cfg config.Config) {
	t.Helper()
	db, err := store.Open(bg, cfg.DBPath(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := sql.Open("sqlite", "file:"+cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	for _, q := range []string{`CREATE TABLE guard_probe (v TEXT)`, `INSERT INTO guard_probe VALUES ('kept')`, `PRAGMA wal_checkpoint(TRUNCATE)`} {
		if _, err := raw.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
}

func recordVersion(t *testing.T, cfg config.Config, version string) {
	t.Helper()
	g := newDataGuard(cfg, "", "x", nil)
	if err := g.write(guardRecord{Version: version, OpenedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
}

func hashOf(t *testing.T, path string) string {
	t.Helper()
	h, err := fileSHA256(path)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func backupsIn(t *testing.T, dir string) []string {
	t.Helper()
	es, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range es {
		if strings.HasPrefix(e.Name(), "workspace-") && strings.HasSuffix(e.Name(), ".sqlite") {
			out = append(out, e.Name())
		}
	}
	return out
}

func TestAFirstStartHasNothingToBackUpAndRecordsItsVersion(t *testing.T) {
	cfg := guardCfg(t)
	g := newDataGuard(cfg, "", "5.5.0", nil)
	// No database yet: a single-file start writes nothing at all.
	if err := g.protectFile(bg); err != nil {
		t.Fatal(err)
	}
	if _, ok := g.read(); ok || len(backupsIn(t, cfg.BackupDir)) != 0 {
		t.Fatal("something was recorded or backed up for data that does not exist")
	}
	// A cluster's first start (a new host) records the version and takes no backup.
	called := false
	err := g.protect(bg, true, func(context.Context, string) (string, error) { called = true; return "x", nil })
	if err != nil || called {
		t.Fatalf("a first start asked for a backup: %v %v", called, err)
	}
	if rec, ok := g.read(); !ok || rec.Version != "5.5.0" || rec.Backup != "" {
		t.Fatalf("%+v %v", rec, ok)
	}
}

func TestTheSameVersionStartsWithoutABackup(t *testing.T) {
	cfg := guardCfg(t)
	singleFileWorkspace(t, cfg)
	recordVersion(t, cfg, "5.5.0")
	before := hashOf(t, cfg.DBPath())
	if err := newDataGuard(cfg, "", "v5.5.0", nil).protectFile(bg); err != nil {
		t.Fatal(err)
	}
	if len(backupsIn(t, cfg.BackupDir)) != 0 || hashOf(t, cfg.DBPath()) != before {
		t.Fatal("a restart of the same version made a backup or touched the data")
	}
}

func TestAnUpgradeBacksUpAndVerifiesBeforeTheDatabaseIsOpened(t *testing.T) {
	cfg := guardCfg(t)
	singleFileWorkspace(t, cfg)
	recordVersion(t, cfg, "5.4.0")
	before := hashOf(t, cfg.DBPath())

	g := newDataGuard(cfg, "", "5.5.0", nil)
	if err := g.protectFile(bg); err != nil {
		t.Fatal(err)
	}
	names := backupsIn(t, cfg.BackupDir)
	if len(names) != 1 {
		t.Fatalf("backups: %v", names)
	}
	// The backup is a whole copy, with a record beside it that matches it, and it verifies again.
	if err := verifyDatabaseFileBackup(bg, cfg.BackupDir, names[0], nil); err != nil {
		t.Fatal(err)
	}
	raw, err := sql.Open("sqlite", "file:"+filepath.Join(cfg.BackupDir, names[0])+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	var v string
	err = raw.QueryRow(`SELECT v FROM guard_probe`).Scan(&v)
	raw.Close()
	if err != nil || v != "kept" {
		t.Fatalf("the backup does not hold the data: %q %v", v, err)
	}
	if fi, err := os.Stat(filepath.Join(cfg.BackupDir, names[0])); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("%v %v", fi, err)
	}
	// The versions are recorded, and the window can read it.
	rec, _ := g.read()
	if rec.Version != "5.5.0" || rec.From != "5.4.0" || rec.Backup != names[0] || rec.By != "service" {
		t.Fatalf("%+v", rec)
	}
	if view := guardView(cfg.DataDir); view["state"] != "protected" || view["from"] != "5.4.0" || view["backup"] != names[0] {
		t.Fatalf("%v", view)
	}
	// The database itself was not touched by taking the backup.
	if hashOf(t, cfg.DBPath()) != before {
		t.Fatal("the guard changed the database")
	}
	// And the next start of this version does nothing more.
	if err := g.protectFile(bg); err != nil || len(backupsIn(t, cfg.BackupDir)) != 1 {
		t.Fatalf("a second start backed up again: %v", err)
	}
}

func TestDataFromBeforeTheRecordWasKeptIsBackedUpAsOfAnUnknownVersion(t *testing.T) {
	cfg := guardCfg(t)
	singleFileWorkspace(t, cfg)
	if err := newDataGuard(cfg, "", "5.5.0", nil).protectFile(bg); err != nil {
		t.Fatal(err)
	}
	rec, _ := readGuardRecord(cfg.DataDir)
	if rec.From != "unknown" || rec.Backup == "" || len(backupsIn(t, cfg.BackupDir)) != 1 {
		t.Fatalf("%+v", rec)
	}
}

func TestAnUpgradeWhoseBackupDoesNotVerifyRefusesToOpenAndLeavesTheDataUntouched(t *testing.T) {
	cfg := guardCfg(t)
	singleFileWorkspace(t, cfg)
	// A database from a schema this version does not know: its backup cannot be opened by this version.
	raw, err := sql.Open("sqlite", "file:"+cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`INSERT INTO schema_migrations (version, name, applied_at) VALUES (9999, 'from_the_future', 0)`); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		t.Fatal(err)
	}
	raw.Close()
	recordVersion(t, cfg, "5.4.0")
	before := hashOf(t, cfg.DBPath())

	st, err := openStorage(bg, cfg, nil, newDataGuard(cfg, "", "5.5.0", nil))
	if err == nil {
		_ = st.Close(bg)
		t.Fatal("the database was opened although its backup did not verify")
	}
	if !errors.Is(err, ErrDataNotProtected) || !strings.Contains(err.Error(), "did not open the data") {
		t.Fatalf("%v", err)
	}
	if hashOf(t, cfg.DBPath()) != before {
		t.Fatal("the data was changed")
	}
	if len(backupsIn(t, cfg.BackupDir)) != 0 {
		t.Fatal("a backup that did not verify was kept")
	}
	// Reading it leaves SQLite's empty -wal beside it, as any open of a WAL database does; nothing was written to either.
	if fi, err := os.Stat(cfg.DBPath() + "-wal"); err == nil && fi.Size() != 0 {
		t.Fatal("the database was opened for writing")
	}
	// The window reads why, and the old version is still the one on record.
	view := guardView(cfg.DataDir)
	if view["state"] != "refused" || view["dataUntouched"] != true || view["version"] != "5.4.0" || view["refusedVersion"] != "5.5.0" || !strings.Contains(view["reason"].(string), "newer than") {
		t.Fatalf("%v", view)
	}
}

func TestARefusalIsNotRetriedEverySecondAndIsTriedAgainByANewProcess(t *testing.T) {
	cfg := guardCfg(t)
	singleFileWorkspace(t, cfg)
	recordVersion(t, cfg, "5.4.0")
	g := newDataGuard(cfg, "", "5.5.0", nil)
	calls := 0
	failing := func(context.Context, string) (string, error) { calls++; return "", errors.New("disk full") }
	if err := g.protect(bg, false, failing); !errors.Is(err, ErrDataNotProtected) || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("%v", err)
	}
	if err := g.protect(bg, false, failing); !errors.Is(err, ErrDataNotProtected) || calls != 1 {
		t.Fatalf("the backup was tried again at once (%d)", calls)
	}
	// After the retry window, and in a new process, it is tried again, and success clears the refusal.
	g.now = func() time.Time { return time.Now().Add(guardRetryAfter + time.Second) }
	if err := g.protect(bg, false, func(context.Context, string) (string, error) { return "workspace-ok.sqlite", nil }); err != nil {
		t.Fatal(err)
	}
	rec, _ := g.read()
	if rec.Refused != nil || rec.Version != "5.5.0" || rec.Backup != "workspace-ok.sqlite" {
		t.Fatalf("%+v", rec)
	}
	if guardView(cfg.DataDir)["state"] != "protected" {
		t.Fatal("the window still shows a refusal")
	}
}

func TestABackupDirectoryThatCannotBeUsedRefuses(t *testing.T) {
	cfg := guardCfg(t)
	singleFileWorkspace(t, cfg)
	recordVersion(t, cfg, "5.4.0")
	if err := os.WriteFile(cfg.BackupDir, []byte("a file, not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	before := hashOf(t, cfg.DBPath())
	_, err := openStorage(bg, cfg, nil, newDataGuard(cfg, "", "5.5.0", nil))
	if !errors.Is(err, ErrDataNotProtected) || !strings.Contains(err.Error(), "backup directory") {
		t.Fatalf("%v", err)
	}
	if hashOf(t, cfg.DBPath()) != before {
		t.Fatal("the data was changed")
	}
}

func TestAJoinedMemberHasNoDatabaseAndIsNeverAskedForABackup(t *testing.T) {
	cfg := guardCfg(t)
	// What a member keeps: its identity, none of the places a host keeps data.
	for _, d := range []string{"pki", "network"} {
		if err := os.MkdirAll(filepath.Join(cfg.DataDir, d), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if holdsDatabase(cfg.DataDir) {
		t.Fatal("a joined member was taken for a host")
	}
	g := newDataGuard(cfg, "", "5.5.0", nil)
	if err := g.protectFile(bg); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(cfg.BackupDir); err == nil {
		t.Fatal("a backup directory was made for a computer with no database")
	}
	if view := guardView(cfg.DataDir); view != nil {
		t.Fatalf("a member shows a data record: %v", view)
	}
	for _, name := range []string{"storage.json", "storage", "team.db"} {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, name), []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
		if !holdsDatabase(dir) {
			t.Fatalf("%s did not count as a database", name)
		}
	}
}

func TestTheBackupMaintenanceTookIsAcceptedAndNotMadeAgain(t *testing.T) {
	cfg := guardCfg(t)
	singleFileWorkspace(t, cfg)
	recordVersion(t, cfg, "5.4.0")
	// What the installer does: a verified backup in the backup directory, and a note about it.
	if err := os.MkdirAll(cfg.BackupDir, 0o700); err != nil {
		t.Fatal(err)
	}
	name, err := backupDatabaseFile(bg, cfg.DBPath(), cfg.BackupDir, time.Now().Add(-time.Minute), nil)
	if err != nil {
		t.Fatal(err)
	}
	note := func(slot, from, to, sha string) {
		b, _ := json.Marshal(handoff{Slot: slot, From: from, To: to, Backup: name, SHA256: sha})
		if err := os.WriteFile(filepath.Join(cfg.BackupDir, handoffPrefix+slot+".json"), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	sum := hashOf(t, filepath.Join(cfg.BackupDir, name))

	// A note for another version, another slot, or with a hash that does not match is not trusted: a backup is made.
	for _, bad := range []struct{ slot, from, to, sha string }{
		{"main", "5.4.0", "5.6.0", sum}, {"main", "5.3.0", "5.5.0", sum}, {"main", "5.4.0", "5.5.0", strings.Repeat("0", 64)}, {"other", "5.4.0", "5.5.0", sum},
	} {
		note(bad.slot, bad.from, bad.to, bad.sha)
		calls := 0
		g := newDataGuard(cfg, "", "5.5.0", nil)
		recordVersion(t, cfg, "5.4.0")
		err := g.protect(bg, false, func(context.Context, string) (string, error) { calls++; return "workspace-own.sqlite", nil })
		if err != nil || calls != 1 {
			t.Fatalf("%+v: %v (%d)", bad, err, calls)
		}
		_ = os.Remove(filepath.Join(cfg.BackupDir, handoffPrefix+bad.slot+".json"))
	}

	recordVersion(t, cfg, "5.4.0")
	note("main", "v5.4.0", "5.5.0", sum)
	g := newDataGuard(cfg, "main", "5.5.0", nil)
	if err := g.protect(bg, false, func(context.Context, string) (string, error) { t.Fatal("the work was done twice"); return "", nil }); err != nil {
		t.Fatal(err)
	}
	rec, _ := g.read()
	if rec.Backup != name || rec.By != "maintenance" || rec.From != "5.4.0" || rec.Version != "5.5.0" {
		t.Fatalf("%+v", rec)
	}
	if _, err := os.Stat(filepath.Join(cfg.BackupDir, handoffPrefix+"main.json")); err == nil {
		t.Fatal("the note was left to be used again")
	}
}

func TestOnlyAnOlderVersionTriggersABackup(t *testing.T) {
	for _, c := range []struct {
		recorded, now string
		backup        bool
	}{
		{"5.4.0", "5.5.0", true},
		{"5.5.2-preview.1", "5.5.2-preview.2", true},
		{"5.5.2-preview.2", "5.5.2", true},
		{"5.5.0", "v5.5.0", false},
		{"5.5.0", "5.4.0", false}, // a downgrade: nothing older opened the data
		{"5.5.2", "5.5.2-preview.1", false},
		{"5.5.0-preview.1-3-g1a2b3c4", "5.5.0-preview.1-4-g9f9f9f9", true}, // built from source: cannot be ordered, so be safe
		{"dev", "5.5.0", true},
	} {
		cfg := guardCfg(t)
		recordVersion(t, cfg, c.recorded)
		calls := 0
		err := newDataGuard(cfg, "", c.now, nil).protect(bg, false, func(context.Context, string) (string, error) { calls++; return "workspace-x.sqlite", nil })
		if err != nil || (calls == 1) != c.backup {
			t.Errorf("%s -> %s: backup=%v want %v (%v)", c.recorded, c.now, calls == 1, c.backup, err)
		}
		if rec, _ := readGuardRecord(cfg.DataDir); !sameVersion(rec.Version, c.now) {
			t.Errorf("%s -> %s: recorded %q", c.recorded, c.now, rec.Version)
		}
	}
}

func TestAnUnstampedBuildGuardsNothing(t *testing.T) {
	if newDataGuard(guardCfg(t), "", "", nil) != nil {
		t.Fatal("a build with no version made a guard")
	}
	var g *dataGuard
	if err := g.protectFile(bg); err != nil || g.beforeMigrate(false) != nil {
		t.Fatal("a nil guard did something")
	}
}

func TestTheWindowReadsAGuardRefusalFromTheDeviceState(t *testing.T) {
	d := testDaemon(t)
	cfg := d.workspaceConfig()
	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	var st map[string]any
	read := func() {
		w := daemonCall(d, "GET", "/api/device/v1/state", "")
		if w.Code != 200 {
			t.Fatal(w.Body.String())
		}
		st = map[string]any{}
		if err := json.Unmarshal(w.Body.Bytes(), &st); err != nil {
			t.Fatal(err)
		}
	}
	read()
	if _, ok := st["dataProtection"]; ok {
		t.Fatal("a computer with no record shows one")
	}
	g := newDataGuard(cfg, "", "5.5.0", nil)
	recordVersion(t, cfg, "5.4.0")
	if err := g.protect(bg, false, func(context.Context, string) (string, error) { return "", errors.New("the backup directory is full") }); err == nil {
		t.Fatal("expected a refusal")
	}
	read()
	dp, _ := st["dataProtection"].(map[string]any)
	if dp["state"] != "refused" || !strings.Contains(dp["reason"].(string), "the backup directory is full") || dp["dataUntouched"] != true {
		t.Fatalf("%v", st)
	}
	if e, _ := st["error"].(string); !strings.Contains(e, "the backup directory is full") {
		t.Fatalf("the window's error does not say why: %q", e)
	}
	if err := g.protect(bg, false, func(context.Context, string) (string, error) { return "workspace-ok.sqlite", nil }); err == nil {
		// still inside the retry window for this process: the refusal stands
		t.Fatal("a refusal was retried at once")
	}
	g.now = func() time.Time { return time.Now().Add(guardRetryAfter + time.Second) }
	if err := g.protect(bg, false, func(context.Context, string) (string, error) { return "workspace-ok.sqlite", nil }); err != nil {
		t.Fatal(err)
	}
	read()
	if dp, _ := st["dataProtection"].(map[string]any); dp["state"] != "protected" || dp["backup"] != "workspace-ok.sqlite" {
		t.Fatalf("%v", st)
	}
	if e, _ := st["error"].(string); e != "" {
		t.Fatalf("an old refusal is still the error: %q", e)
	}
}

func TestAWorkspaceMadeByThisBuildIsNotBackedUpAtItsFirstStart(t *testing.T) {
	cfg := guardCfg(t)
	singleFileWorkspace(t, cfg)
	if err := (&dataGuard{cfg: cfg}).write(guardRecord{Created: true, OpenedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if guardView(cfg.DataDir) != nil {
		t.Fatal("a workspace that has not started shows a data record")
	}
	if err := newDataGuard(cfg, "", "5.5.0", nil).protectFile(bg); err != nil {
		t.Fatal(err)
	}
	rec, _ := readGuardRecord(cfg.DataDir)
	if rec.Version != "5.5.0" || rec.Backup != "" || len(backupsIn(t, cfg.BackupDir)) != 0 {
		t.Fatalf("%+v", rec)
	}
}
