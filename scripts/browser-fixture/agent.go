package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"devboard/internal/agent"
	"devboard/internal/domain"
)

type fixtureAdapter struct{}

func (fixtureAdapter) ID() string { return "fixture" }
func (fixtureAdapter) Detect(context.Context) domain.Agent {
	return domain.Agent{ID: "fixture", Name: "Disposable process fixture", Available: true, Installed: true}
}
func (fixtureAdapter) Start(_ context.Context, req agent.StartRequest) (agent.Session, error) {
	bin, err := os.Executable()
	if err != nil {
		return nil, err
	}
	s := &fixtureSession{Base: agent.NewBase(agent.ProcSpec{Command: bin, Args: []string{"--agent-fixture"}, Dir: req.WorkDir, Env: agent.SanitizedEnv(os.Environ())})}
	s.OnLine = func(line []byte, _ bool) {
		switch string(line) {
		case "turn":
			s.Emit(agent.Event{Kind: agent.KindTurnEnd})
		case "question":
			s.Emit(agent.Event{Kind: agent.KindQuestion, Question: &agent.Question{Ref: "fixture-question", Kind: domain.QuestionDecision, Prompt: "Commit the fixture work?", Options: []string{"Yes", "No"}}})
		default:
			s.Emit(agent.Event{Kind: agent.KindOutput, Stream: domain.StreamAssistant, Text: string(line)})
		}
	}
	if err := s.Launch(); err != nil {
		return nil, err
	}
	return s, nil
}

type fixtureSession struct{ *agent.Base }

func (s *fixtureSession) Send(_ context.Context, text string) error { return s.WriteLine([]byte(text)) }
func (s *fixtureSession) Respond(_ context.Context, _, answer string) error {
	return s.WriteLine([]byte("answer:" + answer))
}
func (s *fixtureSession) Close(context.Context) error { return s.CloseInput() }

func runFixtureAgent() error {
	fmt.Println("Fixture process ready")
	fmt.Println("turn")
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		switch scanner.Text() {
		case "fail":
			return fmt.Errorf("intentional fixture agent failure")
		case "ask":
			fmt.Println("question")
			continue
		case "commit", "answer:Yes":
			if err := os.WriteFile("executed.txt", []byte("Committed by the disposable runner agent\n"), 0600); err != nil {
				return err
			}
			for _, args := range [][]string{{"add", "executed.txt"}, {"-c", "user.name=Fixture agent", "-c", "user.email=fixture@example.test", "-c", "core.hooksPath=/dev/null", "commit", "-m", "Actual runner work for Team review"}} {
				out, err := exec.Command("git", args...).CombinedOutput()
				if err != nil {
					return fmt.Errorf("fixture Git: %w: %s", err, strings.TrimSpace(string(out)))
				}
			}
			fmt.Println("Committed fixture work")
		}
		fmt.Println("turn")
	}
	return scanner.Err()
}
