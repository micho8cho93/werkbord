package codex

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"devboard/internal/agent"
	"devboard/internal/domain"
)

func TestMain(m *testing.M) {
	if os.Getenv("DEVBOARD_FAKE_CODEX") != "" {
		os.Exit(fakeMain())
	}
	os.Exit(m.Run())
}

func fakeCodex(t *testing.T) string {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "codex")
	script := "#!/bin/sh\nDEVBOARD_FAKE_CODEX=1 exec '" + self + "' \"$@\"\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func start(t *testing.T, cfg Config, prompt string) (agent.Session, string) {
	t.Helper()
	if cfg.Command == "" {
		cfg.Command = fakeCodex(t)
	}
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	s, err := New(cfg).Start(context.Background(), agent.StartRequest{RunID: "run_1", WorkDir: dir, Prompt: prompt})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = s.Stop(ctx)
		for range s.Events() {
		}
	})
	return s, dir
}

func next(t *testing.T, s agent.Session, keep func(agent.Event) bool) agent.Event {
	t.Helper()
	timeout := time.After(10 * time.Second)
	for {
		select {
		case ev, ok := <-s.Events():
			if !ok {
				t.Fatal("events closed while waiting for an event")
			}
			if keep(ev) {
				return ev
			}
		case <-timeout:
			t.Fatal("timed out waiting for an event")
		}
	}
}

func kind(k agent.EventKind) func(agent.Event) bool {
	return func(e agent.Event) bool { return e.Kind == k }
}

func collect(t *testing.T, s agent.Session) []agent.Event {
	t.Helper()
	var evs []agent.Event
	timeout := time.After(10 * time.Second)
	for {
		select {
		case ev, ok := <-s.Events():
			if !ok {
				t.Fatalf("events closed before the turn ended: %+v", evs)
			}
			evs = append(evs, ev)
			if ev.Kind == agent.KindTurnEnd {
				return evs
			}
		case <-timeout:
			t.Fatalf("timed out; events so far: %+v", evs)
		}
	}
}

func texts(evs []agent.Event, stream domain.OutputStream) []string {
	var out []string
	for _, e := range evs {
		if e.Kind == agent.KindOutput && e.Stream == stream {
			out = append(out, e.Text)
		}
	}
	return out
}

func TestDetect(t *testing.T) {
	ctx := context.Background()
	info := New(Config{Command: fakeCodex(t)}).Detect(ctx)
	if !info.Available || info.Version != "7.7.7" || info.ID != ID || info.Name != "Codex" {
		t.Fatalf("detect = %+v", info)
	}
	if missing := New(Config{Command: "/no/such/codex"}).Detect(ctx); missing.Available || !strings.Contains(missing.Detail, "not found") {
		t.Fatalf("missing = %+v", missing)
	}
	t.Setenv("FAKE_CODEX_LOGGED_OUT", "1")
	out := New(Config{Command: fakeCodex(t)}).Detect(ctx)
	if out.Available || !strings.Contains(out.Detail, "not signed in") || out.Version != "7.7.7" {
		t.Fatalf("signed out = %+v", out)
	}
}

func TestHandshakeSendsTheConfiguredPolicy(t *testing.T) {
	logFile := filepath.Join(t.TempDir(), "log")
	t.Setenv("FAKE_CODEX_LOG", logFile)
	s, dir := start(t, Config{Model: "gpt-x", Sandbox: "read-only", ApprovalPolicy: "untrusted"}, "hello")
	ref := next(t, s, kind(agent.KindSessionRef))
	if ref.SessionRef != "thread-1" {
		t.Fatalf("session ref = %q", ref.SessionRef)
	}
	collect(t, s)

	b, _ := os.ReadFile(logFile)
	var thread map[string]any
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		var rec struct {
			Method string
			Params map[string]any
		}
		_ = json.Unmarshal([]byte(line), &rec)
		switch rec.Method {
		case "initialize":
			caps, _ := rec.Params["capabilities"].(map[string]any)
			if caps["experimentalApi"] != true {
				t.Errorf("initialize params = %v", rec.Params)
			}
		case "thread/start":
			thread = rec.Params
		}
	}
	if thread["cwd"] != dir || thread["sandbox"] != "read-only" || thread["approvalPolicy"] != "untrusted" || thread["model"] != "gpt-x" {
		t.Fatalf("thread/start params = %v", thread)
	}
}

