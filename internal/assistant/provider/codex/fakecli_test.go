package codex

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

// The fake `codex`: the test binary re-executes itself with ASSISTANT_TEST_FAKE_CODEX set and speaks the app-server
// protocol as Codex 0.155.1 does (checked against the real one; see docs/ASSISTANT.md). What a turn does depends on
// the first word of the message it is sent.

var out = json.NewEncoder(os.Stdout)

func send(v any) { _ = out.Encode(v) }

func notify(method string, params any) { send(map[string]any{"method": method, "params": params}) }

func fakeMain() int {
	args := os.Args[1:]
	switch {
	case len(args) > 0 && args[0] == "--version":
		fmt.Println("codex-cli 9.9.9")
		return 0
	case len(args) > 1 && args[0] == "login" && args[1] == "status":
		if os.Getenv("FAKE_CODEX_LOGGED_OUT") != "" {
			fmt.Println("Not logged in")
			return 1
		}
		if os.Getenv("FAKE_CODEX_API_KEY") != "" {
			fmt.Println("Logged in using an API key - sk-***")
			return 0
		}
		fmt.Println("Logged in using ChatGPT")
		return 0
	case len(args) > 1 && args[0] == "features" && args[1] == "list":
		// Like the real one: name, stage, state. Not every feature we would turn off exists here.
		fmt.Println("shell_tool                               stable             true")
		fmt.Println("plugins                                  stable             true")
		fmt.Println("hooks                                    stable             true")
		fmt.Println("multi_agent_v2                           removed            false")
		fmt.Println("fast_mode                                stable             true")
		return 0
	case len(args) > 1 && args[0] == "app-server" && args[1] == "--help":
		fmt.Println("[experimental] Run the app server")
		fmt.Println("Usage: codex app-server [OPTIONS] [COMMAND]")
		return 0
	case len(args) > 0 && args[0] == "app-server":
		return server(args)
	}
	fmt.Fprintln(os.Stderr, "fake codex: unknown command", args)
	return 2
}

func server(args []string) int {
	if f := os.Getenv("FAKE_CODEX_ARGS_FILE"); f != "" {
		wd, _ := os.Getwd()
		_ = os.WriteFile(f, []byte(strings.Join(args, "\x00")+"\x00cwd="+wd), 0o600)
	}
	for i, a := range args {
		if a == "--disable" && i+1 < len(args) {
			switch args[i+1] {
			case "shell_tool", "plugins", "hooks", "multi_agent_v2":
			default:
				fmt.Fprintln(os.Stderr, "error: unknown feature "+args[i+1]) // the real one exits on a feature it does not know
				return 1
			}
		}
	}
	known := map[string]bool{"thr-known": true}
	thread, turnSeq := "", 0
	in := bufio.NewReaderSize(os.Stdin, 1<<20)
	for {
		line, err := in.ReadBytes('\n')
		if len(line) == 0 && err != nil {
			return 0
		}
		var m struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params map[string]any  `json:"params"`
		}
		if json.Unmarshal(line, &m) != nil {
			continue
		}
		reply := func(result any) { send(map[string]any{"id": m.ID, "result": result}) }
		fail := func(msg string) {
			send(map[string]any{"id": m.ID, "error": map[string]any{"code": -32600, "message": msg}})
		}
		switch m.Method {
		case "initialize":
			reply(map[string]any{"userAgent": "fake/9.9.9"})
		case "initialized":
		case "model/list":
			reply(map[string]any{"data": []map[string]any{
				{"id": "fake-big", "model": "fake-big", "displayName": "Fake Big", "description": "the big one", "isDefault": true,
					"supportedReasoningEfforts": []map[string]any{{"reasoningEffort": "low"}, {"reasoningEffort": "high"}}},
				{"id": "fake-hidden", "model": "fake-hidden", "hidden": true},
			}})
		case "thread/start":
			thread = "thr-new-" + fmt.Sprint(time.Now().UnixNano())
			known[thread] = true
			reply(map[string]any{"thread": map[string]any{"id": thread}})
			notify("thread/started", map[string]any{"thread": map[string]any{"id": thread}})
		case "thread/resume":
			id, _ := m.Params["threadId"].(string)
			if !known[id] {
				fail("no rollout found for thread id " + id)
				continue
			}
			thread = id
			reply(map[string]any{"thread": map[string]any{"id": thread}})
		case "turn/start":
			turnSeq++
			turn := fmt.Sprintf("turn-%d", turnSeq)
			reply(map[string]any{"turn": map[string]any{"id": turn}})
			input, _ := m.Params["input"].([]any)
			text := ""
			if len(input) > 0 {
				text, _ = input[0].(map[string]any)["text"].(string)
			}
			if r := runTurn(thread, turn, text, m.Params, in); r >= 0 {
				return r
			}
		case "turn/interrupt":
		}
	}
}

