package nebula

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/slackhq/nebula/cert"

	"devboard/internal/team/infra/overlay"
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

// Status describes the node, for the API and for people. It holds no secret.
type Status struct {
	State        State     `json:"state"`
	PID          int       `json:"pid,omitempty"`
	StartedAt    time.Time `json:"startedAt,omitempty"`
	Restarts     int       `json:"restarts"`
	LastError    string    `json:"lastError,omitempty"`
	Version      string    `json:"version"`
	BinarySHA256 string    `json:"binarySha256,omitempty"`
	// Tail is the last lines the node logged (no secret is ever logged by it).
	Tail []string `json:"tail,omitempty"`
}

// Options configures a Supervisor.
type Options struct {
	// BinaryDirs are the absolute directories the pinned program may have been shipped
	// in, searched in order. The user's PATH is never searched.
	BinaryDirs []string
	Log        *slog.Logger
	// Now is the clock (tests).
	Now func() time.Time
	// artifact, grace and healthy are set by tests only: a stand-in program with its own
	// pin, and shorter times.
	artifact                   *Artifact
	grace, healthyFor, backoff time.Duration
}

// NebulaConfig is everything StartNebula needs, and all it takes: there is no
// field for a program, an argument, an environment variable or a path outside
// DataDir. The files are written by the supervisor, from the PEM text here.
type NebulaConfig struct {
	// DataDir is a directory the supervisor owns: it makes it private, keeps the node's
	// files and the verified copy of the program in it, and writes nowhere else. It
	// must be absolute and owned by the user running Werkbord Team.
	DataDir string
	// Node describes the node. Its certificate and key paths are the supervisor's to
	// set and are overwritten.
	Node overlay.NodeSpec
	// CACertPEM and NodeCertPEM are the authority's and the node's certificates;
	// NodeKeyPEM is the node's private network key.
	CACertPEM, NodeCertPEM, NodeKeyPEM []byte
}

// Supervisor starts, watches and stops the one network program. It is not a way to
// run anything else: its only entry that starts a process is StartNebula, which starts
// the pinned, verified Nebula with arguments this package builds.
type Supervisor struct {
	opts Options
	art  Artifact
	log  *slog.Logger

	mu     sync.Mutex
	status Status
	cfg    *NebulaConfig
	cancel context.CancelFunc
	done   chan struct{}
	proc   *os.Process
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
	if opts.grace == 0 {
		opts.grace = graceStart
	}
	if opts.healthyFor == 0 {
		opts.healthyFor = healthy
	}
	if opts.backoff == 0 {
		opts.backoff = time.Second
	}
	return &Supervisor{opts: opts, art: art, log: log.With("component", "overlay-network"), status: Status{State: StateStopped, Version: Version}, logs: newTail(200)}, nil
}

// Status returns a snapshot.
func (s *Supervisor) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.status
	st.Tail = s.logs.lines()
	return st
}

// Validate checks a configuration the way StartNebula does, without starting anything.
func Validate(cfg NebulaConfig, now time.Time) error {
	_, err := prepare(cfg, now, false)
	return err
}

type prepared struct {
	dir                       string
	spec                      overlay.NodeSpec
	configPath                string
	caPath, certPath, keyPath string
}

