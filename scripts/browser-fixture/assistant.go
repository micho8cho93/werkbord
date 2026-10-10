package main

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"devboard/internal/appops"
	"devboard/internal/assistant"
	"devboard/internal/assistant/provider"
	"devboard/internal/runner"
	"devboard/internal/service"
	"devboard/internal/store"
)

// The assistant in the browser fixture. It is the real engine, the real operations and the real API, with the real command lines
// replaced by a scripted "provider" that answers by what the message says, so the screen can be driven end to end without a Claude
// or Codex account. WERKBORD_BROWSER_ASSISTANT picks the situation:
//
//	1     two providers, both signed in
//	none  two providers, neither usable (the "sign in first" screen)
//	off   no assistant at all (the "turned off" screen)
//
// Never built into the product: this is a script's main package.
func newFixtureAssistant(mode string, dir string, db store.Store, deps service.Deps, projects *service.Projects, tasks *service.Tasks, runs *service.Runs, mgr *runner.Manager) *assistant.Engine {
	if mode == "" || mode == "off" {
		return nil
	}
	reg := provider.NewRegistry()
	for _, p := range []*scripted{newScripted("claude-code", "Claude Code", mode == "1"), newScripted("codex", "Codex", mode == "1")} {
		if err := reg.Register(p); err != nil {
			panic(err)
		}
	}
	backend := appops.NewBackend(appops.Services{Projects: projects, Tasks: tasks, Labels: &service.Labels{Deps: deps}, Runs: runs, Control: &service.ControlCenter{Deps: deps}, Answer: mgr.Answer})
	ops := appops.New(appops.Config{Backend: backend, Store: db})
	eng := assistant.New(assistant.Config{Providers: reg, Ops: ops, Store: db, WorkDir: filepath.Join(dir, "assistant"), AttachmentRetention: 30 * time.Minute})
	if err := eng.Recover(context.Background()); err != nil {
		panic(err)
	}
	eng.Start()
	return eng
}

type scripted struct {
	id, name string
	ready    bool
	mu       sync.Mutex
	n        int
}

func newScripted(id, name string, ready bool) *scripted {
	return &scripted{id: id, name: name, ready: ready}
}

func (s *scripted) ID() string { return s.id }

func (s *scripted) Detect(context.Context) provider.Info {
	yes, no := true, false
	info := provider.Info{ID: s.id, Name: s.name, Installed: s.ready, SignedIn: &yes, Available: s.ready, Version: "fixture", Auth: "fixture account",
		Capabilities: provider.Capabilities{Streaming: provider.StreamTokens, Resume: true, Cancel: true, ModelChoice: true, ReasoningChoice: true, NativeTools: "none", Voice: "none"}}
	if !s.ready {
		info.SignedIn = &no
		info.Installed = s.id == "claude-code" // one is installed but signed out; the other is not installed
		info.Detail = map[bool]string{true: "not signed in", false: `"codex" was not found on PATH`}[info.Installed]
		info.Guidance = map[bool]string{true: "Run `claude auth login` in a terminal and follow the prompts.", false: "Install Codex, then run `codex login`."}[info.Installed]
	}
	return info
}

func (s *scripted) Models(context.Context) provider.Models {
	if s.id == "codex" {
		return provider.Models{ProviderID: s.id, Source: "provider", Models: []provider.Model{
			{ID: "gpt-fixture", Name: "GPT Fixture", Default: true, Reasoning: []string{"low", "medium", "high"}},
			{ID: "gpt-small", Name: "GPT Small", Reasoning: []string{"low"}},
		}}
	}
	return provider.Models{ProviderID: s.id, Source: "builtin", Custom: true, Reasoning: []string{"low", "medium", "high"}, Models: []provider.Model{
		{ID: "sonnet", Name: "Sonnet"}, {ID: "opus", Name: "Opus"},
	}}
}