func completed(thread, turn, status, errMsg string) {
	t := map[string]any{"id": turn, "status": status, "items": []any{}}
	if errMsg != "" {
		t["error"] = map[string]any{"message": errMsg}
	}
	notify("turn/completed", map[string]any{"threadId": thread, "turn": t})
}

func delta(text string) {
	notify("item/agentMessage/delta", map[string]any{"delta": text, "itemId": "msg"})
}

func agentMessage(text string) {
	notify("item/completed", map[string]any{"item": map[string]any{"type": "agentMessage", "id": "msg", "text": text}})
}

// runTurn scripts a turn. It returns an exit code to stop the process, or -1 to keep serving.
func runTurn(thread, turn, text string, params map[string]any, in *bufio.Reader) int {
	word, _, _ := strings.Cut(text, " ")
	notify("turn/started", map[string]any{"threadId": thread, "turn": map[string]any{"id": turn}})
	notify("item/started", map[string]any{"item": map[string]any{"type": "userMessage", "id": "u"}})
	switch word {
	case "TOOL":
		notify("item/started", map[string]any{"item": map[string]any{"type": "commandExecution", "id": "c", "command": "cat ~/.ssh/id_rsa"}})
		return waitInterrupt(thread, turn, in, true)
	case "COLLAB":
		notify("item/started", map[string]any{"item": map[string]any{"type": "collabAgentToolCall", "id": "c"}})
		return waitInterrupt(thread, turn, in, true)
	case "ASK":
		send(map[string]any{"id": 77, "method": "item/commandExecution/requestApproval", "params": map[string]any{"command": "rm -rf /"}})
		return waitInterrupt(thread, turn, in, true)
	case "MODEL":
		notify("error", map[string]any{"error": map[string]any{"message": `{"type":"error","status":400,"error":{"type":"invalid_request_error","message":"The 'x' model is not supported when using Codex with a ChatGPT account."}}`}, "willRetry": false})
		completed(thread, turn, "failed", `{"type":"error","status":400,"error":{"type":"invalid_request_error","message":"The 'x' model is not supported when using Codex with a ChatGPT account."}}`)
	case "RATE":
		completed(thread, turn, "failed", `{"status":429,"error":{"message":"You have hit your usage limit"}}`)
	case "AUTH":
		completed(thread, turn, "failed", `{"status":401,"error":{"message":"Your access token could not be refreshed. Please log in again."}}`)
	case "RETRY":
		notify("error", map[string]any{"error": map[string]any{"message": "stream disconnected, retrying"}, "willRetry": true})
		delta("recovered")
		agentMessage("recovered")
		completed(thread, turn, "completed", "")
	case "DIE":
		fmt.Fprintln(os.Stderr, "thread 'main' panicked")
		return 101
	case "HANG":
		delta("partial")
		return waitInterrupt(thread, turn, in, false)
	case "DEAF":
		delta("partial")
		time.Sleep(time.Minute)
	case "WHOLE":
		agentMessage("whole reply") // a server that does not stream
		completed(thread, turn, "completed", "")
	default:
		model, _ := params["model"].(string)
		effort, _ := params["effort"].(string)
		delta("Hel")
		delta("lo")
		delta(fmt.Sprintf(" model=%s effort=%s", model, effort))
		agentMessage(fmt.Sprintf("Hello model=%s effort=%s", model, effort))
		notify("thread/tokenUsage/updated", map[string]any{"tokenUsage": map[string]any{"last": map[string]any{"inputTokens": 12, "outputTokens": 3}}})
		completed(thread, turn, "completed", "")
	}
	return -1
}

// waitInterrupt waits for the client to interrupt the turn and then ends it as Codex does.
func waitInterrupt(thread, turn string, in *bufio.Reader, ack bool) int {
	for {
		line, err := in.ReadBytes('\n')
		if err != nil && len(line) == 0 {
			return 0
		}
		if strings.Contains(string(line), `"turn/interrupt"`) {
			completed(thread, turn, "interrupted", "")
			return -1
		}
	}
}
