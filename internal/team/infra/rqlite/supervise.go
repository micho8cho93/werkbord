package rqlite

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// supervise watches the process and restarts it when it ends unexpectedly.
func (s *Supervisor) supervise(ctx context.Context, v verified, cfg NodeConfig, cur *spawnedProc, firstExit chan<- error) {
	defer close(s.done)
	started := s.opts.Now()
	delay := s.opts.RestartBackoff
	reported := false
	grace := 3 * time.Second
	for {
		err := <-cur.wait
		if ctx.Err() != nil {
			s.mu.Lock()
			s.status.State, s.status.PID = StateStopped, 0
			s.mu.Unlock()
			return
		}
		if !reported && s.opts.Now().Sub(started) < grace {
			// It ended inside the grace period: it did not start (bad flags, a port in use, an address that is not there).
			reported = true
			if err == nil {
				err = errors.New("exited")
			}
			firstExit <- err
			return
		}
		reported = true
		if s.opts.NoRestart {
			s.mu.Lock()
			s.status.State, s.status.PID = StateFailed, 0
			s.status.LastError = fmt.Sprintf("the database program ended (%v) and is configured not to be restarted", err)
			s.mu.Unlock()
			<-ctx.Done()
			s.mu.Lock()
			s.status.State = StateStopped
			s.mu.Unlock()
			return
		}
		if s.opts.Now().Sub(started) > 30*time.Second {
			delay = s.opts.RestartBackoff
		}
		s.mu.Lock()
		s.status.State, s.status.PID = StateRestarting, 0
		s.status.Restarts++
		s.status.LastError = fmt.Sprintf("the database program ended (%v); restarting in %s%s", err, delay, s.hint())
		s.mu.Unlock()
		s.log.Warn("the database program ended unexpectedly; restarting", "err", err, "in", delay)
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
		next, spawnErr := s.spawn(ctx, v, cfg)
		s.mu.Lock()
		if spawnErr != nil {
			s.status.State, s.status.LastError = StateFailed, spawnErr.Error()
			s.mu.Unlock()
			s.log.Error("the database program could not be restarted", "err", spawnErr)
			cur = &spawnedProc{wait: make(chan error, 1)}
			cur.wait <- spawnErr
			started = s.opts.Now()
			continue
		}
		s.status.State, s.status.PID, s.status.StartedAt = StateRunning, next.cmd.Process.Pid, s.opts.Now()
		s.mu.Unlock()
		cur, started = next, s.opts.Now()
	}
}

// Stop stops the node, gracefully (a leader steps down first), and waits for it to end. Stopping a
// stopped supervisor does nothing. It does not take the node out of the cluster: that is a membership
// change (Admin.Remove), made on purpose.
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
	s.cancel, s.cfg = nil, nil
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
