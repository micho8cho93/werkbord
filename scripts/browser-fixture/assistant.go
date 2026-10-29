package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"werkbord/internal/appops"
	"werkbord/internal/assistant"
	"werkbord/internal/assistant/provider"
	"werkbord/internal/domain"
	"werkbord/internal/runner"
	"werkbord/internal/service"
	"werkbord/internal/store"
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
func newFixtureAssistant(mode string, dir string, db store.Store, deps service.Deps, projects *service.Projects, tasks *service.Tasks, runs *service.Runs, mgr *runner.Manager, knowledge *service.Knowledge, recommender *service.Recommender) *assistant.Engine {
	if mode == "" || mode == "off" {
		return nil
	}
	ready := mode == "1" || mode == "readonly" || mode == "short-ttl"
	reg := provider.NewRegistry()
	for _, p := range []*scripted{newScripted("claude-code", "Claude Code", ready), newScripted("codex", "Codex", ready)} {
		if err := reg.Register(p); err != nil {
			panic(err)
		}
	}
	backend := appops.NewBackend(appops.Services{Knowledge: knowledge, Recommender: recommender, Projects: projects, Tasks: tasks, Labels: &service.Labels{Deps: deps}, Runs: runs, Control: &service.ControlCenter{Deps: deps}, Answer: mgr.Answer})
	confirmTTL := time.Duration(0)
	if millis, err := strconv.Atoi(os.Getenv("WERKBORD_BROWSER_ASSISTANT_TTL_MS")); err == nil && millis > 0 {
		confirmTTL = time.Duration(millis) * time.Millisecond
	}
	ops := appops.New(appops.Config{Backend: backend, Store: db, ConfirmTTL: confirmTTL})
	var grants []appops.Permission
	if mode == "readonly" {
		grants = []appops.Permission{appops.PermProjectsRead, appops.PermTicketsRead, appops.PermAnalyticsRead, appops.PermScheduleRead, appops.PermRunsRead, appops.PermQuestionsRead}
	}
	eng := assistant.New(assistant.Config{Selection: func(_ context.Context, in domain.SelectionRequest) (domain.SelectionDecision, error) {
		return domain.SelectionDecision{Agent: "claude-code", Model: "sonnet", Reasoning: "high", Mode: in.Mode, Method: "local_rules", Reason: "Disposable browser fixture selection"}, nil
	}, Providers: reg, Ops: ops, Store: db, WorkDir: filepath.Join(dir, "assistant"), AttachmentRetention: 30 * time.Minute, Grants: grants})
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
	intent   map[string]string
}

