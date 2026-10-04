package api

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"devboard/internal/domain"
	"devboard/internal/service"
)

// These tests are about the boundary between projects. Everything that belongs
// to a project is reached through it, and a request that names one project can
// neither read nor act on another's: not by guessing an ID, and not through a
// route that forgets to check. The Control Center is the one deliberate
// exception, and is tested to really cross them.

// addProject registers a second repository with the same server.
func (rs *runsServer) addProject(t *testing.T, name string) service.ProjectDetail {
	t.Helper()
	repo := gitRepo(t)
	env := append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e.com", "GIT_CONFIG_GLOBAL=/dev/null")
	cmd := exec.Command("git", "-C", repo, "commit", "-q", "--allow-empty", "-m", "init")
	cmd.Env = env
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v %s", err, out)
	}
	var p service.ProjectDetail
	if code := do(t, "POST", rs.url+"/api/projects", `{"path":`+jsonString(repo)+`,"name":`+jsonString(name)+`}`, &p); code != 201 {
		t.Fatalf("register %s: %d", name, code)
	}
	return p
}

func (rs *runsServer) taskIn(t *testing.T, projectID, title string) domain.Task {
	t.Helper()
	var task domain.Task
	if code := do(t, "POST", rs.url+"/api/projects/"+projectID+"/tasks", `{"title":`+jsonString(title)+`}`, &task); code != 201 {
		t.Fatalf("create task: %d", code)
	}
	return task
}

func (rs *runsServer) startIn(t *testing.T, projectID string, task domain.Task) domain.Run {
	t.Helper()
	var run domain.Run
	if code := do(t, "POST", rs.url+"/api/projects/"+projectID+"/tasks/"+task.ID+"/runs", `{"agentId":"fake"}`, &run); code != 201 {
		t.Fatalf("start run: %d", code)
	}
	return run
}

// Two projects, each with a task, a run with an open question and a worktree.
type twoProjects struct {
	rs     *runsServer
	a, b   service.ProjectDetail
	ta, tb domain.Task
	ra, rb domain.Run
	qa, qb domain.Question
}

func newTwoProjects(t *testing.T) *twoProjects {
	t.Helper()
	w := &twoProjects{rs: newRunsServer(t)}
	w.a = w.rs.project
	w.b = w.rs.addProject(t, "Beta")
	w.ta, w.tb = w.rs.taskIn(t, w.a.ID, "Alpha task"), w.rs.taskIn(t, w.b.ID, "Beta task")
	w.ra = w.rs.startIn(t, w.a.ID, w.ta)
	w.rs.adapter.Last().Ask("qa", "Alpha asks")
	w.rb = w.rs.startIn(t, w.b.ID, w.tb)
	w.rs.adapter.Last().Ask("qb", "Beta asks")
	waitFor(t, "both questions", func() bool {
		return len(w.rs.questionsOf(t, w.a.ID)) == 1 && len(w.rs.questionsOf(t, w.b.ID)) == 1
	})
	w.qa, w.qb = w.rs.questionsOf(t, w.a.ID)[0], w.rs.questionsOf(t, w.b.ID)[0]
	return w
}

func (rs *runsServer) questionsOf(t *testing.T, projectID string) []domain.Question {
	t.Helper()
	var out struct{ Questions []domain.Question }
	do(t, "GET", rs.url+"/api/projects/"+projectID+"/questions", "", &out)
	return out.Questions
}

func TestProjectViewsHoldOnlyThatProject(t *testing.T) {
	w := newTwoProjects(t)
	rs := w.rs

	for _, p := range []struct {
		project service.ProjectDetail
		task    domain.Task
		run     domain.Run
		q       domain.Question
	}{{w.a, w.ta, w.ra, w.qa}, {w.b, w.tb, w.rb, w.qb}} {
		var tasks struct{ Tasks []domain.Task }
		do(t, "GET", rs.url+"/api/projects/"+p.project.ID+"/tasks", "", &tasks)
		if len(tasks.Tasks) != 1 || tasks.Tasks[0].ID != p.task.ID {
			t.Errorf("%s tasks = %+v", p.project.Name, tasks.Tasks)
		}
		var board struct{ Runs []domain.Run }
		do(t, "GET", rs.url+"/api/projects/"+p.project.ID+"/runs", "", &board)
		if len(board.Runs) != 1 || board.Runs[0].ID != p.run.ID || board.Runs[0].ProjectID != p.project.ID {
			t.Errorf("%s board runs = %+v", p.project.Name, board.Runs)
		}
		var act struct{ Runs []domain.Run }
		do(t, "GET", rs.url+"/api/projects/"+p.project.ID+"/activity", "", &act)
		if len(act.Runs) != 1 || act.Runs[0].ID != p.run.ID {
			t.Errorf("%s activity = %+v", p.project.Name, act.Runs)
		}
		if qs := rs.questionsOf(t, p.project.ID); len(qs) != 1 || qs[0].ID != p.q.ID || qs[0].ProjectID != p.project.ID {
			t.Errorf("%s questions = %+v", p.project.Name, qs)
		}
		var page struct{ Events []domain.Event }
		do(t, "GET", rs.url+"/api/projects/"+p.project.ID+"/runs/"+p.run.ID+"/events?limit=1000", "", &page)
		for _, ev := range page.Events {
			if ev.ProjectID != p.project.ID {
				t.Errorf("%s run history holds %s's event %+v", p.project.Name, ev.ProjectID, ev)
			}
		}
	}
	if code := do(t, "GET", rs.url+"/api/projects/"+w.a.ID+"/activity?limit=0", "", nil); code != 400 {
		t.Errorf("bad limit: %d", code)
	}
}

