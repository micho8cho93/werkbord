// Package runner owns the agent processes.
//
// The controller, not a browser tab, is what keeps an agent running: the
// Manager starts each session, reads what the agent does for as long as the
// process lives, and writes every change of state and every piece of output
// through service.Runs, so the database and the event log always describe the
// run. Nothing here depends on an HTTP request: a phone that disconnects, or a
// page that is closed, does not touch a session.
//
// Each live run has one goroutine (see live) that is the only reader of its
// session's events. User actions (send, answer, finish, stop) come from other
// goroutines and are serialised against it by the run's own lock.
package runner

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"devboard/internal/agent"
	"devboard/internal/domain"
	"devboard/internal/gitrepo"
	"devboard/internal/service"
)

// Options configures a Manager. Everything without a default is required.
type Options struct {
	Runs      *service.Runs
	Tasks     *service.Tasks
	Projects  *service.Projects
	Worktrees *service.Worktrees
	Git       gitrepo.Worktrees
	Agents    *agent.Registry
	Log       *slog.Logger

	// WorktreeRoot is the canonical directory worktrees are made in. It must
	// be the same directory service.Worktrees was given.
	WorktreeRoot string

	// FlushInterval is how long output is gathered before it is written, and
	// FlushBytes how much, whichever comes first. Output is written in batches
	// because every write is a durable transaction. Defaults: 150 ms, 64 KiB.
	FlushInterval time.Duration
	FlushBytes    int
	// MaxOutputBytes bounds the output kept for one run; beyond it output is
	// dropped (with one notice) while everything else continues. Default 16 MiB.
	MaxOutputBytes int64
	// SetupTimeout bounds preparing a worktree and starting an agent. Default 2 minutes.
	SetupTimeout time.Duration
	// StopTimeout bounds how long Stop waits for a process to go. Default 20 s.
	StopTimeout time.Duration
	// FinishTimeout is how long an agent has to exit after being asked to
	// finish before it is stopped instead. Default 30 s.
	FinishTimeout time.Duration
	// ReapGrace is how long a process a previous controller left behind has to
	// obey SIGTERM. Default 5 s.
	ReapGrace time.Duration
}

func (o *Options) defaults() {
	if o.FlushInterval <= 0 {
		o.FlushInterval = 150 * time.Millisecond
	}
	if o.FlushBytes <= 0 {
		o.FlushBytes = 64 << 10
	}
	if o.MaxOutputBytes <= 0 {
		o.MaxOutputBytes = 16 << 20
	}
	if o.SetupTimeout <= 0 {
		o.SetupTimeout = 2 * time.Minute
	}
	if o.StopTimeout <= 0 {
		o.StopTimeout = 20 * time.Second
	}
	if o.FinishTimeout <= 0 {
		o.FinishTimeout = 30 * time.Second
	}
	if o.ReapGrace <= 0 {
		o.ReapGrace = 5 * time.Second
	}
}

// Manager starts, drives and ends agent sessions.
type Manager struct {
	opt Options

	mu      sync.Mutex
	live    map[string]*live // by run ID
	locks   map[string]chan struct{}
	closing bool
	wg      sync.WaitGroup // in-flight starts and resumes
}

// New returns a Manager. It starts nothing: call Recover once before serving.
func New(opt Options) *Manager {
	opt.defaults()
	return &Manager{opt: opt, live: map[string]*live{}, locks: map[string]chan struct{}{}}
}

func (m *Manager) log() *slog.Logger {
	if m.opt.Log == nil {
		return slog.New(slog.DiscardHandler)
	}
	return m.opt.Log
}

// bg is the context for work that must not be tied to a request.
func bg() context.Context { return context.Background() }

// StartInput says which task to run, with which agent.
type StartInput struct {
	TaskID  string
	AgentID string
	// Instructions are added to the task's own text in the agent's first message.
	Instructions string
	// Resume continues the task's previous session with this agent, instead of
	// beginning a new conversation, in the same worktree.
	Resume bool
}

