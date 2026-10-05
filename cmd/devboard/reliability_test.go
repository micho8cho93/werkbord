package main

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTokenRotationTakesEffectWithoutRestart(t *testing.T) {
	e := newTestEnv(t)
	if err := e.app.cmdSetup(bg, []string{"--no-network", "--no-open"}); err != nil {
		t.Fatal(err)
	}
	old, err := e.app.client()
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := cmdToken(e.app.cfg, []string{"--rotate"}, &out, &out); err != nil {
		t.Fatal(err)
	}
	fresh, err := e.app.client()
	if err != nil {
		t.Fatal(err)
	}
	if old.token == fresh.token || len(fresh.token) != 64 {
		t.Fatal("credential did not rotate")
	}
	if err := old.do(bg, "GET", "/api/projects", nil, nil); err == nil {
		t.Fatal("old credential authenticated")
	}
	if err := fresh.do(bg, "GET", "/api/projects", nil, nil); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(e.app.cfg.TokenPath())
	if info.Mode().Perm() != 0600 {
		t.Fatal(info.Mode())
	}
}

func TestUpdateRestoresDatabaseAsWellAsBinary(t *testing.T) {
	e := newTestEnv(t)
	if err := e.app.cmdSetup(bg, []string{"--no-network", "--no-open"}); err != nil {
		t.Fatal(err)
	}
	setVersion(t, "v1.0.0")
	publishRelease(t, "v1.1.0", false)
	bin, _ := e.app.executable()
	mutated := false
	e.mgr.startCheck = func() error {
		if strings.Contains(b(bin), "v1.1.0") && !mutated {
			mutated = true
			db, err := sql.Open("sqlite", e.app.cfg.DBPath())
			if err != nil {
				return err
			}
			defer db.Close()
			if _, err = db.Exec(`INSERT INTO schema_migrations (version, name, applied_at) VALUES (999, 'simulated future migration', 0)`); err != nil {
				return err
			}
			return fmt.Errorf("migration succeeded but new controller failed")
		}
		return nil
	}
	err := e.app.cmdUpdate(bg, nil)
	if err == nil || !strings.Contains(err.Error(), "running again") || !e.running() {
		t.Fatalf("rollback: %v", err)
	}
	files, _ := filepath.Glob(filepath.Join(e.app.cfg.DataDir, "backups", "failed-update-*", "devboard.db"))
	if len(files) != 1 {
		t.Fatalf("failed database was not retained: %v", files)
	}
}

func TestInterruptionRequiresExplicitForceForUncertainRunnerWork(t *testing.T) {
	e := newTestEnv(t)
	dir := filepath.Join(runnerDir(e.app.cfg), "runs")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "run.json")
	if err := os.WriteFile(file, []byte(`{"phase":"uncertain"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := e.app.checkInterruption(bg, false); err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("uncertain work silently interrupted: %v", err)
	}
	if err := e.app.checkInterruption(bg, true); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(`{"phase":"ended"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := e.app.checkInterruption(bg, false); err != nil {
		t.Fatal(err)
	}
}
func TestInvalidRestoreLeavesControllerRunning(t *testing.T) {
	e := newTestEnv(t)
	if err := e.app.cmdSetup(bg, []string{"--no-network", "--no-open"}); err != nil {
		t.Fatal(err)
	}
	if err := e.app.cmdDB(bg, []string{"restore", filepath.Join(t.TempDir(), "missing.db")}); err == nil {
		t.Fatal("missing backup accepted")
	}
	if !e.running() {
		t.Fatal("invalid restore stopped the controller")
	}
}

func TestLatestRestoreRejectsAPositionalBackup(t *testing.T) {
	e := newTestEnv(t)
	if err := e.app.cmdSetup(bg, []string{"--no-network", "--no-open"}); err != nil {
		t.Fatal(err)
	}
	if err := e.app.cmdDB(bg, []string{"restore", "--latest", "backup.db"}); err == nil || !strings.Contains(err.Error(), "cannot be combined") {
		t.Fatalf("ambiguous restore accepted: %v", err)
	}
	if !e.running() {
		t.Fatal("invalid restore stopped the controller")
	}
}

func TestNewestCompatibleBackupSkipsDisappearedAndInvalidFiles(t *testing.T) {
	dir := t.TempDir()
	old, newest := filepath.Join(dir, "old.db"), filepath.Join(dir, "newest.db")
	damaged, future := filepath.Join(dir, "damaged.db"), filepath.Join(dir, "future.db")
	for i, path := range []string{old, newest, future} {
		db, err := sql.Open("sqlite", path)
		if err != nil {
			t.Fatal(err)
		}
		v := 0
		if i == 2 {
			v = 999
		}
		_, err = db.Exec("CREATE TABLE schema_migrations (version INTEGER); INSERT INTO schema_migrations VALUES (?)", v)
		if closeErr := db.Close(); err != nil || closeErr != nil {
			t.Fatalf("create backup: %v %v", err, closeErr)
		}
	}
	if err := os.WriteFile(damaged, []byte("not a database"), 0600); err != nil {
		t.Fatal(err)
	}
	for i, path := range []string{old, newest, damaged, future} {
		at := time.Now().Add(time.Duration(i) * time.Hour)
		if err := os.Chtimes(path, at, at); err != nil {
			t.Fatal(err)
		}
	}
	vanished := filepath.Join(dir, "vanished.db")
	if err := os.WriteFile(vanished, nil, 0600); err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(vanished); err != nil {
		t.Fatal(err)
	}
	if got := newestCompatibleBackup(bg, files); got != newest {
		t.Fatalf("selected %q, want %q", got, newest)
	}
	if got := newestCompatibleBackup(bg, []string{vanished, damaged, future}); got != "" {
		t.Fatalf("selected an invalid backup: %q", got)
	}
}

func TestCanceledUpdateStillCompletesRollback(t *testing.T) {
	e := newTestEnv(t)
	if err := e.app.cmdSetup(bg, []string{"--no-network", "--no-open"}); err != nil {
		t.Fatal(err)
	}
	setVersion(t, "v1.0.0")
	publishRelease(t, "v1.1.0", false)
	bin, _ := e.app.executable()
	ctx, cancel := context.WithCancel(bg)
	defer cancel()
	e.mgr.startCheck = func() error {
		if strings.Contains(b(bin), "v1.1.0") {
			cancel()
			return fmt.Errorf("new controller failed during interruption")
		}
		return nil
	}
	err := e.app.cmdUpdate(ctx, nil)
	if err == nil || !strings.Contains(err.Error(), "running again") || !e.running() {
		t.Fatalf("canceled update did not recover: %v", err)
	}
}