// The heart of it: every route that takes an ID refuses an ID from another project,
// with the answer it gives for an ID that does not exist.
func TestAnotherProjectsIDsAreNotFound(t *testing.T) {
	w := newTwoProjects(t)
	rs := w.rs
	wt := rs.getRun(t, w.ra.ID).WorktreeID
	if wt == "" {
		t.Fatal("the run has no worktree")
	}
	b := rs.url + "/api/projects/" + w.b.ID // asks as project B about project A's things

	for _, c := range []struct{ method, path, body string }{
		{"PATCH", "/tasks/" + w.ta.ID, `{"title":"hijacked","version":1}`},
		{"GET", "/tasks/" + w.ta.ID + "/runs", ""},
		{"POST", "/tasks/" + w.ta.ID + "/runs", `{"agentId":"fake"}`},
		{"GET", "/runs/" + w.ra.ID, ""},
		{"GET", "/runs/" + w.ra.ID + "/events", ""},
		{"POST", "/runs/" + w.ra.ID + "/input", `{"text":"hello"}`},
		{"POST", "/runs/" + w.ra.ID + "/finish", ``},
		{"POST", "/runs/" + w.ra.ID + "/stop", ``},
		{"GET", "/questions/" + w.qa.ID, ""},
		{"POST", "/questions/" + w.qa.ID + "/answer", `{"answer":"yes"}`},
		{"GET", "/worktrees/" + wt, ""},
	} {
		var wrong, missing apiError
		code := do(t, c.method, b+c.path, c.body, &wrong)
		// What an ID that exists nowhere gets: the same status and code, so existence is not revealed.
		ghost := strings.NewReplacer(w.ta.ID, "tsk_ghost", w.ra.ID, "run_ghost", w.qa.ID, "qst_ghost", wt, "wt_ghost").Replace(c.path)
		ghostCode := do(t, c.method, b+ghost, c.body, &missing)
		if code != 404 || wrong.Error.Code != "not_found" || code != ghostCode || wrong.Error.Code != missing.Error.Code {
			t.Errorf("%s %s as the wrong project: %d %+v (a missing ID gives %d %+v)", c.method, c.path, code, wrong, ghostCode, missing)
		}
	}

	// Nothing of A's was touched by any of that.
	if run := rs.getRun(t, w.ra.ID); run.State != domain.RunWaitingForUser || run.Waiting != domain.WaitQuestion {
		t.Errorf("project A's run = %+v", run)
	}
	var tasks struct{ Tasks []domain.Task }
	do(t, "GET", rs.url+"/api/projects/"+w.a.ID+"/tasks", "", &tasks)
	if task := tasks.Tasks[0]; task.Title != "Alpha task" || task.Version != 2 { // 2: starting the run moved it to Doing
		t.Errorf("project A's task = %+v", task)
	}
	if qs := rs.questionsOf(t, w.a.ID); len(qs) != 1 || qs[0].State != domain.QuestionPending {
		t.Errorf("project A's questions = %+v", qs)
	}
	if len(rs.adapter.Last().Responses()) != 0 {
		t.Error("an answer from the wrong project reached an agent")
	}

	// And through its own project the same calls work.
	if code := do(t, "POST", rs.url+"/api/projects/"+w.a.ID+"/questions/"+w.qa.ID+"/answer", `{"answer":"yes"}`, nil); code != 200 {
		t.Errorf("answer through its own project: %d", code)
	}

	// An unknown project is the same 404 again.
	for _, path := range []string{"/tasks", "/runs", "/activity", "/questions"} {
		var e apiError
		if code := do(t, "GET", rs.url+"/api/projects/prj_ghost"+path, "", &e); code != 404 || e.Error.Code != "not_found" {
			t.Errorf("GET /projects/prj_ghost%s: %d %+v", path, code, e)
		}
	}
}

