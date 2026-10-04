// Package fake is a deterministic, in-memory agent.Adapter for tests of the
// code that drives agents. It starts no process: a test scripts what the
// session says and when it ends, and can inspect what it was sent.
package fake

import (
	"context"
	"fmt"
	"sync"

	"devboard/internal/agent"
	"devboard/internal/domain"
)

// Adapter is a scriptable agent.Adapter.
type Adapter struct {
	Name      string // the ID; defaults to "fake"
	Info      domain.Agent
	StartFunc func(req agent.StartRequest) error // returns an error to make Start fail

	mu       sync.Mutex
	sessions []*Session
}

var _ agent.Adapter = (*Adapter)(nil)

// ID implements agent.Adapter.
func (a *Adapter) ID() string {
	if a.Name == "" {
		return "fake"
	}
	return a.Name
}

// Detect implements agent.Adapter. By default the agent is available.
func (a *Adapter) Detect(context.Context) domain.Agent {
	info := a.Info
	if info.ID == "" {
		info = domain.Agent{ID: a.ID(), Name: "Fake agent", Available: true, Version: "0.0.0"}
	}
	return info
}

// Start implements agent.Adapter.
func (a *Adapter) Start(_ context.Context, req agent.StartRequest) (agent.Session, error) {
	if a.StartFunc != nil {
		if err := a.StartFunc(req); err != nil {
			return nil, err
		}
	}
	s := newSession(req)
	a.mu.Lock()
	a.sessions = append(a.sessions, s)
	a.mu.Unlock()
	return s, nil
}

// Sessions returns every session started so far, oldest first.
func (a *Adapter) Sessions() []*Session {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]*Session(nil), a.sessions...)
}

// Last returns the most recently started session.
func (a *Adapter) Last() *Session {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.sessions) == 0 {
		return nil
	}
	return a.sessions[len(a.sessions)-1]
}

// Session is a scripted agent.Session. Everything the test calls on it is
// asynchronous with respect to the code under test, like a real agent.
type Session struct {
	Req agent.StartRequest

	events chan agent.Event
	done   chan struct{}

	mu        sync.Mutex
	sent      []string
	responses []Response
	open      map[string]bool
	ended     bool
	result    agent.Result
	closeReq  bool
	stopReq   bool
	pid       int

	// OnSend, if set, is called with each message the user sends, before Send
	// returns; returning an error makes Send fail.
	OnSend func(text string) error
	// OnClose and OnStop decide what happens when the controller ends the
	// session. By default both end it: Close successfully, Stop as a failure
	// (the controller reports it as stopped).
	OnClose func(s *Session)
	OnStop  func(s *Session)
}

// Response is an answer the code under test gave to a question.
type Response struct{ Ref, Answer string }

var nextPID = 100000

func newSession(req agent.StartRequest) *Session {
	nextPID++
	return &Session{Req: req, events: make(chan agent.Event, 1024), done: make(chan struct{}), open: map[string]bool{}, pid: nextPID}
}

// Say emits agent output.
func (s *Session) Say(stream domain.OutputStream, text string) {
	s.emit(agent.Event{Kind: agent.KindOutput, Stream: stream, Text: text})
}

// Assistant emits something the agent said.
func (s *Session) Assistant(text string) { s.Say(domain.StreamAssistant, text) }

// Ref reports a resumable session handle.
func (s *Session) Ref(ref string) { s.emit(agent.Event{Kind: agent.KindSessionRef, SessionRef: ref}) }

// TurnEnd reports that the agent finished its turn.
func (s *Session) TurnEnd() { s.emit(agent.Event{Kind: agent.KindTurnEnd}) }

// Ask makes the agent ask a question and wait.
func (s *Session) Ask(ref, prompt string, options ...string) {
	s.mu.Lock()
	s.open[ref] = true
	s.mu.Unlock()
	s.emit(agent.Event{Kind: agent.KindQuestion, Question: &agent.Question{Ref: ref, Kind: domain.QuestionAsk, Prompt: prompt, Options: options}})
}