func TestResumeUsesTheThreadID(t *testing.T) {
	logFile := filepath.Join(t.TempDir(), "log")
	t.Setenv("FAKE_CODEX_LOG", logFile)
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	s, err := New(Config{Command: fakeCodex(t)}).Start(context.Background(), agent.StartRequest{WorkDir: dir, Prompt: "hello", ResumeRef: "thread-old"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = s.Stop(context.Background())
		for range s.Events() {
		}
	}()
	if ref := next(t, s, kind(agent.KindSessionRef)); ref.SessionRef != "thread-old" {
		t.Fatalf("session ref = %q", ref.SessionRef)
	}
	b, _ := os.ReadFile(logFile)
	if !strings.Contains(string(b), `"method":"thread/resume"`) || strings.Contains(string(b), `"method":"thread/start"`) {
		t.Fatalf("log:\n%s", b)
	}
}

func TestTurnsAndGracefulEnd(t *testing.T) {
	s, _ := start(t, Config{}, "hello")
	evs := collect(t, s)
	if got := texts(evs, domain.StreamAssistant); len(got) != 1 || got[0] != "hi from codex" {
		t.Fatalf("assistant text = %v", got)
	}
	if err := s.Send(context.Background(), "again"); err != nil {
		t.Fatal(err)
	}
	if got := texts(collect(t, s), domain.StreamAssistant); len(got) != 1 || got[0] != "echo: again" {
		t.Fatalf("second turn = %v", got)
	}
	if err := s.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	res := s.Wait()
	if res.State != domain.RunCompleted || res.ExitCode != 0 {
		t.Fatalf("result = %+v", res)
	}
	if err := s.Send(context.Background(), "late"); err == nil {
		t.Fatal("Send after the end must fail")
	}
}

func TestCommandApproval(t *testing.T) {
	for _, tc := range []struct{ answer, decision string }{
		{"Allow", "accept"}, {"Allow for this session", "acceptForSession"}, {"Deny", "decline"}, {"do something else", "decline"},
	} {
		t.Run(tc.answer, func(t *testing.T) {
			s, _ := start(t, Config{}, "run the tests")
			q := next(t, s, kind(agent.KindQuestion)).Question
			if q.Kind != domain.QuestionApproval || !strings.Contains(q.Prompt, "$ npm test") || !strings.Contains(q.Prompt, "run the tests") {
				t.Fatalf("question = %+v", q)
			}
			if len(q.Options) != 3 || q.Options[1] != "Allow for this session" {
				t.Fatalf("options = %v", q.Options)
			}
			if err := s.Respond(context.Background(), q.Ref, tc.answer); err != nil {
				t.Fatal(err)
			}
			evs := collect(t, s)
			if got := texts(evs, domain.StreamAssistant); len(got) != 1 || got[0] != "decision: "+tc.decision {
				t.Fatalf("assistant = %v, want decision %s", got, tc.decision)
			}
			tools := strings.Join(texts(evs, domain.StreamTool), "|")
			if tc.decision == "decline" && !strings.Contains(tools, "declined") {
				t.Errorf("a declined command should say so: %q", tools)
			}
			if tc.decision != "decline" && (!strings.Contains(tools, "$ npm test") || !strings.Contains(tools, "exit 1") || !strings.Contains(tools, "TypeError")) {
				t.Errorf("an approved command that failed should show it: %q", tools)
			}
			if err := s.Respond(context.Background(), q.Ref, "Allow"); err == nil {
				t.Fatal("a question can only be answered once")
			}
		})
	}
}

func TestFileChangeApproval(t *testing.T) {
	s, _ := start(t, Config{}, "patch it")
	q := next(t, s, kind(agent.KindQuestion)).Question
	if q.Kind != domain.QuestionApproval || !strings.Contains(q.Prompt, "Apply these file changes?") || !strings.Contains(q.Prompt, "/w/vendor") {
		t.Fatalf("question = %+v; it must say what access the change grants", q)
	}
	if err := s.Respond(context.Background(), q.Ref, "Allow"); err != nil {
		t.Fatal(err)
	}
	evs := collect(t, s)
	if got := texts(evs, domain.StreamAssistant); got[0] != "patch decision: accept" {
		t.Fatalf("assistant = %v", got)
	}
	if got := strings.Join(texts(evs, domain.StreamTool), "|"); got != "Edited a.go, Created b.go, Deleted /elsewhere/c.go" {
		t.Fatalf("tool output = %q", got)
	}
}

func TestRequestUserInputIsAskedOneAtATime(t *testing.T) {
	s, _ := start(t, Config{}, "ask me things")
	first := next(t, s, kind(agent.KindQuestion)).Question
	if first.Kind != domain.QuestionAsk || !strings.Contains(first.Prompt, "(1 of 2)") || !strings.Contains(first.Prompt, "Pick a colour") || len(first.Options) != 2 {
		t.Fatalf("first = %+v", first)
	}
	if err := s.Respond(context.Background(), first.Ref, "Blue"); err != nil {
		t.Fatal(err)
	}
	second := next(t, s, kind(agent.KindQuestion)).Question
	if !strings.Contains(second.Prompt, "(2 of 2)") || second.Ref == first.Ref {
		t.Fatalf("second = %+v", second)
	}
	if err := s.Respond(context.Background(), first.Ref, "Red"); err == nil {
		t.Fatal("the first question is closed")
	}
	if err := s.Respond(context.Background(), second.Ref, "XL"); err != nil {
		t.Fatal(err)
	}
	if got := texts(collect(t, s), domain.StreamAssistant); len(got) != 1 || got[0] != "colour=Blue size=XL" {
		t.Fatalf("assistant = %v", got)
	}
}

func TestSecretsAreNotCollected(t *testing.T) {
	s, _ := start(t, Config{}, "secret please")
	evs := collect(t, s) // no question may be raised: the turn just ends
	for _, e := range evs {
		if e.Kind == agent.KindQuestion {
			t.Fatalf("a secret was asked of the user: %+v", e.Question)
		}
	}
	if got := texts(evs, domain.StreamAssistant); len(got) != 1 || got[0] != "secret answers: 0" {
		t.Fatalf("assistant = %v", got)
	}
	if got := strings.Join(texts(evs, domain.StreamSystem), "|"); !strings.Contains(got, "does not collect secrets") {
		t.Fatalf("the user should be told: %q", got)
	}
}

func TestUnsupportedRequestsAreRefused(t *testing.T) {
	s, _ := start(t, Config{}, "unsupported thing")
	evs := collect(t, s)
	if got := texts(evs, domain.StreamAssistant); len(got) != 1 || got[0] != "unsupported got error: true" {
		t.Fatalf("assistant = %v; an unanswered request would hang the agent", got)
	}
}

func TestMessageDuringATurnSteersIt(t *testing.T) {
	s, _ := start(t, Config{}, "steerme")
	next(t, s, kind(agent.KindSessionRef))
	// Wait until the turn is known to be running.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cs := s.(*session); cs.activeTurn() != "" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := s.Send(context.Background(), "use the other approach"); err != nil {
		t.Fatal(err)
	}
	if got := texts(collect(t, s), domain.StreamAssistant); len(got) != 1 || got[0] != "steered: use the other approach" {
		t.Fatalf("assistant = %v", got)
	}
}