// The old routes that took a bare task, run or question ID are gone: there is no way to
// reach project-owned data without saying which project it is in.
func TestUnscopedRoutesNoLongerExist(t *testing.T) {
	w := newTwoProjects(t)
	for _, c := range []struct{ method, path string }{
		{"GET", "/api/tasks/" + w.ta.ID + "/runs"},
		{"PATCH", "/api/tasks/" + w.ta.ID},
		{"POST", "/api/tasks/" + w.ta.ID + "/runs"},
		{"GET", "/api/runs"},
		{"GET", "/api/runs/" + w.ra.ID},
		{"GET", "/api/runs/" + w.ra.ID + "/events"},
		{"POST", "/api/runs/" + w.ra.ID + "/input"},
		{"POST", "/api/runs/" + w.ra.ID + "/stop"},
		{"GET", "/api/questions"},
		{"GET", "/api/questions/" + w.qa.ID},
		{"POST", "/api/questions/" + w.qa.ID + "/answer"},
		{"GET", "/api/worktrees/" + w.rs.getRun(t, w.ra.ID).WorktreeID},
	} {
		var e apiError
		if code := do(t, c.method, w.rs.url+c.path, `{}`, &e); code != 404 || e.Error.Code != "not_found" {
			t.Errorf("%s %s = %d %+v, want the route to be gone", c.method, c.path, code, e)
		}
	}
}

func TestControlCenterCrossesProjects(t *testing.T) {
	w := newTwoProjects(t)
	var cc service.Overview
	if code := do(t, "GET", w.rs.url+"/api/control-center", "", &cc); code != 200 {
		t.Fatalf("control center: %d", code)
	}
	if len(cc.Projects) != 2 || cc.Projects[0].ProjectID != w.a.ID || cc.Projects[1].ProjectID != w.b.ID ||
		cc.Projects[0].NeedsInput != 1 || cc.Projects[1].NeedsInput != 1 {
		t.Fatalf("projects = %+v", cc.Projects)
	}
	if len(cc.Questions) != 2 {
		t.Fatalf("questions = %+v", cc.Questions)
	}
	byProject := map[string]service.AttentionQuestion{}
	for _, q := range cc.Questions {
		byProject[q.Question.ProjectID] = q
	}
	if q := byProject[w.a.ID]; q.ProjectName != w.a.Name || q.TaskTitle != "Alpha task" || q.Question.ID != w.qa.ID {
		t.Errorf("alpha question = %+v", q)
	}
	if q := byProject[w.b.ID]; q.ProjectName != "Beta" || q.TaskTitle != "Beta task" || q.Question.ID != w.qb.ID {
		t.Errorf("beta question = %+v", q)
	}
	if len(cc.Runs) != 2 {
		t.Fatalf("runs = %+v", cc.Runs)
	}

	// Answering from the Control Center goes through the question's own project.
	q := byProject[w.b.ID].Question
	if code := do(t, "POST", w.rs.url+"/api/projects/"+q.ProjectID+"/questions/"+q.ID+"/answer", `{"answer":"yes"}`, nil); code != 200 {
		t.Fatalf("answer: %d", code)
	}
	do(t, "GET", w.rs.url+"/api/control-center", "", &cc)
	if len(cc.Questions) != 1 || cc.Questions[0].Question.ID != w.qa.ID {
		t.Fatalf("after answering Beta's: %+v", cc.Questions)
	}
}

type streamed struct {
	mu     sync.Mutex
	events []domain.Event
}

func (s *streamed) all() []domain.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]domain.Event(nil), s.events...)
}

// openStream follows /api/events (with a query) until the test ends.
func openStream(t *testing.T, url string) *streamed {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("GET %s: %d", url, resp.StatusCode)
	}
	s := &streamed{}
	go func() {
		defer resp.Body.Close()
		r := bufio.NewReader(resp.Body)
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			if data, ok := strings.CutPrefix(strings.TrimSpace(line), "data: "); ok {
				var ev domain.Event
				if json.Unmarshal([]byte(data), &ev) == nil {
					s.mu.Lock()
					s.events = append(s.events, ev)
					s.mu.Unlock()
				}
			}
		}
	}()
	return s
}

