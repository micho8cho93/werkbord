package claude

import (
	"encoding/json"
	"fmt"
	"strings"

	"devboard/internal/assistant/provider"
	"devboard/internal/assistant/provider/cli"
)

// line is the part of Claude Code's stream-json output that the assistant reads.
type line struct {
	Type      string `json:"type"`
	Subtype   string `json:"subtype"`
	SessionID string `json:"session_id"`
	Model     string `json:"model"`

	// system/init
	Tools      []json.RawMessage `json:"tools"`
	MCPServers []json.RawMessage `json:"mcp_servers"`

	// stream_event
	Event *struct {
		Type         string `json:"type"`
		ContentBlock *struct {
			Type string `json:"type"`
			Name string `json:"name"`
		} `json:"content_block"`
		Delta *struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"delta"`
	} `json:"event"`

	// assistant
	Message *struct {
		Model   string `json:"model"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
			Name string `json:"name"`
		} `json:"content"`
	} `json:"message"`

	// result
	IsError        bool   `json:"is_error"`
	Result         string `json:"result"`
	APIErrorStatus *int   `json:"api_error_status"`
	TerminalReason string `json:"terminal_reason"`
	Usage          *struct {
		InputTokens  int64 `json:"input_tokens"`
		OutputTokens int64 `json:"output_tokens"`
		CacheRead    int64 `json:"cache_read_input_tokens"`
		CacheWrite   int64 `json:"cache_creation_input_tokens"`
	} `json:"usage"`
}

// turnState follows one turn's output.
type turnState struct {
	emit func(provider.Event)
	ref  string

	text      strings.Builder
	sawDelta  bool
	model     string
	result    *line
	violation string
	notJSON   []string
}

// line handles one line of output. It returns false to stop the process: a rule was broken.
func (s *turnState) line(raw []byte) bool {
	raw = []byte(strings.TrimSpace(string(raw)))
	if len(raw) == 0 {
		return true
	}
	var l line
	if err := json.Unmarshal(raw, &l); err != nil {
		// Not JSON: a stray message the CLI printed. Kept to explain a failure, never shown as the reply.
		if len(s.notJSON) < 8 {
			s.notJSON = append(s.notJSON, clip(string(raw), 300))
		}
		return true
	}
	switch l.Type {
	case "system":
		if l.Subtype == "init" {
			if l.SessionID != "" && l.SessionID != s.ref {
				s.ref = l.SessionID
				s.emit(provider.Event{Kind: provider.EventRef, Ref: s.ref})
			}
			if l.Model != "" {
				s.model = l.Model
			}
			// The turn is run with no tools at all. If Claude Code says it has any, whatever the reason (a setting this
			// version does not honour, a plugin), nothing it says may be acted on.
			if len(l.Tools) > 0 || len(l.MCPServers) > 0 {
				s.violation = fmt.Sprintf("Claude Code started with %d tool(s) and %d MCP server(s) although it was told to use none", len(l.Tools), len(l.MCPServers))
				return false
			}
		}
	case "stream_event":
		if l.Event == nil {
			return true
		}
		switch l.Event.Type {
		case "content_block_start":
			if b := l.Event.ContentBlock; b != nil && isToolBlock(b.Type) {
				s.violation = "Claude Code tried to use a tool (" + clip(b.Name, 40) + ")"
				return false
			}
		case "content_block_delta":
			switch {
			case l.Event.Delta == nil:
			case l.Event.Delta.Type == "text_delta" && l.Event.Delta.Text != "":
				s.sawDelta = true
				s.text.WriteString(l.Event.Delta.Text)
				s.emit(provider.Event{Kind: provider.EventText, Text: l.Event.Delta.Text})
			default:
				// Thinking is never shown or kept; it only proves the turn is alive.
				s.emit(provider.Event{Kind: provider.EventAlive})
			}
		}
	case "assistant":
		if l.Message == nil {
			return true
		}
		if l.Message.Model != "" && l.Message.Model != "<synthetic>" {
			s.model = l.Message.Model
		}
		for _, b := range l.Message.Content {
			if isToolBlock(b.Type) {
				s.violation = "Claude Code tried to use a tool (" + clip(b.Name, 40) + ")"
				return false
			}
			// A version that does not stream partial messages delivers the reply here, whole.
			if b.Type == "text" && b.Text != "" && !s.sawDelta {
				s.text.WriteString(b.Text)
				s.emit(provider.Event{Kind: provider.EventText, Text: b.Text})
			}
		}
	case "result":
		s.result = &l
	}
	return true
}

