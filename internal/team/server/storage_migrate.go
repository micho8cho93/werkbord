package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"path/filepath"
	"time"

	"devboard/internal/team/config"
	"devboard/internal/team/infra/pki"
	"devboard/internal/team/infra/rqlite"
	"devboard/internal/team/store/replicated"
)

// Moving a workspace that Team kept in one SQLite file (team.db) into a cluster, and back.
//
// Nothing here is destructive, and nothing is switched until everything has been checked:
//
//  1. the program is found and verified, and the file is checked not to be in use (the server must be stopped);
//  2. a copy of the file is made and kept (VACUUM INTO, so the original is only read), checked by SQLite, and kept as the
//     rollback data in <data>/backups; the original team.db is never changed or removed;
//  3. a first database node is started, empty, in a directory of its own, on loopback; the copy is loaded into it with
//     rqlite's own restore; the cluster's data is read back and compared with the original (schema, the rows of every table
//     counted and hashed, the schema version, SQLite's integrity check);
//  4. the store is opened on it, which applies any migrations this build has that the file lacked, as one guarded write each, and
//     the host's copy is checked against the cluster's;
//  5. only then is storage.json written, which is what makes the workspace be in the cluster.
//
// If any step fails the node is stopped, what was made is moved aside (never deleted), no storage.json exists, and the next start
// uses team.db exactly as before.

// MigrationReport says what a migration did (or, for a dry run, would do).
type MigrationReport struct {
	DryRun        bool           `json:"dryRun"`
	From          string         `json:"from"`
	RollbackCopy  string         `json:"rollbackCopy,omitempty"`
	SchemaVersion int            `json:"schemaVersion"`
	Tables        map[string]int `json:"tables"`
	NodeID        string         `json:"nodeId,omitempty"`
	Steps         []string       `json:"steps"`
}

// migrateFault lets a test make a migration fail at a named stage, to see that it is undone.
var migrateFault func(stage string) error

func (r *MigrationReport) step(format string, a ...any) {
	r.Steps = append(r.Steps, fmt.Sprintf(format, a...))
}

