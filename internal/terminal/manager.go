// Package terminal owns native Individual coding-agent terminals, independently of UI connections.
package terminal

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"werkbord/internal/agent"
	"werkbord/internal/agentcatalog"
	"werkbord/internal/domain"
	"werkbord/internal/gitrepo"
	"werkbord/internal/service"
	"werkbord/internal/store"
)

const BufferSize = 512 << 10
const MaxInput = 16 << 10
const MaxSessions = 8

// MaxRecords bounds the retained metadata. MaxInputClients bounds the input receipts one process keeps; a receipt is only
// ever evicted after ReceiptIdle without use, far beyond any HTTP or relay retry, so a long-lived terminal cannot be
// locked out of input by many short-lived browser tabs while a duplicated request still cannot be written twice.
const MaxRecords = 64
const MaxInputClients = 64
const ReceiptIdle = time.Hour

type Metadata struct {
	ID         string    `json:"id"`
	ProjectID  string    `json:"projectId"`
	AgentID    string    `json:"agentId"`
	Device     string    `json:"device"`
	Repository string    `json:"repository"`
	State      string    `json:"state"`
	Phones     []string  `json:"phones,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
}
type record struct {
	Metadata
	Root      string            `json:"root"`
	CommonDir string            `json:"commonDir"`
	Process   agent.ProcessInfo `json:"process"`
}
type session struct {
	record
	p             *process
	buffer        []byte
	base          uint64
	changed       chan struct{}
	done          chan struct{}
	input         chan []byte
	receipts      map[string]inputReceipt
	answered      uint64
	grantRevision uint64
}
type Options struct {
	Projects *service.Projects
	Settings *service.Settings
	Agents   *agent.Registry
	// Commands is trusted controller configuration, never request data. No arguments are accepted.
	Commands  map[string]string
	Device    string
	StatePath string
	// Log reports a damaged state file that was set aside. Optional.
	Log *slog.Logger
}
type Manager struct {
	mu       sync.Mutex
	opt      Options
	sessions map[string]*session
	closed   bool
	readers  chan struct{}
	now      func() time.Time
}

func New(opt Options) (*Manager, error) {
	m := &Manager{opt: opt, sessions: map[string]*session{}, readers: make(chan struct{}, 32), now: time.Now}
	b, err := os.ReadFile(opt.StatePath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err == nil {
		var records []record
		if err = json.Unmarshal(b, &records); err != nil {
			// A file cut short by a crash describes processes that can no longer be found. It must not stop the controller,
			// which also runs tasks: keep it for inspection and start with no terminals.
			damaged := opt.StatePath + ".damaged"
			if renameErr := os.Rename(opt.StatePath, damaged); renameErr != nil {
				return nil, fmt.Errorf("terminal metadata is damaged (%v) and could not be set aside: %w", err, renameErr)
			}
			if opt.Log != nil {
				opt.Log.Warn("terminal metadata was damaged; set aside and started with no terminals", "file", damaged, "error", err)
			}
			records = nil
		}
		// Every running record is reaped, however many are kept: an old process must not outlive its record.
		sort.SliceStable(records, func(i, j int) bool { return records[i].CreatedAt.After(records[j].CreatedAt) })
		for i, r := range records {
			if r.State == "running" {
				_, _ = agent.Reap(r.Process.PID, r.Process.ID, 200*time.Millisecond)
				r.State = "interrupted"
			}
			if i >= MaxRecords {
				continue
			}
			r.Process = agent.ProcessInfo{}
			m.sessions[r.ID] = &session{record: r, changed: make(chan struct{}), done: make(chan struct{})}
		}
		if err = m.persist(); err != nil {
			return nil, err
		}
	}
	return m, nil
}
func (m *Manager) Device() string { return m.opt.Device }
func (m *Manager) Enabled() bool  { return supported }
func (m *Manager) AuthorizedDevice(ctx context.Context) bool {
	if !supported || m.opt.Settings == nil {
		return false
	}
	return m.opt.Settings.Store.View(ctx, func(tx store.Tx) error {
		r, e := tx.Runners().Get(ctx, m.opt.Device)
		if e != nil {
			return e
		}
		if r.Kind != domain.RunnerLocal || r.Disabled || r.Removed {
			return domain.ErrForbidden
		}
		return nil
	}) == nil
}
func (m *Manager) persist() error {
	if m.opt.StatePath == "" {
		return nil
	}
	records := make([]record, 0, len(m.sessions))
	for _, s := range m.sessions {
		records = append(records, s.record)
	}
	b, err := json.Marshal(records)
	if err != nil {
		return err
	}
	// Written, flushed and then renamed, so a crash leaves the previous file or the new one, never a cut-short one.
	tmp := m.opt.StatePath + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, m.opt.StatePath)
}
func (m *Manager) Create(ctx context.Context, projectID, agentID, device string, cols, rows int) (Metadata, error) {
	if !m.AuthorizedDevice(ctx) || !agentcatalog.NativeTerminal(agentID) || device != m.opt.Device {
		return Metadata{}, domain.ErrForbidden
	}
	if !validSize(cols, rows) {
		return Metadata{}, domain.ErrInvalid
	}
	prefs, err := m.opt.Settings.Selection(ctx)
	if err != nil {
		return Metadata{}, err
	}
	pref := prefs.Agents[agentID]
	if !pref.DirectCLI {
		return Metadata{}, domain.ErrForbidden
	}
	a, lookupErr := m.opt.Agents.Get(agentID)
	if lookupErr != nil {
		return Metadata{}, domain.ErrForbidden
	}
	detected := a.Detect(ctx)
	if !detected.Installed || !detected.Available || detected.SignIn == domain.SignedOut || detected.SignIn != domain.SignedIn && !pref.CLISignedIn {
		return Metadata{}, domain.ErrForbidden
	}
	project, err := m.opt.Projects.Get(ctx, projectID)
	if err != nil {
		return Metadata{}, err
	}
	if project.Inactive() || !project.HasRepository() {
		return Metadata{}, domain.ErrForbidden
	}
	root, err := filepath.EvalSymlinks(project.RepoPath)
	if err != nil {
		return Metadata{}, domain.ErrInvalid
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return Metadata{}, err
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return Metadata{}, domain.ErrInvalid
	}
	if project.Repository == nil || root != project.RepoPath {
		return Metadata{}, domain.ErrForbidden
	}
	repo, err := (&gitrepo.CLI{}).Inspect(ctx, root)
	if err != nil {
		return Metadata{}, err
	}
	if repo.RootPath != root || repo.CommonDir != project.Repository.CommonDir {
		return Metadata{}, domain.ErrForbidden
	}

	command := m.opt.Commands[agentID]
	if command == "" {
		command = agentcatalog.NativeCommand(agentID)
	}
	path, err := exec.LookPath(command)
	if err != nil {
		return Metadata{}, domain.ErrInvalid
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return Metadata{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return Metadata{}, domain.ErrConflict
	}
	count := 0
	for _, s := range m.sessions {
		if s.State == "running" {
			count++
		}
	}
	if count >= MaxSessions {
		return Metadata{}, fmt.Errorf("%w: terminal session limit reached", domain.ErrConflict)
	}
	if len(m.sessions) >= MaxRecords {
		var oldest *session
		for _, s := range m.sessions {
			if s.State != "running" && (oldest == nil || s.CreatedAt.Before(oldest.CreatedAt)) {
				oldest = s
			}
		}
		if oldest != nil {
			delete(m.sessions, oldest.ID)
		}
	}
	var token [24]byte
	if _, err = rand.Read(token[:]); err != nil {
		return Metadata{}, err
	}
	id := hex.EncodeToString(token[:])
	p, err := start(path, root, cols, rows)
	if err != nil {
		return Metadata{}, err
	}
	s := &session{record: record{Metadata: Metadata{ID: id, ProjectID: projectID, AgentID: agentID, Device: device, Repository: project.Name, State: "running", CreatedAt: time.Now().UTC()}, Root: root, CommonDir: project.Repository.CommonDir, Process: p.info()}, p: p, changed: make(chan struct{}), done: make(chan struct{}), input: make(chan []byte, 8)}
	m.sessions[id] = s
	if err = m.persist(); err != nil {
		p.kill()
		_ = p.wait()
		p.close()
		delete(m.sessions, id)
		return Metadata{}, err
	}
	readDone := make(chan struct{})
	go func() { m.read(s); close(readDone) }()
	go func() {
		_ = s.p.wait()
		m.mu.Lock()
		s.p.kill()
		s.Process = agent.ProcessInfo{}
		m.notify(s)
		m.mu.Unlock()
		select {
		case <-readDone:
		case <-time.After(250 * time.Millisecond):
			s.p.close()
			<-readDone
		}
		s.p.close()
		m.mu.Lock()
		if s.State == "running" {
			s.State = "exited"
		}
		m.notify(s)
		_ = m.persist()
		close(s.done)
		m.mu.Unlock()
	}()
	go m.write(s)
	return s.Metadata, nil
}
func validSize(cols, rows int) bool  { return cols >= 2 && cols <= 500 && rows >= 2 && rows <= 300 }
func (m *Manager) notify(s *session) { close(s.changed); s.changed = make(chan struct{}) }
func (m *Manager) read(s *session) {
	buf := make([]byte, 16384)
	for {
		n, err := s.p.read(buf)
		if n > 0 {
			m.mu.Lock()
			s.buffer = append(s.buffer, buf[:n]...)
			if len(s.buffer) > BufferSize {
				drop := len(s.buffer) - BufferSize
				s.buffer = append([]byte(nil), s.buffer[drop:]...)
				s.base += uint64(drop)
			}
			m.notify(s)
			m.mu.Unlock()
		}
		if err != nil {
			break
		}
	}
}

func (m *Manager) write(s *session) {
	for {
		select {
		case <-s.done:
			return
		case data := <-s.input:
			if err := s.p.write(data); err != nil {
				return
			}
		}
	}
}
func (m *Manager) List() []Metadata {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Metadata{}
	for _, s := range m.sessions {
		out = append(out, s.Metadata)
	}
	return out
}
func (m *Manager) get(project, id string) (*session, error) {
	s := m.sessions[id]
	if s == nil || s.ProjectID != project {
		return nil, domain.ErrNotFound
	}
	return s, nil
}
func (m *Manager) Input(project, id string, data []byte) error {
	if len(data) == 0 || len(data) > MaxInput {
		return domain.ErrInvalid
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	s, err := m.get(project, id)
	if err != nil {
		return err
	}
	if s.State != "running" {
		return domain.ErrConflict
	}
	select {
	case s.input <- append([]byte(nil), data...):
		return nil
	default:
		return fmt.Errorf("%w: input queue full", domain.ErrConflict)
	}
}
func (m *Manager) Resize(project, id string, cols, rows int) error {
	if !validSize(cols, rows) {
		return domain.ErrInvalid
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	s, err := m.get(project, id)
	if err != nil {
		return err
	}
	if s.State != "running" {
		return domain.ErrConflict
	}
	return s.p.resize(cols, rows)
}
func (m *Manager) Stop(project, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, err := m.get(project, id)
	if err != nil {
		return err
	}
	if s.State == "running" {
		s.State = "stopped"
		if s.Process.PID != 0 {
			s.p.kill()
		}
		s.p.close()
		m.notify(s)
	}
	return m.persist()
}

type Output struct {
	Metadata Metadata `json:"session"`
	Data     []byte   `json:"data"`
	Next     uint64   `json:"next"`
	Reset    bool     `json:"reset"`
	Replay   bool     `json:"replay"`
}

// Output is a bounded long poll. Offsets count raw PTY bytes; []byte is transported as base64.
func (m *Manager) Output(ctx context.Context, project, id string, after uint64) (Output, error) {
	return m.output(ctx, project, id, after, 20*time.Second)
}

// OutputNow probes current state without waiting for new bytes after a reconnect.
func (m *Manager) OutputNow(ctx context.Context, project, id string, after uint64) (Output, error) {
	return m.output(ctx, project, id, after, 0)
}
func (m *Manager) output(ctx context.Context, project, id string, after uint64, wait time.Duration) (Output, error) {
	select {
	case m.readers <- struct{}{}:
		defer func() { <-m.readers }()
	default:
		return Output{}, domain.ErrConflict
	}
	var revision uint64
	first := true
	timer := time.NewTimer(wait)
	defer timer.Stop()
	for {
		m.mu.Lock()
		s, err := m.get(project, id)
		if err != nil {
			m.mu.Unlock()
			return Output{}, err
		}
		if first {
			revision = s.grantRevision
			first = false
		}
		end := s.base + uint64(len(s.buffer))
		reset := after < s.base || after > end
		if reset {
			after = s.base
		}
		if after < end || s.State != "running" || revision != s.grantRevision {
			next := min(end, after+65536)
			replay := after < s.answered
			if replay {
				next = min(next, s.answered)
			}
			data := append([]byte(nil), s.buffer[after-s.base:next-s.base]...)
			out := Output{Metadata: s.Metadata, Data: data, Next: next, Reset: reset, Replay: replay}
			m.mu.Unlock()
			return out, nil
		}
		changed := s.changed
		meta := s.Metadata
		m.mu.Unlock()
		select {
		case <-ctx.Done():
			return Output{}, ctx.Err()
		case <-timer.C:
			return Output{Metadata: meta, Next: after}, nil
		case <-changed:
		}
	}
}
func (m *Manager) Close() {
	m.mu.Lock()
	m.closed = true
	var done []chan struct{}
	for _, s := range m.sessions {
		if s.p != nil {
			done = append(done, s.done)
		}
		if s.State == "running" {
			s.State = "stopped"
			if s.Process.PID != 0 {
				s.p.kill()
			}
			s.p.close()
		}
	}

	_ = m.persist()
	m.mu.Unlock()
	for _, d := range done {
		<-d
	}
}

// Access revalidates the execution device and registered repository on every attachment/control request.
func (m *Manager) Access(ctx context.Context, project, id, phone string) error {
	if !m.AuthorizedDevice(ctx) {
		return domain.ErrForbidden
	}
	m.mu.Lock()
	s, err := m.get(project, id)
	if err != nil {
		m.mu.Unlock()
		return err
	}
	root, common, device := s.Root, s.CommonDir, s.Device
	allowed := phone == ""
	for _, grant := range s.Phones {
		if grant == phone {
			allowed = true
		}
	}
	m.mu.Unlock()
	if !allowed || device != m.opt.Device {
		return domain.ErrForbidden
	}
	p, err := m.opt.Projects.Get(ctx, project)
	if err != nil {
		return err
	}
	if p.Inactive() || p.Repository == nil || p.RepoPath != root || p.Repository.CommonDir != common {
		return domain.ErrForbidden
	}
	repo, err := (&gitrepo.CLI{}).Inspect(ctx, root)
	if err != nil || repo.RootPath != root || repo.CommonDir != common {
		return domain.ErrForbidden
	}
	return nil
}

// Grant is called only by the authenticated local owner, for one enrolled phone and session.
func (m *Manager) Grant(project, id, phone string, enabled bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, err := m.get(project, id)
	if err != nil {
		return err
	}
	before := append([]string(nil), s.Phones...)
	grants := []string{}
	for _, p := range s.Phones {
		if p != phone {
			grants = append(grants, p)
		}
	}
	if enabled {
		grants = append(grants, phone)
	}
	s.Phones = grants
	if err := m.persist(); err != nil {
		s.Phones = before
		return err
	}
	s.grantRevision++
	m.notify(s)
	return nil
}

type inputReceipt struct {
	Sequence uint64
	Hash     [32]byte
	Rendered uint64
	At       time.Time
}

// idleReceipt names the receipt unused for longest, if it has been idle for ReceiptIdle. Callers hold m.mu.
func (m *Manager) idleReceipt(s *session) string {
	oldest, at := "", m.now()
	for client, r := range s.receipts {
		if r.At.Before(at) {
			oldest, at = client, r.At
		}
	}
	if oldest == "" || m.now().Sub(at) < ReceiptIdle {
		return ""
	}
	return oldest
}

// InputOnce fences duplicate and out-of-order requests without ever replaying uncertain input.
func (m *Manager) InputOnce(project, id, client string, sequence uint64, data []byte, rendered uint64) error {
	if len(client) < 16 || len(client) > 80 || sequence == 0 || len(data) == 0 || len(data) > MaxInput {
		return domain.ErrInvalid
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	s, err := m.get(project, id)
	if err != nil {
		return err
	}
	if s.receipts == nil {
		s.receipts = map[string]inputReceipt{}
	}
	cursor := rendered
	if cursor > s.base+uint64(len(s.buffer)) {
		return domain.ErrInvalid
	}
	previous, exists := s.receipts[client]
	hash := sha256.Sum256(data)
	if exists && sequence == previous.Sequence && hash == previous.Hash && cursor == previous.Rendered {
		previous.At = m.now()
		s.receipts[client] = previous
		return nil
	}
	if sequence != previous.Sequence+1 {
		return domain.ErrConflict
	}
	// A new client needs room. Only a receipt long idle is given up, and only once the input is accepted.
	evict := ""
	if !exists && len(s.receipts) >= MaxInputClients {
		if evict = m.idleReceipt(s); evict == "" {
			return fmt.Errorf("%w: too many input clients for this terminal", domain.ErrConflict)
		}
	}
	if s.State != "running" {
		return domain.ErrConflict
	}
	select {
	case s.input <- append([]byte(nil), data...):
		if evict != "" {
			delete(s.receipts, evict)
		}
		s.receipts[client] = inputReceipt{sequence, hash, cursor, m.now()}
		s.answered = max(s.answered, cursor)
		return nil
	default:
		return domain.ErrConflict
	}
}
