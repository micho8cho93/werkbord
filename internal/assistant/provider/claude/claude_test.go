package claude

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"devboard/internal/assistant/provider"
)

func TestMain(m *testing.M) {
	switch {
	case os.Getenv("ASSISTANT_TEST_FAKE_CLAUDE_SLEEPER") != "":
		os.Exit(sleeperMain())
	case os.Getenv("ASSISTANT_TEST_FAKE_CLAUDE") != "":
		os.Exit(fakeMain())
	}
	os.Exit(m.Run())
}

var bg = context.Background()

func newProvider(t *testing.T) *Provider {
	t.Helper()
	t.Setenv("ASSISTANT_TEST_FAKE_CLAUDE", "1")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return New(Config{Command: exe})
}

type collected struct{ events []provider.Event }

func (c *collected) emit(e provider.Event) { c.events = append(c.events, e) }

func (c *collected) texts() []string {
	var out []string
	for _, e := range c.events {
		if e.Kind == provider.EventText {
			out = append(out, e.Text)
		}
	}
	return out
}

func run(t *testing.T, p *Provider, turn provider.Turn) (provider.Result, *collected, error) {
	t.Helper()
	if turn.WorkDir == "" {
		turn.WorkDir = t.TempDir()
	}
	c := &collected{}
	ctx, cancel := context.WithTimeout(bg, 30*time.Second)
	defer cancel()
	res, err := p.Run(ctx, turn, c.emit)
	return res, c, err
}

func TestDetectReportsWhatIsThereAndWhatIsMissing(t *testing.T) {
	p := newProvider(t)
	info := p.Detect(bg)
	if !info.Available || !info.Installed || info.Version != "9.9.9" || info.Auth != "claude.ai" || info.SignedIn == nil || !*info.SignedIn {
		t.Fatalf("info = %+v", info)
	}
	c := info.Capabilities
	if c.Streaming != provider.StreamTokens || !c.Resume || !c.Cancel || !c.ModelChoice || !c.ReasoningChoice || c.NativeTools != "mcp" || c.Voice != "none" {
		t.Fatalf("capabilities = %+v", c)
	}

	if info.UsesAPIKey {
		t.Error("signed in with a plan, not a key")
	}
	t.Setenv("FAKE_CLAUDE_API_KEY", "1")
	if k := newProvider(t).Detect(bg); !k.Available || !k.UsesAPIKey {
		t.Fatalf("a key in the person's environment is reported, not hidden: %+v", k)
	}
	t.Setenv("FAKE_CLAUDE_API_KEY", "")
	t.Setenv("FAKE_CLAUDE_LOGGED_OUT", "1")
	out := newProvider(t).Detect(bg)
	if out.Available || out.SignedIn == nil || *out.SignedIn || !strings.Contains(out.Guidance, "auth login") || !strings.Contains(out.Guidance, "never asks for a key") {
		t.Fatalf("signed out = %+v", out)
	}

	t.Setenv("FAKE_CLAUDE_LOGGED_OUT", "")
	t.Setenv("FAKE_CLAUDE_OLD", "1")
	old := newProvider(t).Detect(bg)
	if old.Available || !old.Installed || !strings.Contains(old.Detail, "--tools") {
		t.Fatalf("an old version without --tools must be refused, not run with fewer protections: %+v", old)
	}

	missing := New(Config{Command: filepath.Join(t.TempDir(), "no-such-claude")}).Detect(bg)
	if missing.Installed || missing.Available || missing.Guidance == "" {
		t.Fatalf("missing = %+v", missing)
	}
}

func TestModelsAreAliasesOrTheConfiguredList(t *testing.T) {
	m := newProvider(t).Models(bg)
	if m.Source != "builtin" || !m.Custom || len(m.Models) != 3 || m.Models[0].ID != "sonnet" || strings.Join(m.Reasoning, ",") != "low,medium,high,xhigh,max" {
		t.Fatalf("models = %+v", m)
	}
	p := newProvider(t)
	p.cfg.Models = []provider.Model{{ID: "my-model", Name: "Mine"}}
	p.cfg.Reasoning = []string{"low"}
	m = p.Models(bg)
	if m.Source != "configured" || len(m.Models) != 1 || m.Models[0].ID != "my-model" || len(m.Reasoning) != 1 {
		t.Fatalf("configured = %+v", m)
	}
}

