package replicated

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
	"sort"
	"strings"
	"time"

	"devboard/internal/sqlitekit"
	"devboard/internal/team/domain"
	"devboard/internal/team/store"
)

// Replication is not a backup: a write that deletes the wrong thing is replicated to every host in a moment.
// A backup is a copy of the whole database, taken from the cluster at a position of its history, kept somewhere
// that is not the cluster, by the customer, on storage the customer owns. Werkbord hosts no bucket and no service
// for it. A destination is a directory (a local disk, or a network share that is mounted); the interface is
// shaped so that an S3-compatible bucket the customer owns can be another one later, but nothing here reaches
// one yet.

// Destination is where a workspace's backups are kept.
type Destination interface {
	// Put stores an object under a name, whole or not at all.
	Put(ctx context.Context, name string, r io.Reader) error
	// Open reads an object.
	Open(ctx context.Context, name string) (io.ReadCloser, error)
	// Local returns a path on this machine that holds the object, for tools that need a file; the
	// object is not copied (a destination that is not a directory copies it into dir).
	Local(ctx context.Context, name, dir string) (string, error)
	// List returns the names of the objects, sorted.
	List(ctx context.Context) ([]string, error)
	// Delete removes an object.
	Delete(ctx context.Context, name string) error
	// Describe says where this is, for people: never a credential.
	Describe() string
}

// DirDestination keeps backups in a directory: a local disk, or a network share mounted there.
type DirDestination struct{ Dir string }

