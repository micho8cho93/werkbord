package acp

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	agentapi "werkbord/internal/agent"
	"werkbord/internal/domain"

	acpsdk "github.com/caelis-labs/acp-go-sdk"
)

func testClient(t *testing.T) (*runClient, string, *session) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	owner := &session{ctx: ctx, stream: agentapi.NewEventStream()}
	t.Cleanup(owner.stream.Close)
	client := &runClient{owner: owner, root: root, questions: map[string]*permission{}, terminals: map[string]*terminal{}, approvals: map[string]int{}}
	return client, root, owner
}

func TestWorkspaceFileWritesRequireAndRespectApproval(t *testing.T) {
	c, root, owner := testClient(t)
	path := filepath.Join(root, "nested", "note.txt")
	result := make(chan error, 1)
	go func() {
		_, err := c.WriteTextFile(context.Background(), acpWrite(path, "approved"))
		result <- err
	}()
	select {
	case ev := <-owner.Events():
		if ev.Kind != agentapi.KindQuestion || ev.Question == nil || ev.Question.Kind != domain.QuestionApproval {
			t.Fatalf("approval event = %+v", ev)
		}
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("file exists before approval: %v", err)
		}
		if err := c.respond(ev.Question.Ref, domain.AnswerAllow); err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("write did not ask for approval")
	}
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != "approved" {
		t.Fatalf("written file = %q, %v", got, err)
	}

	denied := filepath.Join(root, "denied.txt")
	go func() {
		_, err := c.WriteTextFile(context.Background(), acpWrite(denied, "must not write"))
		result <- err
	}()
	ev := <-owner.Events()
	if ev.Question == nil {
		t.Fatalf("approval event = %+v", ev)
	}
	if err := c.respond(ev.Question.Ref, domain.AnswerDeny); err != nil {
		t.Fatal(err)
	}
	if err := <-result; err == nil {
		t.Fatal("denied write succeeded")
	}
	if _, err := os.Stat(denied); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("denied file exists: %v", err)
	}
}

func TestWorkspacePathCannotEscapeThroughSymlink(t *testing.T) {
	c, root, _ := testClient(t)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "outside")); err != nil {
		t.Fatal(err)
	}
	if _, err := c.safePath(filepath.Join(root, "outside", "secret.txt"), true); err == nil {
		t.Fatal("accepted a path that resolves outside the run worktree")
	}
	if _, err := c.safePath(filepath.Join(root, "..", filepath.Base(outside), "secret.txt"), true); err == nil {
		t.Fatal("accepted a path outside the run worktree")
	}
}

func acpWrite(path, content string) acpsdk.WriteTextFileRequest {
	return acpsdk.WriteTextFileRequest{Path: path, Content: content}
}

func TestACPAdapterStreamsLoadsApprovalsAndStops(t *testing.T) {
	command := filepath.Join(t.TempDir(), "opencode")
	build := exec.Command("go", "build", "-o", command, "./testdata/fake")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build ACP test agent: %v\n%s", err, output)
	}
	optionsFile := filepath.Join(t.TempDir(), "options")
	t.Setenv("ACP_TEST_OPTIONS_FILE", optionsFile)
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	adapter := New(Config{ID: "opencode", Name: "OpenCode", Command: command, DocsURL: "test", Args: []string{"--pure", "acp"}, SessionOptions: true})
	sess, err := adapter.Start(context.Background(), agentapi.StartRequest{WorkDir: root, Prompt: "approval-test", ResumeRef: "existing-session", Model: "new/model", Reasoning: "high"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = sess.Stop(ctx)
	})
	if ref := sess.Process(); ref.PID != 0 || ref.ID != "" {
		t.Fatalf("ACP SDK process identity should not be guessed: %+v", ref)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	seenRef, seenQuestion, sawTurnEnd := false, false, false
	for !sawTurnEnd {
		select {
		case ev := <-sess.Events():
			switch ev.Kind {
			case agentapi.KindSessionRef:
				seenRef = ev.SessionRef == "existing-session"
			case agentapi.KindQuestion:
				if ev.Question == nil || ev.Question.Kind != domain.QuestionApproval {
					t.Fatalf("approval question = %+v", ev)
				}
				seenQuestion = true
				if err := sess.Respond(ctx, ev.Question.Ref, domain.AnswerAllow); err != nil {
					t.Fatal(err)
				}
			case agentapi.KindTurnEnd:
				sawTurnEnd = true
			}
		case <-ctx.Done():
			t.Fatal("ACP turn did not finish")
		}
	}
	if !seenRef || !seenQuestion {
		t.Fatalf("session ref=%v approval=%v", seenRef, seenQuestion)
	}
	if got, err := os.ReadFile(filepath.Join(root, "from-acp.txt")); err != nil || string(got) != "approved" {
		t.Fatalf("approved write = %q, %v", got, err)
	}
	if got, err := os.ReadFile(optionsFile); err != nil || !strings.Contains(string(got), "model=new/model") || !strings.Contains(string(got), "effort=high") {
		t.Fatalf("selected session options = %q, %v", got, err)
	}
	if err := sess.Send(ctx, "continue"); err != nil {
		t.Fatal(err)
	}
	var streamed strings.Builder
	for {
		select {
		case ev := <-sess.Events():
			if ev.Kind == agentapi.KindOutput && ev.Stream == domain.StreamAssistant {
				streamed.WriteString(ev.Text)
			}
			if ev.Kind == agentapi.KindTurnEnd {
				if streamed.String() != "Hello ACP" {
					t.Fatalf("streamed reply = %q", streamed.String())
				}
				goto streamedTurnDone
			}
		case <-ctx.Done():
			t.Fatal("continued turn did not finish")
		}
	}
streamedTurnDone:
	if err := sess.Send(ctx, "wait-for-cancel"); err != nil {
		t.Fatal(err)
	}
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer stopCancel()
	if err := sess.Stop(stopCtx); err != nil {
		// Stop reports how the agent exited. A fake that is told to stop mid-prompt may leave with a non-zero status,
		// depending on which of the cancel and the close it sees first; what is being tested is that it stopped in time.
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			t.Fatalf("stop ACP process tree: %v", err)
		}
	}
	select {
	case <-stopCtx.Done():
		t.Fatal("ACP process did not stop after cancellation")
	default:
	}
}

func TestDiscoveryUsesAProtocolSessionWithoutPromptsOrExecutionCapabilities(t *testing.T) {
	command := filepath.Join(t.TempDir(), "agent")
	build := exec.Command("go", "build", "-o", command, "./testdata/fake")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, output)
	}
	caps := filepath.Join(t.TempDir(), "caps")
	t.Setenv("ACP_TEST_CAPS_FILE", caps)
	t.Setenv("ACP_TEST_OPTIONS_FILE", filepath.Join(t.TempDir(), "options"))
	adapter := New(Config{ID: "catalogue-agent", Name: "Catalogue agent", Command: command, SessionOptions: true, DiscoverOptions: true})
	options := adapter.Options(context.Background())
	if len(options.Models) != 2 || options.Models[1].ID != "new/model" || !options.Models[1].ReasoningKnown || len(options.Models[1].Reasoning) != 1 || options.Models[1].Reasoning[0] != "high" || options.ModelsSource != domain.OptionsFromAgent || !options.HasReasoning("high") {
		t.Fatalf("discovery: %+v", options)
	}
	data, err := os.ReadFile(caps)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"terminal":true`) || strings.Contains(string(data), `"readTextFile":true`) || strings.Contains(string(data), `"writeTextFile":true`) {
		t.Fatalf("probe advertised execution: %s", data)
	}
}
