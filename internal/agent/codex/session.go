package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"devboard/internal/agent"
	"devboard/internal/domain"
)

// session speaks Codex's app-server protocol: JSON-RPC 2.0 messages, one per
// line, without the "jsonrpc" member.
type session struct {
	*agent.Base
	workDir string
	cfg     Config

	nextID atomic.Int64

	mu        sync.Mutex
	calls     map[int64]chan rpcMessage
	threadID  string
	turnID    string // the turn in progress, if any
	pending   map[string]*request
	lastError string
}

// request is a question the server sent and is waiting on.
type request struct {
	id     json.RawMessage // the JSON-RPC id, echoed in the reply
	method string
	// requestUserInput carries several questions, put to the user one at a time.
	questions []inputQuestion
	next      int
	answers   map[string][]string
}

type inputQuestion struct {
	ID       string `json:"id"`
	Header   string `json:"header"`
	Question string `json:"question"`
	IsSecret bool   `json:"isSecret"`
	Options  []struct {
		Label string `json:"label"`
	} `json:"options"`
}

type rpcMessage struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string { return fmt.Sprintf("%s (code %d)", e.Message, e.Code) }

func newSession(spec agent.ProcSpec, workDir string, cfg Config) *session {
	s := &session{Base: agent.NewBase(spec), workDir: workDir, cfg: cfg, calls: map[int64]chan rpcMessage{}, pending: map[string]*request{}}
	s.OnLine = s.onLine
	s.Failure = func() string {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.lastError
	}
	return s
}

func (s *session) thread() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.threadID
}

// ---- calls to the server ----

// call sends a request and waits for its response, the end of the session, or ctx.
func (s *session) call(ctx context.Context, method string, params, out any) error {
	id := s.nextID.Add(1)
	ch := make(chan rpcMessage, 1)
	s.mu.Lock()
	s.calls[id] = ch
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.calls, id)
		s.mu.Unlock()
	}()

	if err := s.send(map[string]any{"id": id, "method": method, "params": params}); err != nil {
		return err
	}
	select {
	case msg := <-ch:
		if msg.Error != nil {
			return fmt.Errorf("%s: %w", method, msg.Error)
		}
		if out != nil && len(msg.Result) > 0 {
			return json.Unmarshal(msg.Result, out)
		}
		return nil
	case <-s.Done():
		return agent.ErrEnded
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *session) send(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return s.WriteLine(b)
}

// handshake opens the thread. instructions, if any, are the run's standing
// instructions from its execution policy: Codex keeps them as developer
// instructions for the thread, which is also what a resumed thread needs again.
func (s *session) handshake(ctx context.Context, resumeRef, instructions string) error {
	if err := s.call(ctx, "initialize", map[string]any{
		"clientInfo":   map[string]any{"name": "devboard", "title": "Devboard", "version": "1"},
		"capabilities": map[string]any{"experimentalApi": true}, // request_user_input is experimental
	}, nil); err != nil {
		return err
	}
	if err := s.send(map[string]any{"method": "initialized"}); err != nil {
		return err
	}

	params := map[string]any{
		"cwd": s.workDir, "approvalPolicy": s.cfg.ApprovalPolicy, "sandbox": s.cfg.Sandbox, "serviceName": "devboard",
	}
	if s.cfg.Model != "" {
		params["model"] = s.cfg.Model
	}
	if instructions != "" {
		params["developerInstructions"] = instructions
	}
	method := "thread/start"
	if resumeRef != "" {
		method, params["threadId"] = "thread/resume", resumeRef
	}
	var resp struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if err := s.call(ctx, method, params, &resp); err != nil {
		return err
	}
	if resp.Thread.ID == "" {
		return fmt.Errorf("%s: the server returned no thread id", method)
	}
	s.mu.Lock()
	s.threadID = resp.Thread.ID
	s.mu.Unlock()
	return nil
}

