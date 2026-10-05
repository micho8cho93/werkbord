// Package remote executes on a user's runner machine. It owns no Board, SQLite,
// calendar, scheduler, or controller-admin credential. Its journal protects
// execution ownership and retransmits observations after a network disconnect.
package remote

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
	"unicode/utf8"

	"devboard/internal/agent"
	"devboard/internal/domain"
	"devboard/internal/gitrepo"
	"devboard/internal/machine"
	"devboard/internal/runnerwire"
)

type Identity struct {
	Controller         string   `json:"controller"`
	RunnerID           string   `json:"runnerId"`
	PrivateKey         []byte   `json:"privateKey"`
	Sequence           int64    `json:"sequence"`
	RequiresRecovery   bool     `json:"requiresRecovery,omitempty"`
	ControllerSequence int64    `json:"controllerSequence,omitempty"`
	Projects           []string `json:"projects"`
	AllowClone         bool     `json:"allowClone"`
}
type Record struct {
	Diagnostic     string                   `json:"diagnostic,omitempty"`
	Job            runnerwire.Job           `json:"job"`
	Phase          string                   `json:"phase"` // accepted, ready, preparing, launching, active, uncertain, ended
	Process        agent.ProcessInfo        `json:"process"`
	Path           string                   `json:"path,omitempty"`
	Next           int64                    `json:"next"`
	Pending        []runnerwire.Observation `json:"pending"`
	Commands       map[string]bool          `json:"commands"`
	WorkspaceHead  string                   `json:"workspaceHead,omitempty"`
	WorkspaceDirty *bool                    `json:"workspaceDirty,omitempty"`
	OutputBytes    int64                    `json:"outputBytes"`
}
type Worker struct {
	Git          func(context.Context, string, ...string) (string, error)
	LoadBindings func() map[string]string
	Version      string
	Dir          string
	Identity     Identity
	Bindings     map[string]string
	AllowClone   bool
	Agents       *agent.Registry
	Client       *http.Client
	Now          func() time.Time
	// Capabilities overrides host detection for deterministic simulations.
	Capabilities    func(context.Context) domain.RunnerCapabilities
	Prepare         func(context.Context, runnerwire.Job) (string, string, string, error)
	mu              sync.Mutex
	records         map[string]*Record
	sessions        map[string]agent.Session
	lease           time.Time
	disabled        bool
	stopKind        map[string]string
	workspaceCursor int
	preparing       map[string]context.CancelFunc
	wg              sync.WaitGroup
}

func (w *Worker) now() time.Time {
	if w.Now != nil {
		return w.Now()
	}
	return time.Now()
}
func (w *Worker) journalPath(id string) string { return filepath.Join(w.Dir, "runs", id+".json") }
func (w *Worker) save(r *Record) error         { return AtomicJSON(w.journalPath(r.Job.Run.ID), r) }
func validID(id string) bool {
	if len(id) < 5 || len(id) > 100 {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_') {
			return false
		}
	}
	return true
}

// Load recovers owned processes only. An unidentifiable process never produces
// a terminal report; the owner must resolve it on this machine first.
func (w *Worker) Load(ctx context.Context) error {
	if len(w.Identity.PrivateKey) != ed25519.PrivateKeySize {
		return fmt.Errorf("invalid runner identity")
	}
	w.records = map[string]*Record{}
	w.sessions = map[string]agent.Session{}
	w.stopKind = map[string]string{}
	w.preparing = map[string]context.CancelFunc{}
	files, e := os.ReadDir(filepath.Join(w.Dir, "runs"))
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil {
		return e
	}
	for _, f := range files {
		if f.IsDir() || filepath.Ext(f.Name()) != ".json" {
			continue
		}
		data, e := os.ReadFile(filepath.Join(w.Dir, "runs", f.Name()))
		if e != nil {
			return e
		}
		r := &Record{}
		if e := json.Unmarshal(data, r); e != nil {
			return e
		}
		if !validID(r.Job.Run.ID) || r.Job.Run.RunnerID != w.Identity.RunnerID {
			return fmt.Errorf("invalid journal ownership")
		}
		if r.Commands == nil {
			r.Commands = map[string]bool{}
		}
		w.records[r.Job.Run.ID] = r
		if r.Phase == "ended" || r.Phase == "uncertain" {
			continue
		}
		safe := r.Phase == "accepted" || r.Phase == "ready" || r.Phase == "preparing"
		if r.Process.PID > 0 {
			result, e := agent.Reap(r.Process.PID, r.Process.ID, 5*time.Second)
			safe = e == nil && result != agent.ReapForeign
		}
		if safe {
			res := agent.Result{State: domain.RunFailed, Reason: "runner restarted; execution stopped and work retained", ExitCode: -1}
			r.Phase = "ended"
			r.Next++
			r.Pending = append(r.Pending, runnerwire.Observation{Seq: r.Next, Kind: "ended", Result: &res})
		} else {
			r.Phase = "uncertain"
		}
		if e := w.save(r); e != nil {
			return e
		}
	}
	return nil
}