func TestEventStreamCanBeScopedToOneProject(t *testing.T) {
	rs := newRunsServer(t)
	a := rs.project
	b := rs.addProject(t, "Beta")
	if code := do(t, "GET", rs.url+"/api/events?project=prj_ghost", "", &apiError{}); code != 404 {
		t.Fatalf("unknown project: %d", code)
	}

	all := openStream(t, rs.url+"/api/events")
	onlyB := openStream(t, rs.url+"/api/events?project="+b.ID)
	time.Sleep(50 * time.Millisecond) // both are subscribed and at the end of the log

	ta := rs.taskIn(t, a.ID, "in alpha")
	rs.startIn(t, a.ID, ta)
	tb := rs.taskIn(t, b.ID, "in beta")
	rs.startIn(t, b.ID, tb)
	rs.adapter.Last().Assistant("beta speaking")

	has := func(s *streamed, typ domain.EventType, project string) bool {
		for _, ev := range s.all() {
			if ev.Type == typ && ev.ProjectID == project {
				return true
			}
		}
		return false
	}
	waitFor(t, "the unscoped stream to carry both projects", func() bool {
		return has(all, domain.EventTaskCreated, a.ID) && has(all, domain.EventTaskCreated, b.ID) && has(all, domain.EventAgentOutput, b.ID)
	})
	waitFor(t, "the scoped stream to carry Beta's events", func() bool {
		return has(onlyB, domain.EventTaskCreated, b.ID) && has(onlyB, domain.EventAgentOutput, b.ID)
	})
	for _, ev := range onlyB.all() {
		if ev.ProjectID != b.ID {
			t.Fatalf("Beta's stream carried %q's event: %+v", ev.ProjectID, ev)
		}
	}
	if len(onlyB.all()) >= len(all.all()) {
		t.Errorf("the scoped stream (%d events) is not narrower than the full one (%d)", len(onlyB.all()), len(all.all()))
	}
}

// The policy is part of the task and of the run it starts, over the wire.
func TestPolicyOverHTTP(t *testing.T) {
	rs := newRunsServer(t)
	pid := rs.project.ID
	base := rs.url + "/api/projects/" + pid

	// The default is interactive, and shown.
	plain := rs.taskIn(t, pid, "plain")
	if plain.Policy.Interaction != domain.InteractionInteractive {
		t.Fatalf("default = %+v", plain.Policy)
	}
	var raw map[string]any
	do(t, "POST", base+"/tasks", `{"title":"raw"}`, &raw)
	if p, _ := raw["policy"].(map[string]any); p["interaction"] != "interactive" {
		t.Fatalf("the JSON does not say the policy: %v", raw)
	}

	// Chosen at creation, and edited afterwards.
	var task domain.Task
	if code := do(t, "POST", base+"/tasks", `{"title":"auto","policy":{"interaction":"autonomous"}}`, &task); code != 201 || task.Policy.Interaction != domain.InteractionAutonomous {
		t.Fatalf("create = %d %+v", code, task)
	}
	var edited domain.Task
	if code := do(t, "PATCH", base+"/tasks/"+task.ID, `{"policy":{"interaction":"autonomous_stop_if_blocked"},"version":1}`, &edited); code != 200 ||
		edited.Policy.Interaction != domain.InteractionAutonomousStopIfBlocked || edited.Title != "auto" || edited.Version != 2 {
		t.Fatalf("edit = %d %+v", code, edited)
	}
	var e apiError
	for _, body := range []string{`{"title":"x","policy":{"interaction":"yolo"}}`, `{"title":"x","policy":{"interaction":"autonomous","permissions":"all"}}`, `{"title":"x","policy":"autonomous"}`} {
		if code := do(t, "POST", base+"/tasks", body, &e); code != 400 || e.Error.Code != "invalid" {
			t.Errorf("POST %s = %d %+v", body, code, e)
		}
	}
	if code := do(t, "PATCH", base+"/tasks/"+task.ID, `{"policy":{"interaction":"yolo"},"version":2}`, &e); code != 400 || e.Error.Code != "invalid" {
		t.Errorf("edit to an unknown policy = %d %+v", code, e)
	}

	// A run is given the task's policy and keeps it; a run can also be given its own.
	var run domain.Run
	if code := do(t, "POST", base+"/tasks/"+task.ID+"/runs", `{"agentId":"fake"}`, &run); code != 201 || run.Policy.Interaction != domain.InteractionAutonomousStopIfBlocked {
		t.Fatalf("run = %d %+v", code, run.Policy)
	}
	if got := rs.adapter.Last().Req.Policy.Interaction; got != domain.InteractionAutonomousStopIfBlocked {
		t.Fatalf("the agent was given %q", got)
	}
	if code := do(t, "POST", base+"/runs/"+run.ID+"/finish", ``, nil); code != 200 {
		t.Fatal("finish")
	}
	if code := do(t, "POST", base+"/tasks/"+task.ID+"/runs", `{"agentId":"fake","policy":{"interaction":"yolo"}}`, &e); code != 400 {
		t.Errorf("unknown run policy: %d", code)
	}
	if code := do(t, "POST", base+"/tasks/"+task.ID+"/runs", `{"agentId":"fake","policy":{"interaction":"interactive"}}`, &run); code != 201 || run.Policy.Interaction != domain.InteractionInteractive {
		t.Fatalf("overridden run = %d %+v", code, run.Policy)
	}
}

