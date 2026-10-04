package claude

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"devboard/internal/agent"
	"devboard/internal/domain"
)

// session speaks Claude Code's stream-json protocol over the agent's stdio.
type session struct {
	*agent.Base
	workDir string

	mu           sync.Mutex
	sessionRef   string
	pending      map[string]*request // by request ID
	lastError    string
	resumedUsage bool
}

// request is a permission request the agent is blocked on.
type request struct {
	id    string
	tool  string
	input map[string]any

	// AskUserQuestion carries several questions; they are put to the user one at
	// a time, and the agent gets all the answers at once.
	questions []ask
	next      int
	answers   map[string]string
}

type ask struct {
	Question string `json:"question"`
	Header   string `json:"header"`
	Options  []struct {
		Label       string `json:"label"`
		Description string `json:"description"`
	} `json:"options"`
	MultiSelect bool `json:"multiSelect"`
}

func newSession(spec agent.ProcSpec, workDir string) *session {
	s := &session{Base: agent.NewBase(spec), workDir: workDir, pending: map[string]*request{}}
	s.OnLine = s.onLine
	s.Failure = func() string {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.lastError
	}
	return s
}

// Send implements agent.Session.
func (s *session) Send(_ context.Context, text string) error {
	return s.write(map[string]any{
		"type": "user",
		"message": map[string]any{
			"role":    "user",
			"content": []map[string]string{{"type": "text", "text": text}},
		},
	})
}

// Close implements agent.Session: end of input is how Claude Code is told to finish.
func (s *session) Close(context.Context) error { return s.CloseInput() }

func (s *session) write(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return s.WriteLine(b)
}

// ---- reading ----

type envelope struct {
	Cost       *float64 `json:"total_cost_usd"`
	ModelUsage map[string]struct {
		Input      *int64 `json:"inputTokens"`
		Output     *int64 `json:"outputTokens"`
		Cached     *int64 `json:"cacheReadInputTokens"`
		CacheWrite *int64 `json:"cacheCreationInputTokens"`
	} `json:"modelUsage"`

	Type      string `json:"type"`
	Subtype   string `json:"subtype"`
	SessionID string `json:"session_id"`
	Model     string `json:"model"`
	Message   *struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
	IsError bool   `json:"is_error"`
	Result  string `json:"result"`

	RequestID string `json:"request_id"`
	Request   *struct {
		Subtype     string         `json:"subtype"`
		ToolName    string         `json:"tool_name"`
		Input       map[string]any `json:"input"`
		Description string         `json:"description"`
	} `json:"request"`

	RateLimit *struct {
		Status string `json:"status"`
	} `json:"rate_limit_info"`
}

type block struct {
	Type    string         `json:"type"`
	Text    string         `json:"text"`
	Name    string         `json:"name"`
	Input   map[string]any `json:"input"`
	IsError bool           `json:"is_error"`
	Content json.RawMessage
}

func (s *session) onLine(line []byte, truncated bool) {
	if truncated {
		s.Say(domain.StreamSystem, "A very long line of agent output was cut short.")
		return
	}
	line = []byte(strings.TrimSpace(string(line)))
	if len(line) == 0 {
		return
	}
	var e envelope
	if err := json.Unmarshal(line, &e); err != nil {
		// Not JSON: a stray message the CLI printed. Show it rather than lose it.
		s.Say(domain.StreamSystem, "%s", clip(string(line), 500))
		return
	}
	switch e.Type {
	case "system":
		s.onSystem(&e)
	case "assistant":
		s.onAssistant(&e)
	case "user":
		s.onUser(&e)
	case "result":
		s.onResult(&e)
	case "control_request":
		s.onControlRequest(&e)
	case "control_cancel_request":
		s.onCancel(e.RequestID)
	case "rate_limit_event":
		if st := e.RateLimit; st != nil && st.Status != "" && st.Status != "allowed" && st.Status != "allowed_warning" {
			s.Say(domain.StreamSystem, "Rate limit: %s", st.Status)
		}
	}
}

func (s *session) onSystem(e *envelope) {
	if e.Subtype != "init" {
		return
	}
	if e.SessionID != "" {
		s.mu.Lock()
		changed := e.SessionID != s.sessionRef
		s.sessionRef = e.SessionID
		s.mu.Unlock()
		if changed {
			s.Emit(agent.Event{Kind: agent.KindSessionRef, SessionRef: e.SessionID})
		}
	}
	if e.Model != "" {
		s.Say(domain.StreamSystem, "Session started with %s", e.Model)
	}
}

