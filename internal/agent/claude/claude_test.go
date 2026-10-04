package claude

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"devboard/internal/agent"
	"devboard/internal/domain"
)

func TestMain(m *testing.M) {
	if os.Getenv("DEVBOARD_FAKE_CLAUDE") != "" {
		os.Exit(fakeMain())
	}
	settle = 150 * time.Millisecond
	os.Exit(m.Run())
}

// fakeClaude writes an executable that runs this test binary as the fake CLI.
func fakeClaude(t *testing.T) string {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "claude")
	script := "#!/bin/sh\nDEVBOARD_FAKE_CLAUDE=1 exec '" + self + "' \"$@\"\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func start(t *testing.T, cfg Config, prompt string) (agent.Session, string) {
	t.Helper()
	if cfg.Command == "" {
		cfg.Command = fakeClaude(t)
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

// next returns the next event matching keep, failing if it does not come.
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

// collect reads events until the turn ends and returns them.
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
	info := New(Config{Command: fakeClaude(t)}).Detect(ctx)
	if !info.Available || info.Version != "9.9.9" || info.ID != ID || info.Name != "Claude Code" {
		t.Fatalf("detect = %+v", info)
	}

	missing := New(Config{Command: "/no/such/claude"}).Detect(ctx)
	if missing.Available || !strings.Contains(missing.Detail, "not found") {
		t.Fatalf("missing = %+v", missing)
	}

	t.Setenv("FAKE_CLAUDE_LOGGED_OUT", "1")
	out := New(Config{Command: fakeClaude(t)}).Detect(ctx)
	if out.Available || !strings.Contains(out.Detail, "not signed in") || out.Version != "9.9.9" {
		t.Fatalf("signed out = %+v; it should say so, and still report the version", out)
	}
}

func TestDetectIsCached(t *testing.T) {
	counter := filepath.Join(t.TempDir(), "calls")
	script := filepath.Join(t.TempDir(), "claude")
	body := "#!/bin/sh\necho x >> '" + counter + "'\n[ \"$1\" = \"--version\" ] && echo '1.2.3 (Claude Code)'\n[ \"$1\" = auth ] && echo '{\"loggedIn\":true}'\nexit 0\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	a := New(Config{Command: script})
	for i := 0; i < 5; i++ {
		if !a.Detect(context.Background()).Available {
			t.Fatal("not available")
		}
	}
	b, _ := os.ReadFile(counter)
	if n := strings.Count(string(b), "x"); n != 2 { // --version and auth status, once
		t.Fatalf("the CLI was run %d times for 5 detections; every request would spawn processes", n)
	}
}

func TestStartPassesTheRightFlags(t *testing.T) {
	argsFile := filepath.Join(t.TempDir(), "args")
	t.Setenv("FAKE_CLAUDE_ARGS_FILE", argsFile)
	s, dir := start(t, Config{Model: "sonnet"}, "hello")
	collect(t, s)
	b, _ := os.ReadFile(argsFile)
	args := string(b)
	for _, want := range []string{"-p", "stream-json", "--permission-prompt-tool", "stdio", "--permission-mode", "acceptEdits", "--model", "sonnet", "--session-id", "cwd=" + dir} {
		if !strings.Contains(args, want) {
			t.Errorf("args lack %q:\n%s", want, args)
		}
	}
	if strings.Contains(args, "--resume") || strings.Contains(args, "dangerously") {
		t.Errorf("unexpected flags:\n%s", args)
	}
}

