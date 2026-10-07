package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"devboard/internal/logging"
	"devboard/internal/team/config"
	"devboard/internal/team/domain"
	"devboard/internal/team/server"
	"devboard/internal/team/store/replicated"
)

// Where the workspace's data is kept, and what to do about it. Commands that ask the workspace (status, backup) are
// requests to the Team server's API with an administrator's token. Those that act on this host's own files and its
// own database node (migrate, rollback, restore) run on the host, with the Team server stopped.

const storageUsage = `usage: werkbord-team storage <command>

  status [--json]                  where the workspace's data is: the hosts that hold it, whether a quorum answers, whether it is read-only
  backup                           take a backup now, on the host the server runs on, into its backup directory
  backups [--dir D]                list the backups in a directory (default: this host's backup directory)
  verify-backup <name> [--dir D]   check a backup the way a restore relies on it, and say whether it can be restored with this version
  restore <name> --safety-dir D2 --yes [--dir D]
                                   (on a host, server stopped) replace the workspace's data with a backup; what it replaces is kept in D2
  migrate [--dry-run]              (on the host, server stopped) move a workspace kept in one file (team.db) into a cluster of Workspace Hosts
  rollback --yes                   (on the host, server stopped) move a workspace held by one host back into one file

The server is $WERKBORD_TEAM_SERVER (default http://127.0.0.1:7430); your token is $WERKBORD_TEAM_TOKEN.
Replication is not a backup: configure WERKBORD_TEAM_BACKUP_DIR. See docs/TEAM_STORAGE.md.
`

func cmdStorage(ctx context.Context, cfg config.Config, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		fmt.Fprint(stderr, storageUsage)
		return flag.ErrHelp
	}
	switch args[0] {
	case "status":
		return cmdStorageStatus(ctx, args[1:], stdout, stderr)
	case "backup":
		return cmdStorageBackup(ctx, args[1:], stdout, stderr)
	case "backups":
		return cmdStorageBackups(ctx, cfg, args[1:], stdout, stderr)
	case "verify-backup":
		return cmdStorageVerify(ctx, cfg, args[1:], stdout, stderr)
	case "restore":
		return cmdStorageRestore(ctx, cfg, args[1:], stdout, stderr)
	case "migrate":
		return cmdStorageMigrate(ctx, cfg, args[1:], stdout, stderr)
	case "rollback":
		return cmdStorageRollback(ctx, cfg, args[1:], stdout, stderr)
	case "help", "-h", "--help":
		fmt.Fprint(stdout, storageUsage)
		return nil
	}
	fmt.Fprint(stderr, storageUsage)
	return fmt.Errorf("unknown storage command %q", args[0])
}