func (d DirDestination) path(name string) (string, error) {
	if name == "" || strings.ContainsAny(name, `/\`) || name == "." || name == ".." || strings.HasPrefix(name, ".") {
		return "", fmt.Errorf("replicated: %q is not a backup's name", name)
	}
	return filepath.Join(d.Dir, name), nil
}

func (d DirDestination) Describe() string { return d.Dir }

func (d DirDestination) Put(ctx context.Context, name string, r io.Reader) error {
	p, err := d.path(name)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(d.Dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(d.Dir, ".partial-*")
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(tmp.Name())
		}
	}()
	if _, err := io.Copy(tmp, r); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), p); err != nil {
		return err
	}
	ok = true
	return nil
}

func (d DirDestination) Open(ctx context.Context, name string) (io.ReadCloser, error) {
	p, err := d.path(name)
	if err != nil {
		return nil, err
	}
	return os.Open(p)
}

func (d DirDestination) Local(ctx context.Context, name, dir string) (string, error) {
	return d.path(name)
}

func (d DirDestination) List(ctx context.Context) ([]string, error) {
	es, err := os.ReadDir(d.Dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range es {
		if !e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out, nil
}

func (d DirDestination) Delete(ctx context.Context, name string) error {
	p, err := d.path(name)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// BackupInfo describes one backup. It is written beside the backup, and holds no secret.
type BackupInfo struct {
	// Name is the backup's file in its destination.
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt"`
	// Position is how many writes the database had had when the backup was taken.
	Position      int64          `json:"position"`
	Epoch         string         `json:"epoch"`
	Chain         string         `json:"chain"`
	SchemaVersion int            `json:"schemaVersion"`
	Tables        map[string]int `json:"tables"`
	SHA256        string         `json:"sha256"`
	Size          int64          `json:"size"`
	// ClusterID names the cluster whose database it is.
	ClusterID string `json:"clusterId"`
	// FromLocalCopy is set when no leader could confirm it and it was taken from this host's own node: it is a
	// consistent point of the history and may be a little behind it.
	FromLocalCopy bool `json:"fromLocalCopy,omitempty"`
	// Host is the Workspace Host that took it.
	Host string `json:"host,omitempty"`
}

const backupPrefix = "workspace-"

func backupName(at time.Time, pos int64) string {
	return fmt.Sprintf("%s%s-p%08d.sqlite", backupPrefix, at.UTC().Format("20060102T150405Z"), pos)
}

// Backup takes a backup from the cluster (the leader's copy, confirmed by a quorum; if there is no leader,
// this host's own node's, which is marked), checks it, and puts it in dest. A backup that fails its check is
// not kept.
func (s *Store) Backup(ctx context.Context, dest Destination) (BackupInfo, error) {
	work, err := os.MkdirTemp(s.o.Dir, "backup-")
	if err != nil {
		return BackupInfo{}, err
	}
	defer os.RemoveAll(work)
	file := filepath.Join(work, "db.sqlite")
	fromLocal := false
	take := func(local bool) error {
		f, err := os.OpenFile(file, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
		if err != nil {
			return err
		}
		bctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
		defer cancel()
		if err := s.client.Backup(bctx, f, local); err != nil {
			f.Close()
			return err
		}
		if err := f.Sync(); err != nil {
			f.Close()
			return err
		}
		return f.Close()
	}
	if err := take(false); err != nil {
		if !errors.Is(err, ErrNoLeader) && !errors.Is(err, ErrUnreachable) {
			return BackupInfo{}, err
		}
		if lerr := take(true); lerr != nil {
			return BackupInfo{}, fmt.Errorf("replicated: no backup could be taken from the cluster: %w", err)
		}
		fromLocal = true
	}
	info, err := inspectBackup(ctx, file)
	if err != nil {
		return BackupInfo{}, fmt.Errorf("replicated: the backup that was taken failed its check and was not kept: %w", err)
	}
	info.CreatedAt = s.o.Now().UTC()
	info.Name = backupName(info.CreatedAt, info.Position)
	info.FromLocalCopy = fromLocal
	info.Host = s.host
	if err := putBackup(ctx, dest, file, info); err != nil {
		return BackupInfo{}, err
	}
	// And read back from where it was put, because a backup that cannot be read is not one.
	if _, err := VerifyBackup(ctx, dest, info.Name, s.o.Dir); err != nil {
		_ = dest.Delete(ctx, info.Name)
		_ = dest.Delete(ctx, info.Name+".json")
		return BackupInfo{}, fmt.Errorf("replicated: the backup was written but could not be read back, and was removed: %w", err)
	}
	return info, nil
}

func putBackup(ctx context.Context, dest Destination, file string, info BackupInfo) error {
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := dest.Put(ctx, info.Name, f); err != nil {
		return fmt.Errorf("replicated: writing the backup to %s: %w", dest.Describe(), err)
	}
	meta, _ := json.MarshalIndent(info, "", "  ")
	return dest.Put(ctx, info.Name+".json", strings.NewReader(string(meta)))
}

// inspectBackup opens a backup file and reads what describes it, checking it is a whole workspace database.
func inspectBackup(ctx context.Context, file string) (BackupInfo, error) {
	f, err := checkFile(ctx, file)
	if err != nil {
		return BackupInfo{}, err
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(file)+"?mode=ro&_pragma=busy_timeout(5000)")
	if err != nil {
		return BackupInfo{}, err
	}
	defer db.Close()
	info := BackupInfo{Position: f.Seq, Epoch: f.Epoch, Chain: f.Chain, Tables: map[string]int{}}
	if info.SchemaVersion, err = sqlitekit.SchemaVersion(ctx, db); err != nil {
		return BackupInfo{}, err
	}
	_ = db.QueryRowContext(ctx, `SELECT value FROM _meta WHERE key = 'cluster_id'`).Scan(&info.ClusterID)
	d, err := digestOf(ctx, sqlQuerier{db})
	if err != nil {
		return BackupInfo{}, err
	}
	info.Tables = d.Tables
	h := sha256.New()
	fh, err := os.Open(file)
	if err != nil {
		return BackupInfo{}, err
	}
	defer fh.Close()
	n, err := io.Copy(h, fh)
	if err != nil {
		return BackupInfo{}, err
	}
	info.SHA256, info.Size = hex.EncodeToString(h.Sum(nil)), n
	return info, nil
}

// VerifyBackup checks a backup in dest the way a restore would rely on it: it reads it back, checks its hash
// against the one written beside it, has SQLite check its integrity, finds its position in the workspace's
// history, and then does what a restore does to a database that is older than this build: it restores it into a
// scratch copy and applies this build's migrations to it. A backup that passes can be restored with this build.
// work is a directory to make the scratch copy in.
func VerifyBackup(ctx context.Context, dest Destination, name, work string) (BackupInfo, error) {
	scratch, err := os.MkdirTemp(work, "verify-")
	if err != nil {
		return BackupInfo{}, err
	}
	defer os.RemoveAll(scratch)
	src, err := dest.Open(ctx, name)
	if err != nil {
		return BackupInfo{}, err
	}
	defer src.Close()
	file := filepath.Join(scratch, "db.sqlite")
	out, err := os.OpenFile(file, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return BackupInfo{}, err
	}
	if _, err := io.Copy(out, src); err != nil {
		out.Close()
		return BackupInfo{}, err
	}
	if err := out.Close(); err != nil {
		return BackupInfo{}, err
	}
	info, err := inspectBackup(ctx, file)
	if err != nil {
		return BackupInfo{}, err
	}
	info.Name = name
	// The record written beside it, if there is one, must agree.
	if side, err := dest.Open(ctx, name+".json"); err == nil {
		var rec BackupInfo
		derr := json.NewDecoder(side).Decode(&rec)
		side.Close()
		switch {
		case derr != nil:
			return BackupInfo{}, fmt.Errorf("replicated: the record beside the backup is unreadable: %w", derr)
		case rec.SHA256 != info.SHA256:
			return BackupInfo{}, errors.New("replicated: the backup does not match the hash written beside it: it was altered or damaged")
		}
		info.CreatedAt, info.FromLocalCopy, info.Host = rec.CreatedAt, rec.FromLocalCopy, rec.Host
	}
	ms, err := store.Migrations()
	if err != nil {
		return BackupInfo{}, err
	}
	if info.SchemaVersion > len(ms) {
		return BackupInfo{}, ErrSchemaNewer{Have: info.SchemaVersion, Max: len(ms)}
	}
	// Restore it into a scratch database and bring it to this build's schema.
	p, err := sqlitekit.Open(ctx, file, sqlitekit.Options{Replica: true})
	if err != nil {
		return BackupInfo{}, err
	}
	defer p.Close()
	if _, err := sqlitekit.Migrate(ctx, p.Writer, ms, "werkbord-team"); err != nil {
		return BackupInfo{}, fmt.Errorf("replicated: the backup cannot be brought to this version's schema: %w", err)
	}
	rows, err := p.Reader.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return BackupInfo{}, err
	}
	broken := rows.Next()
	_ = rows.Close()
	if broken {
		return BackupInfo{}, errors.New("replicated: the backup has rows that point at rows that do not exist")
	}
	return info, nil
}

// ListBackups lists the backups in dest, newest first, from the records written beside them. A backup with no
// readable record is listed with only its name.
func ListBackups(ctx context.Context, dest Destination) ([]BackupInfo, error) {
	names, err := dest.List(ctx)
	if err != nil {
		return nil, err
	}
	var out []BackupInfo
	for _, n := range names {
		if !strings.HasPrefix(n, backupPrefix) || !strings.HasSuffix(n, ".sqlite") {
			continue
		}
		info := BackupInfo{Name: n}
		if r, err := dest.Open(ctx, n+".json"); err == nil {
			_ = json.NewDecoder(r).Decode(&info)
			r.Close()
			info.Name = n
		}
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name > out[j].Name })
	return out, nil
}

