package claude

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// The fake `claude`: the test binary re-executes itself with ASSISTANT_TEST_FAKE_CLAUDE set and speaks the stream-json
// protocol as Claude Code 2.1.285 does (checked against the real one, see docs/ASSISTANT.md). What it does depends on
// the first word of the prompt it is given on stdin, so a test scripts it by choosing its prompt.

func emit(v any) {
	b, _ := json.Marshal(v)
	fmt.Println(string(b))
}

func delta(text string) {
	emit(map[string]any{"type": "stream_event", "event": map[string]any{"type": "content_block_delta", "index": 0,
		"delta": map[string]any{"type": "text_delta", "text": text}}})
}

func result(isError bool, msg string, status int, session string) {
	m := map[string]any{"type": "result", "subtype": "success", "is_error": isError, "result": msg, "session_id": session,
		"usage": map[string]any{"input_tokens": 10, "output_tokens": 4, "cache_read_input_tokens": 100, "cache_creation_input_tokens": 0}}
	if isError {
		m["subtype"] = "error_during_execution"
	}
	if status != 0 {
		m["api_error_status"] = status
	}
	emit(m)
}

func fakeMain() int {
	args := os.Args[1:]
	if len(args) > 0 && args[0] == "--version" {
		fmt.Println("9.9.9 (Claude Code)")
		return 0
	}
	if len(args) > 0 && args[0] == "--help" {
		fmt.Println("Usage: claude [options]")
		if os.Getenv("FAKE_CLAUDE_OLD") == "" {
			for _, f := range []string{"--tools", "--system-prompt", "--include-partial-messages", "--session-id", "--resume", "--safe-mode",
				"--strict-mcp-config", "--disable-slash-commands", "--setting-sources", "--system-prompt-snapshot"} {
				fmt.Println("  " + f + " <x>   text")
			}
		} else {
			fmt.Println("  --resume <x>   text")
		}
		fmt.Println("  --effort <level>                      Effort level for the current session")
		fmt.Println("                                        (low, medium, high, xhigh, max)")
		return 0
	}
	if len(args) > 1 && args[0] == "auth" && args[1] == "status" {
		if os.Getenv("FAKE_CLAUDE_LOGGED_OUT") != "" {
			fmt.Println(`{"loggedIn": false}`)
		} else if os.Getenv("FAKE_CLAUDE_API_KEY") != "" {
			fmt.Println(`{"loggedIn": true, "authMethod": "claude.ai", "apiKeySource": "ANTHROPIC_API_KEY"}`)
		} else {
			fmt.Println(`{"loggedIn": true, "authMethod": "claude.ai"}`)
		}
		return 0
	}
	if f := os.Getenv("FAKE_CLAUDE_ARGS_FILE"); f != "" {
		wd, _ := os.Getwd()
		_ = os.WriteFile(f, []byte(strings.Join(args, "\x00")+"\x00cwd="+wd), 0o600)
	}
	if f := os.Getenv("FAKE_CLAUDE_ENV_FILE"); f != "" {
		_ = os.WriteFile(f, []byte(strings.Join(os.Environ(), "\n")), 0o600)
	}
	session := "sess-from-cli"
	for i, a := range args {
		if (a == "--session-id" || a == "--resume") && i+1 < len(args) {
			session = args[i+1]
		}
	}
	in, _ := io.ReadAll(bufio.NewReader(os.Stdin))
	prompt := strings.TrimSpace(string(in))
	word, _, _ := strings.Cut(prompt, " ")

	tools := []string{}
	if word == "TOOLS_LISTED" {
		tools = []string{"Bash"}
	}
	emit(map[string]any{"type": "system", "subtype": "init", "session_id": session, "tools": tools, "mcp_servers": []string{}, "model": "claude-fake-1"})

	switch word {
	case "TOOL_USE":
		emit(map[string]any{"type": "stream_event", "event": map[string]any{"type": "content_block_start", "index": 0,
			"content_block": map[string]any{"type": "tool_use", "name": "Bash"}}})
		time.Sleep(50 * time.Millisecond)
		delta("should never be seen")
		result(false, "x", 0, session)
	case "AUTH":
		result(true, "Invalid API key · Please run /login", 401, session)
	case "MODEL":
		result(true, "There's an issue with the selected model (nope). It may not exist or you may not have access to it.", 404, session)
	case "RATE":
		result(true, "You've hit your usage limit. Resets at 5pm.", 429, session)
	case "OVERLOAD":
		result(true, "Overloaded", 529, session)
	case "NOSESSION":
		fmt.Fprintln(os.Stderr, "No conversation found with session ID: "+session)
		result(true, "", 0, session)
	case "DIE":
		fmt.Fprintln(os.Stderr, "panic: something broke")
		return 3
	case "NOPARTIAL":
		emit(map[string]any{"type": "assistant", "message": map[string]any{"model": "claude-fake-1", "content": []map[string]any{{"type": "text", "text": "whole reply"}}}})
		result(false, "whole reply", 0, session)
	case "HANG", "CHILD":
		if word == "CHILD" {
			c := exec.Command(os.Args[0], "-test.run=NONE")
			c.Env = append(os.Environ(), "ASSISTANT_TEST_FAKE_CLAUDE_SLEEPER=1")
			_ = c.Start()
			_ = os.WriteFile(os.Getenv("FAKE_CLAUDE_CHILD_FILE"), []byte(strconv.Itoa(c.Process.Pid)), 0o600)
		}
		delta("partial")
		if os.Getenv("FAKE_CLAUDE_IGNORE_TERM") != "" {
			signal.Ignore(syscall.SIGTERM)
		}
		time.Sleep(time.Minute)
	case "BIG":
		fmt.Println(strings.Repeat("x", 5<<20))
		delta("after the big line")
		result(false, "after the big line", 0, session)
	case "THINK":
		emit(map[string]any{"type": "stream_event", "event": map[string]any{"type": "content_block_delta", "delta": map[string]any{"type": "thinking_delta", "thinking": ""}}})
		delta("done thinking")
		result(false, "done thinking", 0, session)
	default:
		delta("Hel")
		delta("lo")
		delta(", " + strconv.Itoa(len(prompt)) + " chars")
		result(false, "Hello, "+strconv.Itoa(len(prompt))+" chars", 0, session)
	}
	return 0
}

func sleeperMain() int {
	time.Sleep(time.Minute)
	return 0
}