// A run that must stop rather than guess, seen from a client: blocked, with its
// blocker, and no question to answer.
func TestABlockedRunOverHTTP(t *testing.T) {
	rs := newRunsServer(t)
	pid := rs.project.ID
	base := rs.url + "/api/projects/" + pid
	var task domain.Task
	do(t, "POST", base+"/tasks", `{"title":"careful","policy":{"interaction":"autonomous_stop_if_blocked"}}`, &task)
	run := rs.startIn(t, pid, task)
	rs.adapter.Last().Ask("q", "Which database?", "sqlite", "postgres")
	waitFor(t, "the run to be blocked", func() bool { return rs.getRun(t, run.ID).State == domain.RunBlocked })

	got := rs.getRun(t, run.ID)
	if got.Blocker == nil || got.Blocker.Summary != "Which database?" || len(got.Blocker.Options) != 2 || got.Blocker.Source != domain.BlockerQuestion {
		t.Fatalf("blocker = %+v", got.Blocker)
	}
	var raw struct{ State string }
	do(t, "GET", base+"/runs/"+run.ID, "", &raw)
	if raw.State != "blocked" {
		t.Fatalf("state on the wire = %q", raw.State)
	}
	if len(rs.questionsOf(t, pid)) != 0 {
		t.Fatal("a blocked run has no question to answer")
	}
	var tasks struct{ Tasks []domain.Task }
	do(t, "GET", base+"/tasks", "", &tasks)
	if tasks.Tasks[0].State != domain.TaskDoing {
		t.Fatalf("task = %+v: blocked is not a column", tasks.Tasks[0])
	}
	var cc service.Overview
	do(t, "GET", rs.url+"/api/control-center", "", &cc)
	if len(cc.Questions) != 0 || cc.Projects[0].Blocked != 1 || cc.Projects[0].NeedsInput != 0 || cc.Runs[0].Run.Blocker == nil {
		t.Fatalf("control center = %+v", cc)
	}

	// The user settles it with a message.
	var after domain.Run
	if code := do(t, "POST", base+"/runs/"+run.ID+"/input", `{"text":"sqlite"}`, &after); code != 200 || after.State != domain.RunRunning || after.Blocker != nil {
		t.Fatalf("message = %d %+v", code, after)
	}
}

func TestAnAutonomousRunDoesNotInterruptOverHTTP(t *testing.T) {
	rs := newRunsServer(t)
	pid := rs.project.ID
	var task domain.Task
	do(t, "POST", rs.url+"/api/projects/"+pid+"/tasks", `{"title":"hands off","policy":{"interaction":"autonomous"}}`, &task)
	run := rs.startIn(t, pid, task)
	s := rs.adapter.Last()

	s.Ask("q", "Which database?")
	waitFor(t, "the reply", func() bool { return len(s.Responses()) == 1 })
	if len(rs.questionsOf(t, pid)) != 0 || rs.getRun(t, run.ID).State != domain.RunRunning {
		t.Fatalf("the user was interrupted: %+v / %+v", rs.questionsOf(t, pid), rs.getRun(t, run.ID))
	}

	// But permission is still the user's, and reaches them.
	s.RequestApproval("a", "Run `git push --force`?")
	waitFor(t, "the approval", func() bool { return len(rs.questionsOf(t, pid)) == 1 })
	q := rs.questionsOf(t, pid)[0]
	if q.Kind != domain.QuestionApproval || len(s.Responses()) != 1 {
		t.Fatalf("question = %+v, responses = %+v", q, s.Responses())
	}
	var cc service.Overview
	do(t, "GET", rs.url+"/api/control-center", "", &cc)
	if len(cc.Questions) != 1 || cc.Projects[0].NeedsInput != 1 {
		t.Fatalf("control center = %+v", cc)
	}
}