func (s *session) blocks(e *envelope) []block {
	if e.Message == nil {
		return nil
	}
	var bs []block
	if err := json.Unmarshal(e.Message.Content, &bs); err != nil {
		return nil // plain-string content: a user message, which is echoed elsewhere
	}
	return bs
}

func (s *session) onAssistant(e *envelope) {
	for _, b := range s.blocks(e) {
		switch b.Type {
		case "text":
			if t := strings.TrimSpace(b.Text); t != "" {
				s.Emit(agent.Event{Kind: agent.KindOutput, Stream: domain.StreamAssistant, Text: t})
			}
		case "tool_use":
			s.Emit(agent.Event{Kind: agent.KindOutput, Stream: domain.StreamTool, Text: summarizeTool(b.Name, b.Input, s.workDir)})
		}
	}
}

// onUser reports failed tool calls. Successful results are skipped: they can be
// enormous and the call that produced them is already in the feed.
func (s *session) onUser(e *envelope) {
	for _, b := range s.blocks(e) {
		if b.Type == "tool_result" && b.IsError {
			s.Emit(agent.Event{Kind: agent.KindOutput, Stream: domain.StreamTool, Text: "Tool error: " + clip(firstLine(resultText(b.Content)), 300)})
		}
	}
}

// resultText extracts text from a tool_result's content, a string or blocks.
func resultText(raw json.RawMessage) string {
	var str string
	if json.Unmarshal(raw, &str) == nil {
		return str
	}
	var parts []struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) == nil {
		var sb strings.Builder
		for _, p := range parts {
			sb.WriteString(p.Text)
		}
		return sb.String()
	}
	return ""
}

func (s *session) onResult(e *envelope) {
	if !s.resumedUsage && (e.Cost != nil || len(e.ModelUsage) > 0) {
		u := domain.Usage{CostKind: "usage_only", Source: "Claude CLI cumulative modelUsage; API-equivalent estimate"}
		if e.Cost != nil {
			u.CostUSD = e.Cost
			u.CostKind = "estimated_api_equivalent"
		}
		if len(e.ModelUsage) > 0 {
			var input, output, cached int64
			complete := true
			for _, m := range e.ModelUsage {
				if m.Input == nil || m.Output == nil {
					complete = false
					break
				}
				input += *m.Input
				output += *m.Output
				if m.Cached != nil {
					cached += *m.Cached
					input += *m.Cached
				}
				if m.CacheWrite != nil {
					input += *m.CacheWrite
				}
			}
			if complete {
				u.InputTokens = &input
				u.OutputTokens = &output
				u.CachedTokens = &cached
			}
		}
		if u.Validate() == nil {
			s.Emit(agent.Event{Kind: agent.KindUsage, Usage: &u})
		}
	}

	if e.IsError || strings.HasPrefix(e.Subtype, "error") {
		why := strings.TrimSpace(e.Result)
		if why == "" {
			why = e.Subtype
		}
		s.mu.Lock()
		s.lastError = clip(why, 500)
		s.mu.Unlock()
		s.Emit(agent.Event{Kind: agent.KindOutput, Stream: domain.StreamSystem, Text: "The turn failed: " + clip(why, 500)})
	}
	s.Emit(agent.Event{Kind: agent.KindTurnEnd})
}

// ---- permission requests ----

func (s *session) onControlRequest(e *envelope) {
	if e.Request == nil || e.RequestID == "" {
		return
	}
	if e.Request.Subtype != "can_use_tool" {
		_ = s.write(map[string]any{"type": "control_response", "response": map[string]any{
			"subtype": "error", "request_id": e.RequestID, "error": "devboard does not support " + e.Request.Subtype,
		}})
		return
	}
	req := &request{id: e.RequestID, tool: e.Request.ToolName, input: e.Request.Input}
	if req.input == nil {
		req.input = map[string]any{}
	}

	if req.tool == "AskUserQuestion" {
		if qs := parseAsks(req.input); len(qs) > 0 {
			req.questions, req.answers = qs, map[string]string{}
			s.mu.Lock()
			s.pending[req.id] = req
			s.mu.Unlock()
			s.emitAsk(req)
			return
		}
	}
	s.mu.Lock()
	s.pending[req.id] = req
	s.mu.Unlock()
	prompt, detail := approvalRequest(req.tool, req.input, s.workDir)
	s.Emit(agent.Event{Kind: agent.KindQuestion, Question: &agent.Question{
		Ref: req.id, Kind: domain.QuestionApproval, Prompt: prompt, Context: detail,
		Options: []string{domain.AnswerAllow, domain.AnswerDeny},
	}})
}