// Send implements agent.Session. A message sent while a turn is running steers
// it; otherwise it starts the next turn.
func (s *session) Send(ctx context.Context, text string) error {
	s.mu.Lock()
	thread, turn := s.threadID, s.turnID
	s.mu.Unlock()
	input := []map[string]any{{"type": "text", "text": text}}

	if turn != "" {
		err := s.call(ctx, "turn/steer", map[string]any{"threadId": thread, "expectedTurnId": turn, "input": input}, nil)
		if err == nil {
			return nil
		}
		var rpc *rpcError
		if !errors.As(err, &rpc) {
			return err
		}
		// The turn ended, or cannot be steered, between our look and the call:
		// fall through and begin a new one.
	}
	var resp struct {
		Turn struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	if err := s.call(ctx, "turn/start", map[string]any{"threadId": thread, "input": input}, &resp); err != nil {
		return err
	}
	s.mu.Lock()
	if s.turnID == "" {
		s.turnID = resp.Turn.ID
	}
	s.mu.Unlock()
	return nil
}

// Close implements agent.Session: Codex's app-server exits when its input ends.
func (s *session) Close(context.Context) error { return s.CloseInput() }

// ---- reading ----

func (s *session) onLine(line []byte, truncated bool) {
	if truncated {
		s.Say(domain.StreamSystem, "A very long message from Codex was cut short.")
		return
	}
	var m rpcMessage
	if err := json.Unmarshal(line, &m); err != nil {
		if t := strings.TrimSpace(string(line)); t != "" {
			s.Say(domain.StreamSystem, "%s", clip(t, 500))
		}
		return
	}
	switch {
	case m.Method != "" && len(m.ID) > 0:
		s.onServerRequest(m)
	case m.Method != "":
		s.onNotification(m.Method, m.Params)
	case len(m.ID) > 0:
		id, err := strconv.ParseInt(string(m.ID), 10, 64)
		if err != nil {
			return
		}
		s.mu.Lock()
		ch := s.calls[id]
		s.mu.Unlock()
		if ch != nil {
			ch <- m
		}
	}
}

type item struct {
	Type   string `json:"type"`
	ID     string `json:"id"`
	Text   string `json:"text"`
	Status string `json:"status"`

	Command          string `json:"command"`
	ExitCode         *int   `json:"exitCode"`
	AggregatedOutput string `json:"aggregatedOutput"`

	Changes []struct {
		Path string `json:"path"`
		Kind struct {
			Type string `json:"type"`
		} `json:"kind"`
	} `json:"changes"`

	Server string `json:"server"`
	Tool   string `json:"tool"`
	Query  string `json:"query"`
}

func (s *session) onNotification(method string, params json.RawMessage) {
	switch method {
	case "turn/started":
		var p struct {
			Turn struct {
				ID string `json:"id"`
			} `json:"turn"`
		}
		if json.Unmarshal(params, &p) == nil {
			s.mu.Lock()
			s.turnID = p.Turn.ID
			s.mu.Unlock()
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
		s.mu.Lock()
		if s.turnID == p.Turn.ID || p.Turn.ID == "" {
			s.turnID = ""
		}
		s.mu.Unlock()
		switch p.Turn.Status {
		case "failed":
			why := "unknown error"
			if p.Turn.Error != nil && p.Turn.Error.Message != "" {
				why = humanize(p.Turn.Error.Message)
			}
			s.mu.Lock()
			s.lastError = clip(why, 500)
			s.mu.Unlock()
			s.Say(domain.StreamSystem, "The turn failed: %s", clip(why, 500))
		case "interrupted":
			s.Say(domain.StreamSystem, "The turn was interrupted.")
		}
		s.Emit(agent.Event{Kind: agent.KindTurnEnd})

	case "item/started":
		var p struct {
			Item item `json:"item"`
		}
		if json.Unmarshal(params, &p) != nil {
			return
		}
		switch it := p.Item; it.Type {
		case "commandExecution":
			s.Emit(agent.Event{Kind: agent.KindOutput, Stream: domain.StreamTool, Text: "$ " + clip(firstLine(it.Command), 300)})
		case "mcpToolCall":
			s.Emit(agent.Event{Kind: agent.KindOutput, Stream: domain.StreamTool, Text: "MCP " + it.Server + "/" + it.Tool})
		case "dynamicToolCall":
			s.Emit(agent.Event{Kind: agent.KindOutput, Stream: domain.StreamTool, Text: "Tool " + it.Tool})
		}

	case "item/completed":
		var p struct {
			Item item `json:"item"`
		}
		if json.Unmarshal(params, &p) != nil {
			return
		}
		s.onItemCompleted(p.Item)

	case "error":
		var p struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
			WillRetry bool `json:"willRetry"`
		}
		if json.Unmarshal(params, &p) != nil || p.Error.Message == "" {
			return
		}
		msg := humanize(p.Error.Message)
		if p.WillRetry {
			s.Say(domain.StreamSystem, "Temporary error, retrying: %s", clip(msg, 300))
			return
		}
		s.mu.Lock()
		s.lastError = clip(msg, 500)
		s.mu.Unlock()
		s.Say(domain.StreamSystem, "Error: %s", clip(msg, 500))

	case "serverRequest/resolved":
		var p struct {
			RequestID json.RawMessage `json:"requestId"`
		}
		if json.Unmarshal(params, &p) != nil {
			return
		}
		s.dropRequest(string(p.RequestID))
	}
}

func (s *session) onItemCompleted(it item) {
	tool := func(format string, args ...any) {
		s.Emit(agent.Event{Kind: agent.KindOutput, Stream: domain.StreamTool, Text: fmt.Sprintf(format, args...)})
	}
	switch it.Type {
	case "agentMessage":
		if t := strings.TrimSpace(it.Text); t != "" {
			s.Emit(agent.Event{Kind: agent.KindOutput, Stream: domain.StreamAssistant, Text: t})
		}
	case "plan":
		if t := strings.TrimSpace(it.Text); t != "" {
			s.Emit(agent.Event{Kind: agent.KindOutput, Stream: domain.StreamAssistant, Text: "Plan:\n" + t})
		}
	case "commandExecution":
		switch {
		case it.Status == "declined":
			tool("Command declined")
		case it.Status == "failed" || (it.ExitCode != nil && *it.ExitCode != 0):
			code := -1
			if it.ExitCode != nil {
				code = *it.ExitCode
			}
			msg := fmt.Sprintf("Command failed (exit %d)", code)
			if out := lastLine(strings.TrimSpace(it.AggregatedOutput)); out != "" {
				msg += ": " + clip(out, 200)
			}
			tool("%s", msg)
		}
	case "fileChange":
		if it.Status == "declined" {
			tool("File changes declined")
			return
		}
		if len(it.Changes) == 0 {
			return
		}
		var parts []string
		for _, c := range it.Changes {
			verb := map[string]string{"add": "Created", "delete": "Deleted"}[c.Kind.Type]
			if verb == "" {
				verb = "Edited"
			}
			parts = append(parts, verb+" "+relativeTo(s.workDir, c.Path))
		}
		tool("%s", clip(strings.Join(parts, ", "), 400))
	case "webSearch":
		if it.Query != "" {
			tool("Web search: %s", clip(it.Query, 200))
		}
	case "contextCompaction":
		s.Say(domain.StreamSystem, "Context compacted.")
	}
}

// ---- requests from the server ----

func (s *session) onServerRequest(m rpcMessage) {
	ref := string(m.ID)
	switch m.Method {
	case "item/commandExecution/requestApproval":
		var p struct {
			Command string `json:"command"`
			Reason  string `json:"reason"`
		}
		_ = json.Unmarshal(m.Params, &p)
		detail := "$ " + clip(p.Command, 1500)
		if p.Reason != "" {
			detail += "\n\n" + clip(p.Reason, 500)
		}
		s.openApproval(m, ref, "Run this command?", detail)

	case "item/fileChange/requestApproval":
		var p struct {
			Reason    string `json:"reason"`
			GrantRoot string `json:"grantRoot"`
		}
		_ = json.Unmarshal(m.Params, &p)
		var detail []string
		if p.Reason != "" {
			detail = append(detail, clip(p.Reason, 500))
		}
		if p.GrantRoot != "" {
			detail = append(detail, "This lets Codex write under "+p.GrantRoot+" for the rest of the session.")
		}
		s.openApproval(m, ref, "Apply these file changes?", strings.Join(detail, "\n\n"))

	case "item/tool/requestUserInput":
		var p struct {
			Questions []inputQuestion `json:"questions"`
		}
		if json.Unmarshal(m.Params, &p) != nil || len(p.Questions) == 0 {
			s.reply(m.ID, map[string]any{"answers": map[string]any{}})
			return
		}
		for _, q := range p.Questions {
			if q.IsSecret {
				// Answers are stored and shown in the activity feed; a secret does not belong there.
				s.Say(domain.StreamSystem, "Codex asked for a secret value (%s). Devboard does not collect secrets; it was told there is no answer.", clip(q.Header, 80))
				answers := map[string]any{}
				for _, q := range p.Questions {
					answers[q.ID] = map[string]any{"answers": []string{}}
				}
				s.reply(m.ID, map[string]any{"answers": answers})
				return
			}
		}
		req := &request{id: m.ID, method: m.Method, questions: p.Questions, answers: map[string][]string{}}
		s.mu.Lock()
		s.pending[ref] = req
		s.mu.Unlock()
		s.emitInput(ref, req)

	default:
		// Anything else would leave the agent waiting forever for a reply.
		s.send(map[string]any{"id": m.ID, "error": map[string]any{"code": -32601, "message": "devboard does not support " + m.Method}})
		s.Say(domain.StreamSystem, "Codex asked for something Devboard does not support (%s); it was declined.", m.Method)
	}
}

func (s *session) openApproval(m rpcMessage, ref, prompt, detail string) {
	s.mu.Lock()
	s.pending[ref] = &request{id: m.ID, method: m.Method}
	s.mu.Unlock()
	s.Emit(agent.Event{Kind: agent.KindQuestion, Question: &agent.Question{
		Ref: ref, Kind: domain.QuestionApproval, Prompt: prompt, Context: detail,
		Options: []string{domain.AnswerAllow, "Allow for this session", domain.AnswerDeny},
	}})
}

func (s *session) emitInput(ref string, req *request) {
	q := req.questions[req.next]
	prompt := q.Question
	if q.Header != "" && !strings.Contains(q.Question, q.Header) {
		prompt = q.Header + ": " + prompt
	}
	if len(req.questions) > 1 {
		prompt = fmt.Sprintf("(%d of %d) %s", req.next+1, len(req.questions), prompt)
	}
	opts := make([]string, 0, len(q.Options))
	for _, o := range q.Options {
		opts = append(opts, o.Label)
	}
	kind := domain.QuestionClarification
	if len(opts) > 0 {
		kind = domain.QuestionSelection
	}
	s.Emit(agent.Event{Kind: agent.KindQuestion, Question: &agent.Question{
		Ref: ref + "#" + strconv.Itoa(req.next), Kind: kind, Prompt: prompt, Options: opts, AllowFreeText: true,
	}})
}

func (s *session) reply(id json.RawMessage, result any) {
	_ = s.send(map[string]any{"id": id, "result": result})
}

func (s *session) dropRequest(id string) {
	s.mu.Lock()
	req := s.pending[id]
	delete(s.pending, id)
	s.mu.Unlock()
	if req == nil {
		return
	}
	ref := id
	if len(req.questions) > 0 {
		ref = id + "#" + strconv.Itoa(req.next)
	}
	s.Emit(agent.Event{Kind: agent.KindQuestionClosed, Ref: ref})
}

// Respond implements agent.Session.
func (s *session) Respond(_ context.Context, ref, answer string) error {
	id, idx := ref, -1
	if base, n, ok := strings.Cut(ref, "#"); ok {
		v, err := strconv.Atoi(n)
		if err != nil || v < 0 {
			return fmt.Errorf("%s: %w", ref, agent.ErrUnknownQuestion)
		}
		id, idx = base, v
	}

	s.mu.Lock()
	req := s.pending[id]
	switch {
	case req == nil:
		s.mu.Unlock()
		return fmt.Errorf("%s: %w", ref, agent.ErrUnknownQuestion)
	case len(req.questions) > 0 && idx != req.next, len(req.questions) == 0 && idx >= 0:
		s.mu.Unlock()
		return fmt.Errorf("%s: %w", ref, agent.ErrUnknownQuestion)
	}

	if len(req.questions) > 0 {
		q := req.questions[req.next]
		req.answers[q.ID] = []string{strings.TrimSpace(answer)}
		req.next++
		if req.next < len(req.questions) {
			s.mu.Unlock()
			s.emitInput(id, req)
			return nil
		}
		delete(s.pending, id)
		answers := map[string]any{}
		for qid, a := range req.answers {
			answers[qid] = map[string]any{"answers": a}
		}
		s.mu.Unlock()
		return s.send(map[string]any{"id": req.id, "result": map[string]any{"answers": answers}})
	}

	delete(s.pending, id)
	s.mu.Unlock()
	return s.send(map[string]any{"id": req.id, "result": map[string]any{"decision": decision(answer)}})
}

func decision(answer string) string {
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "allow", "approve", "yes", "y", "ok":
		return "accept"
	case "allow for this session", "allow for session", "session":
		return "acceptForSession"
	}
	return "decline"
}

// humanize turns an error the API returned as a JSON document into its message.
func humanize(msg string) string {
	msg = strings.TrimSpace(msg)
	if !strings.HasPrefix(msg, "{") {
		return msg
	}
	var doc struct {
		Message string `json:"message"`
		Error   struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal([]byte(msg), &doc) != nil {
		return msg
	}
	if doc.Error.Message != "" {
		return doc.Error.Message
	}
	if doc.Message != "" {
		return doc.Message
	}
	return msg
}

func clip(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i] + " …"
	}
	return s
}

func relativeTo(dir, path string) string {
	if dir == "" {
		return path
	}
	if rel, ok := strings.CutPrefix(path, strings.TrimSuffix(dir, "/")+"/"); ok {
		return rel
	}
	return path
}

func (s *session) activeTurn() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.turnID
}