// MigrateStorage moves the workspace in team.db into a cluster of one Workspace Host (this one), which other hosts can then
// join. With dryRun it checks everything it can without changing anything.
func MigrateStorage(ctx context.Context, cfg config.Config, log *slog.Logger, dryRun bool) (rep MigrationReport, err error) {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	if err := cfg.Validate(); err != nil {
		return rep, err
	}
	rep = MigrationReport{DryRun: dryRun, From: cfg.DBPath()}
	if m, ok, err := readMarker(cfg); err != nil {
		return rep, err
	} else if ok && m.Kind == config.StorageReplicated {
		return rep, errors.New("this workspace's data is already in a cluster of Workspace Hosts; there is nothing to move")
	}
	if _, err := os.Stat(cfg.DBPath()); err != nil {
		return rep, fmt.Errorf("there is no database to move: %w", err)
	}
	if err := checkProgram(ctx, cfg, log); err != nil {
		return rep, err
	}
	rep.step("the database program is the pinned release %s", rqlite.Tag)
	if err := checkNotInUse(ctx, cfg.DBPath()); err != nil {
		return rep, err
	}
	rep.step("%s is not in use", cfg.DBPath())

	stamp := time.Now().UTC().Format("20060102T150405Z")
	work := filepath.Join(cfg.DataDir, "migration-"+stamp)
	info, err := replicated.PrepareImport(ctx, cfg.DBPath(), work)
	if err != nil {
		_ = os.RemoveAll(work)
		return rep, fmt.Errorf("the database cannot be moved as it is: %w", err)
	}
	rep.SchemaVersion, rep.Tables = info.SchemaVersion, info.Legacy.Tables
	rep.step("a copy of %s was made and checked by SQLite (schema version %d, %d tables)", cfg.DBPath(), info.SchemaVersion, len(info.Legacy.Tables))
	if dryRun {
		_ = os.RemoveAll(work)
		rep.step("dry run: nothing was changed")
		return rep, nil
	}

	// The rollback data: a verified copy, kept in the backups directory, apart from the original.
	if err := os.MkdirAll(filepath.Join(cfg.DataDir, "backups"), 0o700); err != nil {
		return rep, err
	}
	keep := filepath.Join(cfg.DataDir, "backups", "before-replication-"+stamp+".db")
	if err := copyFileSync(filepath.Join(work, "legacy-copy.sqlite"), keep); err != nil {
		return rep, err
	}
	rep.RollbackCopy = keep
	rep.step("the rollback copy is %s (and %s itself is left as it is)", keep, cfg.DBPath())

	finished := false
	var started *rqlite.Supervisor
	defer func() {
		if finished {
			return
		}
		stop, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		if started != nil {
			_ = started.Stop(stop)
		}
		undoMigration(cfg, log, stamp)
	}()

	// Where this host's node is named after its device, if it has one.
	nodeID := "host-" + randomID()
	if v, verr := (&Storage{cfg: cfg}).vault(); verr == nil && v.Exists() {
		if mat, lerr := v.Load(time.Now()); lerr == nil {
			nodeID = mat.Meta.HostDeviceID
		}
	}
	rep.NodeID = nodeID
	// A first attempt that did not finish left its files: they are put aside, never removed.
	if _, err := os.Stat(cfg.StorageDir()); err == nil {
		aside := cfg.StorageDir() + ".previous-" + stamp
		if err := os.Rename(cfg.StorageDir(), aside); err != nil {
			return rep, err
		}
		rep.step("files of an earlier attempt were put aside in %s", aside)
	}
	if err := prepareCluster(cfg, nodeID); err != nil {
		return rep, err
	}
	// The marker is not wanted until the end: the vault's credentials are, to start the node.
	_ = os.Remove(cfg.StorageMarkerPath())
	st := &Storage{cfg: cfg}
	if err := st.loadSecretsFor(); err != nil {
		return rep, err
	}
	m := storageMarker{Kind: config.StorageReplicated, NodeID: nodeID, HTTP: netip.AddrPortFrom(netip.MustParseAddr("127.0.0.1"), uint16(cfg.StoragePort)).String(),
		Raft: netip.AddrPortFrom(netip.MustParseAddr("127.0.0.1"), uint16(cfg.StorageRaftPort)).String(), Role: RoleVoter}
	st.marker = m
	ncfg, err := st.nodeConfig()
	if err != nil {
		return rep, err
	}
	sup2, err := rqlite.New(rqlite.Options{BinaryDirs: databaseDirs(cfg), Log: log})
	if err != nil {
		return rep, err
	}
	started = sup2
	nctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if err := sup2.Start(nctx, ncfg); err != nil {
		return rep, fmt.Errorf("starting the first database node: %w", err)
	}
	if err := sup2.WaitReady(nctx, false); err != nil {
		return rep, fmt.Errorf("the first database node did not become ready: %w", err)
	}
	rep.step("a first database node is running on %s", ncfg.HTTPAddr)

	client, err := replicated.NewClient([]string{ncfg.HTTPAddr.String()}, replicated.Auth{User: rqlite.UserApp, Pass: st.secrets.AppPassword})
	if err != nil {
		return rep, err
	}
	raw, err := os.ReadFile(info.File)
	if err != nil {
		return rep, err
	}
	if err := client.Load(nctx, raw); err != nil {
		return rep, fmt.Errorf("loading the database into the cluster: %w", err)
	}
	rep.step("the database was loaded with the database program's own restore")
	if migrateFault != nil {
		if err := migrateFault("loaded"); err != nil {
			return rep, err
		}
	}
	if err := replicated.VerifyImport(nctx, client, info); err != nil {
		return rep, err
	}
	rep.step("the cluster's data was read back and is the original's: schema, version, and every row of %d tables", len(info.Legacy.Tables))

	repl, err := replicated.Open(nctx, replicated.Options{Dir: cfg.ReplicaDir(), Nodes: []string{ncfg.HTTPAddr.String()}, Auth: replicated.Auth{User: rqlite.UserApp, Pass: st.secrets.AppPassword},
		HostID: nodeID, Log: log, Create: false, WaitForCluster: time.Minute})
	if err != nil {
		return rep, fmt.Errorf("opening the store on the loaded database: %w", err)
	}
	defer repl.Close()
	if _, pos, err := repl.Verify(nctx); err != nil {
		return rep, fmt.Errorf("this host's copy does not match the cluster: %w", err)
	} else {
		rep.step("this build's migrations were applied to the cluster (once) and this host's copy matches it at position %d", pos)
	}

	m.MigratedFrom, m.RollbackCopy, m.MigratedAt = cfg.DBPath(), keep, time.Now().UTC()
	if err := writeMarker(cfg, m); err != nil {
		return rep, err
	}
	finished = true
	_ = repl.Close()
	stop, cancel2 := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel2()
	_ = sup2.Stop(stop)
	_ = os.RemoveAll(work)
	rep.step("the workspace is now in the cluster (storage.json); %s is untouched, and is what `storage rollback` can use", cfg.DBPath())
	return rep, nil
}

// loadSecretsFor reads the cluster's credentials from the vault, for a command that has no Storage running.
func (st *Storage) loadSecretsFor() error { return st.loadSecrets() }

// undoMigration puts away what a migration that did not finish made: the cluster's files are moved aside, the vault's storage
// credentials removed, and no storage.json is left, so the workspace is still in team.db.
func undoMigration(cfg config.Config, log *slog.Logger, stamp string) {
	_ = os.Remove(cfg.StorageMarkerPath())
	aside := cfg.StorageDir() + ".failed-" + stamp
	if err := os.Rename(cfg.StorageDir(), aside); err == nil {
		log.Warn("the files of the migration that did not finish were put aside", "dir", aside)
	}
	if v, err := (&Storage{cfg: cfg}).vault(); err == nil && !v.Exists() {
		_ = os.Remove(filepath.Join(cfg.PKIDir(), "storage.sealed"))
	}
}