// RequestApproval makes the agent ask for permission and wait.
func (s *Session) RequestApproval(ref, prompt string) {
	s.mu.Lock()
	s.open[ref] = true
	s.mu.Unlock()
	s.emit(agent.Event{Kind: agent.KindQuestion, Question: &agent.Question{Ref: ref, Kind: domain.QuestionApproval, Prompt: prompt, Options: []string{"Allow", "Deny"}}})
}

// WithdrawQuestion reports that the agent no longer needs an answer.
func (s *Session) WithdrawQuestion(ref string) {
	s.mu.Lock()
	delete(s.open, ref)
	s.mu.Unlock()
	s.emit(agent.Event{Kind: agent.KindQuestionClosed, Ref: ref})
}

// Exit ends the session as if the process exited with code. Zero completes it;
// anything else fails it with reason.
func (s *Session) Exit(code int, reason string) {
	res := agent.Result{State: domain.RunCompleted, ExitCode: code}
	if code != 0 {
		res.State, res.Reason = domain.RunFailed, reason
	}
	s.end(res)
}

func (s *Session) emit(ev agent.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ended {
		return
	}
	s.events <- ev
}

func (s *Session) end(res agent.Result) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ended {
		return
	}
	s.ended, s.result = true, res
	close(s.events)
	close(s.done)
}

// Sent returns the messages the user sent, in order (the first is not the
// prompt: see Req.Prompt).
func (s *Session) Sent() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.sent...)
}

// Responses returns the answers given to questions, in order.
func (s *Session) Responses() []Response {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Response(nil), s.responses...)
}

// CloseRequested and StopRequested report how the controller ended the session.
func (s *Session) CloseRequested() bool { s.mu.Lock(); defer s.mu.Unlock(); return s.closeReq }
func (s *Session) StopRequested() bool  { s.mu.Lock(); defer s.mu.Unlock(); return s.stopReq }

// Events implements agent.Session.
func (s *Session) Events() <-chan agent.Event { return s.events }

// Send implements agent.Session.
func (s *Session) Send(_ context.Context, text string) error {
	s.mu.Lock()
	ended, fn := s.ended, s.OnSend
	s.mu.Unlock()
	if ended {
		return agent.ErrEnded
	}
	if fn != nil {
		if err := fn(text); err != nil {
			return err
		}
	}
	s.mu.Lock()
	s.sent = append(s.sent, text)
	s.mu.Unlock()
	return nil
}

// Respond implements agent.Session.
func (s *Session) Respond(_ context.Context, ref, answer string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ended {
		return agent.ErrEnded
	}
	if !s.open[ref] {
		return fmt.Errorf("%s: %w", ref, agent.ErrUnknownQuestion)
	}
	delete(s.open, ref)
	s.responses = append(s.responses, Response{ref, answer})
	return nil
}

// Close implements agent.Session.
func (s *Session) Close(context.Context) error {
	s.mu.Lock()
	if s.ended {
		s.mu.Unlock()
		return agent.ErrEnded
	}
	s.closeReq = true
	fn := s.OnClose
	s.mu.Unlock()
	if fn != nil {
		fn(s)
		return nil
	}
	s.end(agent.Result{State: domain.RunCompleted})
	return nil
}

// Stop implements agent.Session.
func (s *Session) Stop(ctx context.Context) error {
	s.mu.Lock()
	s.stopReq = true
	fn := s.OnStop
	s.mu.Unlock()
	if fn != nil {
		fn(s)
	} else {
		s.end(agent.Result{State: domain.RunFailed, Reason: "terminated", ExitCode: -1})
	}
	select {
	case <-s.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Wait implements agent.Session.
func (s *Session) Wait() agent.Result {
	<-s.done
	return s.result
}

// Process implements agent.Session. The PID is fake and the ID is empty, so it
// can never be signalled.
func (s *Session) Process() agent.ProcessInfo { return agent.ProcessInfo{PID: s.pid} }