func parseAsks(input map[string]any) []ask {
	raw, err := json.Marshal(input["questions"])
	if err != nil {
		return nil
	}
	var qs []ask
	if json.Unmarshal(raw, &qs) != nil {
		return nil
	}
	out := qs[:0]
	for _, q := range qs {
		if strings.TrimSpace(q.Question) != "" {
			out = append(out, q)
		}
	}
	return out
}

func askRef(id string, i int) string { return fmt.Sprintf("%s#%d", id, i) }

func (s *session) emitAsk(req *request) {
	q := req.questions[req.next]
	prompt := q.Question
	if len(req.questions) > 1 {
		prompt = fmt.Sprintf("(%d of %d) %s", req.next+1, len(req.questions), prompt)
	}
	// The options' descriptions are what lets the user choose between them.
	opts := make([]string, 0, len(q.Options))
	var described []string
	for _, o := range q.Options {
		opts = append(opts, o.Label)
		if d := strings.TrimSpace(o.Description); d != "" {
			described = append(described, o.Label+": "+d)
		}
	}
	kind := domain.QuestionClarification
	if len(opts) > 0 {
		kind = domain.QuestionSelection
	}
	// Claude's own interface always offers "Other", so a typed answer is accepted.
	s.Emit(agent.Event{Kind: agent.KindQuestion, Question: &agent.Question{
		Ref: askRef(req.id, req.next), Kind: kind, Prompt: prompt, Context: strings.Join(described, "\n"),
		Options: opts, AllowFreeText: true,
	}})
}

func (s *session) onCancel(requestID string) {
	s.mu.Lock()
	req := s.pending[requestID]
	delete(s.pending, requestID)
	s.mu.Unlock()
	if req == nil {
		return
	}
	ref := req.id
	if len(req.questions) > 0 {
		ref = askRef(req.id, req.next)
	}
	s.Emit(agent.Event{Kind: agent.KindQuestionClosed, Ref: ref})
}

// Respond implements agent.Session.
func (s *session) Respond(_ context.Context, ref, answer string) error {
	id := ref
	idx := -1
	if base, n, ok := strings.Cut(ref, "#"); ok {
		var err error
		if idx, err = parseIndex(n); err != nil {
			return fmt.Errorf("%s: %w", ref, agent.ErrUnknownQuestion)
		}
		id = base
	}

	s.mu.Lock()
	req := s.pending[id]
	switch {
	case req == nil, idx >= 0 && (len(req.questions) == 0 || idx != req.next), idx < 0 && len(req.questions) > 0:
		s.mu.Unlock()
		return fmt.Errorf("%s: %w", ref, agent.ErrUnknownQuestion)
	}

	if len(req.questions) > 0 {
		req.answers[req.questions[req.next].Question] = strings.TrimSpace(answer)
		req.next++
		if req.next < len(req.questions) {
			s.mu.Unlock()
			s.emitAsk(req) // the next question; the agent keeps waiting
			return nil
		}
		input := cloneMap(req.input)
		input["answers"] = req.answers
		delete(s.pending, id)
		s.mu.Unlock()
		return s.allow(id, input)
	}

	delete(s.pending, id)
	s.mu.Unlock()
	if isAllow(answer) {
		return s.allow(id, req.input)
	}
	msg := "The user denied this request."
	if t := strings.TrimSpace(answer); t != "" && !strings.EqualFold(t, "deny") {
		msg += " They said: " + t
	}
	return s.write(map[string]any{"type": "control_response", "response": map[string]any{
		"subtype": "success", "request_id": id, "response": map[string]any{"behavior": "deny", "message": msg},
	}})
}

func (s *session) allow(id string, input map[string]any) error {
	return s.write(map[string]any{"type": "control_response", "response": map[string]any{
		"subtype": "success", "request_id": id, "response": map[string]any{"behavior": "allow", "updatedInput": input},
	}})
}

func isAllow(answer string) bool {
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "allow", "approve", "yes", "y", "ok":
		return true
	}
	return false
}

func parseIndex(s string) (int, error) {
	var n int
	if _, err := fmt.Sscanf(s, "%d", &n); err != nil || n < 0 {
		return 0, fmt.Errorf("bad index %q", s)
	}
	return n, nil
}

func cloneMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m)+1)
	for k, v := range m {
		out[k] = v
	}
	return out
}
