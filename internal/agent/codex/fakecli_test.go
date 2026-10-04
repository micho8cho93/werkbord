package codex

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

// The fake `codex`: the test binary re-executes itself with DEVBOARD_FAKE_CODEX
// set and speaks the app-server protocol as Codex 0.155 does (checked against
// its generated JSON schema and a live handshake). What a turn does depends on
// the text of the message that starts it.

type msg = map[string]any

func out(v any) {
	b, _ := json.Marshal(v)
	fmt.Println(string(b))
}

func fakeMain() int {
	args := os.Args[1:]
	if len(args) > 0 && args[0] == "--version" {
		fmt.Println("codex-cli 7.7.7")
		return 0
	}
	if len(args) > 1 && args[0] == "login" && args[1] == "status" {
		if os.Getenv("FAKE_CODEX_LOGGED_OUT") != "" {
			fmt.Println("Not logged in")
			return 1
		}
		fmt.Println("Logged in using ChatGPT")
		return 0
	}
	if len(args) == 0 || args[0] != "app-server" {
		fmt.Fprintln(os.Stderr, "unexpected arguments:", args)
		return 64
	}
	if os.Getenv("FAKE_CODEX_DIE_AT_START") != "" {
		fmt.Fprintln(os.Stderr, "error: cannot start the app server")
		return 1
	}

	in := bufio.NewReaderSize(os.Stdin, 1<<20)
	next := func() (msg, bool) {
		for {
			line, err := in.ReadBytes('\n')
			if len(line) > 0 {
				var m msg
				if json.Unmarshal(line, &m) == nil {
					return m, true
				}
			}
			if err != nil {
				return nil, false
			}
		}
	}
	reply := func(id any, result any) { out(msg{"id": id, "result": result}) }
	record := func(method string, params msg) {
		if f := os.Getenv("FAKE_CODEX_LOG"); f != "" {
			fh, _ := os.OpenFile(f, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
			b, _ := json.Marshal(msg{"method": method, "params": params})
			fmt.Fprintln(fh, string(b))
			fh.Close()
		}
	}

	threadID := "thread-1"
	turns := 0
	var currentTurn string

	notify := func(method string, params msg) { out(msg{"method": method, "params": params}) }
	agentMessage := func(text string) {
		notify("item/started", msg{"threadId": threadID, "turnId": currentTurn, "item": msg{"type": "agentMessage", "id": "m", "text": ""}})
		notify("item/completed", msg{"threadId": threadID, "turnId": currentTurn, "item": msg{"type": "agentMessage", "id": "m", "text": text}})
	}
	completeTurn := func(status string, errMsg string) {
		turn := msg{"id": currentTurn, "status": status, "items": []any{}}
		if errMsg != "" {
			turn["error"] = msg{"message": errMsg}
		}
		notify("turn/completed", msg{"threadId": threadID, "turn": turn})
		currentTurn = ""
	}
	// request sends a server request and waits for its response.
	request := func(id int, method string, params msg) msg {
		out(msg{"id": id, "method": method, "params": params})
		for {
			m, ok := next()
			if !ok {
				os.Exit(0)
			}
			if _, isResp := m["method"]; !isResp && fmt.Sprint(m["id"]) == fmt.Sprint(id) {
				return m
			}
		}
	}
	userText := func(params msg) string {
		input, _ := params["input"].([]any)
		var sb strings.Builder
		for _, i := range input {
			if im, ok := i.(map[string]any); ok {
				sb.WriteString(fmt.Sprint(im["text"]))
			}
		}
		return sb.String()
	}

	runTurn := func(prompt string) {
		switch {
		case strings.HasPrefix(prompt, "hello"):
			agentMessage("hi from codex")
			completeTurn("completed", "")

		case strings.HasPrefix(prompt, "run"):
			resp := request(900, "item/commandExecution/requestApproval", msg{
				"threadId": threadID, "turnId": currentTurn, "itemId": "c1", "command": "npm test", "cwd": "/w", "reason": "run the tests", "startedAtMs": 1,
			})
			res, _ := resp["result"].(map[string]any)
			decision := fmt.Sprint(res["decision"])
			if decision == "decline" {
				notify("item/completed", msg{"threadId": threadID, "turnId": currentTurn, "item": msg{"type": "commandExecution", "id": "c1", "command": "npm test", "status": "declined"}})
			} else {
				notify("item/started", msg{"threadId": threadID, "turnId": currentTurn, "item": msg{"type": "commandExecution", "id": "c1", "command": "npm test", "status": "inProgress"}})
				notify("item/completed", msg{"threadId": threadID, "turnId": currentTurn, "item": msg{"type": "commandExecution", "id": "c1", "command": "npm test", "status": "completed", "exitCode": 1, "aggregatedOutput": "1 failing\nTypeError: x"}})
			}
			agentMessage("decision: " + decision)
			completeTurn("completed", "")

		case strings.HasPrefix(prompt, "patch"):
			resp := request(901, "item/fileChange/requestApproval", msg{"threadId": threadID, "turnId": currentTurn, "itemId": "f1", "reason": "needs to edit", "grantRoot": "/w/vendor", "startedAtMs": 1})
			res, _ := resp["result"].(map[string]any)
			notify("item/completed", msg{"threadId": threadID, "turnId": currentTurn, "item": msg{"type": "fileChange", "id": "f1", "status": "completed", "changes": []any{
				msg{"path": cwd() + "/a.go", "kind": msg{"type": "update"}, "diff": ""}, msg{"path": cwd() + "/b.go", "kind": msg{"type": "add"}, "diff": ""}, msg{"path": "/elsewhere/c.go", "kind": msg{"type": "delete"}, "diff": ""}}}})
			agentMessage("patch decision: " + fmt.Sprint(res["decision"]))
			completeTurn("completed", "")

		case strings.HasPrefix(prompt, "ask"):
			resp := request(902, "item/tool/requestUserInput", msg{"threadId": threadID, "turnId": currentTurn, "itemId": "q1", "isBlocking": true, "questions": []any{
				msg{"id": "colour", "header": "Colour", "question": "Pick a colour", "options": []any{msg{"label": "Red", "description": ""}, msg{"label": "Blue", "description": ""}}},
				msg{"id": "size", "header": "Size", "question": "Pick a size"},
			}})
			res, _ := resp["result"].(map[string]any)
			answers, _ := res["answers"].(map[string]any)
			pick := func(id string) string {
				a, _ := answers[id].(map[string]any)
				l, _ := a["answers"].([]any)
				if len(l) == 0 {
					return ""
				}
				return fmt.Sprint(l[0])
			}
			agentMessage("colour=" + pick("colour") + " size=" + pick("size"))
			completeTurn("completed", "")

		case strings.HasPrefix(prompt, "secret"):
			resp := request(903, "item/tool/requestUserInput", msg{"threadId": threadID, "turnId": currentTurn, "itemId": "q2", "isBlocking": true, "questions": []any{
				msg{"id": "key", "header": "API key", "question": "Paste your key", "isSecret": true},
			}})
			res, _ := resp["result"].(map[string]any)
			answers, _ := res["answers"].(map[string]any)
			a, _ := answers["key"].(map[string]any)
			l, _ := a["answers"].([]any)
			agentMessage(fmt.Sprintf("secret answers: %d", len(l)))
			completeTurn("completed", "")

		case strings.HasPrefix(prompt, "unsupported"):
			resp := request(904, "item/tool/call", msg{"threadId": threadID, "turnId": currentTurn, "callId": "x", "tool": "mystery", "arguments": msg{}})
			_, isErr := resp["error"]
			agentMessage(fmt.Sprintf("unsupported got error: %v", isErr))
			completeTurn("completed", "")

		case strings.HasPrefix(prompt, "steerme"):
			// A long turn: wait to be steered, then finish.
			for {
				m, ok := next()
				if !ok {
					os.Exit(0)
				}
				if m["method"] == "turn/steer" {
					params, _ := m["params"].(map[string]any)
					reply(m["id"], msg{"turnId": currentTurn})
					agentMessage("steered: " + userText(params))
					completeTurn("completed", "")
					return
				}
			}

		case strings.HasPrefix(prompt, "fail"):
			notify("error", msg{"threadId": threadID, "turnId": currentTurn, "willRetry": true, "error": msg{"message": "stream dropped"}})
			completeTurn("failed", "usage limit reached")

		case strings.HasPrefix(prompt, "crash"):
			fmt.Fprintln(os.Stderr, "panic: the sky fell")
			os.Exit(3)

		case strings.HasPrefix(prompt, "hang"):
			time.Sleep(time.Hour)

		default:
			agentMessage("echo: " + prompt)
			completeTurn("completed", "")
		}
	}

	for {
		m, ok := next()
		if !ok {
			return 0 // end of input: the graceful way out
		}
		method, _ := m["method"].(string)
		params, _ := m["params"].(map[string]any)
		switch method {
		case "initialize":
			record(method, params)
			reply(m["id"], msg{"userAgent": "fake", "codexHome": "/h", "platformFamily": "unix", "platformOs": "macos"})
		case "initialized":
		case "thread/start":
			record(method, params)
			reply(m["id"], msg{"thread": msg{"id": threadID, "status": msg{"type": "idle"}, "turns": []any{}}})
			notify("thread/started", msg{"thread": msg{"id": threadID}})
		case "thread/resume":
			record(method, params)
			threadID = fmt.Sprint(params["threadId"])
			reply(m["id"], msg{"thread": msg{"id": threadID, "status": msg{"type": "idle"}, "turns": []any{}}})
		case "turn/start":
			record(method, params)
			turns++
			currentTurn = fmt.Sprintf("turn-%d", turns)
			reply(m["id"], msg{"turn": msg{"id": currentTurn, "status": "inProgress", "items": []any{}}})
			notify("turn/started", msg{"threadId": threadID, "turn": msg{"id": currentTurn, "status": "inProgress", "items": []any{}}})
			runTurn(userText(params))
		case "turn/steer":
			// Only meaningful during a steerable turn; otherwise an error, as the real server does.
			out(msg{"id": m["id"], "error": msg{"code": -32600, "message": "no active turn to steer"}})
		default:
			if _, isReq := m["id"]; isReq {
				out(msg{"id": m["id"], "error": msg{"code": -32601, "message": "unknown method " + method}})
			}
		}
	}
}

func cwd() string {
	d, _ := os.Getwd()
	return d
}