// Start runs an agent on a task:
//
//  1. validate the task, its project and the agent;
//  2. prepare the task's Git worktree;
//  3. create the run, in the starting state;
//  4. launch the agent in the worktree;
//  5. mark the run running and move the task to Doing, together;
//  6. from then on stream and persist what the agent does.
//
// Nothing says the agent is running until step 5, after the process is up. If
// anything before that fails, the run (if one was made) is marked failed, the
// card stays where it was, and a worktree made just now is taken away again.
//
// ctx only bounds how long the caller waits to hear whether it worked: once
// the request is accepted, setup is carried through even if the caller
// disconnects, and the session never depends on the caller again.
func (m *Manager) Start(ctx context.Context, in StartInput) (*domain.Run, error) {
	if err := m.enter(); err != nil {
		return nil, err
	}
	defer m.wg.Done()
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), m.opt.SetupTimeout)
	defer cancel()

	// 1. Validate.
	if in.AgentID == "" {
		return nil, fmt.Errorf("%w: agentId is required", domain.ErrInvalid)
	}
	task, err := m.opt.Tasks.Get(ctx, in.TaskID)
	if err != nil {
		return nil, err
	}
	if task.State == domain.TaskDone {
		return nil, fmt.Errorf("%w: %q is in Done; move it back before running an agent on it", domain.ErrInvalid, task.Title)
	}
	unlock, err := m.lock(ctx, "task:"+task.ID)
	if err != nil {
		return nil, err
	}
	defer unlock()

	project, err := m.opt.Projects.Get(ctx, task.ProjectID)
	if err != nil {
		return nil, err
	}
	adapter, err := m.opt.Agents.Available(ctx, in.AgentID)
	if err != nil {
		return nil, err
	}
	prior, err := m.opt.Runs.ListByTask(ctx, task.ID)
	if err != nil {
		return nil, err
	}
	resumeRef := ""
	for _, r := range prior {
		if !r.State.Terminal() {
			return nil, fmt.Errorf("task %q already has an active run (%s, %s): %w", task.Title, r.ID, r.State, domain.ErrConflict)
		}
		if in.Resume && r.AgentID == in.AgentID && r.SessionRef != "" {
			resumeRef = r.SessionRef
		}
	}
	if in.Resume && resumeRef == "" {
		return nil, fmt.Errorf("%w: there is no earlier %s session of this task to continue", domain.ErrInvalid, in.AgentID)
	}
	prompt := buildPrompt(task, in.Instructions, in.Resume)

	// 2. Prepare the worktree.
	ws, err := m.prepareWorkspace(ctx, project, task, prior)
	if err != nil {
		return nil, err
	}
	discard := func() {
		if ws.created {
			m.retire(ctx, ws.repoRoot, ws.wt, ws.branchCreated)
		}
	}

	// 3. Create the run.
	run, err := m.opt.Runs.Create(ctx, service.NewRun{TaskID: task.ID, AgentID: in.AgentID, Prompt: prompt, WorktreeID: ws.wt.ID})
	if err != nil {
		discard()
		return nil, err
	}

	// 4. Launch.
	sess, err := adapter.Start(ctx, agent.StartRequest{RunID: run.ID, WorkDir: ws.wt.Path, Prompt: prompt, ResumeRef: resumeRef})
	if err != nil {
		m.log().Warn("agent failed to start", "run", run.ID, "agent", in.AgentID, "err", err)
		_, _ = m.opt.Runs.End(bg(), run.ID, service.Ended{State: domain.RunFailed, Reason: truncateReason("could not start " + in.AgentID + ": " + err.Error())})
		discard()
		return nil, fmt.Errorf("could not start %s: %w", in.AgentID, errors.Join(domain.ErrAgent, err))
	}

	// 5. Running, and the card moves: one transaction.
	l := newLive(m, run, sess)
	if !m.register(l) { // shutting down
		m.abandon(l, "controller is shutting down")
		return nil, fmt.Errorf("%w: the controller is shutting down", domain.ErrConflict)
	}
	info := sess.Process()
	started, err := m.opt.Runs.MarkStarted(bg(), run.ID, service.Started{PID: info.PID, ProcessID: info.ID})
	if err != nil {
		m.log().Error("cannot record that the agent started", "run", run.ID, "err", err)
		m.abandon(l, "the controller could not record the session: "+err.Error())
		discard()
		return nil, err
	}

	// 6. From here the run is the controller's, not the caller's.
	l.start()
	m.log().Info("agent started", "run", run.ID, "task", task.ID, "agent", in.AgentID, "pid", info.PID, "worktree", ws.wt.Path)
	return started, nil
}

// Send delivers a message from the user to a run's agent: the next turn if it
// is waiting, otherwise queued behind its current work. A run that was waiting
// when the controller last stopped has no process; the message starts one that
// continues the session.
func (m *Manager) Send(ctx context.Context, runID, text string) error {
	if text = trimMessage(text); text == "" {
		return fmt.Errorf("%w: a message is required", domain.ErrInvalid)
	}
	if l := m.liveRun(runID); l != nil {
		return l.send(ctx, text)
	}
	return m.resume(ctx, runID, text)
}