func newScripted(id, name string, ready bool) *scripted {
	return &scripted{id: id, name: name, ready: ready, intent: map[string]string{}}
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
	streamFixture := func(text string) (provider.Result, error) {
		all := text
		for len(text) > 0 {
			n := 18
			if len(text) < n {
				n = len(text)
			}
			select {
			case <-time.After(2 * time.Millisecond):
			case <-ctx.Done():
				return provider.Result{}, ctx.Err()
			}
			emit(provider.Event{Kind: provider.EventText, Text: text[:n]})
			text = text[n:]
		}
		return provider.Result{Text: all, Ref: ref, Usage: provider.Usage{InputTokens: 10, OutputTokens: 5}}, nil
	}
	p := t.Prompt
	low := strings.ToLower(p)
	afterResults := strings.Contains(p, "<werkbord-results>")
	s.mu.Lock()
	if !afterResults {
		switch {
		case strings.Contains(low, "single portfolio ticket"):
			s.intent[ref] = "single"
		case strings.Contains(low, "portfolio tickets"):
			s.intent[ref] = "batch"
		}
	}
	intent := s.intent[ref]
	s.mu.Unlock()
	switch {
	case strings.Contains(low, "memory evidence") && !afterResults:
		return say("```werkbord-call\n" + fmt.Sprintf(`{"id":"memory","name":"retrieve_memory","arguments":{"projectId":%q,"requestType":"architecture"}}`, firstProject(p)) + "\n```")
	case afterResults && strings.Contains(p, `name="retrieve_memory"`):
		return say("The documented decision favours transactions; see the memory source below. Suggested next step: review the current board.")
	case strings.Contains(low, "what should i do next") && !afterResults:
		return say("```werkbord-call\n" + fmt.Sprintf(`{"id":"rec","name":"recommend_next_actions","arguments":{"projectId":%q}}`, firstProject(p)) + "\n```")
	case afterResults && strings.Contains(p, `name="recommend_next_actions"`):
		return say("The first card matters most; each one opens a screen where you decide.")
	case strings.Contains(low, "failed board action") && !afterResults:
		return say("I'll read that board.\n```werkbord-call\n{\"id\":\"c1\",\"name\":\"list_tickets\",\"arguments\":{\"projectId\":\"prj_missing\"}}\n```")
	case afterResults && strings.Contains(p, `status="error"`):
		return say("I could not read that board.")
	case strings.Contains(low, "single portfolio ticket") && !afterResults:
		return say("Looking up the portfolio project.\n```werkbord-call\n{\"id\":\"p1\",\"name\":\"list_projects\",\"arguments\":{}}\n```")
	case afterResults && intent == "single" && strings.Contains(p, "workspace:") && strings.Contains(p, `name="list_projects"`):
		pid := regexp.MustCompile(`workspace:[A-Za-z0-9:_-]+/[A-Za-z0-9_-]+`).FindString(p)
		return say("```werkbord-call\n" + fmt.Sprintf(`{"id":"single","name":"create_ticket","arguments":{"projectId":%q,"title":"A single approved ticket"}}`, pid) + "\n```")
	case strings.Contains(low, "portfolio tickets") && !afterResults:
		return say("Looking up the portfolio project.\n```werkbord-call\n{\"id\":\"p1\",\"name\":\"list_projects\",\"arguments\":{}}\n```")
	case afterResults && (intent == "batch" || intent == "") && strings.Contains(p, "workspace:") && strings.Contains(p, `name="list_projects"`):
		pid := regexp.MustCompile(`workspace:[A-Za-z0-9:_-]+/[A-Za-z0-9_-]+`).FindString(p)
		var calls strings.Builder
		for i, title := range []string{"Extract LinkedIn and CV information", "Extract coding projects from GitHub", "Create a website design system"} {
			fmt.Fprintf(&calls, "```werkbord-call\n{\"id\":\"c%d\",\"name\":\"create_ticket\",\"arguments\":{\"projectId\":%q,\"title\":%q}}\n```\n", i+1, pid, title)
		}
		return say(calls.String())
	case strings.Contains(low, "fail model") && t.Model == "":
		return provider.Result{}, provider.Errorf(provider.KindModelUnavailable, false, "The default model is not available to your account.")
	case strings.Contains(low, "openui form demo"):
		projectID := firstProject(p)
		if projectID == "" {
			return say("I need a project identifier from the board before I can show that form.")
		}
		return say("Review this draft, then choose what to ask me to do.\n```openui-lang\nroot = Stack([plan])\n",
			"plan = Form(\"sprint-plan\", \"Draft sprint tasks\", \"Review the task, project and date before requesting proposals.\", [project, title, start, includeDocs, reference, create])\n",
			fmt.Sprintf("project = SelectField(\"projectId\", \"Project\", [{value: %q, label: \"Assistant project\"}], %q, true)\n", projectID, projectID),
			"title = TextField(\"title\", \"Task title\", \"From the plan\", \"A short title\", true, 120)\n",
			"start = DateField(\"plannedStart\", \"First planned day\", \"2026-10-19\", false)\n",
			"includeDocs = CheckboxField(\"includeDocs\", \"Include documentation tasks\", false)\n",
			"reference = TextField(\"referenceUrl\", \"Reference URL\", \"\", \"Optional external reference\", false, 500)\n",
			"create = ConversationAction(\"Create tasks from this plan\")\n```\n")
	case strings.Contains(low, "openui dashboard"):
		return say(
			"Here is the sprint view.\n",
			"```openui-lang\nroot = Stack([title, metrics, tasks, details, bars, comparison, trend])\n",
			"title = Heading(\"Sprint snapshot\", 1)\nmetrics = Grid([open, done], 2, \"small\")\n",
			"open = Metric(\"Open tasks\", \"4\", \"Across active projects\", \"2026-10-12T09:00:00Z\")\ndone = Metric(\"Done this week\", \"8\", \"Recorded completions\", \"2026-10-12T09:00:00Z\")\n",
			"tasks = DataTable(\"Next tasks\", [\"Task\", \"State\", \"Owner\", \"Next step\"], [[\"Review API\", \"Review\", \"Mara\", \"Check limits\"], [\"Update docs\", \"Doing\", \"Ivo\", \"Add examples\"]], \"2026-10-12T09:00:00Z\", true, [{column: \"State\", label: \"Filter by state\", options: [\"Review\", \"Doing\"]}])\n",
			"details = Disclosure(\"How these counts work\", [explanation, untrusted])\n",
			"bars = BarChart(\"Tasks by state\", [\"Review\", \"Doing\"], [1, 3], \"tasks\", \"Current board counts\", \"2026-10-12T09:00:00Z\")\n",
			"comparison = ComparisonChart(\"Agent run attempts\", [\"Attempts\", \"Retries\"], \"Claude Code\", [5, 1], \"Codex\", [3, 2], \"runs\", \"Run outcomes, not task completion attribution\", \"2026-10-12T09:00:00Z\")\n",
			"trend = LineChart(\"Daily completions\", [\"2026-10-10\", \"2026-10-11\", \"2026-10-12\"], [1, 3, 2], \"tasks\", \"Historical task state changes\", \"2026-10-12T09:00:00Z\")\n",
			"explanation = Text(\"Counts are from the current board.\")\nuntrusted = Text(\"<img src=x onerror=window.__pwned=1>\")\n```\n",
			"The values are from the current board.",
		)
	case strings.Contains(low, "stream benchmark short"):
		return streamFixture("A short controlled stream checks that each update reaches the browser and appears in the conversation promptly.")
	case strings.Contains(low, "stream benchmark long"):
		return streamFixture(strings.Repeat("A long controlled reply keeps earlier text stable while the next words arrive. ", 42))
	case strings.Contains(low, "stream benchmark mixed"):
		detail := strings.TrimSpace(strings.Repeat("Generated details remain readable as this interface streams. ", 12))
		return streamFixture("The prose and generated interface arrive together.\n\n```openui-lang\nroot = Stack([title, detail])\ntitle = Heading(\"Streaming result\")\ndetail = Text(\"" + detail + "\")\n```\n")
	case strings.Contains(low, "malformed openui"):
		return say("```openui-lang\nroot = Heading()\n```")
	case strings.Contains(low, "incomplete openui"):
		return say("```openui-lang\nroot = Stack([heading])\nheading = Heading(\"Partial interface\")")
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
	case strings.Contains(low, "create tasks from this plan") && !afterResults:
		values := formValues(p)
		projectID, _ := values["projectId"].(string)
		title, _ := values["title"].(string)
		plannedStart, _ := values["plannedStart"].(string)
		if title == "" {
			title = "From the plan"
		}
		args := map[string]any{"projectId": projectID, "title": title}
		if plannedStart != "" {
			args["plannedStart"] = plannedStart
		}
		encoded, _ := json.Marshal(args)
		return say("Preparing the task.\n```werkbord-call\n" + fmt.Sprintf(`{"id":"plan-1","name":"create_ticket","arguments":%s}`, encoded) + "\n```\n")
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

func formValues(prompt string) map[string]any {
	match := regexp.MustCompile(`(?m)^Form values: ([^\r\n]+)$`).FindStringSubmatch(prompt)
	if len(match) != 2 {
		return nil
	}
	values := map[string]any{}
	if err := json.Unmarshal([]byte(match[1]), &values); err != nil {
		return nil
	}
	return values
}