func TestFailedTurnAndRetryNotice(t *testing.T) {
	s, _ := start(t, Config{}, "fail this")
	evs := collect(t, s)
	sys := strings.Join(texts(evs, domain.StreamSystem), "|")
	if !strings.Contains(sys, "Temporary error, retrying: stream dropped") || !strings.Contains(sys, "The turn failed: usage limit reached") {
		t.Fatalf("system output = %q", sys)
	}
	if err := s.Send(context.Background(), "hello"); err != nil {
		t.Fatalf("a failed turn is not the end of the session: %v", err)
	}
	collect(t, s)
	_ = s.Send(context.Background(), "crash")
	res := s.Wait()
	if res.State != domain.RunFailed || res.ExitCode != 3 {
		t.Fatalf("result = %+v", res)
	}
	if !strings.Contains(res.Reason, "usage limit reached") {
		t.Fatalf("reason = %q; the agent's own account should win", res.Reason)
	}
}

func TestCrashIsAFailureWithItsReason(t *testing.T) {
	s, _ := start(t, Config{}, "hello")
	collect(t, s)
	_ = s.Send(context.Background(), "crash")
	res := s.Wait()
	if res.State != domain.RunFailed || res.ExitCode != 3 || !strings.Contains(res.Reason, "the sky fell") {
		t.Fatalf("result = %+v", res)
	}
	for range s.Events() {
	}
}

