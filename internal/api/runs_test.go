package api

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"devboard/internal/agent"
	"devboard/internal/agent/fake"
	"devboard/internal/domain"
	"devboard/internal/gitrepo"
	"devboard/internal/runner"
	"devboard/internal/service"
)

// runsServer is a server with a fake agent and a repository that has a commit.
type runsServer struct {
	url     string
	adapter *fake.Adapter
	project service.ProjectDetail
}

func newRunsServer(t *testing.T) *runsServer {
	t.Helper()
	rs := &runsServer{adapter: &fake.Adapter{}}
	ts := newTestServer(t, func(o *Options) {
		root, _ := filepath.EvalSymlinks(t.TempDir())
		reg := agent.NewRegistry()
		if err := reg.Register(rs.adapter); err != nil {
			t.Fatal(err)
		}
		deps := o.Runs.Deps
		mgr := runner.New(runner.Options{
			Runs: o.Runs, Tasks: o.Tasks, Projects: o.Projects, Worktrees: &service.Worktrees{Deps: deps, Root: root},
			Git: &gitrepo.CLI{}, Agents: reg, WorktreeRoot: root, FlushInterval: 10 * time.Millisecond, FinishTimeout: time.Second,
		})
		t.Cleanup(func() { _ = mgr.Shutdown(context.Background()) })
		o.Runner, o.Agents, o.Worktrees = mgr, reg, &service.Worktrees{Deps: deps, Root: root}
	})
	rs.url = ts.URL

	repo := gitRepo(t)
	env := append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e.com", "GIT_CONFIG_GLOBAL=/dev/null")
	for _, args := range [][]string{{"commit", "-q", "--allow-empty", "-m", "init"}} {
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		cmd.Env = env
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	if code := do(t, "POST", rs.url+"/api/projects", `{"path":`+jsonString(repo)+`}`, &rs.project); code != 201 {
		t.Fatalf("register: %d", code)
	}
	return rs
}

func (rs *runsServer) task(t *testing.T, title string) domain.Task {
	t.Helper()
	var task domain.Task
	if code := do(t, "POST", rs.url+"/api/projects/"+rs.project.ID+"/tasks", `{"title":`+jsonString(title)+`,"description":"details"}`, &task); code != 201 {
		t.Fatalf("create task: %d", code)
	}
	return task
}

func (rs *runsServer) start(t *testing.T, task domain.Task) domain.Run {
	t.Helper()
	var run domain.Run
	if code := do(t, "POST", rs.url+"/api/projects/"+rs.project.ID+"/tasks/"+task.ID+"/runs", `{"agentId":"fake"}`, &run); code != 201 {
		t.Fatalf("start run: %d", code)
	}
	return run
}

func (rs *runsServer) getRun(t *testing.T, id string) domain.Run {
	t.Helper()
	var run domain.Run
	if code := do(t, "GET", rs.url+"/api/projects/"+rs.project.ID+"/runs/"+id, "", &run); code != 200 {
		t.Fatalf("get run: %d", code)
	}
	return run
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

type apiError struct {
	Error struct{ Code, Message string }
}

func TestRunLifecycleOverHTTP(t *testing.T) {
	rs := newRunsServer(t)
	task := rs.task(t, "Add a login page")

	// The agent shows up as available.
	var agents struct{ Agents []domain.Agent }
	if code := do(t, "GET", rs.url+"/api/agents", "", &agents); code != 200 || len(agents.Agents) != 1 || !agents.Agents[0].Available {
		t.Fatalf("agents = %d %+v", code, agents)
	}

	run := rs.start(t, task)
	if run.State != domain.RunRunning || run.TaskID != task.ID || run.AgentID != "fake" {
		t.Fatalf("run = %+v", run)
	}
	var tasks struct{ Tasks []domain.Task }
	do(t, "GET", rs.url+"/api/projects/"+rs.project.ID+"/tasks", "", &tasks)
	if tasks.Tasks[0].State != domain.TaskDoing {
		t.Fatalf("task = %+v", tasks.Tasks[0])
	}

	s := rs.adapter.Last()
	s.Assistant("Looking at the router.")
	s.Say(domain.StreamTool, "Read src/router.ts")
	s.Ask("q1", "Which auth provider?", "OAuth", "Password")
	waitFor(t, "the run to wait", func() bool { return rs.getRun(t, run.ID).Waiting == domain.WaitQuestion })

	// Board view: the latest run of each task, with what the card needs.
	var board struct{ Runs []domain.Run }
	do(t, "GET", rs.url+"/api/projects/"+rs.project.ID+"/runs", "", &board)
	if len(board.Runs) != 1 || board.Runs[0].ID != run.ID || board.Runs[0].Activity != "Read src/router.ts" || board.Runs[0].Waiting != domain.WaitQuestion {
		t.Fatalf("board runs = %+v", board.Runs)
	}
	var taskRuns struct{ Runs []domain.Run }
	if code := do(t, "GET", rs.url+"/api/projects/"+rs.project.ID+"/tasks/"+task.ID+"/runs", "", &taskRuns); code != 200 || len(taskRuns.Runs) != 1 {
		t.Fatalf("task runs = %d %+v", code, taskRuns)
	}

	// The question, and sending a message while it is open.
	var qs struct{ Questions []domain.Question }
	do(t, "GET", rs.url+"/api/projects/"+rs.project.ID+"/questions", "", &qs)
	if len(qs.Questions) != 1 || qs.Questions[0].Prompt != "Which auth provider?" || len(qs.Questions[0].Options) != 2 {
		t.Fatalf("questions = %+v", qs)
	}
	var apiErr apiError
	if code := do(t, "POST", rs.url+"/api/projects/"+rs.project.ID+"/runs/"+run.ID+"/input", `{"text":"hello"}`, &apiErr); code != 409 || apiErr.Error.Code != "conflict" {
		t.Fatalf("message while a question is open: %d %+v", code, apiErr)
	}

	var answered domain.Question
	if code := do(t, "POST", rs.url+"/api/projects/"+rs.project.ID+"/questions/"+qs.Questions[0].ID+"/answer", `{"answer":"OAuth"}`, &answered); code != 200 || answered.State != domain.QuestionAnswered || answered.Answer != "OAuth" {
		t.Fatalf("answer = %d %+v", code, answered)
	}
	if got := s.Responses(); len(got) != 1 || got[0].Answer != "OAuth" {
		t.Fatalf("agent got %+v", got)
	}
	if code := do(t, "POST", rs.url+"/api/projects/"+rs.project.ID+"/questions/"+qs.Questions[0].ID+"/answer", `{"answer":"again"}`, &apiErr); code != 409 {
		t.Fatalf("answering twice: %d", code)
	}

	// Turn ends, user follows up.
	s.TurnEnd()
	waitFor(t, "the agent to wait", func() bool { return rs.getRun(t, run.ID).Waiting == domain.WaitIdle })
	var after domain.Run
	if code := do(t, "POST", rs.url+"/api/projects/"+rs.project.ID+"/runs/"+run.ID+"/input", `{"text":"Use OAuth with Google."}`, &after); code != 200 || after.State != domain.RunRunning {
		t.Fatalf("input = %d %+v", code, after)
	}
	if got := s.Sent(); len(got) != 1 || got[0] != "Use OAuth with Google." {
		t.Fatalf("agent got %v", got)
	}

	// Finish.
	s.TurnEnd()
	waitFor(t, "the agent to wait", func() bool { return rs.getRun(t, run.ID).Waiting == domain.WaitIdle })
	var done domain.Run
	if code := do(t, "POST", rs.url+"/api/projects/"+rs.project.ID+"/runs/"+run.ID+"/finish", "", &done); code != 200 || done.State != domain.RunCompleted {
		t.Fatalf("finish = %d %+v", code, done)
	}
	if code := do(t, "POST", rs.url+"/api/projects/"+rs.project.ID+"/runs/"+run.ID+"/stop", "", &apiErr); code != 409 {
		t.Fatalf("stop after finish: %d", code)
	}
	if code := do(t, "POST", rs.url+"/api/projects/"+rs.project.ID+"/runs/"+run.ID+"/input", `{"text":"late"}`, &apiErr); code != 409 {
		t.Fatalf("input after finish: %d", code)
	}

	// Nothing is active now.
	var cc service.Overview
	do(t, "GET", rs.url+"/api/control-center", "", &cc)
	if len(cc.Runs) != 0 {
		t.Fatalf("active = %+v", cc.Runs)
	}
}

func TestWorktreeOfARunIsReadable(t *testing.T) {
	rs := newRunsServer(t)
	run := rs.start(t, rs.task(t, "Where is my work"))
	var wt domain.Worktree
	if code := do(t, "GET", rs.url+"/api/projects/"+rs.project.ID+"/worktrees/"+run.WorktreeID, "", &wt); code != 200 {
		t.Fatalf("get worktree: %d", code)
	}
	if wt.ID != run.WorktreeID || !strings.HasPrefix(wt.Branch, "devboard/where-is-my-work-") || wt.State != domain.WorktreeActive || !filepath.IsAbs(wt.Path) {
		t.Fatalf("worktree = %+v", wt)
	}
	var e apiError
	if code := do(t, "GET", rs.url+"/api/projects/"+rs.project.ID+"/worktrees/wt_missing", "", &e); code != 404 {
		t.Fatalf("unknown worktree: %d", code)
	}
}

func TestRunActivityHistoryPages(t *testing.T) {
	rs := newRunsServer(t)
	run := rs.start(t, rs.task(t, "Chatty"))
	for i := 0; i < 10; i++ {
		rs.adapter.Last().Assistant(strings.Repeat("x", i+1))
	}
	waitFor(t, "output", func() bool {
		var page struct{ Events []domain.Event }
		do(t, "GET", rs.url+"/api/projects/"+rs.project.ID+"/runs/"+run.ID+"/events?limit=1000", "", &page)
		return len(page.Events) >= 13 // run, task..., started, 10 outputs
	})

	type page struct {
		Events  []domain.Event
		HasMore bool
	}
	var newest page
	if code := do(t, "GET", rs.url+"/api/projects/"+rs.project.ID+"/runs/"+run.ID+"/events?limit=4", "", &newest); code != 200 || len(newest.Events) != 4 || !newest.HasMore {
		t.Fatalf("newest = %d %+v", code, newest)
	}
	var last domain.AgentOutput
	_ = json.Unmarshal(newest.Events[3].Payload, &last)
	if last.Text != strings.Repeat("x", 10) {
		t.Fatalf("the newest page must end with the latest event: %+v", last)
	}
	var older page
	do(t, "GET", rs.url+"/api/projects/"+rs.project.ID+"/runs/"+run.ID+"/events?limit=1000&before="+itoa(newest.Events[0].Seq), "", &older)
	if older.HasMore || len(older.Events) == 0 || older.Events[len(older.Events)-1].Seq >= newest.Events[0].Seq {
		t.Fatalf("older = %+v", older)
	}
	if len(older.Events)+len(newest.Events) < 13 {
		t.Fatalf("paging lost events: %d + %d", len(older.Events), len(newest.Events))
	}

	var apiErr apiError
	for _, q := range []string{"limit=0", "limit=abc", "limit=5000", "before=-1"} {
		if code := do(t, "GET", rs.url+"/api/projects/"+rs.project.ID+"/runs/"+run.ID+"/events?"+q, "", &apiErr); code != 400 || apiErr.Error.Code != "invalid" {
			t.Errorf("?%s: %d %+v", q, code, apiErr)
		}
	}
	if code := do(t, "GET", rs.url+"/api/projects/"+rs.project.ID+"/runs/run_missing/events", "", &apiErr); code != 404 {
		t.Errorf("unknown run: %d", code)
	}
}

func itoa(n int64) string { b, _ := json.Marshal(n); return string(b) }

func TestStartRunErrors(t *testing.T) {
	rs := newRunsServer(t)
	task := rs.task(t, "Errors")
	var e apiError
	post := func(path, body string) int { return do(t, "POST", rs.url+path, body, &e) }

	if code := post("/api/projects/"+rs.project.ID+"/tasks/"+task.ID+"/runs", `{"agentId":"gemini"}`); code != 404 || e.Error.Code != "not_found" {
		t.Errorf("unknown agent: %d %+v", code, e)
	}
	if code := post("/api/projects/"+rs.project.ID+"/tasks/"+task.ID+"/runs", `{}`); code != 400 || e.Error.Code != "invalid" {
		t.Errorf("no agent: %d %+v", code, e)
	}
	if code := post("/api/projects/"+rs.project.ID+"/tasks/tsk_missing/runs", `{"agentId":"fake"}`); code != 404 {
		t.Errorf("unknown task: %d", code)
	}
	if code := post("/api/projects/"+rs.project.ID+"/tasks/"+task.ID+"/runs", `{"agentId":"fake","surprise":1}`); code != 400 {
		t.Errorf("unknown field: %d", code)
	}
	if code := post("/api/projects/"+rs.project.ID+"/tasks/"+task.ID+"/runs", `{"agentId":"fake","resume":true}`); code != 400 {
		t.Errorf("nothing to resume: %d", code)
	}

	// An agent that cannot start: the user is told why, and nothing is running.
	rs.adapter.StartFunc = func(agent.StartRequest) error { return errors.New("claude exited immediately: unknown option") }
	if code := post("/api/projects/"+rs.project.ID+"/tasks/"+task.ID+"/runs", `{"agentId":"fake"}`); code != 502 || e.Error.Code != "agent_failed" || !strings.Contains(e.Error.Message, "unknown option") {
		t.Errorf("agent failed to start: %d %+v", code, e)
	}
	var tasks struct{ Tasks []domain.Task }
	do(t, "GET", rs.url+"/api/projects/"+rs.project.ID+"/tasks", "", &tasks)
	if tasks.Tasks[0].State != domain.TaskBacklog {
		t.Errorf("the card moved though no agent ran: %+v", tasks.Tasks[0])
	}
	rs.adapter.StartFunc = nil

	rs.start(t, task)
	if code := post("/api/projects/"+rs.project.ID+"/tasks/"+task.ID+"/runs", `{"agentId":"fake"}`); code != 409 {
		t.Errorf("second active run: %d", code)
	}

	rs.adapter.Info = domain.Agent{ID: "fake", Name: "Fake", Detail: "not signed in"}
	other := rs.task(t, "Other")
	if code := post("/api/projects/"+rs.project.ID+"/tasks/"+other.ID+"/runs", `{"agentId":"fake"}`); code != 409 || !strings.Contains(e.Error.Message, "not signed in") {
		t.Errorf("unavailable agent: %d %+v", code, e)
	}
}

func TestRunActionErrors(t *testing.T) {
	rs := newRunsServer(t)
	var e apiError
	for _, c := range []struct{ path, body string }{
		{"/runs/run_missing/input", `{"text":"hi"}`},
		{"/runs/run_missing/finish", ``},
		{"/runs/run_missing/stop", ``},
		{"/questions/qst_missing/answer", `{"answer":"x"}`},
	} {
		if code := do(t, "POST", rs.url+"/api/projects/"+rs.project.ID+c.path, c.body, &e); code != 404 {
			t.Errorf("POST %s: %d, want 404", c.path, code)
		}
	}
	run := rs.start(t, rs.task(t, "Bad input"))
	if code := do(t, "POST", rs.url+"/api/projects/"+rs.project.ID+"/runs/"+run.ID+"/input", `{"text":"   "}`, &e); code != 400 {
		t.Errorf("blank message: %d", code)
	}
	if code := do(t, "POST", rs.url+"/api/projects/"+rs.project.ID+"/runs/"+run.ID+"/input", `not json`, &e); code != 400 {
		t.Errorf("bad body: %d", code)
	}
	if code := do(t, "GET", rs.url+"/api/projects/"+rs.project.ID+"/runs/run_missing", "", &e); code != 404 {
		t.Errorf("get unknown run: %d", code)
	}
	if code := do(t, "GET", rs.url+"/api/projects/"+rs.project.ID+"/tasks/tsk_missing/runs", "", &e); code != 404 {
		t.Errorf("runs of unknown task: %d", code)
	}
	if code := do(t, "GET", rs.url+"/api/projects/prj_missing/runs", "", &e); code != 404 {
		t.Errorf("runs of unknown project: %d", code)
	}

	// Stop works, and answers with the stopped run.
	var stopped domain.Run
	if code := do(t, "POST", rs.url+"/api/projects/"+rs.project.ID+"/runs/"+run.ID+"/stop", "", &stopped); code != 200 || stopped.State != domain.RunStopped {
		t.Errorf("stop: %d %+v", code, stopped)
	}
}

func TestExecutionIsUnavailableWithoutARunner(t *testing.T) {
	ts := newTestServer(t, nil)
	var e apiError
	for _, path := range []string{"/api/projects/p/tasks/t/runs", "/api/projects/p/runs/r/input", "/api/projects/p/runs/r/finish", "/api/projects/p/runs/r/stop", "/api/projects/p/questions/q/answer"} {
		if code := do(t, "POST", ts.URL+path, `{}`, &e); code != 503 || e.Error.Code != "unavailable" {
			t.Errorf("POST %s: %d %+v", path, code, e)
		}
	}
}

func TestAgentEventsStreamLive(t *testing.T) {
	rs := newRunsServer(t)
	task := rs.task(t, "Live feed")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", rs.url+"/api/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	r := bufio.NewReader(resp.Body)
	type msg struct {
		typ  string
		data string
	}
	stream := make(chan msg, 100)
	go func() {
		var m msg
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimSpace(line)
			switch {
			case strings.HasPrefix(line, "event: "):
				m.typ = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				m.data = strings.TrimPrefix(line, "data: ")
			case line == "" && m.typ != "":
				stream <- m
				m = msg{}
			}
		}
	}()
	expect := func(typ string) msg {
		t.Helper()
		for {
			select {
			case m := <-stream:
				if m.typ == typ {
					return m
				}
			case <-ctx.Done():
				t.Fatalf("no %s event arrived", typ)
			}
		}
	}

	run := rs.start(t, task)
	expect("agent.started")
	rs.adapter.Last().Assistant("streamed text")
	out := expect("agent.output")
	var ev domain.Event
	if err := json.Unmarshal([]byte(out.data), &ev); err != nil || ev.RunID != run.ID || !strings.Contains(string(ev.Payload), "streamed text") {
		t.Fatalf("event = %s", out.data)
	}
	rs.adapter.Last().Ask("q", "Proceed?")
	expect("agent.question")
	rs.adapter.Last().Exit(1, "gone")
	failed := expect("agent.failed")
	if !strings.Contains(failed.data, "gone") {
		t.Fatalf("failed = %s", failed.data)
	}
}