// prepare validates cfg and (when write is set) writes its files into DataDir.
func prepare(cfg NebulaConfig, now time.Time, write bool) (prepared, error) {
	p := prepared{dir: cfg.DataDir}
	if cfg.DataDir == "" || !filepath.IsAbs(cfg.DataDir) || filepath.Clean(cfg.DataDir) != cfg.DataDir {
		return p, errors.New("nebula: the data directory must be a clean absolute path")
	}
	if strings.ContainsAny(cfg.DataDir, "\"\r\n\x00") {
		return p, errors.New("nebula: the data directory has a character a configuration cannot hold")
	}
	if write {
		if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
			return p, err
		}
		if err := checkOwnedPrivate(cfg.DataDir, true); err != nil {
			return p, err
		}
	}
	ca, _, err := cert.UnmarshalCertificateFromPEM(cfg.CACertPEM)
	if err != nil || !ca.IsCA() {
		return p, errors.New("nebula: the authority's certificate is not a network authority's certificate")
	}
	node, _, err := cert.UnmarshalCertificateFromPEM(cfg.NodeCertPEM)
	if err != nil || node.IsCA() {
		return p, errors.New("nebula: the node's certificate is not a node certificate")
	}
	pool := cert.NewCAPool()
	if err := pool.AddCA(ca); err != nil {
		return p, fmt.Errorf("nebula: the authority's certificate: %w", err)
	}
	if _, err := pool.VerifyCertificate(now, node); err != nil {
		return p, fmt.Errorf("nebula: the node's certificate is not valid under the authority: %w", err)
	}
	key, _, curve, err := cert.UnmarshalPrivateKeyFromPEM(cfg.NodeKeyPEM)
	if err != nil || node.VerifyPrivateKey(curve, key) != nil {
		return p, errors.New("nebula: the node's private key is not the one for its certificate")
	}
	p.caPath, p.certPath, p.keyPath = filepath.Join(cfg.DataDir, "ca.crt"), filepath.Join(cfg.DataDir, "node.crt"), filepath.Join(cfg.DataDir, "node.key")
	p.configPath = filepath.Join(cfg.DataDir, "config.yml")
	p.spec = cfg.Node
	p.spec.CA, p.spec.Cert, p.spec.Key = p.caPath, p.certPath, p.keyPath
	if n := node.Networks(); len(n) != 1 || n[0].Addr() != p.spec.Addr {
		return p, errors.New("nebula: the node's address does not match its certificate's")
	}
	rendered, err := overlay.Render(p.spec)
	if err != nil {
		return p, err
	}
	if write {
		for _, f := range []struct {
			path string
			data []byte
			mode os.FileMode
		}{{p.caPath, cfg.CACertPEM, 0o600}, {p.certPath, cfg.NodeCertPEM, 0o600}, {p.keyPath, cfg.NodeKeyPEM, 0o600}, {p.configPath, rendered, 0o600}} {
			if err := writeAtomic(f.path, f.data, f.mode); err != nil {
				return p, err
			}
		}
	}
	return p, nil
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

// graceStart is how long a freshly started node must stay up for StartNebula to
// report success; one that ends sooner failed to start (bad configuration, a port in
// use, no right to create the interface) and is reported, not retried.
const graceStart = 2 * time.Second

// healthy is how long a node must have run for a later exit to count as a crash worth
// restarting at once rather than a crash loop.
const healthy = 30 * time.Second