// Retention says which backups to keep: the newest KeepLast of them always, and any younger than KeepFor.
// Everything else is removed. A zero KeepLast keeps at least one.
type Retention struct {
	KeepLast int
	KeepFor  time.Duration
}

// Apply removes the backups the policy does not keep, and returns their names. It never removes the newest one.
func (r Retention) Apply(ctx context.Context, dest Destination, now time.Time) ([]string, error) {
	all, err := ListBackups(ctx, dest)
	if err != nil {
		return nil, err
	}
	keep := r.KeepLast
	if keep < 1 {
		keep = 1
	}
	var removed []string
	for i, b := range all {
		if i < keep {
			continue
		}
		if r.KeepFor > 0 && !b.CreatedAt.IsZero() && now.Sub(b.CreatedAt) < r.KeepFor {
			continue
		}
		if err := dest.Delete(ctx, b.Name); err != nil {
			return removed, err
		}
		_ = dest.Delete(ctx, b.Name+".json")
		removed = append(removed, b.Name)
	}
	return removed, nil
}

// ---- restoring ----

// RestoreOptions control a restore.
type RestoreOptions struct {
	// Destination is where to put the copy of the current state that is taken first, so that a restore can be
	// undone. It must be given.
	SafetyCopy Destination
	// AnotherCluster allows restoring a backup of a different cluster's database (a workspace moved to new hosts).
	AnotherCluster bool
}

