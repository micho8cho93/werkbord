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

const operationRefPrefix = "werkbord-tools:"

// allowedItems are the non-action items a turn may produce. Registered application functions are checked separately;
// every other tool item ends the turn.
var allowedItems = map[string]bool{
	"userMessage": true, "agentMessage": true, "reasoning": true, "contextCompaction": true, "plan": true,
}

// turn follows one turn's notifications.
type turn struct {
	emit          func(provider.Event)
	ctx           context.Context
	operations    map[string]bool
	callOperation func(context.Context, string, string, json.RawMessage) (string, error)

	mu         sync.Mutex
	threadID   string
	turnID     string
	hasText    bool
	messages   []*agentMessageReply
	messageID  string
	usage      provider.Usage
	lastError  string
	violation  string
	status     string // the turn's end: completed, failed, interrupted
	failure    string
	interrupts func() // asks the server to stop the turn
	finished   chan struct{}
	once       sync.Once
}

type agentMessageReply struct {
	id        string
	text      strings.Builder
	final     string // the completed message is authoritative over its own pieces
	completed bool
}

// messageText preserves the boundary between commentary and subsequent messages. Without it a call fence can be
// glued to the previous sentence, making the engine treat the operation as ordinary text. It also handles a server
// that streams some messages but delivers others whole, without dropping or repeating either.
// The caller holds t.mu.
func (t *turn) messageText(id, text string, completed bool) {
	if t.violation != "" {
		return
	}
	if id == "" {
		id = t.messageID
	}
	var message *agentMessageReply
	for _, m := range t.messages {
		if m.id == id {
			message = m
			break
		}
	}
	if message == nil {
		message = &agentMessageReply{id: id}
		t.messages = append(t.messages, message)
	}
	t.messageID = id
	if completed {
		if message.completed {
			return
		}
		message.final, message.completed = text, true
		if message.text.Len() > 0 {
			return // its deltas have already reached the engine
		}
	}
	if text == "" {
		return
	}
	if message.text.Len() == 0 && t.hasText {
		t.emit(provider.Event{Kind: provider.EventText, Text: "\n\n"})
	}
	message.text.WriteString(text)
	t.hasText = true
	t.emit(provider.Event{Kind: provider.EventText, Text: text})
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
			Delta  string `json:"delta"`
			ItemID string `json:"itemId"`
		}
		if json.Unmarshal(params, &p) != nil || p.Delta == "" {
			return
		}
		t.mu.Lock()
		t.messageText(p.ItemID, p.Delta, false)
		t.mu.Unlock()
	case "item/reasoning/summaryTextDelta", "item/reasoning/textDelta":
		// Reasoning is never shown or kept; it only proves the turn is alive.
		t.emit(provider.Event{Kind: provider.EventAlive})
	case "item/started", "item/completed":
		var p struct {
			Item struct {
				Type      string  `json:"type"`
				ID        string  `json:"id"`
				Text      string  `json:"text"`
				Tool      string  `json:"tool"`
				Name      string  `json:"name"`
				Namespace *string `json:"namespace"`
			} `json:"item"`
		}
		if json.Unmarshal(params, &p) != nil {
			return
		}
		applicationCall := p.Item.Type == "dynamicToolCall" && p.Item.Namespace == nil && t.operations[firstNonEmpty(p.Item.Tool, p.Item.Name)]
		if !allowedItems[p.Item.Type] && !applicationCall {
			t.violate(fmt.Sprintf("Codex tried to do something other than reply (%q)", clip(p.Item.Type, 40)))
			return
		}
		if p.Item.Type == "agentMessage" {
			t.mu.Lock()
			if method == "item/completed" {
				t.messageText(p.Item.ID, p.Item.Text, true)
			} else {
				t.messageID = p.Item.ID
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

// request allows only registered application functions through the engine callback. Runtime approvals, choices,
// commands and unregistered tools are rejected and end the turn.
func (t *turn) request(c *client, id json.RawMessage, method string, params json.RawMessage) {
	if method == "item/tool/call" && t.callOperation != nil {
		var p struct {
			CallID    string          `json:"callId"`
			ThreadID  string          `json:"threadId"`
			TurnID    string          `json:"turnId"`
			Tool      string          `json:"tool"`
			Namespace *string         `json:"namespace"`
			Arguments json.RawMessage `json:"arguments"`
		}
		t.mu.Lock()
		threadID, turnID, violation := t.threadID, t.turnID, t.violation
		t.mu.Unlock()
		if json.Unmarshal(params, &p) == nil && violation == "" && t.ctx.Err() == nil && p.Namespace == nil && p.CallID != "" && p.ThreadID == threadID && p.TurnID == turnID && t.operations[p.Tool] && len(p.Arguments) > 0 && len(p.Arguments) <= 64<<10 {
			t.emit(provider.Event{Kind: provider.EventAlive})
			result, err := t.callOperation(t.ctx, p.CallID, p.Tool, p.Arguments)
			if err != nil {
				_ = c.respondError(id, -32603, "Werkbord could not complete the board operation")
				t.violate("Werkbord stopped the board operation")
				return
			}
			_ = c.send(map[string]any{"id": id, "result": map[string]any{"success": true, "contentItems": []map[string]any{{"type": "inputText", "text": result}}}})
			return
		}
	}
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
	native := len(t.Operations) > 0 && t.CallOperation != nil
	if native && t.Ref != "" {
		if !strings.HasPrefix(t.Ref, operationRefPrefix) {
			return provider.Result{}, provider.Errorf(provider.KindSessionLost, false, "This conversation needs to reconnect with Werkbord's board tools.")
		}
		t.Ref = strings.TrimPrefix(t.Ref, operationRefPrefix)
	}
	if native {
		t.System += "\n# Application tool transport\nThe board operations listed above are registered as native function tools for this conversation. Call these functions directly instead of writing werkbord-call blocks. Their results arrive as tool responses wrapped in <werkbord-results>. These are the only native tools you may use. Complete the requested board actions before your final answer; never end with a promise to look something up or make a proposal. The confirmation rules still apply.\n"
	}
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
		if native && e.Kind == provider.EventRef && e.Ref != "" {
			e.Ref = operationRefPrefix + e.Ref
		}
		emitMu.Lock()
		defer emitMu.Unlock()
		emit(e)
	}

	ts := &turn{emit: safeEmit, ctx: ctx, operations: make(map[string]bool), callOperation: t.CallOperation, finished: make(chan struct{})}
	for _, op := range t.Operations {
		ts.operations[op.Name] = true
	}
	// The process runs under its own context, so that a cancellation can first ask Codex to stop the turn (which it
	// writes down properly) and only then end the process.
	runCtx, cancelRun := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelRun()

	args := p.serverArgs(ctx, path)
	if native {
		// Current Codex models reach registered application functions through the isolated code-mode host.
		// Keep that dispatcher available; command, filesystem and other native tools remain disabled/guarded.
		for i := 0; i+1 < len(args); i++ {
			if args[i] == "--disable" && args[i+1] == "code_mode_host" {
				args = append(args[:i], args[i+2:]...)
				break
			}
		}
	}
	c, err := start(runCtx, path, args, t.WorkDir, handlers{notification: ts.notification, request: ts.request})
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
		// Replace the coding-agent persona as well as supplying the application instructions. Otherwise Codex can
		// interpret disabled native tools as a lack of board access and decline without emitting a text operation.
		// Apply this on resume too, including conversations created with the old default persona.
		"baseInstructions":      t.System,
		"developerInstructions": t.System,
	}
	model := firstNonEmpty(t.Model, p.cfg.Model)
	if model != "" {
		threadParams["model"] = model
	}
	method := "thread/start"
	if native && t.Ref == "" {
		var tools []map[string]any
		for _, op := range t.Operations {
			tools = append(tools, map[string]any{"type": "function", "name": op.Name, "description": op.Description, "inputSchema": op.Input})
		}
		threadParams["dynamicTools"] = tools
	}
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
		var messages []string
		for _, m := range ts.messages {
			text := m.text.String()
			if m.completed && m.final != "" {
				text = m.final
			}
			if text != "" {
				messages = append(messages, text)
			}
		}
		ref := ts.threadID
		if ts.callOperation != nil && len(ts.operations) > 0 {
			ref = operationRefPrefix + ref
		}
		return provider.Result{Text: strings.Join(messages, "\n\n"), Ref: ref, Usage: ts.usage}, nil
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
