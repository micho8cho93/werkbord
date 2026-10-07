package rqlite

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// State is where the supervised node is.
type State string

const (
	StateStopped  State = "stopped"
	StateStarting State = "starting"
	StateRunning  State = "running"
	// StateRestarting: the node ended unexpectedly and will be started again.
	StateRestarting State = "restarting"
	StateFailed     State = "failed"
)

// Status describes the supervised program, for the API and for people. It holds no secret.
type Status struct {
	State     State     `json:"state"`
	PID       int       `json:"pid,omitempty"`
	StartedAt time.Time `json:"startedAt,omitempty"`
	Restarts  int       `json:"restarts"`
	LastError string    `json:"lastError,omitempty"`
	// Version is the release this build of Werkbord Team ships; ReportedVersion what the program says it is.
	Version         string `json:"version"`
	ReportedVersion string `json:"reportedVersion,omitempty"`
	BinarySHA256    string `json:"binarySha256,omitempty"`
	// HashPinned says whether the program was checked against a hash in the manifest (a published release),
	// or only against the build record of a program built from the pinned source.
	HashPinned bool `json:"hashPinned"`
	// Tail is the last lines the node logged.
	Tail []string `json:"tail,omitempty"`
}

// Options configures a Supervisor.
type Options struct {
	// BinaryDirs are the absolute directories the pinned program may have been shipped in, searched
	// in order. The user's PATH is never searched.
	BinaryDirs []string
	Log        *slog.Logger
	Now        func() time.Time
	// NoRestart leaves a node that ended unexpectedly ended. A production host restarts it; a test that
	// kills a node to see what the others do needs it to stay dead.
	NoRestart bool
	// StartTimeout is how long Start waits for the node's HTTP API (default one minute); StopTimeout how
	// long Stop waits for a graceful shutdown before ending the process (default 30 seconds).
	StartTimeout, StopTimeout time.Duration
	// RestartBackoff is the first delay before a restart (default one second, doubling to a minute).
	RestartBackoff time.Duration

	// artifact is set by tests only: a stand-in program with its own pin.
	artifact *Artifact
}

// Supervisor starts, watches and stops the one database program. It is not a way to run anything
// else: its only entry that starts a process is Start, which starts the pinned, verified rqlited
// with arguments this package builds.
type Supervisor struct {
	opts Options
	art  Artifact
	log  *slog.Logger

	mu     sync.Mutex
	status Status
	cfg    *NodeConfig
	v      verified
	cancel context.CancelFunc
	done   chan struct{}
	logs   *tailBuffer
}

// New builds a Supervisor for this platform.
func New(opts Options) (*Supervisor, error) {
	var art Artifact
	if opts.artifact != nil {
		art = *opts.artifact
	} else {
		var err error
		if art, err = ThisPlatform(); err != nil {
			return nil, err
		}
	}
	log := opts.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.StartTimeout == 0 {
		opts.StartTimeout = time.Minute
	}
	if opts.StopTimeout == 0 {
		opts.StopTimeout = 30 * time.Second
	}
	if opts.RestartBackoff == 0 {
		opts.RestartBackoff = time.Second
	}
	return &Supervisor{opts: opts, art: art, log: log.With("component", "database"), status: Status{State: StateStopped, Version: Version}, logs: newTail(300)}, nil
}

// Status returns a snapshot.
func (s *Supervisor) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.status
	st.Tail = s.logs.lines()
	return st
}

// Admin returns the client for the running node, or nil when none is running.
func (s *Supervisor) Admin() *Admin {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cfg == nil {
		return nil
	}
	return NewAdmin(s.cfg.HTTPAddr, s.cfg.Credentials)
}

// Config returns the configuration the node was started with (credentials included), and whether it runs.
func (s *Supervisor) Config() (NodeConfig, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cfg == nil {
		return NodeConfig{}, false
	}
	return *s.cfg, true
}

// Prepare writes a node's files (its credentials, in a private directory) and checks the program,
// without starting it. Start does the same.
func prepare(cfg NodeConfig, write bool) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	if !write {
		return nil
	}
	for _, d := range []string{cfg.DataDir, cfg.nodeDir(), cfg.binDir()} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return err
		}
		if err := checkOwnedPrivate(d, true); err != nil {
			return err
		}
	}
	auth, err := cfg.Credentials.authFile()
	if err != nil {
		return err
	}
	return writeAtomic(cfg.authPath(), auth, 0o600)
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(tmp.Name())
		}
	}()
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return err
	}
	ok = true
	return nil
}

// Validate checks a configuration the way Start does, without starting anything.
func Validate(cfg NodeConfig) error { return prepare(cfg, false) }

// Check finds the database program, verifies it against the pin and has it report its version, without starting a node:
// what a command that is about to need the program does first, so that it fails before it has changed anything. dataDir is
// the directory the supervisor keeps its verified copy in (it is made private).
func (s *Supervisor) Check(ctx context.Context, dataDir string) (Status, error) {
	cfg := NodeConfig{DataDir: dataDir}
	if dataDir == "" || !filepath.IsAbs(dataDir) || filepath.Clean(dataDir) != dataDir {
		return Status{}, errors.New("rqlite: the data directory must be a clean absolute path")
	}
	for _, d := range []string{cfg.DataDir, cfg.binDir()} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return Status{}, err
		}
		if err := checkOwnedPrivate(d, true); err != nil {
			return Status{}, err
		}
	}
	v, err := locate(s.opts.BinaryDirs, cfg.binDir(), s.art)
	if err != nil {
		return Status{}, err
	}
	reported, err := verifyVersion(ctx, v)
	if err != nil {
		return Status{}, err
	}
	return Status{State: StateStopped, Version: Version, ReportedVersion: reported, BinarySHA256: v.SHA256, HashPinned: v.Pinned}, nil
}