// StartNebula starts the node described by cfg. It verifies the program against the
// pin, writes the node's files, starts it and returns once it has stayed up past
// graceStart. A node that ends later is restarted with a growing delay until Stop.
func (s *Supervisor) StartNebula(ctx context.Context, cfg NebulaConfig) error {
	s.mu.Lock()
	if s.cancel != nil {
		s.mu.Unlock()
		return errors.New("nebula: already started")
	}
	p, err := prepare(cfg, s.opts.Now(), true)
	if err != nil {
		defer s.mu.Unlock()
		return s.fail(err)
	}
	v, err := locate(s.opts.BinaryDirs, filepath.Join(cfg.DataDir, "bin"), s.art)
	if err != nil {
		defer s.mu.Unlock()
		return s.fail(err)
	}
	s.logs.reset()
	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	first, err := s.spawn(runCtx, v, p)
	if err != nil {
		cancel()
		defer s.mu.Unlock()
		return s.fail(err)
	}
	done := make(chan struct{})
	s.cfg, s.cancel, s.done = &cfg, cancel, done
	s.status = Status{State: StateStarting, Version: Version, BinarySHA256: v.SHA256, PID: first.cmd.Process.Pid, StartedAt: s.opts.Now()}
	s.proc = first.cmd.Process
	exited := make(chan error, 1)
	go s.supervise(runCtx, v, p, first, exited)
	s.mu.Unlock()

	abandon := func() {
		cancel()
		<-done
		s.mu.Lock()
		s.cancel, s.proc, s.cfg = nil, nil, nil
		s.mu.Unlock()
	}
	select {
	case err := <-exited:
		// It ended inside the grace period: it did not start.
		abandon()
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.failLocked(fmt.Errorf("the network program ended at once: %w%s", err, s.hint()))
	case <-time.After(s.opts.grace):
		s.mu.Lock()
		if s.status.State == StateStarting {
			s.status.State = StateRunning
		}
		s.mu.Unlock()
		return nil
	case <-ctx.Done():
		abandon()
		return ctx.Err()
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
	case strings.Contains(text, "operation not permitted"), strings.Contains(text, "permission denied"), strings.Contains(text, "must be root"), strings.Contains(text, "access is denied"):
		return " (creating the network interface needs more privileges than Werkbord Team has: see \"Privileges\" in docs/TEAM_NETWORK.md)"
	case strings.Contains(text, "address already in use"):
		return " (the network's UDP port is already in use by another program)"
	}
	if t := s.logs.lines(); len(t) > 0 {
		return " (last line: " + t[len(t)-1] + ")"
	}
	return ""
}

type spawned struct {
	cmd  *exec.Cmd
	wait chan error
}

// spawn starts the verified program once. This is the only place in the package
// that starts a process; internal/archtest holds it to one call, with a constant
// program name and arguments built from constants and one path this package made.
func (s *Supervisor) spawn(ctx context.Context, v verified, p prepared) (*spawned, error) {
	if err := v.reverify(s.art); err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, "nebula", "-config", p.configPath)
	// exec resolved the bare name through PATH; what runs is the verified private copy, and nothing else.
	cmd.Path, cmd.Err = v.Path, nil
	cmd.Dir = p.dir
	cmd.Env = []string{} // the node needs nothing from the environment, and gets nothing
	cmd.Stdout, cmd.Stderr = s.logs, s.logs
	cmd.Cancel = func() error {
		if err := cmd.Process.Signal(os.Interrupt); err != nil {
			return cmd.Process.Kill()
		}
		return nil
	}
	cmd.WaitDelay = 10 * time.Second
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("nebula: starting the network program: %w", err)
	}
	sp := &spawned{cmd: cmd, wait: make(chan error, 1)}
	go func() { sp.wait <- cmd.Wait() }()
	return sp, nil
}

// supervise watches the process and restarts it when it ends unexpectedly.
func (s *Supervisor) supervise(ctx context.Context, v verified, p prepared, cur *spawned, firstExit chan<- error) {
	defer close(s.done)
	started := s.opts.Now()
	delay := s.opts.backoff
	reported := false
	for {
		err := <-cur.wait
		if ctx.Err() != nil {
			s.mu.Lock()
			s.status.State, s.status.PID = StateStopped, 0
			s.proc = nil
			s.mu.Unlock()
			return
		}
		if !reported && s.opts.Now().Sub(started) < s.opts.grace {
			reported = true
			if err == nil {
				err = errors.New("exited")
			}
			firstExit <- err
			return
		}
		reported = true
		if s.opts.Now().Sub(started) > s.opts.healthyFor {
			delay = s.opts.backoff
		}
		s.mu.Lock()
		s.status.State, s.status.PID = StateRestarting, 0
		s.status.Restarts++
		s.status.LastError = fmt.Sprintf("the network program ended (%v); restarting in %s%s", err, delay, s.hint())
		s.proc = nil
		s.mu.Unlock()
		s.log.Warn("the network program ended unexpectedly; restarting", "err", err, "in", delay)
		select {
		case <-ctx.Done():
			s.mu.Lock()
			s.status.State = StateStopped
			s.mu.Unlock()
			return
		case <-time.After(delay):
		}
		if delay < time.Minute {
			delay *= 2
		}
		next, spawnErr := s.spawn(ctx, v, p)
		s.mu.Lock()
		if spawnErr != nil {
			s.status.State, s.status.LastError = StateFailed, spawnErr.Error()
			s.mu.Unlock()
			s.log.Error("the network program could not be restarted", "err", spawnErr)
			// Try again after the next delay: the file may be put right.
			cur = &spawned{wait: make(chan error, 1)}
			cur.wait <- spawnErr
			started = s.opts.Now()
			continue
		}
		s.status.State, s.status.PID, s.status.StartedAt = StateRunning, next.cmd.Process.Pid, s.opts.Now()
		s.proc = next.cmd.Process
		s.mu.Unlock()
		cur, started = next, s.opts.Now()
	}
}