func TestATurnStreamsInSmallPiecesAndReturnsTheHandle(t *testing.T) {
	p := newProvider(t)
	res, c, err := run(t, p, provider.Turn{System: "be brief", Prompt: "say hello"})
	if err != nil {
		t.Fatal(err)
	}
	if got := c.texts(); len(got) != 3 || got[0] != "Hel" || got[1] != "lo" {
		t.Fatalf("pieces = %q: the reply must arrive as it is generated", got)
	}
	if res.Text != "Hello, 9 chars" || res.Model != "claude-fake-1" || res.Usage.InputTokens != 110 || res.Usage.OutputTokens != 4 {
		t.Fatalf("result = %+v", res)
	}
	if c.events[0].Kind != provider.EventRef || c.events[0].Ref == "" || c.events[0].Ref != res.Ref {
		t.Fatalf("the handle is reported before anything else, so a dying turn can still be continued: %+v / %q", c.events[0], res.Ref)
	}
}

func TestTheCommandLineRunsClaudeWithoutAnyToolAndKeepsThePromptOffIt(t *testing.T) {
	file := filepath.Join(t.TempDir(), "args")
	t.Setenv("FAKE_CLAUDE_ARGS_FILE", file)
	p := newProvider(t)
	dir := t.TempDir()
	res, _, err := run(t, p, provider.Turn{System: "the instructions", Prompt: "the secret question", WorkDir: dir, Model: "opus", Reasoning: "high"})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(file)
	args := strings.Split(string(raw), "\x00")
	has := func(flag string, value ...string) {
		t.Helper()
		for i, a := range args {
			if a == flag {
				for j, v := range value {
					if i+1+j >= len(args) || args[i+1+j] != v {
						t.Errorf("%s is followed by %q, want %q", flag, args[i+1:], value)
					}
				}
				return
			}
		}
		t.Errorf("%s is missing from %q", flag, args)
	}
	has("-p")
	has("--output-format", "stream-json")
	has("--include-partial-messages")
	has("--tools", "") // no tool of its own
	has("--safe-mode")
	has("--strict-mcp-config")
	has("--disable-slash-commands")
	has("--setting-sources", "") // none of the person's settings
	has("--system-prompt", "the instructions")
	has("--system-prompt-snapshot", "off")
	has("--model", "opus")
	has("--effort", "high")
	has("--session-id", res.Ref)
	if strings.Contains(string(raw), "the secret question") {
		t.Error("the prompt must go over stdin, not the command line where any user can read it")
	}
	if !strings.HasSuffix(string(raw), "cwd="+evalSymlinks(t, dir)) {
		t.Errorf("it must run in the private empty directory: %q", args[len(args)-1])
	}
	for _, banned := range []string{"--dangerously-skip-permissions", "--allow-dangerously-skip-permissions", "--mcp-config", "--add-dir", "--permission-mode"} {
		for _, a := range args {
			if a == banned {
				t.Errorf("%s must never be passed", banned)
			}
		}
	}

	// A conversation that has a handle is continued, not started again.
	if _, _, err := run(t, p, provider.Turn{Ref: res.Ref, Prompt: "and then?"}); err != nil {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(file)
	if !strings.Contains(string(raw), "--resume\x00"+res.Ref) || strings.Contains(string(raw), "--session-id") {
		t.Errorf("a continued turn uses --resume: %q", raw)
	}
	if strings.Contains(string(raw), "--model") || strings.Contains(string(raw), "--effort") {
		t.Errorf("no model was chosen, so none is passed: %q", raw)
	}
}

func evalSymlinks(t *testing.T, p string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestTheControllersOwnSecretsAreNotPassedOn(t *testing.T) {
	file := filepath.Join(t.TempDir(), "env")
	t.Setenv("FAKE_CLAUDE_ENV_FILE", file)
	t.Setenv("WERKBORD_TOKEN", "controller-secret")
	t.Setenv("DEVBOARD_TOKEN", "controller-secret")
	if _, _, err := run(t, newProvider(t), provider.Turn{Prompt: "hi"}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(file)
	if strings.Contains(string(raw), "controller-secret") {
		t.Error("the controller's token reached the provider's process")
	}
	if !strings.Contains(string(raw), "PATH=") {
		t.Error("the person's own environment, which their sign-in may depend on, is kept")
	}
}

func TestAProviderThatHasOrUsesAToolIsStopped(t *testing.T) {
	p := newProvider(t)
	for _, prompt := range []string{"TOOLS_LISTED now", "TOOL_USE now"} {
		_, c, err := run(t, p, provider.Turn{Prompt: prompt})
		if provider.KindOf(err) != provider.KindPolicy || !errors.Is(err, provider.ErrPolicy) || provider.Retryable(err) {
			t.Errorf("%s: err = %v", prompt, err)
		}
		for _, text := range c.texts() {
			t.Errorf("%s: text after a violation reached the caller: %q", prompt, text)
		}
	}
}

func TestFailuresAreClassifiedAndSayWhatToDo(t *testing.T) {
	p := newProvider(t)
	for _, c := range []struct {
		prompt string
		kind   provider.Kind
		retry  bool
		text   string
	}{
		{"AUTH x", provider.KindNotSignedIn, false, "auth login"},
		{"MODEL x", provider.KindModelUnavailable, false, "not available to your Claude account"},
		{"RATE x", provider.KindRateLimited, false, "usage limit"},
		{"OVERLOAD x", provider.KindTransient, true, "temporary"},
		{"NOSESSION x", provider.KindSessionLost, false, "no longer has this conversation"},
		{"DIE x", provider.KindTransient, true, "panic: something broke"},
	} {
		_, _, err := run(t, p, provider.Turn{Prompt: c.prompt})
		if provider.KindOf(err) != c.kind || provider.Retryable(err) != c.retry || !strings.Contains(err.Error(), c.text) {
			t.Errorf("%s: kind %s retry %v err %q; want %s %v containing %q", c.prompt, provider.KindOf(err), provider.Retryable(err), err, c.kind, c.retry, c.text)
		}
	}
	_, err := New(Config{Command: filepath.Join(t.TempDir(), "gone")}).Run(bg, provider.Turn{Prompt: "x", WorkDir: t.TempDir()}, func(provider.Event) {})
	if provider.KindOf(err) != provider.KindNotInstalled {
		t.Errorf("err = %v", err)
	}
	t.Setenv("FAKE_CLAUDE_LOGGED_OUT", "1")
	_, _, err = run(t, newProvider(t), provider.Turn{Prompt: "x"})
	if provider.KindOf(err) != provider.KindNotSignedIn || !strings.Contains(err.Error(), "never asks for a key") {
		t.Errorf("signed out: %v", err)
	}
}

func TestAVersionWithoutPartialMessagesStillDeliversTheReplyOnce(t *testing.T) {
	res, c, err := run(t, newProvider(t), provider.Turn{Prompt: "NOPARTIAL x"})
	if err != nil || res.Text != "whole reply" || len(c.texts()) != 1 {
		t.Fatalf("res %+v pieces %q err %v", res, c.texts(), err)
	}
}

func TestThinkingIsNeverShown(t *testing.T) {
	res, c, err := run(t, newProvider(t), provider.Turn{Prompt: "THINK x"})
	if err != nil || res.Text != "done thinking" {
		t.Fatal(res, err)
	}
	alive := 0
	for _, e := range c.events {
		if e.Kind == provider.EventAlive {
			alive++
		}
	}
	if alive == 0 || len(c.texts()) != 1 {
		t.Errorf("thinking only shows the turn is alive: %+v", c.events)
	}
}

func TestAnAbsurdLineIsSkippedNotBuffered(t *testing.T) {
	res, _, err := run(t, newProvider(t), provider.Turn{Prompt: "BIG x"})
	if err != nil || res.Text != "after the big line" {
		t.Fatalf("res %+v err %v", res, err)
	}
}

func TestCancellingStopsTheProcessAndEverythingItStarted(t *testing.T) {
	childFile := filepath.Join(t.TempDir(), "child")
	t.Setenv("FAKE_CLAUDE_CHILD_FILE", childFile)
	p := newProvider(t)
	ctx, cancel := context.WithCancel(bg)
	seen := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, err := p.Run(ctx, provider.Turn{Prompt: "CHILD now", WorkDir: t.TempDir()}, func(e provider.Event) {
			if e.Kind == provider.EventText {
				select {
				case seen <- struct{}{}:
				default:
				}
			}
		})
		done <- err
	}()
	select {
	case <-seen:
	case <-time.After(20 * time.Second):
		t.Fatal("the first piece never arrived")
	}
	start := time.Now()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("cancelling did not stop the turn")
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("stopping took %s", d)
	}
	raw, err := os.ReadFile(childFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, _ := strconv.Atoi(string(raw))
	deadline := time.Now().Add(3 * time.Second)
	for {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the process the provider started (%d) is still running", pid)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestAProcessThatIgnoresTheRequestToStopIsKilled(t *testing.T) {
	t.Setenv("FAKE_CLAUDE_IGNORE_TERM", "1")
	p := newProvider(t)
	ctx, cancel := context.WithCancel(bg)
	seen := make(chan struct{}, 1)
	done := make(chan error, 1)
	go func() {
		_, err := p.Run(ctx, provider.Turn{Prompt: "HANG now", WorkDir: t.TempDir()}, func(e provider.Event) {
			if e.Kind == provider.EventText {
				select {
				case seen <- struct{}{}:
				default:
				}
			}
		})
		done <- err
	}()
	<-seen
	time.Sleep(200 * time.Millisecond) // let it install the handler
	start := time.Now()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("a process that ignores SIGTERM kept the turn alive")
	}
	if d := time.Since(start); d < time.Second || d > 8*time.Second {
		t.Errorf("it should have been killed after the grace period, took %s", d)
	}
}
