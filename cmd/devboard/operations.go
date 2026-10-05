package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"devboard/internal/controller"
	"devboard/internal/service"
	"devboard/internal/store/sqlite"
)

func interruptionFlags(name string, args []string, out io.Writer) (bool, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(out)
	force := fs.Bool("force", false, "interrupt active local agents, retaining their work")
	if err := fs.Parse(args); err != nil {
		return false, err
	}
	if fs.NArg() != 0 {
		return false, fmt.Errorf("usage: devboard %s [--force]", name)
	}
	return *force, nil
}
func (a *app) checkInterruption(ctx context.Context, force bool) error {
	if force {
		return nil
	}
	files, err := filepath.Glob(filepath.Join(runnerDir(a.cfg), "runs", "*.json"))
	if err != nil {
		return err
	}
	for _, file := range files {
		var journal struct {
			Phase string `json:"phase"`
		}
		data, err := os.ReadFile(file)
		if err != nil || json.Unmarshal(data, &journal) != nil {
			return fmt.Errorf("cannot check runner journal %s; inspect it before interruption, or use --force", file)
		}
		if journal.Phase != "ended" && journal.Phase != "resolved" {
			return fmt.Errorf("runner journal %s has %s work; finish or resolve it before interruption, or use --force", filepath.Base(file), journal.Phase)
		}
	}
	if _, up := a.healthy(ctx); !up {
		return nil
	}
	client, err := a.client()
	if err != nil {
		return err
	}
	var overview service.Overview
	if err := client.do(ctx, "GET", "/api/control-center", nil, &overview); err != nil {
		return fmt.Errorf("cannot check active runs before interruption: %w", err)
	}
	for _, item := range overview.Runs {
		if !item.Run.Remote && item.Run.State.Active() {
			return fmt.Errorf("run %s is active on this computer; finish or stop it in Werkbord first, or explicitly use --force to interrupt it and retain its work", item.Run.ID)
		}
	}
	return nil
}

// snapshotDatabase uses SQLite itself to copy a consistent database (including
// WAL pages). Call after stopping the controller, while holding its file lock.
func (a *app) snapshotDatabase(ctx context.Context) (string, error) {
	if _, err := os.Stat(a.cfg.DBPath()); os.IsNotExist(err) {
		return "", nil
	} else if err != nil {
		return "", err
	}
	dir := sqlite.BackupDir(a.cfg.DBPath())
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	dst := filepath.Join(dir, "before-update-"+time.Now().UTC().Format("20060102T150405.000000000Z")+".db")
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(a.cfg.DBPath())+"?mode=ro&_pragma=busy_timeout(5000)")
	if err != nil {
		return "", err
	}
	defer db.Close()
	if _, err = db.ExecContext(ctx, `VACUUM INTO ?`, dst); err != nil {
		return "", err
	}
	return dst, os.Chmod(dst, 0600)
}

// restoreDatabase preserves the failed database and its journals for inspection.
// It only operates with the controller stopped and its exclusive lock held.
func (a *app) restoreDatabase(ctx context.Context, backup string) error {
	info, err := sqlite.Inspect(ctx, backup)
	if err != nil {
		return err
	}
	if !info.Exists || info.Integrity != "ok" || info.NewerThanBuild {
		return fmt.Errorf("backup is missing, damaged or newer than this executable: %s", backup)
	}
	dir := filepath.Join(sqlite.BackupDir(a.cfg.DBPath()), "failed-update-"+time.Now().UTC().Format("20060102T150405.000000000Z"))
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	// Stage the replacement before moving any existing data.
	source, err := os.Open(backup)
	if err != nil {
		return err
	}
	defer source.Close()
	tmp, err := os.CreateTemp(a.cfg.DataDir, ".restore-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	_, err = io.Copy(tmp, source)
	if err == nil {
		err = tmp.Sync()
	}
	closeErr := tmp.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		path := a.cfg.DBPath() + suffix
		if err := os.Rename(path, filepath.Join(dir, filepath.Base(path))); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if err := os.Rename(tmp.Name(), a.cfg.DBPath()); err != nil {
		return err
	}
	a.printf("Database restored from %s; replaced files retained at %s. Remote runners must reconcile ownership before resuming.\n", backup, dir)
	return nil
}
func (a *app) cmdDB(ctx context.Context, args []string) error {
	if len(args) == 0 || args[0] != "restore" {
		return fmt.Errorf("usage: devboard db restore [--force] <backup.db> | --latest")
	}
	fs := flag.NewFlagSet("db restore", flag.ContinueOnError)
	fs.SetOutput(a.errOut)
	latest := fs.Bool("latest", false, "restore the newest compatible pre-upgrade backup")
	force := fs.Bool("force", false, "interrupt active local runs")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	backup := ""
	if *latest {
		files, _ := filepath.Glob(filepath.Join(sqlite.BackupDir(a.cfg.DBPath()), "*.db"))
		sort.Slice(files, func(i, j int) bool {
			a, _ := os.Stat(files[i])
			b, _ := os.Stat(files[j])
			return a.ModTime().After(b.ModTime())
		})
		for _, path := range files {
			info, err := sqlite.Inspect(ctx, path)
			if err == nil && info.Integrity == "ok" && !info.NewerThanBuild {
				backup = path
				break
			}
		}
	} else if fs.NArg() == 1 {
		backup = fs.Arg(0)
	}
	if backup == "" {
		return fmt.Errorf("choose an existing compatible backup, or use --latest")
	}
	info, err := sqlite.Inspect(ctx, backup)
	if err != nil || !info.Exists || info.Integrity != "ok" || info.NewerThanBuild {
		return fmt.Errorf("backup is missing, damaged or newer than this executable: %s", backup)
	}
	if err := a.checkInterruption(ctx, *force); err != nil {
		return err
	}
	_, wasUp := a.healthy(ctx)
	if err := a.stop(ctx, false); err != nil {
		return err
	}
	unlock, err := controller.LockController(a.cfg.LockPath())
	if err != nil {
		return err
	}
	err = a.restoreDatabase(ctx, backup)
	unlock()
	if err != nil {
		return err
	}
	if wasUp {
		return a.start(ctx, true)
	}
	return nil
}
