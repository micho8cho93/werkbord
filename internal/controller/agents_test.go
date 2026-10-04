package controller

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"devboard/internal/config"
	"devboard/internal/domain"
)

// fakeClaude is a shell script that speaks enough of Claude Code's stream-json
// protocol for the real adapter to drive it: version and auth probes, one
// result per message, and an approval request when it is told to ask.
const fakeClaude = `#!/bin/sh
case "$1" in
  --version) echo "9.9.9 (Claude Code)"; exit 0 ;;
  auth) echo '{"loggedIn": true}'; exit 0 ;;
esac
[ -n "$FAKE_ARGS_LOG" ] && echo "$*" >> "$FAKE_ARGS_LOG"
[ -n "$FAKE_PID_FILE" ] && echo $$ > "$FAKE_PID_FILE"
sid=fake-session
while [ $# -gt 0 ]; do
  case "$1" in --session-id|--resume) sid="$2" ;; esac
  shift
done
n=0
while IFS= read -r line; do
  n=$((n+1))
  echo '{"type":"system","subtype":"init","session_id":"'"$sid"'","model":"fake"}'
  case "$line" in
    *'"ask"'*)
      echo '{"type":"control_request","request_id":"r'$n'","request":{"subtype":"can_use_tool","tool_name":"Bash","input":{"command":"rm -rf build"}}}'
      IFS= read -r reply
      case "$reply" in
        *'"allow"'*) verdict=allowed ;;
        *) verdict=denied ;;
      esac
      echo '{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"command '"$verdict"'"}]}}' ;;
    *)
      echo '{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"ack '$n'"}]}}' ;;
  esac
  echo '{"type":"result","subtype":"success","is_error":false,"result":"ok"}'
done
`

type client struct {
	t       *testing.T
	base    string
	project string // set once the test has registered a project
}

func (c client) do(method, path, body string, out any) int {
	c.t.Helper()
	req, _ := http.NewRequest(method, c.base+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if out != nil {
		if err := json.Unmarshal(b, out); err != nil {
			c.t.Fatalf("%s %s: %v: %s", method, path, err, b)
		}
	}
	return resp.StatusCode
}

func (c client) run(id string) domain.Run {
	c.t.Helper()
	var r domain.Run
	if code := c.do("GET", "/api/projects/"+c.project+"/runs/"+id, "", &r); code != 200 {
		c.t.Fatalf("get run: %d", code)
	}
	return r
}

func (c client) waitWaiting(id string, kind domain.WaitingKind) domain.Run {
	c.t.Helper()
	var r domain.Run
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if r = c.run(id); r.State == domain.RunWaitingForUser && r.Waiting == kind {
			return r
		}
		time.Sleep(10 * time.Millisecond)
	}
	c.t.Fatalf("run never waited for %s: %+v", kind, r)
	return r
}

func (c client) outputs(id string) []string {
	c.t.Helper()
	var page struct{ Events []domain.Event }
	c.do("GET", "/api/projects/"+c.project+"/runs/"+id+"/events?limit=1000", "", &page)
	var out []string
	for _, ev := range page.Events {
		if ev.Type == domain.EventAgentOutput {
			var o domain.AgentOutput
			_ = json.Unmarshal(ev.Payload, &o)
			if o.Stream == domain.StreamAssistant || o.Stream == domain.StreamUser {
				out = append(out, string(o.Stream)+":"+o.Text)
			}
		}
	}
	return out
}

func commit(t *testing.T) string {
	t.Helper()
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	env := append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e.com", "GIT_CONFIG_GLOBAL=/dev/null")
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"commit", "-q", "--allow-empty", "-m", "init"}} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = env
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("git unavailable: %v %s", err, out)
		}
	}
	return dir
}

func alive(pid int) bool { return pid > 0 && syscall.Kill(pid, 0) == nil }