func (w *Worker) capabilities(ctx context.Context) domain.RunnerCapabilities {
	var c domain.RunnerCapabilities
	if w.Capabilities != nil {
		c = w.Capabilities(ctx)
	} else {
		c = machine.Capabilities(ctx, w.Agents, w.Dir)
	}
	c.Repositories = []string{}
	for id, path := range w.Bindings {
		if _, e := os.Stat(path); e == nil {
			c.Repositories = append(c.Repositories, id)
		}
	}
	for _, r := range w.records {
		if r.Phase == "uncertain" {
			c.Diagnostics += "Run " + r.Job.Run.ID + ": " + r.Diagnostic + "; uncertain process ownership; inspect this machine and use devboard runner resolve --confirm-stopped. "
		}
	}
	if w.Identity.RequiresRecovery {
		c.Diagnostics += "Controller state was restored; revoke and re-pair this identity after inspecting its machine. "
	}
	if len(c.Diagnostics) > 2000 {
		c.Diagnostics = c.Diagnostics[:2000]
		for !utf8.ValidString(c.Diagnostics) && len(c.Diagnostics) > 0 {
			c.Diagnostics = c.Diagnostics[:len(c.Diagnostics)-1]
		}
	}
	return c
}
func (w *Worker) post(ctx context.Context, path string, body []byte, signed bool, dst any) error {
	req, e := http.NewRequestWithContext(ctx, "POST", w.Identity.Controller+path, bytes.NewReader(body))
	if e != nil {
		return e
	}
	req.Header.Set("Content-Type", "application/json")
	if signed {
		req.Header.Set("X-Runner-Signature", runnerwire.Signature(ed25519.PrivateKey(w.Identity.PrivateKey), body))
	}
	client := w.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	res, e := client.Do(req)
	if e != nil {
		return fmt.Errorf("controller connection failed: %w", e)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 && res.StatusCode != 201 {
		var refusal struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.NewDecoder(io.LimitReader(res.Body, 4096)).Decode(&refusal)
		return fmt.Errorf("controller refused runner request (HTTP %d): %s %s", res.StatusCode, refusal.Error.Code, refusal.Error.Message)
	}
	return json.NewDecoder(io.LimitReader(res.Body, 8<<20)).Decode(dst)
}

// Tick saves its monotonic request counter before sending. A lost response is
// retried with a new counter and the same observations, deduplicated by SQLite.
func (w *Worker) refreshBindingsLocked() {
	if w.LoadBindings != nil {
		w.Bindings = w.LoadBindings()
	}
	if w.Bindings == nil {
		w.Bindings = map[string]string{}
	}
	for _, id := range w.Identity.Projects {
		if !validID(id) || w.Bindings[id] != "" {
			continue
		}
		root := filepath.Join(w.Dir, "repositories", id)
		if _, err := os.Stat(filepath.Join(root, ".git")); err == nil {
			w.Bindings[id] = root
		}
	}
}

func (w *Worker) Tick(ctx context.Context) error {
	w.refreshWorkspaces(ctx)
	w.mu.Lock()
	w.refreshBindingsLocked()
	caps := w.capabilities(ctx)
	caps.CloneEnabled = w.AllowClone
	w.Identity.Sequence++
	if e := AtomicJSON(filepath.Join(w.Dir, "identity.json"), w.Identity); e != nil {
		w.mu.Unlock()
		return e
	}
	in := runnerwire.Sync{Protocol: runnerwire.Protocol, Version: w.Version, ControllerSequence: w.Identity.ControllerSequence, RunnerID: w.Identity.RunnerID, Sequence: w.Identity.Sequence, At: w.now(), Capabilities: caps, Reports: []runnerwire.Report{}}
	const reportBudget = 2 << 20
	budget := reportBudget
	for id, r := range w.records {
		if r.Phase == "uncertain" {
			continue
		}
		if len(in.Reports) >= 128 {
			break
		}
		n := 0
		for n < len(r.Pending) && n < 200 {
			encoded, err := json.Marshal(r.Pending[n])
			if err != nil || len(encoded) > reportBudget {
				r.Next = r.Job.Ack
				w.quarantineLocked(id, "observation cannot be encoded within the runner message limit; inspect this run and resolve or revoke")
				n = 0
				break
			}
			if len(encoded) > budget {
				break
			}
			budget -= len(encoded)
			n++
		}
		if n > 0 {
			in.Reports = append(in.Reports, runnerwire.Report{RunID: id, Observations: append([]runnerwire.Observation{}, r.Pending[:n]...)})
		}
	}
	body, e := json.Marshal(in)
	w.mu.Unlock()
	if e != nil {
		return e
	}
	var reply runnerwire.SyncReply
	e = w.post(ctx, "/api/runner/sync", body, true, &reply)
	w.mu.Lock()
	defer w.mu.Unlock()
	if e == nil && reply.Protocol != runnerwire.Protocol {
		e = fmt.Errorf("incompatible controller protocol; update controller and runner")
	}
	if e == nil && (reply.LeaseSeconds <= 0 || reply.LeaseSeconds > int(runnerwire.Lease.Seconds())) {
		e = fmt.Errorf("invalid controller lease")
	}
	if e != nil {
		_ = AtomicJSON(filepath.Join(w.Dir, "last-error.json"), map[string]any{"at": w.now(), "error": e.Error()})
		if !w.lease.IsZero() && !w.now().Before(w.lease) {
			for id, sess := range w.sessions {
				w.stopKind[id] = "lease expired"
				go sess.Stop(context.Background())
			}
		}
		return e
	}
	_ = os.Remove(filepath.Join(w.Dir, "last-error.json"))
	w.lease = w.now().Add(time.Duration(reply.LeaseSeconds) * time.Second)
	w.Identity.ControllerSequence = reply.Sequence
	if e := AtomicJSON(filepath.Join(w.Dir, "identity.json"), w.Identity); e != nil {
		return e
	}
	if reply.StateLost {
		w.Identity.RequiresRecovery = true
		if e := AtomicJSON(filepath.Join(w.Dir, "identity.json"), w.Identity); e != nil {
			return e
		}
		for id, r := range w.records {
			if ack, ok := reply.Acks[id]; ok {
				r.Next = ack
				r.Job.Ack = ack
			}
			w.quarantineLocked(id, "controller database restored; inspect and recover or revoke runner")
		}
		return nil
	}
	for id, reason := range reply.Rejected {
		if r := w.records[id]; r != nil {
			r.Next = reply.Acks[id]
		}
		w.quarantineLocked(id, reason)
	}
	w.Identity.Projects = reply.Projects
	w.disabled = reply.Disabled
	for id, ack := range reply.Acks {
		r := w.records[id]
		if r == nil {
			continue
		}
		cut := 0
		for cut < len(r.Pending) && r.Pending[cut].Seq <= ack {
			cut++
		}
		r.Pending = append([]runnerwire.Observation{}, r.Pending[cut:]...)
		if e := w.save(r); e != nil {
			return e
		}
	}
	for _, j := range reply.Jobs {
		if !validID(j.Run.ID) || !validID(j.Run.ProjectID) || j.Run.RunnerID != w.Identity.RunnerID {
			return fmt.Errorf("invalid job identity")
		}
		r := w.records[j.Run.ID]
		if r == nil {
			r = &Record{Job: j, Phase: "accepted", Next: j.Ack, Commands: map[string]bool{}, Pending: []runnerwire.Observation{}}
			w.records[j.Run.ID] = r
			if e := w.save(r); e != nil {
				return e
			}
			if w.Identity.RequiresRecovery || j.Protocol != runnerwire.Protocol || j.Run.State != domain.RunStarting || j.Ack != 0 || j.Rejected != "" {
				w.quarantineLocked(j.Run.ID, "controller lists accepted work but this machine has no journal; inspect the machine and resolve or revoke")
				continue
			}
			if reply.Disabled || !contains(reply.Projects, j.Run.ProjectID) {
				w.endLocked(r, agent.Result{State: domain.RunFailed, Reason: "runner or project disabled before launch", ExitCode: -1})
				continue
			}
			// Accept durably and wait for the controller acknowledgement before execution.
			if e := w.appendLocked(r, runnerwire.Observation{Kind: "accepted"}); e != nil {
				return e
			}
		}
		r.Job = j
		if j.Protocol != runnerwire.Protocol {
			w.quarantineLocked(j.Run.ID, "legacy job has no durable acceptance contract; inspect this machine, then resolve or revoke before creating a new run")
			continue
		}
		if r.Phase == "uncertain" {
			r.Next = j.Ack
			if e := w.save(r); e != nil {
				return e
			}
			continue
		}
		if r.Phase == "accepted" && j.Ack > 0 {
			r.Phase = "ready"
			if e := w.save(r); e != nil {
				return e
			}
			w.wg.Add(1)
			go func() { defer w.wg.Done(); w.launch(j.Run.ID) }()
		}
		for _, cmd := range j.Commands {
			if r.Commands[cmd.ID] {
				continue
			}
			sess := w.sessions[j.Run.ID]
			if sess == nil {
				continue
			}
			r.Commands[cmd.ID] = true
			if e := w.save(r); e != nil {
				return e
			}
			w.wg.Add(1)
			go func(id string, c runnerwire.Command, sess agent.Session) { defer w.wg.Done(); w.command(id, c, sess) }(j.Run.ID, cmd, sess)
		}
	}
	return nil
}
func contains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}
func (w *Worker) appendLocked(r *Record, ob runnerwire.Observation) error {
	if r.Phase == "uncertain" {
		return nil
	}
	r.Next++
	ob.Seq = r.Next
	r.Pending = append(r.Pending, ob)
	return w.save(r)
}
func (w *Worker) endLocked(r *Record, res agent.Result) error {
	if r.Phase == "uncertain" {
		return nil
	}
	r.Phase = "ended"
	return w.appendLocked(r, runnerwire.Observation{Kind: "ended", Result: &res})
}
func (w *Worker) launch(id string) {
	w.mu.Lock()
	r := w.records[id]
	if r.Phase != "ready" {
		w.mu.Unlock()
		return
	}
	r.Phase = "preparing"
	e := w.save(r)
	if e != nil {
		w.mu.Unlock()
		return
	}
	j := r.Job
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	w.preparing[id] = cancel
	w.mu.Unlock()
	defer cancel()
	defer func() { w.mu.Lock(); delete(w.preparing, id); w.mu.Unlock() }()
	prep := w.Prepare
	if prep == nil {
		prep = w.prepare
	}
	path, branch, base, e := prep(ctx, j)
	if e != nil {
		w.mu.Lock()
		_ = w.endLocked(r, agent.Result{State: domain.RunFailed, Reason: e.Error(), ExitCode: -1})
		w.mu.Unlock()
		return
	}
	adapter, e := w.Agents.Available(ctx, j.Run.AgentID)
	if e != nil {
		w.mu.Lock()
		_ = w.endLocked(r, agent.Result{State: domain.RunFailed, Reason: "required agent unavailable on runner", ExitCode: -1})
		w.mu.Unlock()
		return
	}
	w.mu.Lock()
	if !w.now().Before(w.lease) || w.disabled || !contains(w.Identity.Projects, j.Run.ProjectID) {
		_ = w.endLocked(r, agent.Result{State: domain.RunFailed, Reason: "lease expired or project access disabled before launch", ExitCode: -1})
		w.mu.Unlock()
		return
	}
	if r.Phase == "uncertain" {
		w.mu.Unlock()
		return
	}
	r.Path = path
	r.Phase = "launching"
	e = w.save(r)
	w.mu.Unlock()
	if e != nil {
		return
	}
	sess, e := adapter.Start(ctx, agent.StartRequest{RunID: id, WorkDir: path, Prompt: j.Run.Prompt, Policy: j.Run.Policy, Model: j.Run.Model, Reasoning: j.Run.Reasoning})
	w.mu.Lock()
	if e != nil {
		_ = w.endLocked(r, agent.Result{State: domain.RunFailed, Reason: "agent failed to start", ExitCode: -1})
		w.mu.Unlock()
		return
	}
	quarantined := r.Phase == "uncertain"
	ownershipExpired := !w.now().Before(w.lease) || w.disabled || !contains(w.Identity.Projects, j.Run.ProjectID)
	if !quarantined && !ownershipExpired {
		r.Phase = "active"
	}
	r.Process = sess.Process()
	w.sessions[id] = sess
	if quarantined || ownershipExpired {
		if ownershipExpired {
			w.stopKind[id] = "lease expired or project access disabled during agent startup"
		}
		_ = w.save(r)
		go sess.Stop(context.Background())
	} else if e := w.appendLocked(r, runnerwire.Observation{Kind: "started", Branch: branch, BaseCommit: base}); e != nil {
		w.stopKind[id] = "journal write failed"
		go sess.Stop(context.Background())
	}
	w.mu.Unlock()
	for ev := range sess.Events() {
		// Drain the stopped session without reporting activity from an unauthorized launch.
		if ownershipExpired {
			continue
		}
		w.mu.Lock()
		if len(ev.Text) > 16384 {
			ev.Text = ev.Text[:16384]
		}
		if ev.Kind == agent.KindOutput {
			r.OutputBytes += int64(len(ev.Text))
			if r.OutputBytes > 16<<20 {
				w.mu.Unlock()
				continue
			}
		}
		if e := w.appendLocked(r, runnerwire.Observation{Kind: "event", Event: &ev}); e != nil {
			w.stopKind[id] = "journal write failed"
			go sess.Stop(context.Background())
		}
		w.mu.Unlock()
	}
	res := sess.Wait()
	head := ""
	var uncommitted *bool
	if path != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		head, _ = (&gitrepo.CLI{}).HeadCommit(ctx, path)
		if state, err := (&gitrepo.CLI{}).Status(ctx, path); err == nil {
			dirty := state.Counts.Staged+state.Counts.Unstaged+state.Counts.Untracked+state.Counts.Conflicted > 0
			uncommitted = &dirty
		}
		cancel()
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	reason := w.stopKind[id]
	if reason != "" {
		res.State = domain.RunStopped
		res.Reason = reason
	}
	delete(w.sessions, id)
	if r.Phase == "uncertain" {
		_ = w.save(r)
		return
	}
	r.Phase = "ended"
	r.WorkspaceHead, r.WorkspaceDirty = head, uncommitted
	_ = w.appendLocked(r, runnerwire.Observation{Kind: "ended", Result: &res, HeadCommit: head, Uncommitted: uncommitted})
}
func (w *Worker) command(id string, c runnerwire.Command, sess agent.Session) {
	w.mu.Lock()
	defer w.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var e error
	switch c.Kind {
	case "send":
		e = sess.Send(ctx, c.Text)
	case "respond":
		e = sess.Respond(ctx, c.Ref, c.Text)
	case "finish":
		e = sess.Close(ctx)
		if e == nil {
			go func() {
				select {
				case <-time.After(30 * time.Second):
					w.mu.Lock()
					_, ok := w.sessions[id]
					if ok {
						w.stopKind[id] = "finish timeout"
					}
					w.mu.Unlock()
					if ok {
						_ = sess.Stop(context.Background())
					}
				}
			}()
		}
	case "stop":
		w.stopKind[id] = "stopped by user"
		e = sess.Stop(ctx)
	default:
		e = fmt.Errorf("unsupported runner command")
	}
	ob := runnerwire.Observation{Kind: "command", CommandID: c.ID}
	if e != nil {
		ob.Error = e.Error()
	}
	_ = w.appendLocked(w.records[id], ob)
}
func (w *Worker) Run(ctx context.Context) error {
	if e := w.Load(ctx); e != nil {
		return e
	}
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	defer func() {
		w.mu.Lock()
		w.lease = time.Time{}
		for _, cancel := range w.preparing {
			cancel()
		}
		for id, s := range w.sessions {
			w.stopKind[id] = "runner shutting down"
			go s.Stop(context.Background())
		}
		w.mu.Unlock()
		w.wg.Wait()
	}()
	for {
		if ctx.Err() != nil {
			return nil
		}
		if err := w.Tick(ctx); err != nil {
			fmt.Fprintln(os.Stderr, "Runner sync:", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

// Resolve is an explicit local-owner assertion for the launch/crash ambiguity.
// It never kills an unknown process and cannot run beside the locked daemon.
func (w *Worker) Resolve(id string) error {
	if !validID(id) {
		return fmt.Errorf("invalid run ID")
	}
	if e := w.Load(context.Background()); e != nil {
		return e
	}
	r := w.records[id]
	if r == nil || r.Phase != "uncertain" {
		return fmt.Errorf("run does not have uncertain ownership")
	}
	r.Phase = "resolved"
	return w.endLocked(r, agent.Result{State: domain.RunFailed, Reason: "owner confirmed the execution has stopped on its runner", ExitCode: -1})
}

func (w *Worker) quarantineLocked(id, reason string) {
	r := w.records[id]
	if r == nil {
		return
	}
	r.Phase, r.Diagnostic = "uncertain", reason
	r.Pending = nil
	if cancel := w.preparing[id]; cancel != nil {
		cancel()
	}
	_ = w.save(r)
	if sess := w.sessions[id]; sess != nil {
		w.stopKind[id] = reason
		go sess.Stop(context.Background())
	}
}