// needsRestart says whether going from a to b changes something the node reads only
// at startup.
func needsRestart(a, b overlay.NodeSpec) bool {
	return a.Port != b.Port || a.Discovery != b.Discovery || a.Relay != b.Relay || a.TunDisabled != b.TunDisabled || a.TunDev != b.TunDev || a.MTU != b.MTU || a.Stats != b.Stats || a.Addr != b.Addr
}

// Reload applies a changed configuration (a renewed certificate, a longer blocklist,
// another discovery host, a new policy) to the running node: it rewrites the files and
// asks the node to re-read them, or restarts it when something changed that is only read at start.
func (s *Supervisor) Reload(ctx context.Context, cfg NebulaConfig) error {
	s.mu.Lock()
	if s.cancel == nil || s.cfg == nil {
		s.mu.Unlock()
		return errors.New("nebula: not started")
	}
	old := s.cfg
	if cfg.DataDir != old.DataDir {
		s.mu.Unlock()
		return errors.New("nebula: the data directory cannot change while running")
	}
	restart := needsRestart(old.Node, cfg.Node)
	p, err := prepare(cfg, s.opts.Now(), true)
	if err != nil {
		s.mu.Unlock()
		return err
	}
	_ = p
	s.cfg = &cfg
	proc := s.proc
	s.mu.Unlock()
	if restart || proc == nil {
		if err := s.Stop(ctx); err != nil {
			return err
		}
		return s.StartNebula(ctx, cfg)
	}
	if err := signalReload(proc); err != nil {
		if err := s.Stop(ctx); err != nil {
			return err
		}
		return s.StartNebula(ctx, cfg)
	}
	return nil
}

// Stop stops the node and waits for it to end. Stopping a stopped supervisor does nothing.
func (s *Supervisor) Stop(ctx context.Context) error {
	s.mu.Lock()
	cancel, done := s.cancel, s.done
	s.mu.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	select {
	case <-done:
	case <-ctx.Done():
		return ctx.Err()
	}
	s.mu.Lock()
	s.cancel, s.proc, s.cfg = nil, nil, nil
	s.status.State, s.status.PID = StateStopped, 0
	s.mu.Unlock()
	return nil
}

// ---- the node's own log, kept short ----

type tailBuffer struct {
	mu   sync.Mutex
	max  int
	buf  []string
	part bytes.Buffer
}

func newTail(max int) *tailBuffer { return &tailBuffer{max: max} }

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.part.Write(p)
	for {
		line, err := t.part.ReadString('\n')
		if err != nil {
			t.part.WriteString(line) // not a whole line yet
			break
		}
		t.buf = append(t.buf, strings.TrimRight(line, "\r\n"))
		if len(t.buf) > t.max {
			t.buf = t.buf[len(t.buf)-t.max:]
		}
	}
	if t.part.Len() > 64<<10 {
		t.part.Reset()
	}
	return len(p), nil
}

func (t *tailBuffer) lines() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]string(nil), t.buf...)
}

func (t *tailBuffer) reset() {
	t.mu.Lock()
	t.buf = nil
	t.part.Reset()
	t.mu.Unlock()
}
