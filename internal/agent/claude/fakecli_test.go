package claude

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

// This file is the fake `claude`: the test binary re-executes itself with
// DEVBOARD_FAKE_CLAUDE set and speaks the stream-json protocol as the real CLI
// does (verified against Claude Code 2.1). What it does depends on the text of
// the message it is sent, so a test scripts it by choosing its prompt.

func emit(v any) {
	b, _ := json.Marshal(v)
	fmt.Println(string(b))
}

func assistant(blocks ...map[string]any) {
	emit(map[string]any{"type": "assistant", "message": map[string]any{"role": "assistant", "content": blocks}, "session_id": "sess"})
}

func text(t string) map[string]any { return map[string]any{"type": "text", "text": t} }
func toolUse(name string, input map[string]any) map[string]any {
	return map[string]any{"type": "tool_use", "id": "toolu_1", "name": name, "input": input}
}

func result(isError bool, msg string) {
	sub := "success"
	if isError {
		sub = "error_during_execution"
	}
	emit(map[string]any{"type": "result", "subtype": sub, "is_error": isError, "result": msg, "session_id": "sess"})
}

func fakeMain() int {
	args := os.Args[1:]
	if len(args) > 0 && args[0] == "--version" {
		fmt.Println("9.9.9 (Claude Code)")
		return 0
	}
	if len(args) > 1 && args[0] == "auth" && args[1] == "status" {
		if os.Getenv("FAKE_CLAUDE_LOGGED_OUT") != "" {
			fmt.Println(`{"loggedIn": false}`)
		} else {
			fmt.Println(`{"loggedIn": true, "authMethod": "claude.ai"}`)
		}
		return 0
	}
	if os.Getenv("FAKE_CLAUDE_ARGS_FILE") != "" {
		_ = os.WriteFile(os.Getenv("FAKE_CLAUDE_ARGS_FILE"), []byte(strings.Join(args, "\n")+"\ncwd="+mustGetwd()), 0o600)
	}
	if os.Getenv("FAKE_CLAUDE_DIE_AT_START") != "" {
		fmt.Fprintln(os.Stderr, "error: unknown option '--permission-prompt-tool'")
		return 1
	}

	sessionID := "sess"
	for i, a := range args {
		if a == "--session-id" || a == "--resume" {
			sessionID = args[i+1]
		}
	}
	in := bufio.NewReaderSize(os.Stdin, 1<<20)
	nextLine := func() (map[string]any, bool) {
		for {
			line, err := in.ReadBytes('\n')
			if len(line) > 0 {
				var m map[string]any
				if json.Unmarshal(line, &m) == nil {
					return m, true
				}
			}
			if err != nil {
				return nil, false
			}
		}
	}
	userText := func(m map[string]any) string {
		msg, _ := m["message"].(map[string]any)
		parts, _ := msg["content"].([]any)
		var sb strings.Builder
		for _, p := range parts {
			if pm, ok := p.(map[string]any); ok {
				sb.WriteString(fmt.Sprint(pm["text"]))
			}
		}
		return sb.String()
	}
	// ask sends a control request and waits for the matching response.
	ask := func(id string, req map[string]any) map[string]any {
		emit(map[string]any{"type": "control_request", "request_id": id, "request": req})
		for {
			m, ok := nextLine()
			if !ok {
				os.Exit(0)
			}
			if m["type"] == "control_response" {
				resp, _ := m["response"].(map[string]any)
				if resp["request_id"] == id {
					return resp
				}
			}
		}
	}

	first := true
	for {
		m, ok := nextLine()
		if !ok {
			return 0 // end of input: the graceful way out
		}
		if m["type"] != "user" {
			continue
		}
		if first {
			emit(map[string]any{"type": "system", "subtype": "init", "session_id": sessionID, "model": "fake-model", "cwd": mustGetwd()})
			first = false
		}
		prompt := userText(m)
		switch {
		case strings.HasPrefix(prompt, "hello"):
			assistant(text("hi there"))
			result(false, "hi there")
		case strings.HasPrefix(prompt, "edit"):
			assistant(toolUse("Edit", map[string]any{"file_path": mustGetwd() + "/main.go"}))
			resp := ask("req-edit", map[string]any{"subtype": "can_use_tool", "tool_name": "Edit", "input": map[string]any{"file_path": mustGetwd() + "/main.go"}})
			inner, _ := resp["response"].(map[string]any)
			if inner["behavior"] == "allow" {
				emit(map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": []any{map[string]any{"type": "tool_result", "tool_use_id": "toolu_1", "content": "ok"}}}})
				assistant(text("edited"))
			} else {
				assistant(text("denied: " + fmt.Sprint(inner["message"])))
			}
			result(false, "")
		case strings.HasPrefix(prompt, "bash"):
			resp := ask("req-bash", map[string]any{"subtype": "can_use_tool", "tool_name": "Bash", "input": map[string]any{"command": "npm test", "description": "run the tests"}})
			inner, _ := resp["response"].(map[string]any)
			assistant(text("bash: " + fmt.Sprint(inner["behavior"])))
			result(false, "")
		case strings.HasPrefix(prompt, "askme"):
			input := map[string]any{"questions": []any{
				map[string]any{"question": "Which colour?", "header": "Colour", "multiSelect": false, "options": []any{map[string]any{"label": "Red", "description": ""}, map[string]any{"label": "Blue", "description": ""}}},
				map[string]any{"question": "Which size?", "header": "Size", "multiSelect": false, "options": []any{map[string]any{"label": "S", "description": ""}, map[string]any{"label": "L", "description": ""}}},
			}}
			resp := ask("req-ask", map[string]any{"subtype": "can_use_tool", "tool_name": "AskUserQuestion", "input": input, "requires_user_interaction": true})
			inner, _ := resp["response"].(map[string]any)
			updated, _ := inner["updatedInput"].(map[string]any)
			answers, _ := updated["answers"].(map[string]any)
			assistant(text(fmt.Sprintf("colour=%v size=%v", answers["Which colour?"], answers["Which size?"])))
			result(false, "")
		case strings.HasPrefix(prompt, "cancelask"):
			emit(map[string]any{"type": "control_request", "request_id": "req-gone", "request": map[string]any{"subtype": "can_use_tool", "tool_name": "Bash", "input": map[string]any{"command": "rm -rf x"}}})
			time.Sleep(100 * time.Millisecond)
			emit(map[string]any{"type": "control_cancel_request", "request_id": "req-gone"})
			result(false, "")
		case strings.HasPrefix(prompt, "hook"):
			resp := ask("req-hook", map[string]any{"subtype": "hook_callback"})
			assistant(text("hook response: " + fmt.Sprint(resp["subtype"])))
			result(false, "")
		case strings.HasPrefix(prompt, "failturn"):
			result(true, "Credit balance is too low")
		case strings.HasPrefix(prompt, "crash"):
			fmt.Fprintln(os.Stderr, "fatal: the sky fell")
			return 2
		case strings.HasPrefix(prompt, "noise"):
			fmt.Println("this is not json")
			emit(map[string]any{"type": "rate_limit_event", "rate_limit_info": map[string]any{"status": "rejected"}})
			emit(map[string]any{"type": "stream_event", "event": map[string]any{}})
			assistant(text("   "), map[string]any{"type": "thinking", "thinking": "hmm"})
			assistant(text("after noise"))
			result(false, "")
		case strings.HasPrefix(prompt, "hang"):
			time.Sleep(time.Hour)
		default:
			assistant(text("echo: " + prompt))
			result(false, "")
		}
	}
}

func mustGetwd() string {
	d, _ := os.Getwd()
	return d
}