// checkNotInUse refuses a database another process has open for writing: the server must be stopped, or what it writes after
// the copy is made would be left behind.
func checkNotInUse(ctx context.Context, path string) error {
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?_pragma=busy_timeout(1500)")
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `BEGIN EXCLUSIVE`); err != nil {
		return fmt.Errorf("%s is in use (is `werkbord-team serve` still running?): stop it first, so that nothing is written after the copy is made: %w", path, err)
	}
	_, _ = conn.ExecContext(ctx, `ROLLBACK`)
	return nil
}

func copyFileSync(src, dst string) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// ---- and back ----

// RollbackReport says what a rollback did.
type RollbackReport struct {
	Database string   `json:"database"`
	Replaced string   `json:"replaced,omitempty"`
	Kept     string   `json:"kept"`
	Steps    []string `json:"steps"`
}

// RollbackStorage moves a workspace that is in a cluster of one host back into a single file, with what it holds now (not
// what team.db held when it was moved): a backup is taken from the cluster, turned into a database file whose every table is
// checked against the backup's, and put in place of team.db (which is kept, renamed). The cluster's files are kept aside. A
// cluster of several hosts is refused: the other hosts hold copies that would be left behind.
func RollbackStorage(ctx context.Context, cfg config.Config, log *slog.Logger) (rep RollbackReport, err error) {
	if err := cfg.Validate(); err != nil {
		return rep, err
	}
	m, ok, err := readMarker(cfg)
	if err != nil {
		return rep, err
	}
	if !ok || m.Kind != config.StorageReplicated {
		return rep, errors.New("this workspace's data is not in a cluster; there is nothing to move back")
	}
	w, err := OpenWorkspace(ctx, cfg, log)
	if err != nil {
		return rep, err
	}
	closed := false
	defer func() {
		if !closed {
			_ = w.Close()
		}
	}()
	st := w.Storage.Status(ctx)
	for i := 0; i < 40 && (len(st.Hosts) == 0 || st.ReadOnly); i++ {
		time.Sleep(500 * time.Millisecond) // a node that has just started takes a moment to say who its cluster is
		st = w.Storage.Status(ctx)
	}
	if len(st.Hosts) != 1 || st.ReadOnly {
		return rep, fmt.Errorf("a workspace can be moved back into one file only when it is held by exactly one host that is working (it has %d, read-only: %v): remove the other hosts first", len(st.Hosts), st.ReadOnly)
	}
	stamp := time.Now().UTC().Format("20060102T150405Z")
	dir := filepath.Join(cfg.DataDir, "backups", "rollback-"+stamp)
	dest := replicated.DirDestination{Dir: dir}
	repl := w.Storage.store()
	info, err := repl.Backup(ctx, dest)
	if err != nil {
		return rep, fmt.Errorf("taking the backup to move back: %w", err)
	}
	rep.Steps = append(rep.Steps, fmt.Sprintf("a backup of the cluster was taken and checked (position %d)", info.Position))
	out := filepath.Join(cfg.DataDir, "team.db.rollback-"+stamp)
	if err := replicated.ExportLegacy(ctx, filepath.Join(dir, info.Name), out); err != nil {
		return rep, err
	}
	rep.Steps = append(rep.Steps, "it was turned into a database file, and every table of it was checked against the backup's")
	// Close the cluster before the switch.
	closed = true
	if err := w.Close(); err != nil {
		return rep, err
	}
	if _, err := os.Stat(cfg.DBPath()); err == nil {
		rep.Replaced = cfg.DBPath() + ".replaced-" + stamp
		if err := os.Rename(cfg.DBPath(), rep.Replaced); err != nil {
			return rep, err
		}
		for _, suffix := range []string{"-wal", "-shm"} {
			_ = os.Rename(cfg.DBPath()+suffix, rep.Replaced+suffix)
		}
	}
	if err := os.Rename(out, cfg.DBPath()); err != nil {
		return rep, err
	}
	if err := writeMarker(cfg, storageMarker{Kind: config.StorageSingleFile}); err != nil {
		return rep, err
	}
	rep.Kept = cfg.StorageDir() + ".rolled-back-" + stamp
	if err := os.Rename(cfg.StorageDir(), rep.Kept); err != nil {
		return rep, err
	}
	if v, err := (&Storage{cfg: cfg}).vault(); err == nil {
		_ = os.Rename(filepath.Join(v.Dir(), "storage.sealed"), filepath.Join(v.Dir(), "storage.sealed.rolled-back-"+stamp))
	}
	rep.Database = cfg.DBPath()
	rep.Steps = append(rep.Steps, "the workspace is in one file again; the cluster's files and the replaced database were kept")
	return rep, nil
}

var _ = pki.StorageSecrets{}
