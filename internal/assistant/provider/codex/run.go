package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"devboard/internal/assistant/provider"
)

// violationGrace is how long a turn that broke the rules gets to acknowledge being interrupted before it is killed.
var violationGrace = 3 * time.Second

// cancelGrace is how long a cancelled turn gets to acknowledge being interrupted before it is killed.
var cancelGrace = 400 * time.Millisecond

// allowedItems are the only kinds of thing a turn may produce. Anything else is a tool being used, whatever it is
// called, and ends the turn: the list of what is allowed is short, the list of what is not is not.
var allowedItems = map[string]bool{
	"userMessage": true, "agentMessage": true, "reasoning": true, "contextCompaction": true, "plan": true,
}

// turn follows one turn's notifications.
type turn struct {
	emit func(provider.Event)

	mu         sync.Mutex
	threadID   string
	turnID     string
	text       strings.Builder
	final      string // the completed agent message, which is authoritative over the pieces
	sawDelta   bool
	usage      provider.Usage
	lastError  string
	violation  string
	status     string // the turn's end: completed, failed, interrupted
	failure    string
	interrupts func() // asks the server to stop the turn
	finished   chan struct{}
	once       sync.Once
}

func (t *turn) finish() { t.once.Do(func() { close(t.finished) }) }

func (t *turn) notification(method string, params json.RawMessage) {
	switch method {
	case "turn/started":
		var p struct {
			Turn struct {
				ID string `json:"id"`
			} `json:"turn"`
		}
		if json.Unmarshal(params, &p) == nil {
			t.mu.Lock()
			t.turnID = p.Turn.ID
			t.mu.Unlock()
		}
	case "item/agentMessage/delta":
		var p struct {
			Delta string `json:"delta"`
		}
		if json.Unmarshal(params, &p) != nil || p.Delta == "" {
			return
		}
		t.mu.Lock()
		quiet := t.violation != ""
		if !quiet {
			t.sawDelta = true
			t.text.WriteString(p.Delta)
		}
		t.mu.Unlock()
		if !quiet {
			t.emit(provider.Event{Kind: provider.EventText, Text: p.Delta})
		}
	case "item/reasoning/summaryTextDelta", "item/reasoning/textDelta":
		// Reasoning is never shown or kept; it only proves the turn is alive.
		t.emit(provider.Event{Kind: provider.EventAlive})
	case "item/started", "item/completed":
		var p struct {
			Item struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"item"`
		}
		if json.Unmarshal(params, &p) != nil {
			return
		}
		if !allowedItems[p.Item.Type] {
			t.violate(fmt.Sprintf("Codex tried to do something other than reply (%q)", clip(p.Item.Type, 40)))
			return
		}
		if method == "item/completed" && p.Item.Type == "agentMessage" {
			t.mu.Lock()
			if t.violation == "" {
				t.final = p.Item.Text
			}
			t.mu.Unlock()
		}
	case "thread/tokenUsage/updated":
		var p struct {
			TokenUsage struct {
				Last struct {
					Input  int64 `json:"inputTokens"`
					Output int64 `json:"outputTokens"`
				} `json:"last"`
			} `json:"tokenUsage"`
		}
		if json.Unmarshal(params, &p) == nil {
			t.mu.Lock()
			t.usage = provider.Usage{InputTokens: p.TokenUsage.Last.Input, OutputTokens: p.TokenUsage.Last.Output}
			t.mu.Unlock()
		}
	default:
		// Anything else Codex says (status changes, server start-up) shows it is working, which is all it is for.
		t.emit(provider.Event{Kind: provider.EventAlive})
	case "error":
		var p struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
			WillRetry bool `json:"willRetry"`
		}
		if json.Unmarshal(params, &p) == nil && p.Error.Message != "" && !p.WillRetry {
			t.mu.Lock()
			t.lastError = p.Error.Message
			t.mu.Unlock()
		}
	case "turn/completed":
		var p struct {
			Turn struct {
				ID     string `json:"id"`
				Status string `json:"status"`
				Error  *struct {
					Message string `json:"message"`
				} `json:"error"`
			} `json:"turn"`
		}
		if json.Unmarshal(params, &p) != nil {
			return
		}
		t.mu.Lock()
		t.status = p.Turn.Status
		if p.Turn.Error != nil {
			t.failure = p.Turn.Error.Message
		}
		t.mu.Unlock()
		t.finish()
	}
}

// request handles what the server asks of the client. A turn that is only meant to reply has no reason to be asked for
// an approval, a choice or an answer; being asked means it is trying to act.
func (t *turn) request(c *client, id json.RawMessage, method string, _ json.RawMessage) {
	_ = c.respondError(id, -32601, "the assistant does not do that")
	if strings.HasPrefix(method, "item/") || strings.Contains(method, "Approval") || strings.Contains(method, "elicitation") ||
		strings.Contains(method, "requestUserInput") || strings.Contains(method, "ynamicTool") || strings.Contains(method, "tool/call") {
		t.violate(fmt.Sprintf("Codex asked for something (%s) although it is only meant to reply", clip(method, 60)))
	}
}

// violate ends the turn: it is interrupted, and nothing more of what it says is passed on.
func (t *turn) violate(why string) {
	t.mu.Lock()
	first := t.violation == ""
	if first {
		t.violation = why
	}
	stop := t.interrupts
	t.mu.Unlock()
	if first && stop != nil {
		stop()
	}
}

// Run implements provider.Provider.
func (p *Provider) Run(ctx context.Context, t provider.Turn, emit func(provider.Event)) (provider.Result, error) {
	info := p.Detect(ctx)
	if !info.Installed {
		return provider.Result{}, provider.Errorf(provider.KindNotInstalled, false, "%s. %s", info.Detail, info.Guidance)
	}
	if !info.Available && info.SignedIn != nil && !*info.SignedIn {
		return provider.Result{}, provider.Errorf(provider.KindNotSignedIn, false, "Codex is not signed in. %s", info.Guidance)
	}
	if !info.Available {
		return provider.Result{}, provider.Errorf(provider.KindFailed, false, "Codex cannot be used: %s. %s", info.Detail, info.Guidance)
	}
	path, err := exec.LookPath(p.cfg.Command)
	if err != nil {
		return provider.Result{}, provider.Errorf(provider.KindNotInstalled, false, "%q was not found on PATH", p.cfg.Command)
	}

	var emitMu sync.Mutex
	safeEmit := func(e provider.Event) {
		emitMu.Lock()
		defer emitMu.Unlock()
		emit(e)
	}

	ts := &turn{emit: safeEmit, finished: make(chan struct{})}
	// The process runs under its own context, so that a cancellation can first ask Codex to stop the turn (which it
	// writes down properly) and only then end the process.
	runCtx, cancelRun := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelRun()

	c, err := start(runCtx, path, p.serverArgs(ctx, path), t.WorkDir, handlers{notification: ts.notification, request: ts.request})
	if err != nil {
		return provider.Result{}, startError(err)
	}
	defer c.close()

	interrupt := func(grace time.Duration) {
		ts.mu.Lock()
		thread, turnID := ts.threadID, ts.turnID
		ts.mu.Unlock()
		if thread != "" && turnID != "" {
			_ = c.send(map[string]any{"id": -1, "method": "turn/interrupt", "params": map[string]any{"threadId": thread, "turnId": turnID}})
		}
		time.AfterFunc(grace, cancelRun)
	}
	ts.mu.Lock()
	ts.interrupts = func() { interrupt(violationGrace) }
	ts.mu.Unlock()
	go func() { // a cancelled turn is interrupted politely, then ended
		select {
		case <-ctx.Done():
			interrupt(cancelGrace)
		case <-c.done:
		}
	}()

	// Open the thread: a new one, or the one the last turn left.
	threadParams := map[string]any{
		"cwd": t.WorkDir, "approvalPolicy": "never", "sandbox": "read-only", "serviceName": "werkbord-assistant",
		"developerInstructions": t.System,
	}
	model := firstNonEmpty(t.Model, p.cfg.Model)
	if model != "" {
		threadParams["model"] = model
	}
	method := "thread/start"
	if t.Ref != "" {
		method, threadParams["threadId"] = "thread/resume", t.Ref
	}
	var resp struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if err := c.call(ctx, method, threadParams, &resp); err != nil {
		return provider.Result{}, p.runError(ctx, ts, err, t.Ref != "")
	}
	if resp.Thread.ID == "" {
		return provider.Result{}, provider.Errorf(provider.KindProtocol, false, "Codex returned no conversation id")
	}
	ts.mu.Lock()
	ts.threadID = resp.Thread.ID
	ts.mu.Unlock()
	safeEmit(provider.Event{Kind: provider.EventRef, Ref: resp.Thread.ID})

	turnParams := map[string]any{
		"threadId": resp.Thread.ID, "approvalPolicy": "never",
		"input": []map[string]any{{"type": "text", "text": t.Prompt}},
	}
	if model != "" {
		turnParams["model"] = model
	}
	if t.Reasoning != "" {
		turnParams["effort"] = t.Reasoning
	}
	var started struct {
		Turn struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	if err := c.call(ctx, "turn/start", turnParams, &started); err != nil {
		return provider.Result{}, p.runError(ctx, ts, err, false)
	}
	ts.mu.Lock()
	if ts.turnID == "" {
		ts.turnID = started.Turn.ID
	}
	ts.mu.Unlock()

	select {
	case <-ts.finished:
	case <-c.done:
		// The process went away without finishing the turn.
	case <-ctx.Done():
		<-c.done // the interrupt above ends it, or the grace period does
	}
	return p.result(ctx, ts, c)
}

func (p *Provider) result(ctx context.Context, ts *turn, c *client) (provider.Result, error) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	switch {
	case ts.violation != "":
		return provider.Result{}, &provider.Error{Kind: provider.KindPolicy, Message: ts.violation + "; the turn was ended", Err: provider.ErrPolicy}
	case ctx.Err() != nil:
		return provider.Result{}, ctx.Err()
	case ts.status == "completed":
		text := ts.final
		if text == "" {
			text = ts.text.String()
		}
		if !ts.sawDelta && text != "" { // a version that does not stream: the reply arrives whole
			ts.emit(provider.Event{Kind: provider.EventText, Text: text})
		}
		return provider.Result{Text: text, Ref: ts.threadID, Usage: ts.usage}, nil
	case ts.status == "interrupted":
		return provider.Result{}, provider.Errorf(provider.KindTransient, true, "Codex ended the turn early")
	case ts.status == "failed":
		return provider.Result{}, classify(firstNonEmpty(ts.failure, ts.lastError))
	}
	// No end to the turn: the process died.
	c.mu.Lock()
	exit := c.exit
	c.mu.Unlock()
	why := firstNonEmpty(ts.lastError, exit.Stderr)
	if why == "" {
		return provider.Result{}, provider.Errorf(provider.KindTransient, true, "Codex stopped unexpectedly (exit code %d)", exit.Code)
	}
	if e := classify(why); e.Kind != provider.KindFailed {
		return provider.Result{}, e
	}
	// It stopped without a word of its own about why: a crash, which a second try may well get past.
	return provider.Result{}, provider.Errorf(provider.KindTransient, true, "Codex stopped unexpectedly (exit code %d): %s", exit.Code, clip(why, 300))
}

// runError turns a failed call into the error the engine needs.
func (p *Provider) runError(ctx context.Context, ts *turn, err error, resuming bool) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	ts.mu.Lock()
	violation := ts.violation
	ts.mu.Unlock()
	if violation != "" {
		return &provider.Error{Kind: provider.KindPolicy, Message: violation + "; the turn was ended", Err: provider.ErrPolicy}
	}
	if rpc, ok := isRPC(err); ok {
		low := strings.ToLower(rpc.Message)
		if resuming && containsAny(low, "no rollout", "not found", "no such thread", "unknown thread", "invalid thread", "thread not") {
			return provider.Errorf(provider.KindSessionLost, false, "Codex no longer has this conversation")
		}
		return classify(rpc.Message)
	}
	return startError(err)
}

func startError(err error) error {
	return provider.Errorf(provider.KindTransient, true, "Codex could not be started: %s", clip(err.Error(), 300))
}

// classify says what kind of failure message is. Codex wraps the service's error in JSON; the words that matter are
// inside.
func classify(raw string) *provider.Error {
	msg := raw
	status := 0
	var wrapped struct {
		Status int `json:"status"`
		Error  struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal([]byte(raw), &wrapped) == nil && wrapped.Error.Message != "" {
		msg, status = wrapped.Error.Message, wrapped.Status
	}
	msg = clip(msg, 400)
	low := strings.ToLower(msg)
	switch {
	case containsAny(low, "model") && containsAny(low, "not supported", "does not exist", "not found", "not available", "no access"):
		return provider.Errorf(provider.KindModelUnavailable, false, "That model is not available with your Codex account: %s", msg)
	case status == 401 || containsAny(low, "unauthorized", "not logged in", "log in again", "sign in", "token expired", "refresh token"):
		return provider.Errorf(provider.KindNotSignedIn, false, "Codex is not signed in, or its sign-in has expired. Run `codex login` in a terminal. (%s)", msg)
	case status == 429 || containsAny(low, "usage limit", "rate limit", "too many requests", "quota"):
		return provider.Errorf(provider.KindRateLimited, false, "Your Codex plan has no allowance left for now: %s", msg)
	case status >= 500 || containsAny(low, "overloaded", "stream disconnected", "connection", "timed out", "timeout", "temporarily", "unavailable"):
		return provider.Errorf(provider.KindTransient, true, "Codex had a temporary problem: %s", msg)
	}
	if msg == "" {
		return provider.Errorf(provider.KindTransient, true, "Codex ended without an answer")
	}
	return provider.Errorf(provider.KindFailed, false, "Codex failed: %s", msg)
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

func clip(s string, max int) string {
	r := []rune(strings.Join(strings.Fields(s), " "))
	if len(r) <= max {
		return string(r)
	}
	return string(r[:max-1]) + "…"
}