// Run answers by what the message says.
func (s *scripted) Run(ctx context.Context, t provider.Turn, emit func(provider.Event)) (provider.Result, error) {
	s.mu.Lock()
	ref := t.Ref
	if ref == "" {
		s.n++
		ref = fmt.Sprintf("%s-%d", s.id, s.n)
	}
	s.mu.Unlock()
	emit(provider.Event{Kind: provider.EventRef, Ref: ref})
	say := func(chunks ...string) (provider.Result, error) {
		var all strings.Builder
		for _, c := range chunks {
			select {
			case <-time.After(40 * time.Millisecond):
			case <-ctx.Done():
				return provider.Result{}, ctx.Err()
			}
			all.WriteString(c)
			emit(provider.Event{Kind: provider.EventText, Text: c})
		}
		return provider.Result{Text: all.String(), Ref: ref, Usage: provider.Usage{InputTokens: 10, OutputTokens: 5}}, nil
	}
	p := t.Prompt
	low := strings.ToLower(p)
	afterResults := strings.Contains(p, "<werkbord-results>")
	switch {
	case strings.Contains(low, "portfolio tickets") && !afterResults:
		return say("Looking up the portfolio project.\n```werkbord-call\n{\"id\":\"p1\",\"name\":\"list_projects\",\"arguments\":{}}\n```")
	case afterResults && strings.Contains(p, "workspace:") && strings.Contains(p, `name="list_projects"`):
		pid := regexp.MustCompile(`workspace:[A-Za-z0-9:_-]+/[A-Za-z0-9_-]+`).FindString(p)
		var calls strings.Builder
		for i, title := range []string{"Extract LinkedIn and CV information", "Extract coding projects from GitHub", "Create a website design system"} {
			fmt.Fprintf(&calls, "```werkbord-call\n{\"id\":\"c%d\",\"name\":\"create_ticket\",\"arguments\":{\"projectId\":%q,\"title\":%q}}\n```\n", i+1, pid, title)
		}
		return say(calls.String())
	case strings.Contains(low, "fail model") && t.Model == "":
		return provider.Result{}, provider.Errorf(provider.KindModelUnavailable, false, "The default model is not available to your account.")
	case strings.HasPrefix(low, "hang on"):
		emit(provider.Event{Kind: provider.EventText, Text: "Working on it"})
		<-ctx.Done()
		return provider.Result{}, ctx.Err()
	case strings.Contains(low, "slow"):
		words := strings.Fields("One two three four five six seven eight nine ten eleven twelve")
		chunks := make([]string, len(words))
		for i, w := range words {
			chunks[i] = w + " "
			if i == len(words)-1 {
				chunks[i] = w + "."
			}
		}
		for i := range chunks {
			select {
			case <-time.After(110 * time.Millisecond):
			case <-ctx.Done():
				return provider.Result{}, ctx.Err()
			}
			emit(provider.Event{Kind: provider.EventText, Text: chunks[i]})
		}
		return provider.Result{Text: strings.Join(chunks, ""), Ref: ref}, nil
	case strings.Contains(low, "ticket") && !afterResults:
		projectID := firstProject(p)
		return say("Preparing that.\n", "```werkbord-call\n"+fmt.Sprintf(`{"id":"c1","name":"create_ticket","arguments":{"projectId":%q,"title":"From the assistant"}}`, projectID)+"\n```\n")
	case afterResults && strings.Contains(p, "pending_confirmation"):
		return say("I prepared it. ", "Confirm it when you are ready.")
	case strings.Contains(low, "blocked") && !afterResults:
		time.Sleep(500 * time.Millisecond)
		return say("```werkbord-call\n{\"id\":\"c1\",\"name\":\"list_blockers\",\"arguments\":{}}\n```\n")
	case afterResults:
		time.Sleep(300 * time.Millisecond)
		return say("Nothing is blocked right now.")
	case strings.Contains(low, "attachment"):
		i := strings.Index(p, `<werkbord-attachment name="`)
		if i < 0 {
			return say("There was no file with that.")
		}
		rest := p[i:]
		name := rest[len(`<werkbord-attachment name="`):]
		name = name[:strings.Index(name, `"`)]
		body := rest[strings.Index(rest, ">\n")+2:]
		first := strings.SplitN(body, "\n", 2)[0]
		return say("The file ", name, " begins: ", first)
	case strings.Contains(low, "markdown"):
		return say("Here is **bold**, *italic* and `code`:\n\n- one\n- two\n\n```\nx = 1\n```\n\n<img src=x onerror=\"window.__pwned=1\"> [link](javascript:window.__pwned=1)")
	}
	line := strings.TrimSpace(strings.SplitN(strings.TrimSpace(notices.ReplaceAllString(p, "")), "\n", 2)[0])
	if len(line) > 60 {
		line = line[:60]
	}
	return say("Hello! ", "You said: ", line)
}

// notices are what the engine says to the provider ahead of the person's words (a conversation started afresh, what became of a change).
var notices = regexp.MustCompile(`(?s)<werkbord-notice>.*?</werkbord-notice>\s*`)

// firstProject finds a project id in a prompt, from the results of an earlier call; the fixture's script gives it in the message.
func firstProject(prompt string) string {
	if i := strings.Index(prompt, "prj_"); i >= 0 {
		j := i
		for j < len(prompt) && (prompt[j] == '_' || prompt[j] >= '0' && prompt[j] <= '9' || prompt[j] >= 'a' && prompt[j] <= 'z' || prompt[j] >= 'A' && prompt[j] <= 'Z') {
			j++
		}
		return prompt[i:j]
	}
	return ""
}