func cmdStorageStatus(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("storage status", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var a apiFlags
	a.bind(fs)
	asJSON := fs.Bool("json", false, "print the report as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	base, hc, err := a.client()
	if err != nil {
		return err
	}
	var st domain.StorageStatus
	if err := doJSON(ctx, hc, http.MethodGet, base+"/api/team/v1/storage", a.token, nil, &st); err != nil {
		return err
	}
	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(st)
	}
	printStorage(stdout, st)
	return nil
}

func printStorage(w io.Writer, st domain.StorageStatus) {
	if st.State == domain.StorageSingleFile {
		fmt.Fprintln(w, "The workspace's data is in one file on one host: it is not replicated.")
		for _, n := range st.Notes {
			fmt.Fprintf(w, "  %s\n", n)
		}
		return
	}
	mode := "writable"
	if st.ReadOnly {
		mode = "READ-ONLY"
	}
	fmt.Fprintf(w, "The workspace's data is replicated across %d Workspace Host(s): %s\n", len(st.Hosts), mode)
	if st.ReadOnly && st.Reason != "" {
		fmt.Fprintf(w, "  why: %s\n", st.Reason)
	}
	fmt.Fprintf(w, "\n%s\n", st.Topology.Summary)
	if st.Topology.Recommendation != "" {
		fmt.Fprintf(w, "  Recommended: %s\n", st.Topology.Recommendation)
	}
	fmt.Fprintf(w, "\n  %-30s %-8s %-10s %s\n", "host (node)", "votes", "answers", "")
	for _, h := range st.Hosts {
		role := ""
		if h.Leader {
			role = "leader"
		}
		votes := "no"
		if h.Voter {
			votes = "yes"
		}
		ans := "yes"
		if !h.Reachable {
			ans = "NO"
		}
		fmt.Fprintf(w, "  %-30s %-8s %-10s %s\n", h.NodeID, votes, ans, role)
	}
	fmt.Fprintf(w, "\nThis host's copy is at position %d", st.Local.Position)
	if st.Local.BehindMillis >= 0 {
		fmt.Fprintf(w, " (confirmed current %.1fs ago)", float64(st.Local.BehindMillis)/1000)
	}
	fmt.Fprintf(w, "; schema version %d.\n", st.SchemaVersion)
	if p := st.Program; p != nil {
		fmt.Fprintf(w, "This host's database program: rqlite %s (%s).\n", p.Version, p.State)
	}
	if b := st.Backup; b != nil {
		switch {
		case !b.Configured:
			fmt.Fprintln(w, "\nBackups: none configured. Replication is not a backup (set WERKBORD_TEAM_BACKUP_DIR).")
		default:
			last := "never"
			if b.LastAt > 0 {
				last = time.UnixMilli(b.LastAt).Local().Format(time.RFC3339)
			}
			ok := "ok"
			if !b.LastOK && b.LastError != "" {
				ok = "FAILED: " + b.LastError
			}
			fmt.Fprintf(w, "\nBackups: %d in %s; last %s (%s)\n", b.Count, b.Destination, last, ok)
		}
	}
	for _, n := range st.Notes {
		fmt.Fprintf(w, "  note: %s\n", n)
	}
}

func cmdStorageBackup(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("storage backup", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var a apiFlags
	a.bind(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	base, hc, err := a.client()
	if err != nil {
		return err
	}
	hc.Timeout = 30 * time.Minute
	var st domain.BackupStatus
	if err := doJSON(ctx, hc, http.MethodPost, base+"/api/team/v1/storage/backup", a.token, map[string]any{}, &st); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "A backup was taken, read back and checked. %d backup(s) in %s.\n", st.Count, st.Destination)
	return nil
}

func destOf(cfg config.Config, dir string) (replicated.DirDestination, error) {
	if dir == "" {
		dir = cfg.BackupDir
	}
	if dir == "" {
		return replicated.DirDestination{}, errors.New("give --dir, or set WERKBORD_TEAM_BACKUP_DIR")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return replicated.DirDestination{}, err
	}
	return replicated.DirDestination{Dir: abs}, nil
}

func cmdStorageBackups(ctx context.Context, cfg config.Config, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("storage backups", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dir := fs.String("dir", "", "the directory the backups are in")
	if err := fs.Parse(args); err != nil {
		return err
	}
	dest, err := destOf(cfg, *dir)
	if err != nil {
		return err
	}
	list, err := replicated.ListBackups(ctx, dest)
	if err != nil {
		return err
	}
	if len(list) == 0 {
		fmt.Fprintf(stdout, "No backups in %s.\n", dest.Dir)
		return nil
	}
	for _, b := range list {
		when := "?"
		if !b.CreatedAt.IsZero() {
			when = b.CreatedAt.Local().Format(time.RFC3339)
		}
		fmt.Fprintf(stdout, "  %-48s %s  position %d, schema %d, %d KB\n", b.Name, when, b.Position, b.SchemaVersion, b.Size/1024)
	}
	return nil
}

func cmdStorageVerify(ctx context.Context, cfg config.Config, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("storage verify-backup", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dir := fs.String("dir", "", "the directory the backup is in")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return errors.New("usage: werkbord-team storage verify-backup <name> [--dir D]")
	}
	dest, err := destOf(cfg, *dir)
	if err != nil {
		return err
	}
	work, err := os.MkdirTemp("", "werkbord-verify-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)
	info, err := replicated.VerifyBackup(ctx, dest, pos[0], work)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "%s is a whole workspace database (position %d, schema version %d, %d tables) and can be restored with this version.\n", info.Name, info.Position, info.SchemaVersion, len(info.Tables))
	return nil
}

func cmdStorageRestore(ctx context.Context, cfg config.Config, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("storage restore", flag.ContinueOnError)
	fs.SetOutput(stderr)
	commonFlags(fs, &cfg)
	dir := fs.String("dir", "", "the directory the backup is in")
	safety := fs.String("safety-dir", "", "where to keep a copy of what the restore replaces (required)")
	other := fs.Bool("another-workspace", false, "the backup is of another cluster's database (a workspace moved to new hosts)")
	yes := fs.Bool("yes", false, "yes, replace the workspace's data: whatever was written after the backup is gone from the workspace")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 || *safety == "" {
		return errors.New("usage: werkbord-team storage restore <name> --safety-dir <dir> --yes [--dir D]")
	}
	if !*yes {
		return errors.New("a restore replaces the workspace's data with the backup's: whatever was written after the backup is gone from the workspace (a copy of it is kept in --safety-dir). Say --yes if that is meant")
	}
	from, err := destOf(cfg, *dir)
	if err != nil {
		return err
	}
	safetyAbs, err := filepath.Abs(*safety)
	if err != nil {
		return err
	}
	log, err := logging.New(stderr, "warn", cfg.LogFormat)
	if err != nil {
		return err
	}
	w, err := server.OpenWorkspace(ctx, cfg, log)
	if err != nil {
		return err
	}
	defer w.Close()
	repl := w.Storage.ReplicatedStore()
	if repl == nil {
		return errors.New("this workspace's data is not in a cluster: a single-file workspace is restored by putting the backup's file back as team.db with the server stopped")
	}
	restored, kept, err := repl.Restore(ctx, from, pos[0], replicated.RestoreOptions{SafetyCopy: replicated.DirDestination{Dir: safetyAbs}, AnotherCluster: *other})
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Restored %s (position %d). What it replaced was kept as %s in %s. Every Workspace Host takes the restored data within moments.\n", restored.Name, restored.Position, kept.Name, safetyAbs)
	return nil
}

func cmdStorageMigrate(ctx context.Context, cfg config.Config, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("storage migrate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	commonFlags(fs, &cfg)
	dry := fs.Bool("dry-run", false, "check everything that can be checked, and change nothing")
	fs.IntVar(&cfg.StoragePort, "storage-port", cfg.StoragePort, "the port of this host's database node (HTTP)")
	fs.IntVar(&cfg.StorageRaftPort, "storage-raft-port", cfg.StorageRaftPort, "the port of this host's database node (Raft)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	log, err := logging.New(stderr, "warn", cfg.LogFormat)
	if err != nil {
		return err
	}
	rep, err := server.MigrateStorage(ctx, cfg, log, *dry)
	for _, s := range rep.Steps {
		fmt.Fprintf(stdout, "  - %s\n", s)
	}
	if err != nil {
		fmt.Fprintln(stdout)
		return fmt.Errorf("the workspace was NOT moved, and %s is exactly as it was: %w", cfg.DBPath(), err)
	}
	if *dry {
		fmt.Fprintln(stdout, "\nDry run: nothing was changed. Run it again without --dry-run, with `werkbord-team serve` stopped, to move the workspace.")
		return nil
	}
	fmt.Fprintf(stdout, "\nThe workspace is in a cluster of this one Workspace Host. Start `werkbord-team serve` as before.\n")
	fmt.Fprintf(stdout, "It has no high availability yet: add two more Workspace Hosts (docs/TEAM_STORAGE.md). Your rollback copy: %s\n", rep.RollbackCopy)
	return nil
}

func cmdStorageRollback(ctx context.Context, cfg config.Config, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("storage rollback", flag.ContinueOnError)
	fs.SetOutput(stderr)
	commonFlags(fs, &cfg)
	yes := fs.Bool("yes", false, "yes, move the workspace back into one file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if !*yes {
		return errors.New("this moves the workspace, with what it holds now, back into one file (team.db); the cluster's files and the old team.db are kept. Say --yes if that is meant")
	}
	log, err := logging.New(stderr, "warn", cfg.LogFormat)
	if err != nil {
		return err
	}
	rep, err := server.RollbackStorage(ctx, cfg, log)
	for _, s := range rep.Steps {
		fmt.Fprintf(stdout, "  - %s\n", s)
	}
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "\nThe workspace is in %s again. The cluster's files are in %s", rep.Database, rep.Kept)
	if rep.Replaced != "" {
		fmt.Fprintf(stdout, ", and the database it replaced is %s", rep.Replaced)
	}
	fmt.Fprintln(stdout, ".")
	return nil
}
