package codex

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"devboard/internal/assistant/provider"
)

func TestMain(m *testing.M) {
	if os.Getenv("ASSISTANT_TEST_FAKE_CODEX") != "" {
		os.Exit(fakeMain())
	}
	os.Exit(m.Run())
}

var bg = context.Background()

func newProvider(t *testing.T) *Provider {
	t.Helper()
	t.Setenv("ASSISTANT_TEST_FAKE_CODEX", "1")
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
	info := newProvider(t).Detect(bg)
	if !info.Available || info.Version != "9.9.9" || info.Auth != "ChatGPT" || info.SignedIn == nil || !*info.SignedIn {
		t.Fatalf("info = %+v", info)
	}
	c := info.Capabilities
	if c.Streaming != provider.StreamTokens || !c.Resume || !c.Cancel || !c.ModelChoice || c.Voice != "experimental" || c.NativeTools != "mcp" {
		t.Fatalf("capabilities = %+v", c)
	}
	if info.UsesAPIKey {
		t.Error("signed in with a plan, not a key")
	}
	t.Setenv("FAKE_CODEX_API_KEY", "1")
	if k := newProvider(t).Detect(bg); !k.Available || !k.UsesAPIKey {
		t.Fatalf("a key is reported, not hidden: %+v", k)
	}
	t.Setenv("FAKE_CODEX_API_KEY", "")
	t.Setenv("FAKE_CODEX_LOGGED_OUT", "1")
	out := newProvider(t).Detect(bg)
	if out.Available || out.SignedIn == nil || *out.SignedIn || !strings.Contains(out.Guidance, " login`") || !strings.Contains(out.Guidance, "never asks for a key") {
		t.Fatalf("signed out = %+v", out)
	}
	missing := New(Config{Command: filepath.Join(t.TempDir(), "no-such-codex")}).Detect(bg)
	if missing.Installed || missing.Available || missing.Guidance == "" {
		t.Fatalf("missing = %+v", missing)
	}
}

func TestModelsAreAskedOfCodexAndHiddenOnesLeftOut(t *testing.T) {
	m := newProvider(t).Models(bg)
	if m.Source != "provider" || len(m.Models) != 1 || m.Models[0].ID != "fake-big" || !m.Models[0].Default || strings.Join(m.Models[0].Reasoning, ",") != "low,high" {
		t.Fatalf("models = %+v", m)
	}
	p := New(Config{Command: filepath.Join(t.TempDir(), "gone")})
	if m := p.Models(bg); m.Source != "builtin" || len(m.Models) != 0 || m.Note == "" || !m.Custom {
		t.Fatalf("when Codex cannot list its models the answer says so and a name can still be typed: %+v", m)
	}
	p = newProvider(t)
	p.cfg.Models = []provider.Model{{ID: "mine"}}
	if m := p.Models(bg); m.Source != "configured" || m.Models[0].ID != "mine" {
		t.Fatalf("configured = %+v", m)
	}
}

func TestATurnStreamsInSmallPiecesAndReturnsTheHandle(t *testing.T) {
	p := newProvider(t)
	res, c, err := run(t, p, provider.Turn{System: "be brief", Prompt: "say hello", Model: "fake-big", Reasoning: "low"})
	if err != nil {
		t.Fatal(err)
	}
	if got := c.texts(); len(got) != 3 || got[0] != "Hel" || got[1] != "lo" {
		t.Fatalf("pieces = %q", got)
	}
	if res.Text != "Hello model=fake-big effort=low" || !strings.HasPrefix(res.Ref, "thr-new-") || res.Usage.InputTokens != 12 || res.Usage.OutputTokens != 3 {
		t.Fatalf("result = %+v", res)
	}
	var first provider.Event
	for _, e := range c.events {
		if e.Kind != provider.EventAlive {
			first = e
			break
		}
	}
	if first.Kind != provider.EventRef || first.Ref != res.Ref {
		t.Fatalf("the handle is reported before anything is said: %+v", first)
	}
	// The next turn continues the same thread.
	res2, _, err := run(t, p, provider.Turn{Ref: "thr-known", Prompt: "again"})
	if err != nil || res2.Ref != "thr-known" {
		t.Fatalf("res %+v err %v", res2, err)
	}
}