// Start starts the node described by cfg. It verifies the program against the pin and against its
// own report of its version, writes the node's files, starts it and returns once the node's HTTP API
// answers. Whether the node has joined a cluster and caught up is a different question (WaitReady). A
// node that ends later is restarted with a growing delay until Stop.
func (s *Supervisor) Start(ctx context.Context, cfg NodeConfig) error {
	s.mu.Lock()
	if s.cancel != nil {
		s.mu.Unlock()
		return errors.New("rqlite: already started")
	}
	if err := prepare(cfg, true); err != nil {
		defer s.mu.Unlock()
		return s.fail(err)
	}
	v, err := locate(s.opts.BinaryDirs, cfg.binDir(), s.art)
	if err != nil {
		defer s.mu.Unlock()
		return s.fail(err)
	}
	reported, err := verifyVersion(ctx, v)
	if err != nil {
		defer s.mu.Unlock()
		return s.fail(err)
	}
	s.logs.reset()
	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	first, err := s.spawn(runCtx, v, cfg)
	if err != nil {
		cancel()
		defer s.mu.Unlock()
		return s.fail(err)
	}
	done := make(chan struct{})
	s.cfg, s.v, s.cancel, s.done = &cfg, v, cancel, done
	s.status = Status{State: StateStarting, Version: Version, ReportedVersion: reported, BinarySHA256: v.SHA256, HashPinned: v.Pinned, PID: first.cmd.Process.Pid, StartedAt: s.opts.Now()}
	exited := make(chan error, 1)
	go s.supervise(runCtx, v, cfg, first, exited)
	s.mu.Unlock()

	abandon := func() {
		cancel()
		<-done
		s.mu.Lock()
		s.cancel, s.cfg = nil, nil
		s.mu.Unlock()
	}
	admin := NewAdmin(cfg.HTTPAddr, cfg.Credentials)
	deadline := time.NewTimer(s.opts.StartTimeout)
	defer deadline.Stop()
	tick := time.NewTicker(150 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case err := <-exited:
			abandon()
			s.mu.Lock()
			defer s.mu.Unlock()
			return s.failLocked(fmt.Errorf("the database program ended at once: %w%s", err, s.hint()))
		case <-ctx.Done():
			abandon()
			return ctx.Err()
		case <-deadline.C:
			abandon()
			s.mu.Lock()
			defer s.mu.Unlock()
			return s.failLocked(fmt.Errorf("the database program did not answer within %s%s", s.opts.StartTimeout, s.hint()))
		case <-tick.C:
			pctx, pcancel := context.WithTimeout(ctx, 2*time.Second)
			err := admin.Alive(pctx)
			pcancel()
			if err == nil {
				s.mu.Lock()
				if s.status.State == StateStarting {
					s.status.State = StateRunning
				}
				s.mu.Unlock()
				return nil
			}
		}
	}
}

// WaitReady waits until the node is part of a cluster that has a leader and, with sync, has applied
// everything the leader has committed.
func (s *Supervisor) WaitReady(ctx context.Context, sync bool) error {
	a := s.Admin()
	if a == nil {
		return errors.New("rqlite: not started")
	}
	var last error
	for {
		pctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		last = a.Ready(pctx, sync)
		cancel()
		if last == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("%w (last: %v)", ctx.Err(), last)
		case <-time.After(300 * time.Millisecond):
		}
	}
}

func (s *Supervisor) fail(err error) error {
	s.status = Status{State: StateFailed, Version: Version, LastError: err.Error()}
	return err
}

func (s *Supervisor) failLocked(err error) error {
	s.status.State, s.status.LastError, s.status.PID = StateFailed, err.Error(), 0
	return err
}

// hint turns what the node logged into advice when it is a well-known problem.
func (s *Supervisor) hint() string {
	text := strings.ToLower(strings.Join(s.logs.lines(), "\n"))
	switch {
	case strings.Contains(text, "address already in use"):
		return " (the database's port is already in use by another program)"
	case strings.Contains(text, "can't assign requested address"), strings.Contains(text, "cannot assign requested address"):
		return " (the database's address is not on this machine yet: is the workspace's network node up?)"
	}
	if t := s.logs.lines(); len(t) > 0 {
		return " (last line: " + t[len(t)-1] + ")"
	}
	return ""
}

type spawnedProc struct {
	cmd  *exec.Cmd
	wait chan error
}

// spawn starts the verified program once, for a configuration.
func (s *Supervisor) spawn(ctx context.Context, v verified, cfg NodeConfig) (*spawnedProc, error) {
	if err := v.reverify(s.art); err != nil {
		return nil, err
	}
	cmd := command(ctx, v, cfg.args()...)
	cmd.Dir = cfg.DataDir
	cmd.Stdout, cmd.Stderr = s.logs, s.logs
	// A graceful stop: rqlite steps down if it leads, flushes and closes its files.
	cmd.Cancel = func() error {
		if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
			return cmd.Process.Kill()
		}
		return nil
	}
	cmd.WaitDelay = s.opts.StopTimeout
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("rqlite: starting the database program: %w", err)
	}
	sp := &spawnedProc{cmd: cmd, wait: make(chan error, 1)}
	go func() { sp.wait <- cmd.Wait() }()
	return sp, nil
}