func TestStartFailsWhenTheServerDies(t *testing.T) {
	t.Setenv("FAKE_CODEX_DIE_AT_START", "1")
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	_, err := New(Config{Command: fakeCodex(t)}).Start(context.Background(), agent.StartRequest{WorkDir: dir, Prompt: "hello"})
	if err == nil || !strings.Contains(err.Error(), "cannot start the app server") {
		t.Fatalf("err = %v; a server that dies during the handshake never ran", err)
	}
}

func TestStartFailsForAMissingExecutable(t *testing.T) {
	if _, err := New(Config{Command: "/no/such/codex"}).Start(context.Background(), agent.StartRequest{WorkDir: t.TempDir(), Prompt: "hello"}); err == nil {
		t.Fatal("expected an error")
	}
}

func TestStartTimesOutWithoutAServer(t *testing.T) {
	script := filepath.Join(t.TempDir(), "codex")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexec sleep 300\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	old, oldGrace := startTimeout, agent.StopGrace
	startTimeout, agent.StopGrace = 300*time.Millisecond, 200*time.Millisecond
	t.Cleanup(func() { startTimeout, agent.StopGrace = old, oldGrace })
	begin := time.Now()
	_, err := New(Config{Command: script}).Start(context.Background(), agent.StartRequest{WorkDir: t.TempDir(), Prompt: "hello"})
	if err == nil || time.Since(begin) > 5*time.Second {
		t.Fatalf("err = %v after %v; a silent server must not hang Start", err, time.Since(begin))
	}
}

func TestStopEndsAHungAgent(t *testing.T) {
	old := agent.StopGrace
	agent.StopGrace = 300 * time.Millisecond
	t.Cleanup(func() { agent.StopGrace = old })
	s, _ := start(t, Config{}, "hello")
	collect(t, s)
	_ = s.Send(context.Background(), "hang")
	if err := s.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if res := s.Wait(); res.State != domain.RunFailed {
		t.Fatalf("result = %+v", res)
	}
	for range s.Events() {
	}
}

func TestHumanize(t *testing.T) {
	for in, want := range map[string]string{
		`{"type":"error","status":400,"error":{"type":"invalid_request_error","message":"The model is not supported."}}`: "The model is not supported.",
		`{"message":"plain wrapper"}`: "plain wrapper",
		`{"unrelated":1}`:             `{"unrelated":1}`,
		`not json {`:                  "not json {",
		`  spaced  `:                  "spaced",
	} {
		if got := humanize(in); got != want {
			t.Errorf("humanize(%q) = %q, want %q", in, got, want)
		}
	}
}