// Answer gives the agent the user's answer to one of its questions.
func (m *Manager) Answer(ctx context.Context, questionID, answer string) error {
	q, err := m.opt.Runs.GetQuestion(ctx, questionID)
	if err != nil {
		return err
	}
	if q.Status != domain.QuestionPending {
		return fmt.Errorf("question %s is already %s: %w", q.ID, q.Status, domain.ErrConflict)
	}
	l := m.liveRun(q.RunID)
	if l == nil {
		return fmt.Errorf("the agent that asked is no longer running, so this cannot be answered: %w", domain.ErrConflict)
	}
	return l.answer(ctx, questionID, answer)
}

// Finish ends a session the way the user means it: the agent is told there is
// no more to do, finishes, and the run completes. An agent that does not exit
// in time is stopped instead.
func (m *Manager) Finish(ctx context.Context, runID string) (*domain.Run, error) {
	if l := m.liveRun(runID); l != nil {
		return l.finish(ctx)
	}
	return m.endOffline(ctx, runID, domain.RunCompleted, "finished by user")
}

// Stop ends a session now. The agent and everything it started are killed and
// the run is stopped. Whatever it changed in the worktree stays there.
func (m *Manager) Stop(ctx context.Context, runID string) (*domain.Run, error) {
	if l := m.liveRun(runID); l != nil {
		return l.stop(ctx)
	}
	return m.endOffline(ctx, runID, domain.RunStopped, "stopped by user")
}

// endOffline ends a run that has no process: one that is waiting since the
// controller last stopped. A run that is starting is not ended here, because
// its process is about to exist.
func (m *Manager) endOffline(ctx context.Context, runID string, state domain.RunState, reason string) (*domain.Run, error) {
	run, err := m.opt.Runs.Get(ctx, runID)
	if err != nil {
		return nil, err
	}
	switch {
	case run.State.Terminal():
		return nil, fmt.Errorf("run %s has already ended (%s): %w", runID, run.State, domain.ErrConflict)
	case run.State != domain.RunWaitingForUser:
		return nil, fmt.Errorf("run %s is %s and has no session to end yet: %w", runID, run.State, domain.ErrConflict)
	}
	return m.opt.Runs.End(bg(), runID, service.Ended{State: state, Reason: reason})
}

// resume starts a new process for a run that is waiting but has none, and
// gives it the user's message. It is how a conversation survives a restart.
func (m *Manager) resume(ctx context.Context, runID, text string) error {
	if err := m.enter(); err != nil {
		return err
	}
	defer m.wg.Done()
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), m.opt.SetupTimeout)
	defer cancel()
	unlock, err := m.lock(ctx, "run:"+runID)
	if err != nil {
		return err
	}
	defer unlock()
	if m.liveRun(runID) != nil { // another request got there first
		return m.liveRun(runID).send(ctx, text)
	}

	run, err := m.opt.Runs.Get(ctx, runID)
	if err != nil {
		return err
	}
	switch {
	case run.State.Terminal():
		return fmt.Errorf("run %s has ended (%s) and cannot take a message: %w", runID, run.State, domain.ErrConflict)
	case run.State != domain.RunWaitingForUser || run.Waiting != domain.WaitIdle || run.SessionRef == "":
		return fmt.Errorf("run %s is %s and has no session that can be resumed: %w", runID, run.State, domain.ErrConflict)
	}
	adapter, err := m.opt.Agents.Available(ctx, run.AgentID)
	if err != nil {
		return err
	}
	wt, err := m.opt.Worktrees.Get(ctx, run.WorktreeID)
	if err != nil {
		return err
	}
	if wt.State != domain.WorktreeActive || wt.Removing() || !dirExists(wt.Path) {
		return fmt.Errorf("the worktree of run %s is gone, so its session cannot continue: %w", runID, domain.ErrConflict)
	}

	sess, err := adapter.Start(ctx, agent.StartRequest{RunID: run.ID, WorkDir: wt.Path, Prompt: text, ResumeRef: run.SessionRef})
	if err != nil {
		return fmt.Errorf("could not resume %s: %w", run.AgentID, errors.Join(domain.ErrAgent, err))
	}
	l := newLive(m, run, sess)
	if !m.register(l) {
		m.abandon(l, "controller is shutting down")
		return fmt.Errorf("%w: the controller is shutting down", domain.ErrConflict)
	}
	info := sess.Process()
	if _, err := m.opt.Runs.MarkStarted(bg(), run.ID, service.Started{PID: info.PID, ProcessID: info.ID, Resumed: true}); err != nil {
		m.abandon(l, "the controller could not record the session: "+err.Error())
		return err
	}
	if err := m.opt.Runs.AppendOutput(bg(), run.ID, []service.OutputItem{{Stream: domain.StreamUser, Text: text}}); err != nil {
		m.log().Warn("cannot record the message that resumed a run", "run", run.ID, "err", err)
	}
	l.start()
	m.log().Info("agent session resumed", "run", run.ID, "agent", run.AgentID, "pid", info.PID)
	return nil
}