func TestOnlyTheToolFeaturesThisCodexHasAreTurnedOffAndItRunsReadOnlyInAnEmptyDirectory(t *testing.T) {
	file := filepath.Join(t.TempDir(), "args")
	t.Setenv("FAKE_CODEX_ARGS_FILE", file)
	dir := t.TempDir()
	if _, _, err := run(t, newProvider(t), provider.Turn{Prompt: "hi", WorkDir: dir}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(file)
	args := strings.Split(string(raw), "\x00")
	var disabled []string
	for i, a := range args {
		if a == "--disable" {
			disabled = append(disabled, args[i+1])
		}
	}
	// The fake exits on a feature it does not know, so reaching here proves only existing, non-removed ones were passed.
	if strings.Join(disabled, ",") != "shell_tool,plugins,hooks" {
		t.Fatalf("disabled = %v", disabled)
	}
	wd, _ := filepath.EvalSymlinks(dir)
	if !strings.HasSuffix(string(raw), "cwd="+wd) {
		t.Errorf("cwd: %q", args[len(args)-1])
	}
}

func TestAnythingButAMessageEndsTheTurn(t *testing.T) {
	for _, prompt := range []string{"TOOL now", "COLLAB now", "ASK now"} {
		start := time.Now()
		_, c, err := run(t, newProvider(t), provider.Turn{Prompt: prompt})
		if provider.KindOf(err) != provider.KindPolicy || !errors.Is(err, provider.ErrPolicy) || provider.Retryable(err) {
			t.Errorf("%s: err = %v", prompt, err)
		}
		if got := c.texts(); len(got) != 0 {
			t.Errorf("%s: text after a violation reached the caller: %q", prompt, got)
		}
		if time.Since(start) > 10*time.Second {
			t.Errorf("%s: ending the turn took %s", prompt, time.Since(start))
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
		{"MODEL x", provider.KindModelUnavailable, false, "not available with your Codex account"},
		{"RATE x", provider.KindRateLimited, false, "usage limit"},
		{"AUTH x", provider.KindNotSignedIn, false, "codex login"},
		{"DIE x", provider.KindTransient, true, "panicked"},
	} {
		_, _, err := run(t, p, provider.Turn{Prompt: c.prompt})
		if provider.KindOf(err) != c.kind || provider.Retryable(err) != c.retry || !strings.Contains(err.Error(), c.text) {
			t.Errorf("%s: kind %s retry %v err %q; want %s %v containing %q", c.prompt, provider.KindOf(err), provider.Retryable(err), err, c.kind, c.retry, c.text)
		}
	}
	_, _, err := run(t, p, provider.Turn{Ref: "thr-nonexistent", Prompt: "hi"})
	if provider.KindOf(err) != provider.KindSessionLost {
		t.Errorf("a thread Codex no longer has: %v", err)
	}
	_, err = New(Config{Command: filepath.Join(t.TempDir(), "gone")}).Run(bg, provider.Turn{Prompt: "x", WorkDir: t.TempDir()}, func(provider.Event) {})
	if provider.KindOf(err) != provider.KindNotInstalled {
		t.Errorf("err = %v", err)
	}
	t.Setenv("FAKE_CODEX_LOGGED_OUT", "1")
	_, _, err = run(t, newProvider(t), provider.Turn{Prompt: "x"})
	if provider.KindOf(err) != provider.KindNotSignedIn {
		t.Errorf("signed out: %v", err)
	}
}

func TestAnErrorCodexIsRetryingItselfIsNotAFailure(t *testing.T) {
	res, _, err := run(t, newProvider(t), provider.Turn{Prompt: "RETRY x"})
	if err != nil || res.Text != "recovered" {
		t.Fatalf("res %+v err %v", res, err)
	}
}

func TestAServerThatDoesNotStreamStillDeliversTheReplyOnce(t *testing.T) {
	res, c, err := run(t, newProvider(t), provider.Turn{Prompt: "WHOLE x"})
	if err != nil || res.Text != "whole reply" || len(c.texts()) != 1 {
		t.Fatalf("res %+v pieces %q err %v", res, c.texts(), err)
	}
}

func cancelAfterFirstPiece(t *testing.T, prompt string) (time.Duration, error) {
	t.Helper()
	p := newProvider(t)
	ctx, cancel := context.WithCancel(bg)
	defer cancel()
	seen := make(chan struct{}, 1)
	done := make(chan error, 1)
	go func() {
		_, err := p.Run(ctx, provider.Turn{Prompt: prompt, WorkDir: t.TempDir()}, func(e provider.Event) {
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
		t.Fatal("no piece arrived")
	}
	time.Sleep(100 * time.Millisecond)
	start := time.Now()
	cancel()
	select {
	case err := <-done:
		return time.Since(start), err
	case <-time.After(15 * time.Second):
		t.Fatal("cancelling did not stop the turn")
		return 0, nil
	}
}

func TestCancellingInterruptsTheTurnAndThenEndsTheProcess(t *testing.T) {
	d, err := cancelAfterFirstPiece(t, "HANG now")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if d > 3*time.Second {
		t.Errorf("a server that acknowledges the interrupt is stopped promptly, took %s", d)
	}
}

func TestAServerThatIgnoresTheInterruptIsKilled(t *testing.T) {
	d, err := cancelAfterFirstPiece(t, "DEAF now")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if d > 8*time.Second {
		t.Errorf("took %s", d)
	}
}