// The whole path from configuration to a process: the controller builds the real
// Claude Code adapter from config, drives a (fake) CLI through it, and carries a
// conversation across a restart.
func TestAgentSessionEndToEndAcrossARestart(t *testing.T) {
	script := filepath.Join(t.TempDir(), "claude")
	if err := os.WriteFile(script, []byte(fakeClaude), 0o755); err != nil {
		t.Fatal(err)
	}
	argsLog := filepath.Join(t.TempDir(), "args")
	t.Setenv("FAKE_ARGS_LOG", argsLog)
	pidFile := filepath.Join(t.TempDir(), "pid")
	t.Setenv("FAKE_PID_FILE", pidFile)
	agentPID := func() int {
		b, _ := os.ReadFile(pidFile)
		n, _ := strconv.Atoi(strings.TrimSpace(string(b)))
		return n
	}

	cfg := testConfig(t)
	cfg.Agents = map[string]config.AgentConfig{
		config.AgentClaudeCode: {Command: script, PermissionMode: "acceptEdits"},
		config.AgentCodex:      {Command: "/nonexistent/codex"},
	}
	ctx := context.Background()
	logs := &syncBuffer{}
	c := New(cfg, slog.New(slog.NewTextHandler(logs, nil)), "test")
	if err := c.Start(ctx); err != nil {
		t.Fatal(err)
	}
	api := client{t: t, base: "http://" + c.Addr()}

	// Both agents are listed, with what is wrong with the one that is missing.
	var agents struct{ Agents []domain.Agent }
	api.do("GET", "/api/agents", "", &agents)
	if len(agents.Agents) != 2 || agents.Agents[0].ID != "claude-code" || !agents.Agents[0].Available || agents.Agents[0].Version != "9.9.9" {
		t.Fatalf("agents = %+v", agents.Agents)
	}
	if cx := agents.Agents[1]; cx.ID != "codex" || cx.Available || !strings.Contains(cx.Detail, "not found") {
		t.Fatalf("codex = %+v", cx)
	}

	repo := commit(t)
	var project struct{ ID string }
	if code := api.do("POST", "/api/projects", `{"path":`+quote(repo)+`}`, &project); code != 201 {
		t.Fatalf("register: %d", code)
	}
	api.project = project.ID
	var task domain.Task
	api.do("POST", "/api/projects/"+project.ID+"/tasks", `{"title":"Clean the build","description":"remove stale output"}`, &task)

	// An agent that is not installed cannot be started, and says why.
	var apiErr struct {
		Error struct{ Code, Message string }
	}
	if code := api.do("POST", "/api/projects/"+project.ID+"/tasks/"+task.ID+"/runs", `{"agentId":"codex"}`, &apiErr); code != 409 || !strings.Contains(apiErr.Error.Message, "not found") {
		t.Fatalf("codex: %d %+v", code, apiErr)
	}

	// Start, talk, get asked, approve.
	var run domain.Run
	if code := api.do("POST", "/api/projects/"+project.ID+"/tasks/"+task.ID+"/runs", `{"agentId":"claude-code"}`, &run); code != 201 || run.State != domain.RunRunning {
		t.Fatalf("start: %d %+v", code, run)
	}
	if run.PID != 0 {
		t.Fatalf("the process id is internal and must not be exposed: %d", run.PID)
	}
	pid := agentPID()
	if !alive(pid) {
		t.Fatalf("the agent process (%d) is not running", pid)
	}
	api.waitWaiting(run.ID, domain.WaitIdle)
	if code := api.do("POST", "/api/projects/"+project.ID+"/runs/"+run.ID+"/input", `{"text":"ask"}`, nil); code != 200 {
		t.Fatalf("input: %d", code)
	}
	api.waitWaiting(run.ID, domain.WaitQuestion)
	var qs struct{ Questions []domain.Question }
	api.do("GET", "/api/projects/"+project.ID+"/questions", "", &qs)
	if len(qs.Questions) != 1 || qs.Questions[0].Kind != domain.QuestionApproval || !strings.Contains(qs.Questions[0].Context, "rm -rf build") {
		t.Fatalf("questions = %+v", qs.Questions)
	}
	api.do("POST", "/api/projects/"+project.ID+"/questions/"+qs.Questions[0].ID+"/answer", `{"answer":"Allow"}`, nil)
	idle := api.waitWaiting(run.ID, domain.WaitIdle)
	if got := strings.Join(api.outputs(run.ID), "|"); !strings.Contains(got, "assistant:command allowed") {
		t.Fatalf("outputs = %s", got)
	}
	if idle.SessionRef == "" {
		t.Fatal("no session reference was stored: the conversation could not be resumed")
	}

	// The controller shuts down cleanly with the agent idle: no process is left.
	sessionRef := idle.SessionRef
	stop, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if err := c.Shutdown(stop); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for alive(pid) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if alive(pid) {
		t.Fatal("the agent process outlived the controller")
	}

	// A new controller, same data directory: the conversation is still there.
	c2 := New(cfg, slog.New(slog.NewTextHandler(logs, nil)), "test")
	if err := c2.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer c2.Shutdown(ctx)
	api = client{t: t, base: "http://" + c2.Addr(), project: api.project}
	if got := api.run(run.ID); got.State != domain.RunWaitingForUser || got.Waiting != domain.WaitIdle || got.SessionRef != sessionRef {
		t.Fatalf("after restart: %+v", got)
	}
	if got := strings.Join(api.outputs(run.ID), "|"); !strings.Contains(got, "command allowed") {
		t.Fatalf("history lost across the restart: %s", got)
	}

	if code := api.do("POST", "/api/projects/"+project.ID+"/runs/"+run.ID+"/input", `{"text":"carry on"}`, nil); code != 200 {
		t.Fatalf("input after restart: %d", code)
	}
	api.waitWaiting(run.ID, domain.WaitIdle)
	b, _ := os.ReadFile(argsLog)
	lines := []string{}
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if line != "--help" {
			lines = append(lines, line)
		} // capability discovery is not a session launch
	}
	if len(lines) != 2 || !strings.Contains(lines[0], "--session-id "+sessionRef) || !strings.Contains(lines[1], "--resume "+sessionRef) {
		t.Fatalf("the agent was launched with:\n%s\nwant a fresh session, then a resume of %s", b, sessionRef)
	}
	if !strings.Contains(lines[0], "--permission-mode acceptEdits") || !strings.Contains(lines[0], "--permission-prompt-tool stdio") {
		t.Fatalf("flags = %s", lines[0])
	}

	var done domain.Run
	if code := api.do("POST", "/api/projects/"+project.ID+"/runs/"+run.ID+"/finish", "", &done); code != 200 || done.State != domain.RunCompleted {
		t.Fatalf("finish: %d %+v", code, done)
	}
	if !strings.Contains(strings.Join(api.outputs(run.ID), "|"), "user:carry on") {
		t.Fatal("the message that resumed the run is not in its activity")
	}
	if strings.Contains(logs.String(), "level=ERROR") {
		t.Errorf("errors were logged:\n%s", logs.String())
	}
}