// Recover cleans up after a controller that did not shut down cleanly: it stops
// any agent process still running for a recorded run (only if it is verifiably
// the same process), then reconciles the runs themselves. Call it once at
// startup, before serving.
func (m *Manager) Recover(ctx context.Context) error {
	active, err := m.opt.Runs.ListActive(ctx)
	if err != nil {
		return err
	}
	for _, r := range active {
		if r.PID <= 0 {
			continue
		}
		res, err := agent.Reap(r.PID, r.ProcessID, m.opt.ReapGrace)
		switch {
		case err != nil:
			m.log().Warn("cannot check for a leftover agent process", "run", r.ID, "pid", r.PID, "err", err)
		case res == agent.ReapKilled:
			m.log().Warn("stopped an agent process left behind by a previous controller", "run", r.ID, "pid", r.PID)
		case res == agent.ReapForeign:
			m.log().Warn("not touching process: it is no longer the recorded agent", "run", r.ID, "pid", r.PID)
		}
	}
	_, err = m.opt.Runs.RecoverAfterRestart(ctx)
	return err
}

// Shutdown ends every live session and records what became of each. Runs that
// were working fail with a reason that says the controller stopped; runs that
// were waiting for the user are kept, to be resumed by a message. It returns
// when every process is gone, or ctx is done.
func (m *Manager) Shutdown(ctx context.Context) error {
	m.mu.Lock()
	m.closing = true
	lives := make([]*live, 0, len(m.live))
	for _, l := range m.live {
		lives = append(lives, l)
	}
	m.mu.Unlock()

	for _, l := range lives {
		l.shutdown()
	}
	var errs []error
	for _, l := range lives {
		select {
		case <-l.done:
		case <-ctx.Done():
			errs = append(errs, fmt.Errorf("run %s did not stop: %w", l.runID, ctx.Err()))
		}
	}
	waited := make(chan struct{})
	go func() { m.wg.Wait(); close(waited) }()
	select {
	case <-waited:
	case <-ctx.Done():
		errs = append(errs, fmt.Errorf("starts still in progress: %w", ctx.Err()))
	}
	return errors.Join(errs...)
}

// LiveCount reports how many sessions have a process right now.
func (m *Manager) LiveCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.live)
}

// IsLive reports whether a run has a process right now.
func (m *Manager) IsLive(runID string) bool { return m.liveRun(runID) != nil }

// ---- registry ----

// enter registers an in-flight start, or refuses it while shutting down.
func (m *Manager) enter() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closing {
		return fmt.Errorf("%w: the controller is shutting down", domain.ErrConflict)
	}
	m.wg.Add(1)
	return nil
}

func (m *Manager) register(l *live) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closing {
		return false
	}
	m.live[l.runID] = l
	return true
}

func (m *Manager) unregister(runID string) {
	m.mu.Lock()
	delete(m.live, runID)
	m.mu.Unlock()
}

func (m *Manager) liveRun(runID string) *live {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.live[runID]
}

// abandon kills a session that never became a live run and records why the run failed.
func (m *Manager) abandon(l *live, reason string) {
	defer l.closeDone()
	defer m.unregister(l.runID)
	ctx, cancel := context.WithTimeout(bg(), m.opt.StopTimeout)
	defer cancel()
	_ = l.sess.Stop(ctx)
	go func() {
		for range l.sess.Events() {
		}
	}()
	if _, err := m.opt.Runs.End(bg(), l.runID, service.Ended{State: domain.RunFailed, Reason: truncateReason(reason)}); err != nil {
		m.log().Warn("cannot record a failed start", "run", l.runID, "err", err)
	}
}

// lock serialises work on one key (a task being started, a run being resumed).
func (m *Manager) lock(ctx context.Context, key string) (func(), error) {
	m.mu.Lock()
	ch, ok := m.locks[key]
	if !ok {
		ch = make(chan struct{}, 1)
		m.locks[key] = ch
	}
	m.mu.Unlock()
	select {
	case ch <- struct{}{}:
		return func() { <-ch }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func truncateReason(s string) string {
	if len(s) > 1000 {
		return s[:1000]
	}
	return s
}

// trimMessage trims a user message and cuts it to a size the agent and the log
// can take, on a character boundary.
func trimMessage(s string) string {
	const max = 100 << 10
	s = strings.TrimSpace(s)
	if len(s) > max {
		cut := max
		for cut > 0 && !utf8.RuneStart(s[cut]) {
			cut--
		}
		s = s[:cut]
	}
	return s
}
