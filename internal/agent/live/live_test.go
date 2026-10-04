// Package live holds tests that run the real Claude Code and Codex CLIs. They
// cost a little money and need the CLIs signed in, so they only run when
// DEVBOARD_LIVE_AGENTS is set:
//
//	DEVBOARD_LIVE_AGENTS=1 go test ./internal/agent/live -v -count=1
package live

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"devboard/internal/agent"
	"devboard/internal/agent/claude"
	"devboard/internal/agent/codex"
	"devboard/internal/domain"
)

func requireLive(t *testing.T) {
	t.Helper()
	if os.Getenv("DEVBOARD_LIVE_AGENTS") == "" {
		t.Skip("set DEVBOARD_LIVE_AGENTS=1 to run the real agents")
	}
}

func adapters() map[string]agent.Adapter {
	return map[string]agent.Adapter{
		"claude": claude.New(claude.Config{PermissionMode: "manual"}),
		// DEVBOARD_LIVE_CODEX_MODEL is for machines whose Codex default model the
		// account cannot use.
		"codex": codex.New(codex.Config{ApprovalPolicy: "untrusted", Model: os.Getenv("DEVBOARD_LIVE_CODEX_MODEL")}),
	}
}

// drive answers every question with answer and returns everything said until the turn ends.
func drive(t *testing.T, s agent.Session, answer string) (said []string, asked []agent.Question) {
	t.Helper()
	said, asked, _ = driveRef(t, s, answer)
	return said, asked
}

func driveRef(t *testing.T, s agent.Session, answer string) (said []string, asked []agent.Question, ref string) {
	t.Helper()
	timeout := time.After(3 * time.Minute)
	for {
		select {
		case ev, ok := <-s.Events():
			if !ok {
				t.Fatalf("session ended mid-turn; said so far: %q", said)
			}
			switch ev.Kind {
			case agent.KindOutput:
				t.Logf("[%s] %s", ev.Stream, ev.Text)
				if ev.Stream == domain.StreamAssistant {
					said = append(said, ev.Text)
				}
			case agent.KindQuestion:
				t.Logf("[question %s] %s %v", ev.Question.Kind, ev.Question.Prompt, ev.Question.Options)
				asked = append(asked, *ev.Question)
				if err := s.Respond(context.Background(), ev.Question.Ref, answer); err != nil {
					t.Fatal(err)
				}
			case agent.KindSessionRef:
				t.Logf("[session] %s", ev.SessionRef)
				ref = ev.SessionRef
			case agent.KindTurnEnd:
				return said, asked, ref
			}
		case <-timeout:
			t.Fatal("timed out")
		}
	}
}

func TestLiveRoundTrip(t *testing.T) {
	requireLive(t)
	for name, a := range adapters() {
		t.Run(name, func(t *testing.T) {
			info := a.Detect(context.Background())
			t.Logf("detect: %+v", info)
			if !info.Available {
				t.Skipf("%s is not available: %s", name, info.Detail)
			}
			dir, _ := filepath.EvalSymlinks(t.TempDir())
			s, err := a.Start(context.Background(), agent.StartRequest{WorkDir: dir, Prompt: "Reply with exactly the single word: pong. Do not use any tools."})
			if err != nil {
				t.Fatal(err)
			}
			said, _ := drive(t, s, "Allow")
			if !strings.Contains(strings.ToLower(strings.Join(said, " ")), "pong") {
				t.Fatalf("assistant said %q", said)
			}

			// The session is interactive: a second message continues the conversation.
			if err := s.Send(context.Background(), "Now reply with exactly the single word: ping."); err != nil {
				t.Fatal(err)
			}
			said, _ = drive(t, s, "Allow")
			if !strings.Contains(strings.ToLower(strings.Join(said, " ")), "ping") {
				t.Fatalf("second turn: assistant said %q", said)
			}

			if err := s.Close(context.Background()); err != nil {
				t.Fatal(err)
			}
			done := make(chan agent.Result, 1)
			go func() {
				for range s.Events() {
				}
				done <- s.Wait()
			}()
			select {
			case res := <-done:
				t.Logf("result: %+v", res)
				if res.State != domain.RunCompleted {
					t.Fatalf("result = %+v; a graceful close should complete", res)
				}
			case <-time.After(30 * time.Second):
				_ = s.Stop(context.Background())
				t.Fatal("the agent did not exit after its input was closed")
			}
		})
	}
}

func TestLiveApprovalAndResume(t *testing.T) {
	requireLive(t)
	for name, a := range adapters() {
		t.Run(name, func(t *testing.T) {
			if !a.Detect(context.Background()).Available {
				t.Skip("not available")
			}
			dir, _ := filepath.EvalSymlinks(t.TempDir())
			s, err := a.Start(context.Background(), agent.StartRequest{
				WorkDir: dir,
				Prompt:  "Create a file named note.txt in the current directory containing the single word: saved-by-devboard. Then reply with exactly: done",
			})
			if err != nil {
				t.Fatal(err)
			}
			said, asked, ref := driveRef(t, s, "Allow")
			t.Logf("asked %d question(s); session ref %q", len(asked), ref)
			if b, err := os.ReadFile(filepath.Join(dir, "note.txt")); err != nil || !strings.Contains(string(b), "saved-by-devboard") {
				t.Fatalf("the file was not created after approving: %v %q (said %q)", err, b, said)
			}
			if ref == "" {
				t.Fatal("no session ref was announced")
			}
			if err := s.Close(context.Background()); err != nil {
				t.Fatal(err)
			}
			for range s.Events() {
			}
			if res := s.Wait(); res.State != domain.RunCompleted {
				t.Fatalf("result = %+v", res)
			}

			// A new process continues the same conversation.
			s2, err := a.Start(context.Background(), agent.StartRequest{
				WorkDir: dir, ResumeRef: ref,
				Prompt: "What word did you put in the file? Reply with that word only. Do not use any tools.",
			})
			if err != nil {
				t.Fatal(err)
			}
			said, _, _ = driveRef(t, s2, "Allow")
			if !strings.Contains(strings.Join(said, " "), "saved-by-devboard") {
				t.Fatalf("the resumed session does not remember the conversation: %q", said)
			}
			_ = s2.Close(context.Background())
			for range s2.Events() {
			}
			s2.Wait()
		})
	}
}