func TestResumePassesTheSessionRef(t *testing.T) {
	argsFile := filepath.Join(t.TempDir(), "args")
	t.Setenv("FAKE_CLAUDE_ARGS_FILE", argsFile)
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	s, err := New(Config{Command: fakeClaude(t)}).Start(context.Background(), agent.StartRequest{WorkDir: dir, Prompt: "hello", ResumeRef: "abc-123"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = s.Stop(context.Background())
		for range s.Events() {
		}
	}()
	ref := next(t, s, kind(agent.KindSessionRef))
	if ref.SessionRef != "abc-123" {
		t.Fatalf("session ref = %q", ref.SessionRef)
	}
	collect(t, s) // the fake records its arguments as it starts
	b, _ := os.ReadFile(argsFile)
	if !strings.Contains(string(b), "--resume\nabc-123") || strings.Contains(string(b), "--session-id") {
		t.Fatalf("args:\n%s", b)
	}
}

func TestSessionRefIsKnownBeforeTheAgentSpeaks(t *testing.T) {
	s, _ := start(t, Config{}, "hang")
	ev := next(t, s, kind(agent.KindSessionRef))
	if len(ev.SessionRef) != 36 || strings.Count(ev.SessionRef, "-") != 4 {
		t.Fatalf("session ref %q is not a UUID", ev.SessionRef)
	}
}

func TestOneTurnThenIdle(t *testing.T) {
	s, _ := start(t, Config{}, "hello")
	evs := collect(t, s)
	if got := texts(evs, domain.StreamAssistant); len(got) != 1 || got[0] != "hi there" {
		t.Fatalf("assistant text = %v", got)
	}
	if got := texts(evs, domain.StreamSystem); len(got) != 1 || !strings.Contains(got[0], "fake-model") {
		t.Fatalf("system text = %v", got)
	}

	// The session stays open: a second message is a second turn in the same process.
	if err := s.Send(context.Background(), "tell me more"); err != nil {
		t.Fatal(err)
	}
	evs = collect(t, s)
	if got := texts(evs, domain.StreamAssistant); len(got) != 1 || got[0] != "echo: tell me more" {
		t.Fatalf("second turn = %v", got)
	}

	if err := s.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	res := s.Wait()
	if res.State != domain.RunCompleted || res.ExitCode != 0 {
		t.Fatalf("result = %+v; finishing a session is a clean exit", res)
	}
	if err := s.Send(context.Background(), "late"); err == nil {
		t.Fatal("Send after the end must fail")
	}
}

func TestApprovalIsAllowed(t *testing.T) {
	s, dir := start(t, Config{}, "edit the file")
	q := next(t, s, kind(agent.KindQuestion)).Question
	if q.Kind != domain.QuestionApproval || q.Ref != "req-edit" || q.Prompt != "Edit main.go?" {
		t.Fatalf("question = %+v; paths should be shown relative to the worktree (%s)", q, dir)
	}
	if len(q.Options) != 2 || q.Options[0] != "Allow" || q.Options[1] != "Deny" {
		t.Fatalf("options = %v", q.Options)
	}
	if err := s.Respond(context.Background(), q.Ref, "Allow"); err != nil {
		t.Fatal(err)
	}
	evs := collect(t, s)
	if got := texts(evs, domain.StreamAssistant); len(got) != 1 || got[0] != "edited" {
		t.Fatalf("after allowing: %v", got)
	}
	if err := s.Respond(context.Background(), q.Ref, "Allow"); err == nil {
		t.Fatal("a question can only be answered once")
	}
}

func TestApprovalIsDeniedWithTheUsersWords(t *testing.T) {
	s, _ := start(t, Config{}, "bash please")
	q := next(t, s, kind(agent.KindQuestion)).Question
	if !strings.Contains(q.Prompt, "$ npm test") || !strings.Contains(q.Prompt, "run the tests") {
		t.Fatalf("prompt = %q", q.Prompt)
	}
	if err := s.Respond(context.Background(), q.Ref, "Deny"); err != nil {
		t.Fatal(err)
	}
	if got := texts(collect(t, s), domain.StreamAssistant); len(got) != 1 || got[0] != "bash: deny" {
		t.Fatalf("after denying: %v", got)
	}
}

func TestFreeTextAnswerToAnApprovalDenies(t *testing.T) {
	s, _ := start(t, Config{}, "edit it")
	q := next(t, s, kind(agent.KindQuestion)).Question
	if err := s.Respond(context.Background(), q.Ref, "use the other file instead"); err != nil {
		t.Fatal(err)
	}
	got := texts(collect(t, s), domain.StreamAssistant)
	if len(got) != 1 || !strings.Contains(got[0], "denied") || !strings.Contains(got[0], "use the other file instead") {
		t.Fatalf("the agent should learn why it was denied: %v", got)
	}
}

func TestAskUserQuestionIsAskedOneAtATime(t *testing.T) {
	s, _ := start(t, Config{}, "askme")
	first := next(t, s, kind(agent.KindQuestion)).Question
	if first.Kind != domain.QuestionAsk || first.Prompt != "(1 of 2) Which colour?" || len(first.Options) != 2 || first.Options[1] != "Blue" {
		t.Fatalf("first = %+v", first)
	}
	if err := s.Respond(context.Background(), first.Ref, "Blue"); err != nil {
		t.Fatal(err)
	}
	second := next(t, s, kind(agent.KindQuestion)).Question
	if second.Prompt != "(2 of 2) Which size?" || second.Ref == first.Ref {
		t.Fatalf("second = %+v", second)
	}
	if err := s.Respond(context.Background(), first.Ref, "Red"); err == nil {
		t.Fatal("the first question is closed; answering it again must fail")
	}
	if err := s.Respond(context.Background(), second.Ref, "free text is fine"); err != nil {
		t.Fatal(err)
	}
	if got := texts(collect(t, s), domain.StreamAssistant); len(got) != 1 || got[0] != "colour=Blue size=free text is fine" {
		t.Fatalf("the agent should get every answer: %v", got)
	}
}

func TestAgentCancellingAQuestionClosesIt(t *testing.T) {
	s, _ := start(t, Config{}, "cancelask")
	q := next(t, s, kind(agent.KindQuestion)).Question
	closed := next(t, s, kind(agent.KindQuestionClosed))
	if closed.Ref != q.Ref {
		t.Fatalf("closed %q, asked %q", closed.Ref, q.Ref)
	}
	if err := s.Respond(context.Background(), q.Ref, "Allow"); err == nil {
		t.Fatal("a withdrawn question cannot be answered")
	}
}

func TestUnsupportedControlRequestsAreRefusedNotIgnored(t *testing.T) {
	// An agent waiting on a reply that never comes would hang the whole session.
	s, _ := start(t, Config{}, "hook")
	if got := texts(collect(t, s), domain.StreamAssistant); len(got) != 1 || got[0] != "hook response: error" {
		t.Fatalf("got %v", got)
	}
}

func TestFailedTurnKeepsTheSessionAlive(t *testing.T) {
	s, _ := start(t, Config{}, "failturn")
	evs := collect(t, s)
	if got := texts(evs, domain.StreamSystem); len(got) < 2 || !strings.Contains(got[len(got)-1], "Credit balance is too low") {
		t.Fatalf("the failure should be visible: %v", got)
	}
	if err := s.Send(context.Background(), "hello again"); err != nil {
		t.Fatalf("a failed turn is not the end of the session: %v", err)
	}
}

func TestCrashIsAFailureWithItsReason(t *testing.T) {
	s, _ := start(t, Config{}, "hello")
	collect(t, s)
	if err := s.Send(context.Background(), "crash now"); err != nil {
		t.Fatal(err)
	}
	res := s.Wait()
	if res.State != domain.RunFailed || res.ExitCode != 2 || !strings.Contains(res.Reason, "the sky fell") {
		t.Fatalf("result = %+v", res)
	}
	for range s.Events() {
	}
}

func TestAgentsOwnErrorBecomesTheReason(t *testing.T) {
	// A turn that failed, then a crash: say what the agent said, not just "status 2".
	s, _ := start(t, Config{}, "failturn")
	collect(t, s)
	if err := s.Send(context.Background(), "crash"); err != nil {
		t.Fatal(err)
	}
	res := s.Wait()
	if res.State != domain.RunFailed || !strings.Contains(res.Reason, "Credit balance is too low") {
		t.Fatalf("result = %+v", res)
	}
	for range s.Events() {
	}
}

func TestNoiseIsTolerated(t *testing.T) {
	s, _ := start(t, Config{}, "noise")
	evs := collect(t, s)
	if got := texts(evs, domain.StreamAssistant); len(got) != 1 || got[0] != "after noise" {
		t.Fatalf("assistant text = %v; blank text and thinking must be skipped", got)
	}
	sys := texts(evs, domain.StreamSystem)
	var sawJunk, sawLimit bool
	for _, s := range sys {
		sawJunk = sawJunk || s == "this is not json"
		sawLimit = sawLimit || strings.Contains(s, "Rate limit: rejected")
	}
	if !sawJunk || !sawLimit {
		t.Fatalf("system notices = %v", sys)
	}
}

// patientSettle makes Start wait long enough to see a slow-starting process die
// even on a loaded machine; it returns as soon as the process does.
func patientSettle(t *testing.T) {
	old := settle
	settle = 10 * time.Second
	t.Cleanup(func() { settle = old })
}

func TestCrashOnTheFirstMessageFailsStart(t *testing.T) {
	patientSettle(t)
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	_, err := New(Config{Command: fakeClaude(t)}).Start(context.Background(), agent.StartRequest{WorkDir: dir, Prompt: "crash now"})
	if err == nil || !strings.Contains(err.Error(), "the sky fell") {
		t.Fatalf("err = %v; an agent that dies at once never ran", err)
	}
}

func TestStartFailsWhenTheProcessDiesImmediately(t *testing.T) {
	patientSettle(t)
	t.Setenv("FAKE_CLAUDE_DIE_AT_START", "1")
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	_, err := New(Config{Command: fakeClaude(t)}).Start(context.Background(), agent.StartRequest{WorkDir: dir, Prompt: "hello"})
	if err == nil || !strings.Contains(err.Error(), "unknown option") {
		t.Fatalf("err = %v; a setup failure must fail Start, not look like a run", err)
	}
}

func TestStartFailsForAMissingExecutable(t *testing.T) {
	_, err := New(Config{Command: "/no/such/claude"}).Start(context.Background(), agent.StartRequest{WorkDir: t.TempDir(), Prompt: "hello"})
	if err == nil {
		t.Fatal("expected an error")
	}
}

func TestStopEndsAHungAgent(t *testing.T) {
	old := agent.StopGrace
	agent.StopGrace = 300 * time.Millisecond
	t.Cleanup(func() { agent.StopGrace = old })
	s, _ := start(t, Config{}, "hang")
	if err := s.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if res := s.Wait(); res.State != domain.RunFailed {
		t.Fatalf("result = %+v", res)
	}
	for range s.Events() {
	}
}

func TestProcessInfoIsRecorded(t *testing.T) {
	s, _ := start(t, Config{}, "hang")
	if info := s.Process(); info.PID <= 1 || info.ID == "" {
		t.Fatalf("process = %+v", info)
	}
}

func TestSummarizeTool(t *testing.T) {
	cases := []struct {
		name  string
		input map[string]any
		want  string
	}{
		{"Bash", map[string]any{"command": "go test ./...\nsecond line"}, "Bash: go test ./... …"},
		{"Edit", map[string]any{"file_path": "/w/internal/a.go"}, "Edit internal/a.go"},
		{"Read", map[string]any{"file_path": "/elsewhere/x.go"}, "Read /elsewhere/x.go"},
		{"Grep", map[string]any{"pattern": "TODO", "path": "/w/src"}, "Grep TODO in src"},
		{"TodoWrite", map[string]any{"todos": []any{}}, "Updating the to-do list"},
		{"WebFetch", map[string]any{"url": "https://example.com"}, "WebFetch https://example.com"},
		{"mcp__thing__do", map[string]any{"description": "does it"}, "mcp__thing__do does it"},
		{"Mystery", map[string]any{}, "Mystery"},
	}
	for _, c := range cases {
		if got := summarizeTool(c.name, c.input, "/w"); got != c.want {
			t.Errorf("summarizeTool(%s) = %q, want %q", c.name, got, c.want)
		}
	}
	if got := summarizeTool("Bash", map[string]any{"command": strings.Repeat("x", 1000)}, "/w"); len([]rune(got)) > 310 {
		t.Errorf("a long command is not clipped: %d runes", len([]rune(got)))
	}
}