func isToolBlock(t string) bool {
	return t == "tool_use" || t == "server_tool_use" || t == "mcp_tool_use" || strings.HasSuffix(t, "_tool_use")
}

// finish decides how the turn ended once the process is gone.
func (s *turnState) finish(exit cli.Exit) (provider.Result, error) {
	if s.violation != "" {
		return provider.Result{}, &provider.Error{Kind: provider.KindPolicy, Message: s.violation + "; the turn was ended", Err: provider.ErrPolicy}
	}
	r := s.result
	if r == nil {
		// No result line: it died, or was told to stop before it finished.
		why := exit.Stderr
		if why == "" {
			why = strings.Join(s.notJSON, " ")
		}
		return provider.Result{}, classify("", nil, why, exit.Code, true)
	}
	if r.IsError || strings.HasPrefix(r.Subtype, "error") {
		why := r.Result
		if why == "" {
			why = exit.Stderr
		}
		if why == "" {
			why = strings.Join(s.notJSON, " ")
		}
		return provider.Result{}, classify(r.Result, r.APIErrorStatus, why+" "+exit.Stderr+" "+strings.Join(s.notJSON, " "), exit.Code, false)
	}
	text := s.text.String()
	if text == "" && r.Result != "" {
		text = r.Result
		s.emit(provider.Event{Kind: provider.EventText, Text: text})
	}
	res := provider.Result{Text: text, Ref: s.ref, Model: s.model}
	if r.SessionID != "" {
		res.Ref = r.SessionID
	}
	if u := r.Usage; u != nil {
		res.Usage = provider.Usage{InputTokens: u.InputTokens + u.CacheRead + u.CacheWrite, OutputTokens: u.OutputTokens}
	}
	return res, nil
}

// classify says what kind of failure message is, from what Claude Code reported.
func classify(result string, status *int, evidence string, exitCode int, noResult bool) *provider.Error {
	code := 0
	if status != nil {
		code = *status
	}
	low := strings.ToLower(result + " " + evidence)
	msg := strings.TrimSpace(clip(firstNonEmpty(result, evidence), 400))
	switch {
	case strings.Contains(low, "no conversation found"):
		return provider.Errorf(provider.KindSessionLost, false, "Claude Code no longer has this conversation")
	case code == 401 || code == 403 || containsAny(low, "not logged in", "please run /login", "run `claude auth login`", "invalid api key", "oauth token", "authentication_error", "not authenticated"):
		return provider.Errorf(provider.KindNotSignedIn, false, "Claude Code is not signed in, or its sign-in has expired. Run `claude auth login` in a terminal. (%s)", msg)
	case code == 404 || containsAny(low, "issue with the selected model", "unrecognized_model", "model_not_found"):
		return provider.Errorf(provider.KindModelUnavailable, false, "That model is not available to your Claude account: %s", msg)
	case code == 429 || containsAny(low, "rate limit", "usage limit", "limit reached", "too many requests", "quota", "credit balance is too low"):
		return provider.Errorf(provider.KindRateLimited, false, "Your Claude plan has no allowance left for now: %s", msg)
	case code >= 500 || containsAny(low, "overloaded", "econnreset", "etimedout", "enotfound", "socket hang up", "network error", "fetch failed", "service unavailable", "connection error"):
		return provider.Errorf(provider.KindTransient, true, "Claude had a temporary problem: %s", msg)
	case noResult && msg != "":
		// It stopped without a word of its own about why: a crash, which a second try may well get past.
		return provider.Errorf(provider.KindTransient, true, "Claude Code stopped unexpectedly (exit code %d): %s", exitCode, msg)
	case msg == "" && exitCode != 0:
		return provider.Errorf(provider.KindTransient, true, "Claude Code stopped unexpectedly (exit code %d)", exitCode)
	case msg == "":
		return provider.Errorf(provider.KindTransient, true, "Claude Code ended without an answer")
	}
	return provider.Errorf(provider.KindFailed, false, "Claude Code failed: %s", msg)
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
