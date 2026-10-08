package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"devboard/internal/team/config"
	"devboard/internal/team/domain"
	"devboard/internal/team/infra/pki"
	"devboard/internal/team/infra/rqlite"
	"devboard/internal/team/service"
	"devboard/internal/team/store"
	"devboard/internal/team/store/replicated"
)

// Where a host keeps the workspace's data, and what runs it.
//
// A workspace's data is either in one SQLite file (the way Team kept it before replication), or in a cluster of
// Workspace Hosts: each runs the pinned rqlite as its own database node, supervised (internal/team/infra/rqlite),
// and holds a copy of the data that it reads from and writes through (internal/team/store/replicated). A file in
// the data directory, storage.json, says which; without one a workspace is in a file, which is what every
// workspace was before it existed.

// storageMarker is storage.json. It holds nothing secret: the cluster's passwords are sealed in the key vault.
type storageMarker struct {
	Version int    `json:"version"`
	Kind    string `json:"kind"`
	// For a cluster: this host's node, where it listens, whether it votes, and which nodes it joins.
	NodeID string   `json:"nodeId,omitempty"`
	HTTP   string   `json:"http,omitempty"`
	Raft   string   `json:"raft,omitempty"`
	Role   string   `json:"role,omitempty"`
	Join   []string `json:"join,omitempty"`
	// Fresh is set while the cluster has nothing in it: this host's first start makes the workspace's tables.
	Fresh     bool      `json:"fresh,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	// MigratedFrom is the single file the data came from, and RollbackCopy the verified copy of it kept for
	// going back; MigratedAt is when.
	MigratedFrom string    `json:"migratedFrom,omitempty"`
	RollbackCopy string    `json:"rollbackCopy,omitempty"`
	MigratedAt   time.Time `json:"migratedAt,omitempty"`
}

// A host's part in the cluster.
const (
	// RoleVoter takes part in elections and in the quorum.
	RoleVoter = "voter"
	// RoleReplica holds all the data and takes no part in either: a host that has joined and has not yet been
	// checked and made a voter.
	RoleReplica = "replica"
	// RoleRemoved is a host that was taken out of the cluster; it keeps its files, and runs nothing.
	RoleRemoved = "removed"
)

const markerVersion = 1

func readMarker(cfg config.Config) (storageMarker, bool, error) {
	b, err := os.ReadFile(cfg.StorageMarkerPath())
	if errors.Is(err, os.ErrNotExist) {
		return storageMarker{}, false, nil
	}
	if err != nil {
		return storageMarker{}, false, err
	}
	var m storageMarker
	if err := json.Unmarshal(b, &m); err != nil {
		return storageMarker{}, false, fmt.Errorf("%s: %w", cfg.StorageMarkerPath(), err)
	}
	if m.Version > markerVersion {
		return storageMarker{}, false, fmt.Errorf("%s was written by a newer Werkbord Team; upgrade werkbord-team", cfg.StorageMarkerPath())
	}
	switch m.Kind {
	case config.StorageReplicated, config.StorageSingleFile:
	default:
		return storageMarker{}, false, fmt.Errorf("%s: storage kind %q is not one this version knows", cfg.StorageMarkerPath(), m.Kind)
	}
	return m, true, nil
}

func writeMarker(cfg config.Config, m storageMarker) error {
	m.Version = markerVersion
	if m.CreatedAt.IsZero() {
		m.CreatedAt = time.Now().UTC()
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(cfg.DataDir, ".storage-*.json")
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(tmp.Name())
		}
	}()
	if _, err := tmp.Write(append(b, '\n')); err != nil {
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
	if err := os.Rename(tmp.Name(), cfg.StorageMarkerPath()); err != nil {
		return err
	}
	ok = true
	return nil
}

// ErrNoDatabaseProgram: the pinned database program is not on this host.
var ErrNoDatabaseProgram = errors.New("the database program (rqlited) this version of Werkbord Team ships was not found: install Werkbord Team from its release archive, which carries it, or run scripts/fetch-rqlite.sh and set WERKBORD_TEAM_DATABASE_DIR; a workspace can be kept in one file with --storage single-file for evaluation")

// ---- a store that is there when the database is ----

// lateStore is the store the service is built on while a host's database node is still coming up (it may be waiting
// for the private network, or for the cluster to have a quorum, or for a copy to be taken): until it is ready,
// every call says so, as a read-only workspace does.
type lateStore struct {
	mu    sync.RWMutex
	inner store.Store
	why   string
	ready chan struct{}
}

func newLateStore() *lateStore {
	return &lateStore{why: "this host's database is starting", ready: make(chan struct{})}
}

var _ store.Store = (*lateStore)(nil)

func (l *lateStore) get() (store.Store, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if l.inner == nil {
		return nil, fmt.Errorf("%w: %s", domain.ErrReadOnly, l.why)
	}
	return l.inner, nil
}

func (l *lateStore) set(s store.Store) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.inner == nil {
		close(l.ready)
	}
	l.inner = s
}

func (l *lateStore) setWhy(why string) {
	l.mu.Lock()
	l.why = why
	l.mu.Unlock()
}

func (l *lateStore) View(ctx context.Context, fn func(store.Tx) error) error {
	s, err := l.get()
	if err != nil {
		return err
	}
	return s.View(ctx, fn)
}

func (l *lateStore) Update(ctx context.Context, fn func(store.Tx) error) error {
	s, err := l.get()
	if err != nil {
		return err
	}
	return s.Update(ctx, fn)
}

func (l *lateStore) FreshView(ctx context.Context, fn func(store.Tx) error) error {
	s, err := l.get()
	if err != nil {
		return err
	}
	if fresh, ok := s.(interface {
		FreshView(context.Context, func(store.Tx) error) error
	}); ok {
		return fresh.FreshView(ctx, fn)
	}
	return s.View(ctx, fn)
}

func (l *lateStore) SchemaVersion(ctx context.Context) (int, error) {
	s, err := l.get()
	if err != nil {
		return 0, err
	}
	return s.SchemaVersion(ctx)
}

func (l *lateStore) Ping(ctx context.Context) error {
	s, err := l.get()
	if err != nil {
		return err
	}
	return s.Ping(ctx)
}

func (l *lateStore) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.inner == nil {
		return nil
	}
	err := l.inner.Close()
	l.inner = nil
	l.why = "this host's database is closed"
	return err
}

// ---- the storage a host runs ----

// Storage is what a host runs to hold the workspace's data: a store, and for a cluster the node and the work of
// keeping it joined, caught up and backed up.
type Storage struct {
	cfg config.Config
	log *slog.Logger

	kind string
	db   store.Store
	late *lateStore // for a cluster

	mu        sync.Mutex
	marker    storageMarker
	secrets   pki.StorageSecrets
	sup       *rqlite.Supervisor
	repl      *replicated.Store
	svc       *service.Service
	overlay   func(context.Context) (netip.Addr, error)
	ws, dev   string // this host's workspace and device, when it has a key vault
	prefix    netip.Prefix
	wasMember bool
	activated bool
	backups   *backupRunner
	// planPorts says which ports a host being added listens on (the configured ones, unless a test puts several hosts on one address).
	planPorts func(netip.Addr) (httpPort, raftPort int)

	cancel context.CancelFunc
	done   chan struct{}

	// fatal is closed, with why in fatalErr, when the node cannot be started and trying again will not help (the
	// program is not there, or is not the pinned one): whoever is waiting for the database need not wait for it.
	fatal     chan struct{}
	fatalOnce sync.Once
	fatalErr  error
}

func (st *Storage) fail(err error) {
	st.fatalOnce.Do(func() {
		st.fatalErr = err
		close(st.fatal)
	})
}

// OpenStorage reads where this host keeps the workspace's data and gets ready to run it. For a file it opens the
// file, and is ready; for a cluster it prepares what Start will run, and the store is there when the database is.
//
// A host that was handed the workspace's database credentials (it was made a Workspace Host and has collected
// them) but has no storage.json yet starts joining the cluster.
func OpenStorage(ctx context.Context, cfg config.Config, log *slog.Logger) (*Storage, error) {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	st := &Storage{cfg: cfg, log: log.With("component", "storage"), done: make(chan struct{}), fatal: make(chan struct{})}
	m, ok, err := readMarker(cfg)
	if err != nil {
		return nil, err
	}
	if !ok {
		if adopted, err := st.adoptJoin(); err != nil {
			return nil, err
		} else if adopted {
			m, ok = st.marker, true
		}
	}
	if !ok || m.Kind == config.StorageSingleFile {
		db, err := store.Open(ctx, cfg.DBPath(), log)
		if err != nil {
			return nil, err
		}
		st.kind, st.db, st.marker = config.StorageSingleFile, db, m
		return st, nil
	}
	if m.Role == RoleRemoved {
		return nil, errors.New("this host was taken out of the workspace's Workspace Hosts and no longer holds its data: its old files are kept in " + cfg.StorageDir() + "; to make it a host again, have an administrator add it (docs/TEAM_STORAGE.md)")
	}
	st.kind, st.marker = config.StorageReplicated, m
	if err := st.loadSecrets(); err != nil {
		return nil, err
	}
	st.late = newLateStore()
	st.db = st.late
	return st, nil
}

// adoptJoin turns the database credentials a host collected into a marker, so that its next start joins the cluster.
func (st *Storage) adoptJoin() (bool, error) {
	if _, err := os.Stat(st.cfg.DBPath()); err == nil {
		return false, nil // it has a file: that is where its data is
	}
	v, err := st.vault()
	if err != nil {
		return false, nil
	}
	if !v.HasStorage() {
		return false, nil
	}
	s, err := v.Storage()
	if err != nil {
		return false, fmt.Errorf("the workspace's database credentials on this host: %w", err)
	}
	if len(s.JoinRaft) == 0 {
		return false, nil // this host started the cluster; its marker is what it is missing
	}
	m := storageMarker{Kind: config.StorageReplicated, NodeID: s.NodeID, HTTP: s.HTTPAddr, Raft: s.RaftAddr, Role: RoleReplica, Join: s.JoinRaft}
	if err := writeMarker(st.cfg, m); err != nil {
		return false, err
	}
	st.marker = m
	st.log.Info("this host was made a Workspace Host: its database node will join the workspace's cluster", "node", m.NodeID, "joining", m.Join)
	return true, nil
}

func (st *Storage) vault() (*pki.Vault, error) {
	sealer, err := SealerFor(st.cfg)
	if err != nil {
		return nil, err
	}
	return pki.OpenVault(st.cfg.PKIDir(), sealer)
}

func (st *Storage) loadSecrets() error {
	v, err := st.vault()
	if err != nil {
		return err
	}
	if !v.HasStorage() {
		return fmt.Errorf("this host's storage is a cluster, and the cluster's credentials are not in %s: restore them from the backup of that directory, or have an administrator remove this host and add it again", st.cfg.PKIDir())
	}
	if st.secrets, err = v.Storage(); err != nil {
		return fmt.Errorf("the cluster's credentials on this host: %w", err)
	}
	if v.Exists() {
		if mat, err := v.Load(time.Now()); err == nil {
			st.ws, st.dev = mat.Meta.WorkspaceID, mat.Meta.HostDeviceID
			if p, err := netip.ParsePrefix(mat.Meta.NetworkPrefix); err == nil {
				st.prefix = p
			}
		}
	}
	return nil
}

// DB is the store the service is built on.
func (st *Storage) DB() store.Store { return st.db }

// Kind says where the data is: config.StorageReplicated or config.StorageSingleFile.
func (st *Storage) Kind() string { return st.kind }

// Replicated reports whether the data is in a cluster.
func (st *Storage) Replicated() bool { return st.kind == config.StorageReplicated }

// Attach tells the storage which service it serves, and how to find this host's address on the private network
// (a host whose database listens there has to wait for the address to exist, and a workspace that began on one host
// has to move onto it before it can have a second).
func (st *Storage) Attach(svc *service.Service, overlay func(context.Context) (netip.Addr, error)) {
	st.mu.Lock()
	st.svc, st.overlay = svc, overlay
	st.mu.Unlock()
	if st.Replicated() {
		svc.SetStorage(st)
	}
}

// Start runs what a cluster needs: this host's database node, the store on it, and the upkeep. It returns at once; the
// store is there when the database is (WaitReady). For a file there is nothing to start.
func (st *Storage) Start(ctx context.Context) {
	if !st.Replicated() {
		close(st.done)
		return
	}
	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	st.cancel = cancel
	st.backups = newBackupRunner(st)
	go st.run(runCtx)
}

// WaitReady waits until the store can be used. For a file it already can.
func (st *Storage) WaitReady(ctx context.Context, d time.Duration) error {
	if !st.Replicated() {
		return nil
	}
	select {
	case <-st.late.ready:
		return nil
	case <-st.fatal:
		return st.fatalErr
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		st.mu.Lock()
		sup := st.sup
		st.mu.Unlock()
		msg := ""
		if sup != nil {
			if s := sup.Status(); s.LastError != "" {
				msg = ": " + s.LastError
			}
		}
		return fmt.Errorf("this host's database did not become ready in %s%s", d, msg)
	}
}

// Close stops the upkeep, closes the store and stops this host's database node (gracefully; it stays a member of
// the cluster, which is only ever left by a removal that was asked for).
func (st *Storage) Close(ctx context.Context) error {
	if st.cancel != nil {
		st.cancel()
		<-st.done
	}
	var errs []error
	if st.db != nil {
		errs = append(errs, st.db.Close())
	}
	st.mu.Lock()
	sup := st.sup
	st.mu.Unlock()
	if sup != nil {
		errs = append(errs, sup.Stop(ctx))
	}
	return errors.Join(errs...)
}

// ---- the node ----

func hostPortOf(addr string) (netip.AddrPort, error) { return netip.ParseAddrPort(addr) }

func (st *Storage) nodeConfig() (rqlite.NodeConfig, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	m := st.marker
	h, err := hostPortOf(m.HTTP)
	if err != nil {
		return rqlite.NodeConfig{}, fmt.Errorf("storage.json: the database's address: %w", err)
	}
	r, err := hostPortOf(m.Raft)
	if err != nil {
		return rqlite.NodeConfig{}, fmt.Errorf("storage.json: the database's Raft address: %w", err)
	}
	var join []netip.AddrPort
	for _, j := range m.Join {
		ap, err := hostPortOf(j)
		if err != nil {
			return rqlite.NodeConfig{}, fmt.Errorf("storage.json: a join address: %w", err)
		}
		join = append(join, ap)
	}
	return rqlite.NodeConfig{
		DataDir: st.cfg.StorageDir(), NodeID: m.NodeID, HTTPAddr: h, RaftAddr: r, Network: st.prefix, Join: join,
		NonVoter:    m.Role == RoleReplica,
		Credentials: rqlite.Credentials{App: st.secrets.AppPassword, Admin: st.secrets.AdminPassword, Node: st.secrets.NodePassword},
	}, nil
}

func (st *Storage) supervisor() (*rqlite.Supervisor, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.sup != nil {
		return st.sup, nil
	}
	sup, err := rqlite.New(rqlite.Options{BinaryDirs: databaseDirs(st.cfg), Log: st.log})
	if err != nil {
		return nil, err
	}
	st.sup = sup
	return sup, nil
}

// databaseDirs are the places the pinned program may have been installed: where the operator said, beside the
// executable, and in the directory the installer uses. What is found must still match the pin; the PATH is never searched.
func databaseDirs(cfg config.Config) []string {
	var dirs []string
	dirs = append(dirs, cfg.DatabaseDirs...)
	if exe, err := os.Executable(); err == nil {
		if exe, err = filepath.EvalSymlinks(exe); err == nil {
			d := filepath.Dir(exe)
			dirs = append(dirs, filepath.Join(d, "libexec", "werkbord-team"), filepath.Join(filepath.Dir(d), "libexec", "werkbord-team"), d)
		}
	}
	return dirs
}

// bindReady reports whether the node's addresses exist on this machine: loopback always does; an address on the
// workspace's private network does once the network node is up.
func bindReady(cfg rqlite.NodeConfig) bool {
	return cfg.HTTPAddr.Addr().IsLoopback() || hasLocalAddr(cfg.HTTPAddr.Addr())
}

// run brings the node up and the store on it, then keeps them right.
func (st *Storage) run(ctx context.Context) {
	defer close(st.done)
	delay := 2 * time.Second
	for ctx.Err() == nil {
		err := st.bringUp(ctx)
		if err == nil {
			break
		}
		st.late.setWhy(err.Error())
		st.log.Warn("this host's database is not up yet", "err", err, "retrying in", delay)
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		if delay < 30*time.Second {
			delay *= 2
		}
	}
	if st.backups != nil {
		go st.backups.run(ctx)
	}
	t := time.NewTicker(3 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			st.reconcile(ctx)
		}
	}
}

func (st *Storage) bringUp(ctx context.Context) error {
	cfg, err := st.nodeConfig()
	if err != nil {
		return err
	}
	sup, err := st.supervisor()
	if err != nil {
		return err
	}
	if sup.Status().State != rqlite.StateRunning {
		if !bindReady(cfg) {
			return errors.New("waiting for this host's place on the workspace's private network (its network node must be running)")
		}
		sctx, cancel := context.WithTimeout(ctx, 90*time.Second)
		defer cancel()
		if err := sup.Start(sctx, cfg); err != nil {
			if errors.Is(err, rqlite.ErrBinaryNotFound) {
				st.fail(ErrNoDatabaseProgram)
				return ErrNoDatabaseProgram
			}
			if errors.Is(err, rqlite.ErrBinaryMismatch) {
				st.fail(err)
			}
			return err
		}
	}
	st.mu.Lock()
	repl, marker := st.repl, st.marker
	st.mu.Unlock()
	if repl != nil {
		return nil
	}
	wait := 2 * time.Minute
	repl, err = replicated.Open(ctx, replicated.Options{
		Dir: st.cfg.ReplicaDir(), Nodes: []string{cfg.HTTPAddr.String()}, Auth: replicated.Auth{User: rqlite.UserApp, Pass: st.secrets.AppPassword},
		HostID: marker.NodeID, Log: st.log, Create: marker.Fresh, WaitForCluster: wait,
		OnRemoteChange: st.remoteChanged,
	})
	if err != nil {
		return err
	}
	st.mu.Lock()
	st.repl = repl
	if marker.Fresh {
		st.marker.Fresh = false
		if err := writeMarker(st.cfg, st.marker); err != nil {
			st.log.Warn("could not record that the workspace's database exists", "err", err)
		}
	}
	svc := st.svc
	st.mu.Unlock()
	st.late.set(repl)
	if svc != nil {
		svc.RemoteChanged()
	}
	st.log.Info("this host's database is ready", "node", marker.NodeID, "role", marker.Role)
	return nil
}

func (st *Storage) remoteChanged() {
	st.mu.Lock()
	svc := st.svc
	st.mu.Unlock()
	if svc != nil {
		svc.RemoteChanged()
	}
}

// reconcile does what keeps this host's part right: it makes a replica that has caught up and been checked a voter,
// tells the workspace the host is active, and notices that it was removed.
func (st *Storage) reconcile(ctx context.Context) {
	st.mu.Lock()
	sup, repl, marker, svc := st.sup, st.repl, st.marker, st.svc
	st.mu.Unlock()
	if sup == nil || repl == nil || marker.Role == RoleRemoved {
		return
	}
	if sup.Status().State == rqlite.StateFailed {
		// The supervisor gave up on a start; start again with what is configured now.
		if err := st.bringUp(ctx); err != nil {
			st.log.Warn("this host's database node could not be started again", "err", err)
		}
		return
	}
	admin := sup.Admin()
	if admin == nil {
		return
	}
	hctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	h, err := admin.Health(hctx)
	if err != nil {
		return
	}
	self := false
	for _, m := range h.Members {
		self = self || m.ID == marker.NodeID
	}
	st.mu.Lock()
	if self {
		st.wasMember = true
	}
	wasMember := st.wasMember
	st.mu.Unlock()
	if !self && wasMember && len(h.Members) > 0 {
		st.removedFromCluster(ctx)
		return
	}
	if marker.Role == RoleReplica && self {
		st.makeVoter(ctx, sup, repl)
		return
	}
	if marker.Role == RoleVoter && self {
		st.activate(ctx, svc)
	}
}

// makeVoter checks a replica that has caught up, against the cluster's own data, and only then makes it a voter
// and tells the workspace it is active. Nothing marks it healthy before that.
func (st *Storage) makeVoter(ctx context.Context, sup *rqlite.Supervisor, repl *replicated.Store) {
	rctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if err := sup.WaitReady(rctx, true); err != nil {
		st.log.Info("this host's copy is still catching up with the cluster", "err", err)
		return
	}
	if _, pos, err := repl.Verify(rctx); err != nil {
		st.log.Warn("this host's copy has not been verified against the cluster yet", "err", err)
		return
	} else {
		st.log.Info("this host's copy matches the cluster's data", "position", pos)
	}
	if err := sup.BecomeVoter(rctx); err != nil {
		st.log.Warn("this host could not be made a voter yet", "err", err)
		return
	}
	st.mu.Lock()
	st.marker.Role = RoleVoter
	m := st.marker
	svc := st.svc
	st.mu.Unlock()
	if err := writeMarker(st.cfg, m); err != nil {
		st.log.Error("could not record that this host votes", "err", err)
	}
	st.log.Info("this host is a voting Workspace Host", "node", m.NodeID)
	st.activate(ctx, svc)
}

// activate records in the workspace that this host is an active Workspace Host.
func (st *Storage) activate(ctx context.Context, svc *service.Service) {
	st.mu.Lock()
	done := st.activated
	ws, dev := st.ws, st.dev
	st.mu.Unlock()
	if done || svc == nil || ws == "" || dev == "" {
		return
	}
	actx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if err := svc.ActivateLocalHost(actx, ws, dev); err != nil {
		st.log.Debug("could not record this host as active yet", "err", err)
		return
	}
	st.mu.Lock()
	st.activated = true
	st.mu.Unlock()
}

// removedFromCluster is what a host does when the cluster no longer lists it: it stops its node, keeps its files
// aside (nothing is deleted), and says so. Its store reads what it last had and takes no write.
func (st *Storage) removedFromCluster(ctx context.Context) {
	st.log.Warn("this host is no longer a member of the workspace's database cluster: stopping its database node")
	st.mu.Lock()
	sup := st.sup
	st.marker.Role = RoleRemoved
	m := st.marker
	st.mu.Unlock()
	if err := writeMarker(st.cfg, m); err != nil {
		st.log.Error("could not record that this host was removed", "err", err)
	}
	stop, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	_ = sup.Stop(stop)
	aside := filepath.Join(st.cfg.StorageDir(), "node.removed-"+time.Now().UTC().Format("20060102T150405Z"))
	if err := os.Rename(filepath.Join(st.cfg.StorageDir(), "node"), aside); err == nil {
		st.log.Info("this host's database files were kept", "dir", aside)
	}
	st.late.setWhy("this host was taken out of the workspace's Workspace Hosts")
}

// ---- service.StorageControl ----

var _ service.StorageControl = (*Storage)(nil)

func (st *Storage) store() *replicated.Store {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.repl
}

// ReplicatedStore is the store on the cluster, for the operations on the whole database (a restore); nil for a workspace in a file.
func (st *Storage) ReplicatedStore() *replicated.Store { return st.store() }

// Status is the storage as it is now.
func (st *Storage) Status(ctx context.Context) domain.StorageStatus {
	repl := st.store()
	if repl == nil {
		st.mu.Lock()
		role := st.marker.Role
		st.mu.Unlock()
		return domain.StorageStatus{State: domain.StorageReplicated, ReadOnly: true, Reason: "this host's database is starting", Hosts: []domain.StorageHost{},
			Notes: []string{"This host's role is " + role + "."}}
	}
	status := repl.Status(ctx)
	st.mu.Lock()
	role, br, sup := st.marker.Role, st.backups, st.sup
	st.mu.Unlock()
	if sup != nil {
		ps := sup.Status()
		status.Program = &domain.DatabaseProgram{Version: ps.ReportedVersion, State: string(ps.State), HashPinned: ps.HashPinned}
		if ps.ReportedVersion != "" && !ps.HashPinned {
			status.Notes = append(status.Notes, "This host's database program was built from the pinned source (the project publishes no binary for this platform): it reports the pinned version and commit, but is not checked against a hash pinned in this build.")
		}
	}
	if role == RoleReplica {
		status.Notes = append(status.Notes, "This host is joining the cluster: it holds a full copy and does not vote yet. It becomes a voter once it has caught up and been checked.")
	}
	if br != nil {
		status.Backup = br.status(ctx)
	} else if st.cfg.BackupDir == "" {
		status.Backup = &domain.BackupStatus{Configured: false}
		status.Notes = append(status.Notes, "No backup destination is configured on this host (WERKBORD_TEAM_BACKUP_DIR): replication is not a backup.")
	}
	return status
}

// localOverlay is this host's address on the private network.
func (st *Storage) localOverlay(ctx context.Context) (netip.Addr, error) {
	st.mu.Lock()
	f := st.overlay
	st.mu.Unlock()
	if f == nil {
		return netip.Addr{}, errors.New("this host has no place on a private network")
	}
	return f(ctx)
}

// PlanHost prepares the cluster for a host to join and says what the host needs.
func (st *Storage) PlanHost(ctx context.Context, h service.HostToAdd) (service.StoragePlan, error) {
	repl := st.store()
	if repl == nil {
		return service.StoragePlan{}, fmt.Errorf("%w: this host's database is not up yet", domain.ErrConflict)
	}
	status := repl.Status(ctx)
	// A leader is being elected for a moment after every change of members; a workspace that has none for longer than that has no quorum.
	for until := time.Now().Add(20 * time.Second); status.ReadOnly && time.Now().Before(until) && ctx.Err() == nil; {
		time.Sleep(300 * time.Millisecond)
		status = repl.Status(ctx)
	}
	if status.ReadOnly {
		return service.StoragePlan{}, fmt.Errorf("%w: a host cannot be added while the workspace has no quorum (%s)", domain.ErrConflict, status.Reason)
	}
	for _, n := range status.Hosts {
		if n.NodeID == h.NodeID {
			return service.StoragePlan{}, fmt.Errorf("%w: this device is already a member of the database cluster", domain.ErrConflict)
		}
	}
	// A workspace that began on one host listens on loopback; a second host can only reach it on the private network.
	if err := st.ensureReachable(ctx, status); err != nil {
		return service.StoragePlan{}, err
	}
	addr, err := netip.ParseAddr(h.OverlayAddr)
	if err != nil {
		return service.StoragePlan{}, fmt.Errorf("%w: the device's address on the network: %v", domain.ErrInvalid, err)
	}
	st.mu.Lock()
	secrets := st.secrets
	st.mu.Unlock()
	hp, rp := st.cfg.StoragePort, st.cfg.StorageRaftPort
	if st.planPorts != nil {
		hp, rp = st.planPorts(addr)
	}
	plan := service.StoragePlan{ClusterID: secrets.ClusterID, AppPassword: secrets.AppPassword, AdminPassword: secrets.AdminPassword, NodePassword: secrets.NodePassword,
		NodeID: h.NodeID, HTTPAddr: netip.AddrPortFrom(addr, uint16(hp)).String(), RaftAddr: netip.AddrPortFrom(addr, uint16(rp)).String()}
	// The nodes it joins through: every voting host that is up, by its Raft address.
	nodes, err := repl.Client().ClusterNodes(ctx)
	if err != nil {
		return service.StoragePlan{}, err
	}
	for _, n := range nodes {
		if n.Voter && n.Reachable && n.Raft != "" {
			plan.JoinRaft = append(plan.JoinRaft, n.Raft)
		}
	}
	if len(plan.JoinRaft) == 0 {
		return service.StoragePlan{}, fmt.Errorf("%w: no voting host of the cluster answers", domain.ErrConflict)
	}
	return plan, nil
}

// ensureReachable moves this host's own database node onto the workspace's private network if it is on loopback
// (only a cluster of one can be moved: rqlite's documented way to change a node's address).
func (st *Storage) ensureReachable(ctx context.Context, status domain.StorageStatus) error {
	st.mu.Lock()
	m := st.marker
	sup := st.sup
	st.mu.Unlock()
	cur, err := hostPortOf(m.HTTP)
	if err != nil {
		return err
	}
	if !cur.Addr().IsLoopback() {
		return nil
	}
	addr, err := st.localOverlay(ctx)
	if err != nil {
		return fmt.Errorf("%w: another Workspace Host can only reach this host's database over the workspace's private network, and this host has no address there: %v", domain.ErrConflict, err)
	}
	if addr.IsLoopback() {
		return nil // a "network" that is this machine (tests): there is nowhere to move to
	}
	if len(status.Hosts) != 1 {
		return fmt.Errorf("%w: this host's database listens on loopback but the cluster has %d members; its address cannot be changed while others rely on it", domain.ErrConflict, len(status.Hosts))
	}
	if !hasLocalAddr(addr) {
		return fmt.Errorf("%w: this host's network node is not running (its address %s is not up), and the database has to move onto it before another host can join", domain.ErrConflict, addr)
	}
	cfg, err := st.nodeConfig()
	if err != nil {
		return err
	}
	cfg.HTTPAddr = netip.AddrPortFrom(addr, uint16(st.cfg.StoragePort))
	cfg.RaftAddr = netip.AddrPortFrom(addr, uint16(st.cfg.StorageRaftPort))
	cfg.Join, cfg.NonVoter = nil, false
	st.log.Info("moving this host's database onto the workspace's private network so that other hosts can join it", "address", addr)
	mctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	if err := sup.Readdress(mctx, cfg); err != nil {
		return fmt.Errorf("%w: moving this host's database onto the private network: %v", domain.ErrConflict, err)
	}
	st.mu.Lock()
	st.marker.HTTP, st.marker.Raft = cfg.HTTPAddr.String(), cfg.RaftAddr.String()
	m = st.marker
	repl := st.repl
	st.mu.Unlock()
	if err := writeMarker(st.cfg, m); err != nil {
		return err
	}
	if repl != nil {
		_ = repl.Client().SetNodes([]string{cfg.HTTPAddr.String()})
	}
	return nil
}

// RemoveHost takes a node out of the cluster, safely.
func (st *Storage) RemoveHost(ctx context.Context, nodeID string) (string, error) {
	repl, sup := st.store(), func() *rqlite.Supervisor { st.mu.Lock(); defer st.mu.Unlock(); return st.sup }()
	if repl == nil || sup == nil {
		return "", fmt.Errorf("%w: this host's database is not up yet", domain.ErrConflict)
	}
	admin := sup.Admin()
	if admin == nil {
		return "", fmt.Errorf("%w: this host's database node is not running", domain.ErrConflict)
	}
	nodes, err := repl.Client().ClusterNodes(ctx)
	if err != nil {
		return "", fmt.Errorf("%w: the cluster cannot be asked who its members are: %v", domain.ErrConflict, err)
	}
	hosts := make([]domain.StorageHost, 0, len(nodes))
	found := false
	for _, n := range nodes {
		hosts = append(hosts, domain.StorageHost{NodeID: n.ID, Voter: n.Voter, Reachable: n.Reachable, Leader: n.Leader})
		found = found || n.ID == nodeID
	}
	if !found {
		return "", nil // not a member any more: nothing to change
	}
	if err := domain.CheckRemoval(hosts, nodeID); err != nil {
		return "", err
	}
	warning := domain.RemovalWarning(hosts, nodeID)
	rctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	for _, h := range hosts {
		if h.NodeID == nodeID && h.Leader {
			// Leadership moves first, so that the cluster is never without a leader because of the change.
			if err := whenThereIsALeader(rctx, func() error { return admin.StepDown(rctx, "") }); err != nil {
				return "", fmt.Errorf("%w: the leader could not hand over leadership before it was removed: %v", domain.ErrConflict, err)
			}
		}
	}
	if err := whenThereIsALeader(rctx, func() error { return admin.Remove(rctx, nodeID) }); err != nil {
		return "", fmt.Errorf("%w: the cluster did not take the membership change: %v", domain.ErrConflict, err)
	}
	for i := 0; i < 40; i++ {
		after, err := repl.Client().ClusterNodes(rctx)
		if err == nil {
			still := false
			for _, n := range after {
				still = still || n.ID == nodeID
			}
			if !still {
				st.log.Info("a host was removed from the database cluster", "node", nodeID)
				return warning, nil
			}
		}
		select {
		case <-rctx.Done():
			return "", rctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	return "", fmt.Errorf("%w: the cluster still lists %s after the removal", domain.ErrConflict, nodeID)
}

// Backup takes a backup now.
func (st *Storage) Backup(ctx context.Context) (domain.BackupStatus, error) {
	st.mu.Lock()
	br := st.backups
	st.mu.Unlock()
	if br == nil {
		return domain.BackupStatus{}, fmt.Errorf("%w: this host's database is not up yet", domain.ErrConflict)
	}
	return br.now(ctx)
}

func portString(p int) string { return strconv.Itoa(p) }

// whenThereIsALeader runs a membership change, trying again for a while if the cluster has no leader at that moment (one is
// being elected, as it is just after a node joined). A change refused for want of a leader was not applied, so asking again is safe.
func whenThereIsALeader(ctx context.Context, change func() error) error {
	deadline := time.Now().Add(20 * time.Second)
	for {
		err := change()
		if err == nil || !errors.Is(err, rqlite.ErrNoLeader) || time.Now().After(deadline) {
			return err
		}
		select {
		case <-ctx.Done():
			return err
		case <-time.After(300 * time.Millisecond):
		}
	}
}