func quote(s string) string { b, _ := json.Marshal(s); return string(b) }

func TestWorktreesLiveInTheConfiguredDirectory(t *testing.T) {
	script := filepath.Join(t.TempDir(), "claude")
	if err := os.WriteFile(script, []byte(fakeClaude), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := testConfig(t)
	cfg.WorktreesDir = filepath.Join(t.TempDir(), "elsewhere", "wt")
	cfg.Agents = map[string]config.AgentConfig{config.AgentClaudeCode: {Command: script}}
	c := New(cfg, slog.New(slog.DiscardHandler), "test")
	if err := c.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer c.Shutdown(context.Background())
	api := client{t: t, base: "http://" + c.Addr()}

	var project struct{ ID string }
	api.do("POST", "/api/projects", `{"path":`+quote(commit(t))+`}`, &project)
	var task domain.Task
	api.do("POST", "/api/projects/"+project.ID+"/tasks", `{"title":"Where do I live"}`, &task)
	var run domain.Run
	if code := api.do("POST", "/api/projects/"+project.ID+"/tasks/"+task.ID+"/runs", `{"agentId":"claude-code"}`, &run); code != 201 {
		t.Fatalf("start: %d", code)
	}
	root, _ := filepath.EvalSymlinks(cfg.WorktreesDir)
	entries, _ := os.ReadDir(filepath.Join(root, project.ID))
	if len(entries) != 1 {
		t.Fatalf("the worktree should be under %s: %v", root, entries)
	}
	if _, err := os.Stat(filepath.Join(cfg.DataDir, "worktrees")); err == nil {
		t.Fatal("the default directory was used despite the configuration")
	}
}

func TestRiskyAgentSettingsAreLoggedAsWarnings(t *testing.T) {
	cfg := testConfig(t)
	cfg.Agents = map[string]config.AgentConfig{config.AgentClaudeCode: {PermissionMode: "bypassPermissions"}}
	logs := &syncBuffer{}
	c := New(cfg, slog.New(slog.NewTextHandler(logs, nil)), "test")
	if err := c.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer c.Shutdown(context.Background())
	if out := logs.String(); !strings.Contains(out, "level=WARN") || !strings.Contains(out, "bypassPermissions") {
		t.Fatalf("log:\n%s", out)
	}
}

func TestInvalidAgentConfigurationPreventsStart(t *testing.T) {
	cfg := testConfig(t)
	cfg.Agents = map[string]config.AgentConfig{"claude-code": {PermissionMode: "yolo"}}
	c := New(cfg, slog.New(slog.DiscardHandler), "test")
	if err := c.Start(context.Background()); err == nil || !strings.Contains(err.Error(), "permissionMode") {
		t.Fatalf("err = %v", err)
	}
}