// Restore replaces the whole workspace database with a backup, through the cluster: every host takes the
// restored data. It is the one operation that discards writes: whatever was written after the backup is gone from
// the workspace. So the backup is verified first (VerifyBackup), a copy of the current state is taken and kept
// (SafetyCopy), and a backup of another cluster is refused unless the caller says it is meant. It returns
// the backup that was restored and the copy of what it replaced.
func (s *Store) Restore(ctx context.Context, from Destination, name string, o RestoreOptions) (BackupInfo, BackupInfo, error) {
	if o.SafetyCopy == nil {
		return BackupInfo{}, BackupInfo{}, errors.New("replicated: a restore needs somewhere to keep a copy of what it replaces")
	}
	info, err := VerifyBackup(ctx, from, name, s.o.Dir)
	if err != nil {
		return BackupInfo{}, BackupInfo{}, fmt.Errorf("replicated: this backup cannot be restored: %w", err)
	}
	cur, err := s.clusterFence(ctx, LevelLinearizable)
	if err != nil {
		return BackupInfo{}, BackupInfo{}, s.unavailable(err)
	}
	var clusterID string
	if res, err := s.client.Query(ctx, LevelLinearizable, Stmt{SQL: `SELECT value FROM _meta WHERE key = 'cluster_id'`}); err == nil && len(res[0].Values) > 0 {
		clusterID, _ = res[0].Values[0][0].(string)
	}
	if info.ClusterID != "" && clusterID != "" && info.ClusterID != clusterID && !o.AnotherCluster {
		return BackupInfo{}, BackupInfo{}, errors.New("replicated: this is a backup of another cluster's database, not this workspace's; restoring it would replace the workspace with another one (say so explicitly if that is meant)")
	}
	_ = cur
	safety, err := s.Backup(ctx, o.SafetyCopy)
	if err != nil {
		return BackupInfo{}, BackupInfo{}, fmt.Errorf("replicated: the copy of the current state that a restore keeps could not be taken, so nothing was restored: %w", err)
	}
	file, err := from.Local(ctx, name, s.o.Dir)
	if err != nil {
		return BackupInfo{}, BackupInfo{}, err
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		return BackupInfo{}, BackupInfo{}, err
	}
	if err := s.client.Load(ctx, raw); err != nil {
		return BackupInfo{}, safety, fmt.Errorf("replicated: the cluster did not take the restored database (a copy of what it had is in %s): %w", o.SafetyCopy.Describe(), err)
	}
	// The restored database has the history of the backup, which another host may share a prefix of: give it a
	// new identity, so that every host knows to take a fresh copy and none mistakes itself for merely behind.
	s.writeMu.Lock()
	err = s.snapshot(ctx, false)
	s.writeMu.Unlock()
	if err != nil {
		return info, safety, err
	}
	if err := s.writeBatch(ctx, []Stmt{{SQL: `UPDATE _fence SET epoch = ? WHERE id = 1`, Args: []any{newID()}}}); err != nil {
		return info, safety, fmt.Errorf("replicated: the backup was restored but the hosts could not be told to take it: %w", err)
	}
	s.writeMu.Lock()
	if f, err := s.clusterFence(ctx, LevelLinearizable); err == nil {
		_ = s.catchUp(ctx, f, false)
	}
	s.writeMu.Unlock()
	return info, safety, nil
}

var _ = domain.ErrConflict
