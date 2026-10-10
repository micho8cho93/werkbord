package appops

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"devboard/internal/domain"
	"devboard/internal/events"
	"devboard/internal/service"
	"devboard/internal/store"
	"devboard/internal/store/sqlite"
)

var bg = context.Background()

// fx is a real controller in miniature: the SQLite store, the domain services, and the operations on top of them.
type fx struct {
	t        *testing.T
	db       *sqlite.DB
	path     string
	st       *flakyStore
	svc      *Service
	projects *service.Projects
	tasks    *service.Tasks
	runs     *service.Runs
	labels   *service.Labels
	control  *service.ControlCenter

	clock   atomic.Int64 // unix milliseconds
	mu      sync.Mutex
	answers []string // what reached an agent
}

// flakyStore lets a test make the audit unwritable.
type flakyStore struct {
	store.Store
	broken atomic.Bool
}

func (f *flakyStore) Update(ctx context.Context, fn func(store.Tx) error) error {
	if f.broken.Load() {
		return errors.New("disk is full")
	}
	return f.Store.Update(ctx, fn)
}

func newFx(t *testing.T) *fx {
	t.Helper()
	path := filepath.Join(t.TempDir(), "db")
	db, err := sqlite.Open(bg, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	f := &fx{t: t, db: db, path: path, st: &flakyStore{Store: db}}
	f.clock.Store(time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC).UnixMilli())
	deps := service.Deps{Store: db, Bus: events.NewBroker(), Now: f.now}
	f.projects = &service.Projects{Deps: deps}
	f.tasks = &service.Tasks{Deps: deps}
	f.runs = &service.Runs{Deps: deps}
	f.labels = &service.Labels{Deps: deps}
	f.control = &service.ControlCenter{Deps: deps}
	backend := NewBackend(Services{Projects: f.projects, Tasks: f.tasks, Labels: f.labels, Runs: f.runs, Control: f.control,
		Answer: func(ctx context.Context, id, answer string) (*domain.Question, error) {
			q, err := f.runs.AcceptAnswer(ctx, id, answer)
			if err != nil {
				return nil, err
			}
			f.mu.Lock()
			f.answers = append(f.answers, q.Answer)
			f.mu.Unlock()
			return f.runs.ConfirmDelivery(ctx, id)
		}})
	f.svc = New(Config{Backend: backend, Store: f.st, Now: f.now, ConfirmTTL: 10 * time.Minute})
	return f
}

func (f *fx) now() time.Time { return time.UnixMilli(f.clock.Load()).UTC() }

func (f *fx) advance(d time.Duration) { f.clock.Add(d.Milliseconds()) }

// session makes an assistant session row (proposals belong to one) and returns the principal for it.
func (f *fx) session(id string, grants ...Permission) Principal {
	f.t.Helper()
	if err := f.db.Update(bg, func(tx store.Tx) error {
		return tx.Assistant().CreateSession(bg, &domain.AssistantSession{ID: id, Provider: "fake", State: domain.AssistantIdle, CreatedAt: f.now(), UpdatedAt: f.now()})
	}); err != nil {
		f.t.Fatal(err)
	}
	if len(grants) == 0 {
		grants = AllPermissions
	}
	return Principal{ID: "assistant:" + id, Actor: "assistant:fake", Via: "assistant", SessionID: id, Grants: grants}
}

func (f *fx) project(name string) *service.ProjectDetail {
	f.t.Helper()
	p, err := f.projects.CreateWork(bg, name)
	if err != nil {
		f.t.Fatal(err)
	}
	return p
}

func (f *fx) ticket(projectID, title string) *domain.Task {
	f.t.Helper()
	tk, err := f.tasks.Create(bg, projectID, title, "details of "+title)
	if err != nil {
		f.t.Fatal(err)
	}
	return tk
}

func (f *fx) call(p Principal, name string, args any) (*Result, error) {
	f.t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		f.t.Fatal(err)
	}
	return f.svc.Call(bg, p, name, raw)
}

func (f *fx) mustCall(p Principal, name string, args any) *Result {
	f.t.Helper()
	res, err := f.call(p, name, args)
	if err != nil {
		f.t.Fatalf("%s: %v", name, err)
	}
	return res
}

func (f *fx) confirm(p Principal, actionID string, approve bool) (*Result, error) {
	f.t.Helper()
	return f.svc.Resolve(bg, Decision{SessionID: p.SessionID, ActionID: actionID, Approve: approve, By: "owner", Executor: p})
}

func (f *fx) tickets(projectID string) []domain.Task {
	f.t.Helper()
	ts, err := f.tasks.List(bg, projectID)
	if err != nil {
		f.t.Fatal(err)
	}
	return ts
}

func (f *fx) audit() []domain.AssistantAuditEntry {
	f.t.Helper()
	es, err := f.svc.Audit(bg, "", 0, 500)
	if err != nil {
		f.t.Fatal(err)
	}
	// oldest first
	for i, j := 0, len(es)-1; i < j; i, j = i+1, j-1 {
		es[i], es[j] = es[j], es[i]
	}
	return es
}

func (f *fx) lastAudit() domain.AssistantAuditEntry {
	f.t.Helper()
	es := f.audit()
	if len(es) == 0 {
		f.t.Fatal("the audit is empty")
	}
	return es[len(es)-1]
}

func wantCode(t *testing.T, err error, code string) {
	t.Helper()
	if err == nil {
		t.Fatalf("want error %s, got none", code)
	}
	if got := CodeOf(err); got != code {
		t.Fatalf("error code = %s (%v), want %s", got, err, code)
	}
}

// question records a pending question of the given kind on a fresh running run of a new ticket.
func (f *fx) question(projectID string, in service.NewQuestion) *domain.Question {
	f.t.Helper()
	tk := f.ticket(projectID, "ticket for "+in.Prompt)
	r, err := f.runs.Create(bg, service.NewRun{TaskID: tk.ID, AgentID: "fake", Prompt: "do it"})
	if err != nil {
		f.t.Fatal(err)
	}
	if _, err := f.runs.MarkStarted(bg, r.ID, service.Started{PID: 1, ProcessID: "p", SessionRef: "s"}); err != nil {
		f.t.Fatal(err)
	}
	q, _, err := f.runs.RecordQuestion(bg, r.ID, in)
	if err != nil {
		f.t.Fatal(err)
	}
	return q
}

// ---- the catalog ----

func TestCatalogShowsOnlyWhatThePrincipalMayCall(t *testing.T) {
	f := newFx(t)
	full := f.session("ast_full")
	readOnly := f.session("ast_ro", PermTicketsRead, PermProjectsRead)
	none := f.session("ast_none", PermScheduleRead)

	names := func(p Principal) []string {
		var out []string
		for _, s := range f.svc.Catalog(p) {
			out = append(out, s.Name)
		}
		return out
	}
	all := names(full)
	if len(all) != 13 {
		t.Fatalf("the full catalog has %d operations: %v", len(all), all)
	}
	for _, s := range f.svc.Catalog(full) {
		if s.Description == "" || s.Input == nil || s.Input.Type != "object" || s.Permission == "" {
			t.Errorf("%s is not fully described: %+v", s.Name, s)
		}
		if (s.Kind == KindMutation) != s.RequiresConfirmation {
			t.Errorf("%s: every mutation, and only a mutation, requires confirmation", s.Name)
		}
		if _, err := json.Marshal(s); err != nil {
			t.Errorf("%s does not marshal as a tool description: %v", s.Name, err)
		}
	}
	if got := strings.Join(names(readOnly), ","); got != "get_overview,get_ticket,list_labels,list_projects,list_tickets" {
		t.Errorf("read-only catalog = %s", got)
	}
	if got := strings.Join(names(none), ","); got != "get_schedule" {
		t.Errorf("a schedule-only principal sees %s", got)
	}
	// The set is exactly this. Adding an operation, above all one that starts or stops a run, touches Git, a setting or
	// a repository, or deletes, has to change this list, where a reviewer will see it.
	want := "answer_question,create_ticket,get_overview,get_run_status,get_schedule,get_ticket,list_agent_questions,list_blockers,list_labels,list_projects,list_runs,list_tickets,update_ticket"
	if got := strings.Join(all, ","); got != want {
		t.Errorf("the operations are\n%s\nwant\n%s", got, want)
	}
}

// ---- reads ----

func TestReadsAnswerFromTheRealServicesAndLeakNothingExtra(t *testing.T) {
	f := newFx(t)
	p := f.session("ast_1")
	a := f.project("Alpha")
	b := f.project("Beta")
	long := strings.Repeat("long description. ", 100)
	t1, err := f.tasks.Create(bg, a.ID, "First", long)
	if err != nil {
		t.Fatal(err)
	}
	f.ticket(a.ID, "Second thing")
	f.ticket(b.ID, "Other project")

	res := f.mustCall(p, "list_projects", map[string]any{})
	raw, _ := json.Marshal(res.Data)
	if !strings.Contains(string(raw), "Alpha") || !strings.Contains(string(raw), "Beta") {
		t.Fatalf("projects = %s", raw)
	}
	for _, leaked := range []string{"repoPath", "work:", "execution", "createdAt"} {
		if strings.Contains(string(raw), leaked) {
			t.Errorf("a project view leaks %q: %s", leaked, raw)
		}
	}

	res = f.mustCall(p, "list_tickets", map[string]any{"projectId": a.ID})
	data := res.Data.(map[string]any)
	tickets := data["tickets"].([]TicketView)
	if len(tickets) != 2 || data["matching"].(int) != 2 || data["truncated"].(bool) {
		t.Fatalf("tickets = %+v", data)
	}
	for _, tk := range tickets {
		if tk.ProjectID != a.ID {
			t.Errorf("a ticket of another project leaked into the list: %+v", tk)
		}
	}
	if first := tickets[0]; first.ID != t1.ID || !first.DescriptionTruncated || len(first.Description) > listDescriptionChars+4 {
		t.Errorf("list descriptions are clipped: %+v", first)
	}

	res = f.mustCall(p, "list_tickets", map[string]any{"projectId": a.ID, "query": "SECOND", "limit": 1})
	if got := res.Data.(map[string]any)["tickets"].([]TicketView); len(got) != 1 || got[0].Title != "Second thing" {
		t.Errorf("a title query is case-insensitive: %+v", got)
	}
	res = f.mustCall(p, "list_tickets", map[string]any{"projectId": a.ID, "state": "done"})
	if got := res.Data.(map[string]any)["tickets"].([]TicketView); len(got) != 0 {
		t.Errorf("nothing is done: %+v", got)
	}

	res = f.mustCall(p, "get_ticket", map[string]any{"projectId": a.ID, "ticketId": t1.ID})
	full := res.Data.(map[string]any)["ticket"].(TicketView)
	if full.Description != long[:len(long)] && !strings.HasPrefix(full.Description, "long description.") || full.DescriptionTruncated {
		t.Errorf("get_ticket returns the description in full: truncated=%v len=%d", full.DescriptionTruncated, len(full.Description))
	}

	// A ticket asked for through the wrong project is not found, exactly as one that does not exist.
	_, errWrong := f.call(p, "get_ticket", map[string]any{"projectId": b.ID, "ticketId": t1.ID})
	_, errMissing := f.call(p, "get_ticket", map[string]any{"projectId": b.ID, "ticketId": "tsk_doesnotexist"})
	wantCode(t, errWrong, CodeNotFound)
	wantCode(t, errMissing, CodeNotFound)

	res = f.mustCall(p, "get_overview", map[string]any{})
	if ov := res.Data.(interface{}); ov == nil {
		t.Fatal("no overview")
	}
	res = f.mustCall(p, "list_labels", map[string]any{})
	if res.Data.(map[string]any)["labels"] == nil {
		t.Error("labels list must not be null")
	}
}

func TestRunsBlockersQuestionsAndSchedule(t *testing.T) {
	f := newFx(t)
	p := f.session("ast_1")
	a := f.project("Alpha")
	tk := f.ticket(a.ID, "Needs an answer")
	dep := f.ticket(a.ID, "Depends on it")
	r, err := f.runs.Create(bg, service.NewRun{TaskID: tk.ID, AgentID: "fake", Prompt: "secret prompt text"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.runs.MarkStarted(bg, r.ID, service.Started{PID: 4242, ProcessID: "token", SessionRef: "sess"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.runs.RecordQuestion(bg, r.ID, service.NewQuestion{Prompt: "Which database?", Options: []string{"Postgres", "SQLite"}}); err != nil {
		t.Fatal(err)
	}
	start, end := "2026-03-02", "2026-03-04"
	if _, err := f.tasks.Update(bg, dep.ID, service.TaskPatch{Version: dep.Version, Plan: &domain.Plan{Start: start, End: end}}); err != nil {
		t.Fatal(err)
	}

	res := f.mustCall(p, "list_agent_questions", map[string]any{})
	qs := res.Data.(map[string]any)["questions"].([]QuestionView)
	if len(qs) != 1 || qs[0].Prompt != "Which database?" || qs[0].TicketTitle != "Needs an answer" || !qs[0].AnswerableHere || qs[0].ProjectName != "Alpha" {
		t.Fatalf("questions = %+v", qs)
	}

	res = f.mustCall(p, "get_run_status", map[string]any{"projectId": a.ID, "runId": r.ID})
	raw, _ := json.Marshal(res.Data)
	if !strings.Contains(string(raw), `"state":"waiting_for_user"`) || !strings.Contains(string(raw), `"waiting":"question"`) {
		t.Errorf("run status = %s", raw)
	}
	for _, leaked := range []string{"secret prompt text", "4242", "token", "sessionRef", "pid", "policy"} {
		if strings.Contains(string(raw), leaked) {
			t.Errorf("a run view leaks %q: %s", leaked, raw)
		}
	}
	res = f.mustCall(p, "list_runs", map[string]any{"projectId": a.ID, "activeOnly": true})
	if got := res.Data.(map[string]any)["runs"].([]RunView); len(got) != 1 || got[0].ID != r.ID {
		t.Errorf("active runs = %+v", got)
	}
	res = f.mustCall(p, "list_runs", map[string]any{"projectId": a.ID, "ticketId": dep.ID})
	if got := res.Data.(map[string]any)["runs"].([]RunView); len(got) != 0 {
		t.Errorf("the dependent ticket never ran: %+v", got)
	}

	res = f.mustCall(p, "get_schedule", map[string]any{"projectId": a.ID})
	sched := res.Data.(map[string]any)
	items := sched["tickets"].([]ScheduleItem)
	if len(items) != 1 || items[0].TicketID != dep.ID || items[0].PlannedStart != start || items[0].PlannedEnd != end {
		t.Errorf("schedule = %+v", items)
	}
	if sched["warnings"] == nil || sched["waiting"] == nil {
		t.Error("warnings and waiting are lists, never null")
	}

	res = f.mustCall(p, "list_blockers", map[string]any{})
	bl := res.Data.(map[string]any)
	if bl["questionsWaiting"].(int) != 1 || bl["blockedRuns"] == nil || bl["failedRuns"] == nil || bl["waitingOnDependencies"] == nil {
		t.Errorf("blockers = %+v", bl)
	}
}

// ---- authorization and isolation ----

func TestAPrincipalWithoutAGrantCannotCallAndIsToldNothingElse(t *testing.T) {
	f := newFx(t)
	reader := f.session("ast_r", PermTicketsRead, PermProjectsRead)
	a := f.project("Alpha")
	tk := f.ticket(a.ID, "x")

	for _, c := range []struct {
		op   string
		args map[string]any
	}{
		{"create_ticket", map[string]any{"projectId": a.ID, "title": "no"}},
		{"update_ticket", map[string]any{"projectId": a.ID, "ticketId": tk.ID, "title": "no"}},
		{"answer_question", map[string]any{"projectId": a.ID, "questionId": "qst_1", "answer": "no"}},
		{"list_runs", map[string]any{"projectId": a.ID}},
		{"get_schedule", map[string]any{"projectId": a.ID}},
	} {
		_, err := f.call(reader, c.op, c.args)
		wantCode(t, err, CodePermissionDenied)
		if e := f.lastAudit(); e.Outcome != domain.AuditOutcomeDenied || e.Operation != c.op || e.Actor != "assistant:fake" || e.SessionID != "ast_r" {
			t.Errorf("%s: a refusal is audited: %+v", c.op, e)
		}
	}
	if n := len(f.tickets(a.ID)); n != 1 {
		t.Errorf("a refused call changed the board: %d tickets", n)
	}
	var pending []domain.AssistantAction
	_ = f.db.View(bg, func(tx store.Tx) (err error) { pending, err = tx.Assistant().ListOpenActions(bg); return })
	if len(pending) != 0 {
		t.Errorf("a refused call left a proposal: %+v", pending)
	}
	_, err := f.call(reader, "drop_database", map[string]any{})
	wantCode(t, err, CodeUnknownOperation)
}

func TestAPrincipalLimitedToAProjectNeverSeesAnother(t *testing.T) {
	f := newFx(t)
	a, b := f.project("Alpha"), f.project("Beta")
	ta, tb := f.ticket(a.ID, "alpha ticket"), f.ticket(b.ID, "beta ticket")
	qb := f.question(b.ID, service.NewQuestion{Prompt: "Beta question?"})
	qa := f.question(a.ID, service.NewQuestion{Prompt: "Alpha question?"})

	p := f.session("ast_a")
	p.Projects = []string{a.ID}

	res := f.mustCall(p, "list_projects", map[string]any{})
	if got := res.Data.(map[string]any)["projects"].([]ProjectView); len(got) != 1 || got[0].ID != a.ID {
		t.Fatalf("projects = %+v", got)
	}
	// Every way of naming the other project is a plain "not found", the same as a project that does not exist.
	for _, c := range []struct {
		op   string
		args map[string]any
	}{
		{"list_tickets", map[string]any{"projectId": b.ID}},
		{"get_ticket", map[string]any{"projectId": b.ID, "ticketId": tb.ID}},
		{"get_schedule", map[string]any{"projectId": b.ID}},
		{"list_runs", map[string]any{"projectId": b.ID}},
		{"list_blockers", map[string]any{"projectId": b.ID}},
		{"list_agent_questions", map[string]any{"projectId": b.ID}},
		{"create_ticket", map[string]any{"projectId": b.ID, "title": "sneaky"}},
		{"update_ticket", map[string]any{"projectId": b.ID, "ticketId": tb.ID, "title": "sneaky"}},
		{"answer_question", map[string]any{"projectId": b.ID, "questionId": qb.ID, "answer": "sneaky"}},
	} {
		_, err := f.call(p, c.op, c.args)
		wantCode(t, err, CodeNotFound)
		if strings.Contains(err.Error(), "outside") || strings.Contains(err.Error(), "permitted") {
			t.Errorf("%s: the refusal must not reveal that the project exists: %v", c.op, err)
		}
	}
	// Lists that span projects are narrowed to the principal's.
	res = f.mustCall(p, "list_agent_questions", map[string]any{})
	qs := res.Data.(map[string]any)["questions"].([]QuestionView)
	if len(qs) != 1 || qs[0].ID != qa.ID {
		t.Errorf("questions = %+v", qs)
	}
	res = f.mustCall(p, "get_overview", map[string]any{})
	raw, _ := json.Marshal(res.Data)
	if strings.Contains(string(raw), "Beta") || !strings.Contains(string(raw), "Alpha") {
		t.Errorf("the overview shows another project: %s", raw)
	}
	res = f.mustCall(p, "list_blockers", map[string]any{})
	if res.Data.(map[string]any)["questionsWaiting"].(int) != 1 {
		t.Errorf("blockers count another project's questions: %+v", res.Data)
	}
	// An id from the other project, asked for through the principal's own project, is still not found.
	_, err := f.call(p, "get_ticket", map[string]any{"projectId": a.ID, "ticketId": tb.ID})
	wantCode(t, err, CodeNotFound)
	_, err = f.call(p, "answer_question", map[string]any{"projectId": a.ID, "questionId": qb.ID, "answer": "sneaky"})
	wantCode(t, err, CodeNotFound)
	_ = ta

	nobody := f.session("ast_nobody")
	nobody.Projects = []string{}
	res = f.mustCall(nobody, "list_projects", map[string]any{})
	if got := res.Data.(map[string]any)["projects"].([]ProjectView); len(got) != 0 {
		t.Errorf("an empty project list means no projects, not all of them: %+v", got)
	}
	_, err = f.call(nobody, "list_tickets", map[string]any{"projectId": a.ID})
	wantCode(t, err, CodeNotFound)
}

func TestArgumentsAreStrictlyValidated(t *testing.T) {
	f := newFx(t)
	p := f.session("ast_1")
	a := f.project("Alpha")
	tk := f.ticket(a.ID, "x")
	big := strings.Repeat("x", 70<<10)

	for name, c := range map[string]struct {
		op   string
		args string
	}{
		"unknown argument":     {"list_tickets", `{"projectId":"` + a.ID + `","sort":"asc"}`},
		"missing required":     {"list_tickets", `{}`},
		"null":                 {"list_tickets", `{"projectId":null}`},
		"wrong type":           {"list_tickets", `{"projectId":5}`},
		"bad enum":             {"list_tickets", `{"projectId":"` + a.ID + `","state":"archived"}`},
		"limit too big":        {"list_tickets", `{"projectId":"` + a.ID + `","limit":1000}`},
		"fractional limit":     {"list_tickets", `{"projectId":"` + a.ID + `","limit":2.5}`},
		"not an object":        {"list_tickets", `["a"]`},
		"two objects":          {"list_projects", `{} {}`},
		"title too long":       {"create_ticket", `{"projectId":"` + a.ID + `","title":"` + strings.Repeat("t", 201) + `"}`},
		"too many labels":      {"create_ticket", `{"projectId":"` + a.ID + `","title":"x","labelIds":[` + strings.TrimSuffix(strings.Repeat(`"a",`, 21), ",") + `]}`},
		"bad day":              {"create_ticket", `{"projectId":"` + a.ID + `","title":"x","plannedStart":"tomorrow"}`},
		"execution settings":   {"update_ticket", `{"projectId":"` + a.ID + `","ticketId":"` + tk.ID + `","execution":{"agent":"codex"}}`},
		"orchestration":        {"update_ticket", `{"projectId":"` + a.ID + `","ticketId":"` + tk.ID + `","orchestration":{"enabled":true}}`},
		"archive":              {"update_ticket", `{"projectId":"` + a.ID + `","ticketId":"` + tk.ID + `","archived":true}`},
		"oversize":             {"create_ticket", `{"projectId":"` + a.ID + `","title":"x","description":"` + big + `"}`},
		"bad enum on mutation": {"create_ticket", `{"projectId":"` + a.ID + `","title":"x","workMode":"robot"}`},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := f.svc.Call(bg, p, c.op, json.RawMessage(c.args))
			wantCode(t, err, CodeInvalidArguments)
			if e := f.lastAudit(); e.Outcome != domain.AuditOutcomeInvalid {
				t.Errorf("an invalid call is audited as such: %+v", e)
			}
		})
	}
	if n := len(f.tickets(a.ID)); n != 1 {
		t.Errorf("an invalid call changed the board: %d tickets", n)
	}
	// An absent body is the empty object.
	if _, err := f.svc.Call(bg, p, "list_projects", nil); err != nil {
		t.Errorf("no arguments is fine for an operation that takes none: %v", err)
	}
}

// ---- mutations: propose, confirm, execute ----

func TestAMutationIsOnlyProposedUntilThePersonConfirms(t *testing.T) {
	f := newFx(t)
	p := f.session("ast_1")
	a := f.project("Alpha")
	lbl, err := f.labels.Create(bg, service.NewLabel{Name: "Ops", Color: "#112233"})
	if err != nil {
		t.Fatal(err)
	}

	res := f.mustCall(p, "create_ticket", map[string]any{"projectId": a.ID, "title": "  Rotate the keys ", "description": "quarterly", "workMode": "human",
		"labelIds": []string{lbl.ID}, "plannedStart": "2026-03-10", "plannedEnd": "2026-03-12"})
	if res.Pending == nil || res.Data != nil {
		t.Fatalf("a mutation returns a proposal and nothing else: %+v", res)
	}
	pr := res.Pending
	for _, want := range []string{`"Rotate the keys"`, `"Alpha"`, "human work", "Ops", "2026-03-10 to 2026-03-12", "quarterly"} {
		if !strings.Contains(pr.Summary, want) {
			t.Errorf("the summary shown to the person lacks %s: %s", want, pr.Summary)
		}
	}
	if got := len(f.tickets(a.ID)); got != 0 {
		t.Fatalf("the call itself created %d tickets", got)
	}
	if e := f.lastAudit(); e.Outcome != domain.AuditOutcomeProposed || e.ActionID != pr.ActionID || e.Kind != domain.AuditMutation || e.ArgsHash != domain.ArgsDigest(pr.Args) {
		t.Fatalf("audit = %+v", e)
	}
	pending, err := f.svc.Pending(bg, "ast_1", p)
	if err != nil || len(pending) != 1 || pending[0].ID != pr.ActionID || pending[0].State != domain.ActionPending {
		t.Fatalf("pending = %+v, %v", pending, err)
	}

	done, err := f.confirm(p, pr.ActionID, true)
	if err != nil {
		t.Fatal(err)
	}
	ch := done.Data.(*Changed)
	if ch.Ticket == nil || ch.Ticket.Title != "Rotate the keys" || ch.Ticket.WorkMode != "human" || ch.Ticket.PlannedEnd != "2026-03-12" || len(ch.Ticket.LabelIDs) != 1 {
		t.Fatalf("changed = %+v", ch)
	}
	got := f.tickets(a.ID)
	if len(got) != 1 || got[0].ID != ch.Ticket.ID || got[0].State != domain.TaskBacklog {
		t.Fatalf("tickets = %+v", got)
	}
	if got[0].Orchestration.Enabled {
		t.Error("creating a ticket must not set anything up to start by itself")
	}

	var act *domain.AssistantAction
	_ = f.db.View(bg, func(tx store.Tx) (err error) { act, err = tx.Assistant().GetAction(bg, pr.ActionID); return })
	if act.State != domain.ActionExecuted || act.ResolvedAt == nil || !strings.Contains(act.Outcome, "Created ticket") {
		t.Fatalf("action = %+v", act)
	}
	var outcomes []string
	for _, e := range f.audit() {
		if e.ActionID == pr.ActionID {
			outcomes = append(outcomes, e.Outcome)
		}
	}
	if strings.Join(outcomes, ",") != "proposed,confirmed,ok" {
		t.Fatalf("the trail of a change is proposed, confirmed, then done: %v", outcomes)
	}

	// It cannot be confirmed, or declined, again.
	_, err = f.confirm(p, pr.ActionID, true)
	wantCode(t, err, CodeNotPending)
	_, err = f.confirm(p, pr.ActionID, false)
	wantCode(t, err, CodeNotPending)
	if got := len(f.tickets(a.ID)); got != 1 {
		t.Fatalf("a second confirmation created another ticket: %d", got)
	}
	if rep, err := f.svc.VerifyAudit(bg); err != nil || rep.BrokenAt != 0 || rep.Entries < 3 {
		t.Fatalf("audit = %+v, %v", rep, err)
	}
}

func TestDecliningDropsTheChange(t *testing.T) {
	f := newFx(t)
	p := f.session("ast_1")
	a := f.project("Alpha")
	res := f.mustCall(p, "create_ticket", map[string]any{"projectId": a.ID, "title": "Not wanted"})
	out, err := f.confirm(p, res.Pending.ActionID, false)
	if err != nil {
		t.Fatal(err)
	}
	if out.Data.(map[string]any)["state"] != domain.ActionRejected {
		t.Fatalf("out = %+v", out)
	}
	if len(f.tickets(a.ID)) != 0 {
		t.Fatal("a declined change was carried out")
	}
	if e := f.lastAudit(); e.Outcome != domain.AuditOutcomeRejected || !strings.Contains(e.Detail, "owner") {
		t.Errorf("audit = %+v", e)
	}
	_, err = f.confirm(p, res.Pending.ActionID, true)
	wantCode(t, err, CodeNotPending)
}

func TestConfirmingTwiceAtOnceCarriesTheChangeOutOnce(t *testing.T) {
	f := newFx(t)
	p := f.session("ast_1")
	a := f.project("Alpha")
	res := f.mustCall(p, "create_ticket", map[string]any{"projectId": a.ID, "title": "Once only"})

	var wg sync.WaitGroup
	var ok, refused atomic.Int32
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := f.confirm(p, res.Pending.ActionID, true); err == nil {
				ok.Add(1)
			} else if CodeOf(err) == CodeNotPending {
				refused.Add(1)
			} else {
				t.Errorf("unexpected: %v", err)
			}
		}()
	}
	wg.Wait()
	if ok.Load() != 1 || refused.Load() != 7 || len(f.tickets(a.ID)) != 1 {
		t.Fatalf("%d confirmations went through, %d were refused, %d tickets exist", ok.Load(), refused.Load(), len(f.tickets(a.ID)))
	}
}

func TestAProposalExpires(t *testing.T) {
	f := newFx(t)
	p := f.session("ast_1")
	a := f.project("Alpha")
	res := f.mustCall(p, "create_ticket", map[string]any{"projectId": a.ID, "title": "Too late"})
	f.advance(11 * time.Minute)

	if pending, _ := f.svc.Pending(bg, "ast_1", p); len(pending) != 0 {
		t.Fatalf("an expired proposal is not offered: %+v", pending)
	}
	_, err := f.confirm(p, res.Pending.ActionID, true)
	wantCode(t, err, CodeNotPending) // Pending() already settled it as expired

	res = f.mustCall(p, "create_ticket", map[string]any{"projectId": a.ID, "title": "Too late again"})
	f.advance(10*time.Minute + time.Second)
	_, err = f.confirm(p, res.Pending.ActionID, true)
	wantCode(t, err, CodeExpired)
	if len(f.tickets(a.ID)) != 0 {
		t.Fatal("an expired change was carried out")
	}
	var act *domain.AssistantAction
	_ = f.db.View(bg, func(tx store.Tx) (err error) { act, err = tx.Assistant().GetAction(bg, res.Pending.ActionID); return })
	if act.State != domain.ActionExpired {
		t.Fatalf("action = %+v", act)
	}
}

func TestAnIdenticalProposalIsNotRepeatedAndTheQueueIsBounded(t *testing.T) {
	f := newFx(t)
	p := f.session("ast_1")
	a := f.project("Alpha")
	r1 := f.mustCall(p, "create_ticket", map[string]any{"projectId": a.ID, "title": "Same"})
	r2 := f.mustCall(p, "create_ticket", map[string]any{"title": "Same", "projectId": a.ID}) // key order does not matter
	if r1.Pending.ActionID != r2.Pending.ActionID {
		t.Fatalf("the same request twice made two proposals: %s %s", r1.Pending.ActionID, r2.Pending.ActionID)
	}
	for i := 0; i < 9; i++ {
		f.mustCall(p, "create_ticket", map[string]any{"projectId": a.ID, "title": strings.Repeat("n", i+1)})
	}
	_, err := f.call(p, "create_ticket", map[string]any{"projectId": a.ID, "title": "one too many"})
	wantCode(t, err, CodeTooManyPending)
	// Repeating one that is already waiting is still fine when the queue is full.
	if r := f.mustCall(p, "create_ticket", map[string]any{"projectId": a.ID, "title": "Same"}); r.Pending.ActionID != r1.Pending.ActionID {
		t.Error("a repeat of a waiting proposal must return it")
	}
}

func TestOnlyTheProposingSessionCanConfirm(t *testing.T) {
	f := newFx(t)
	mine, other := f.session("ast_mine"), f.session("ast_other")
	a := f.project("Alpha")
	res := f.mustCall(mine, "create_ticket", map[string]any{"projectId": a.ID, "title": "Mine"})
	id := res.Pending.ActionID

	for name, d := range map[string]Decision{
		"another session, its own principal":   {SessionID: "ast_other", ActionID: id, Approve: true, Executor: other},
		"my session id, another principal":     {SessionID: "ast_mine", ActionID: id, Approve: true, Executor: other},
		"another session id, my principal":     {SessionID: "ast_other", ActionID: id, Approve: true, Executor: mine},
		"my principal forged into a new shape": {SessionID: "ast_mine", ActionID: id, Approve: true, Executor: Principal{ID: mine.ID, SessionID: "ast_other", Grants: AllPermissions}},
		"a made-up action":                     {SessionID: "ast_mine", ActionID: "act_nothing", Approve: true, Executor: mine},
	} {
		_, err := f.svc.Resolve(bg, d)
		if CodeOf(err) != CodeNotFound {
			t.Errorf("%s: err = %v, want not_found", name, err)
		}
	}
	if len(f.tickets(a.ID)) != 0 {
		t.Fatal("someone else's confirmation carried the change out")
	}
	// A proposal of the other session is invisible to this one as well.
	if pending, _ := f.svc.Pending(bg, "ast_other", other); len(pending) != 0 {
		t.Errorf("pending = %+v", pending)
	}
	if _, err := f.confirm(mine, id, true); err != nil {
		t.Fatalf("the proposer can still confirm: %v", err)
	}
}

func TestPermissionsAreCheckedAgainWhenThePersonConfirms(t *testing.T) {
	f := newFx(t)
	p := f.session("ast_1")
	a, b := f.project("Alpha"), f.project("Beta")
	res := f.mustCall(p, "create_ticket", map[string]any{"projectId": a.ID, "title": "Was allowed"})

	// Between the proposal and the confirmation the assistant loses its grant to create tickets.
	narrowed := p
	narrowed.Grants = []Permission{PermTicketsRead}
	_, err := f.confirm(narrowed, res.Pending.ActionID, true)
	wantCode(t, err, CodePermissionDenied)
	if len(f.tickets(a.ID)) != 0 {
		t.Fatal("a change was carried out without the permission")
	}
	var act *domain.AssistantAction
	_ = f.db.View(bg, func(tx store.Tx) (err error) { act, err = tx.Assistant().GetAction(bg, res.Pending.ActionID); return })
	if act.State != domain.ActionFailed {
		t.Fatalf("action = %+v", act)
	}

	// The same for a project the principal no longer sees.
	res = f.mustCall(p, "create_ticket", map[string]any{"projectId": b.ID, "title": "Now off limits"})
	scoped := p
	scoped.Projects = []string{a.ID}
	_, err = f.confirm(scoped, res.Pending.ActionID, true)
	wantCode(t, err, CodeNotFound)
	if len(f.tickets(b.ID)) != 0 {
		t.Fatal("a change was carried out in a project outside the principal's")
	}
}

func TestAStoredProposalThatWasTamperedWithIsNotCarriedOut(t *testing.T) {
	f := newFx(t)
	p := f.session("ast_1")
	a := f.project("Alpha")
	res := f.mustCall(p, "create_ticket", map[string]any{"projectId": a.ID, "title": "As shown"})

	raw, err := sql.Open("sqlite", f.path)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	if _, err := raw.Exec(`UPDATE assistant_actions SET args = '{"projectId":"`+a.ID+`","title":"Something else"}' WHERE id = ?`, res.Pending.ActionID); err != nil {
		t.Fatal(err)
	}
	_, err = f.confirm(p, res.Pending.ActionID, true)
	wantCode(t, err, CodeFailed)
	if len(f.tickets(a.ID)) != 0 {
		t.Fatal("arguments that differ from what was shown were carried out")
	}
}

// ---- update_ticket ----

func TestUpdateTicketUsesVersionsAndShowsExactlyWhatChanges(t *testing.T) {
	f := newFx(t)
	p := f.session("ast_1")
	a := f.project("Alpha")
	tk := f.ticket(a.ID, "Old title")

	res := f.mustCall(p, "update_ticket", map[string]any{"projectId": a.ID, "ticketId": tk.ID, "title": "New title", "state": "doing", "plannedStart": "2026-04-01", "plannedEnd": "2026-04-03"})
	for _, want := range []string{`"Old title" → "New title"`, "moved backlog → doing", "planned nothing → 2026-04-01 to 2026-04-03"} {
		if !strings.Contains(res.Pending.Summary, want) {
			t.Errorf("summary lacks %q: %s", want, res.Pending.Summary)
		}
	}
	var args map[string]any
	_ = json.Unmarshal(res.Pending.Args, &args)
	if int64(args["expectedVersion"].(float64)) != tk.Version {
		t.Errorf("the version it will be applied to is the one that was read: %v", args)
	}

	// Someone else changes the ticket after it was proposed: the confirmation does not overwrite their change.
	if _, err := f.tasks.Update(bg, tk.ID, service.TaskPatch{Version: tk.Version, Description: ptr("edited meanwhile")}); err != nil {
		t.Fatal(err)
	}
	_, err := f.confirm(p, res.Pending.ActionID, true)
	wantCode(t, err, CodeConflict)
	got := f.tickets(a.ID)[0]
	if got.Title != "Old title" || got.Description != "edited meanwhile" {
		t.Fatalf("a stale confirmation overwrote the ticket: %+v", got)
	}
	var act *domain.AssistantAction
	_ = f.db.View(bg, func(tx store.Tx) (err error) { act, err = tx.Assistant().GetAction(bg, res.Pending.ActionID); return })
	if act.State != domain.ActionFailed || !strings.Contains(act.Outcome, "version") {
		t.Fatalf("action = %+v", act)
	}

	// A fresh proposal on the new version goes through, and moving it starts nothing.
	res = f.mustCall(p, "update_ticket", map[string]any{"projectId": a.ID, "ticketId": tk.ID, "state": "review"})
	if _, err := f.confirm(p, res.Pending.ActionID, true); err != nil {
		t.Fatal(err)
	}
	got = f.tickets(a.ID)[0]
	if got.State != domain.TaskReview || got.Title != "Old title" {
		t.Fatalf("got %+v", got)
	}
	if runs, _ := f.runs.ListByTask(bg, tk.ID); len(runs) != 0 {
		t.Fatalf("moving a ticket started a run: %+v", runs)
	}

	// Clearing a date, and the rest of the checks made while proposing.
	res = f.mustCall(p, "update_ticket", map[string]any{"projectId": a.ID, "ticketId": tk.ID, "plannedStart": "2026-05-01", "plannedEnd": "2026-05-02"})
	if _, err := f.confirm(p, res.Pending.ActionID, true); err != nil {
		t.Fatal(err)
	}
	res = f.mustCall(p, "update_ticket", map[string]any{"projectId": a.ID, "ticketId": tk.ID, "plannedStart": "", "plannedEnd": ""})
	if _, err := f.confirm(p, res.Pending.ActionID, true); err != nil {
		t.Fatal(err)
	}
	if got = f.tickets(a.ID)[0]; !got.Plan.IsZero() {
		t.Fatalf("an empty date clears the plan: %+v", got.Plan)
	}
	cur := f.tickets(a.ID)[0]
	for name, c := range map[string]struct {
		args map[string]any
		code string
	}{
		"stale version":    {map[string]any{"projectId": a.ID, "ticketId": tk.ID, "title": "x", "expectedVersion": 1}, CodeConflict},
		"nothing changes":  {map[string]any{"projectId": a.ID, "ticketId": tk.ID, "title": cur.Title}, CodeInvalidArguments},
		"nothing at all":   {map[string]any{"projectId": a.ID, "ticketId": tk.ID}, CodeInvalidArguments},
		"blank title":      {map[string]any{"projectId": a.ID, "ticketId": tk.ID, "title": "   "}, CodeInvalidArguments},
		"unknown label":    {map[string]any{"projectId": a.ID, "ticketId": tk.ID, "labelIds": []string{"lbl_nope"}}, CodeInvalidArguments},
		"end before start": {map[string]any{"projectId": a.ID, "ticketId": tk.ID, "plannedStart": "2026-06-10", "plannedEnd": "2026-06-01"}, CodeInvalidArguments},
		"missing ticket":   {map[string]any{"projectId": a.ID, "ticketId": "tsk_none", "title": "x"}, CodeNotFound},
	} {
		if _, err := f.call(p, "update_ticket", c.args); CodeOf(err) != c.code {
			t.Errorf("%s: err = %v, want %s", name, err, c.code)
		}
	}
}

func TestAnArchivedTicketCannotBeChanged(t *testing.T) {
	f := newFx(t)
	p := f.session("ast_1")
	a := f.project("Alpha")
	tk := f.ticket(a.ID, "Gone")
	if _, err := f.tasks.Update(bg, tk.ID, service.TaskPatch{Version: tk.Version, Archived: ptr(true)}); err != nil {
		t.Fatal(err)
	}
	_, err := f.call(p, "update_ticket", map[string]any{"projectId": a.ID, "ticketId": tk.ID, "title": "back"})
	wantCode(t, err, CodeConflict)
	res := f.mustCall(p, "list_tickets", map[string]any{"projectId": a.ID})
	if got := res.Data.(map[string]any)["tickets"].([]TicketView); len(got) != 0 {
		t.Errorf("archived tickets are not listed by default: %+v", got)
	}
	res = f.mustCall(p, "list_tickets", map[string]any{"projectId": a.ID, "includeArchived": true})
	if got := res.Data.(map[string]any)["tickets"].([]TicketView); len(got) != 1 || !got[0].Archived {
		t.Errorf("archived tickets can be asked for: %+v", got)
	}
}

// ---- answer_question ----

func TestAnsweringAQuestionGoesThroughTheRunnerPathAfterConfirmation(t *testing.T) {
	f := newFx(t)
	p := f.session("ast_1")
	a := f.project("Alpha")
	q := f.question(a.ID, service.NewQuestion{Kind: domain.QuestionSelection, Prompt: "Which database?", Options: []string{"Postgres", "SQLite"}})

	for name, answer := range map[string]string{"not an option": "MySQL", "blank": "   "} {
		if _, err := f.call(p, "answer_question", map[string]any{"projectId": a.ID, "questionId": q.ID, "answer": answer}); CodeOf(err) != CodeInvalidArguments {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	res := f.mustCall(p, "answer_question", map[string]any{"projectId": a.ID, "questionId": q.ID, "answer": "sqlite"})
	if !strings.Contains(res.Pending.Summary, `"Which database?"`) || !strings.Contains(res.Pending.Summary, `"SQLite"`) {
		t.Errorf("summary = %s", res.Pending.Summary)
	}
	if len(f.answers) != 0 {
		t.Fatal("the agent was answered before the person confirmed")
	}
	if got, _ := f.runs.GetQuestion(bg, q.ID); !got.Pending() {
		t.Fatalf("the question changed before confirmation: %+v", got)
	}
	out, err := f.confirm(p, res.Pending.ActionID, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.answers) != 1 || f.answers[0] != "SQLite" {
		t.Fatalf("the agent was given %v; the answer is recorded as the option is spelled", f.answers)
	}
	if ch := out.Data.(*Changed); ch.Question == nil || ch.Question.State != "answered" {
		t.Fatalf("changed = %+v", ch)
	}
	// A question that is no longer open cannot be proposed an answer.
	_, err = f.call(p, "answer_question", map[string]any{"projectId": a.ID, "questionId": q.ID, "answer": "Postgres"})
	wantCode(t, err, CodeConflict)
}

func TestTheAssistantCannotGrantPermissionToAnAgent(t *testing.T) {
	f := newFx(t)
	p := f.session("ast_1")
	a := f.project("Alpha")
	q := f.question(a.ID, service.NewQuestion{Kind: domain.QuestionApproval, Prompt: "Run rm -rf build?", Context: "$ rm -rf build", Options: []string{"Allow", "Deny"}})

	res := f.mustCall(p, "list_agent_questions", map[string]any{})
	qs := res.Data.(map[string]any)["questions"].([]QuestionView)
	if len(qs) != 1 || qs[0].AnswerableHere || qs[0].Note == "" {
		t.Fatalf("an approval is listed but marked as the person's: %+v", qs)
	}
	for _, answer := range []string{"Allow", "Deny"} {
		_, err := f.call(p, "answer_question", map[string]any{"projectId": a.ID, "questionId": q.ID, "answer": answer})
		wantCode(t, err, CodeApprovalIsPersons)
	}
	if len(f.answers) != 0 {
		t.Fatal("an approval reached the agent")
	}
	if got, _ := f.runs.GetQuestion(bg, q.ID); !got.Pending() {
		t.Fatalf("the approval changed: %+v", got)
	}
	if e := f.lastAudit(); e.Outcome != domain.AuditOutcomeDenied || e.Operation != "answer_question" {
		t.Errorf("audit = %+v", e)
	}

	// Even a proposal planted straight into the store, as if an earlier version had allowed it, is refused when run.
	args := json.RawMessage(`{"answer":"Allow","projectId":"` + a.ID + `","questionId":"` + q.ID + `"}`)
	act := &domain.AssistantAction{ID: "act_planted", SessionID: "ast_1", Principal: p.ID, Operation: "answer_question", Args: args,
		ArgsHash: domain.ArgsDigest(args), Summary: "planted", State: domain.ActionPending, CreatedAt: f.now(), ExpiresAt: f.now().Add(time.Hour)}
	if err := f.db.Update(bg, func(tx store.Tx) error { return tx.Assistant().CreateAction(bg, act) }); err != nil {
		t.Fatal(err)
	}
	_, err := f.confirm(p, "act_planted", true)
	wantCode(t, err, CodeApprovalIsPersons)
	if len(f.answers) != 0 {
		t.Fatal("an approval reached the agent through a planted proposal")
	}
}

func TestAQuestionFromAnotherProjectCannotBeAnswered(t *testing.T) {
	f := newFx(t)
	p := f.session("ast_1")
	a, b := f.project("Alpha"), f.project("Beta")
	qb := f.question(b.ID, service.NewQuestion{Prompt: "Beta?"})
	_, err := f.call(p, "answer_question", map[string]any{"projectId": a.ID, "questionId": qb.ID, "answer": "yes"})
	wantCode(t, err, CodeNotFound)
	if len(f.answers) != 0 {
		t.Fatal("answered through the wrong project")
	}
}

// ---- audit ----

func TestEveryCallIsAudited(t *testing.T) {
	f := newFx(t)
	p := f.session("ast_1", PermProjectsRead, PermTicketsRead)
	a := f.project("Alpha")
	_ = f.mustCall(p, "list_projects", map[string]any{})
	_, _ = f.call(p, "list_tickets", map[string]any{"projectId": a.ID, "limit": 0})    // invalid
	_, _ = f.call(p, "create_ticket", map[string]any{"projectId": a.ID, "title": "x"}) // no grant
	_, _ = f.call(p, "nonsense", map[string]any{})                                     // unknown

	var got []string
	for _, e := range f.audit() {
		if e.Actor != "assistant:fake" || e.SessionID != "ast_1" || e.Hash == "" {
			t.Errorf("entry = %+v", e)
		}
		got = append(got, e.Operation+":"+e.Outcome)
	}
	want := "list_projects:ok,list_tickets:invalid,create_ticket:denied,nonsense:invalid"
	if strings.Join(got, ",") != want {
		t.Fatalf("audit = %v, want %s", got, want)
	}
	// Reads put identifiers in the audit, never prose.
	if e := f.audit()[0]; e.Kind != domain.AuditRead {
		t.Errorf("kind = %s", e.Kind)
	}
	f.mustCall(f.session("ast_2"), "list_tickets", map[string]any{"projectId": a.ID, "query": "secret words"})
	if d := f.lastAudit().Detail; !strings.Contains(d, "projectId="+a.ID) {
		t.Errorf("detail = %q", d)
	}
}

func TestTheAuditChainDetectsEditsAndRemovals(t *testing.T) {
	f := newFx(t)
	p := f.session("ast_1")
	a := f.project("Alpha")
	for i := 0; i < 5; i++ {
		f.mustCall(p, "list_tickets", map[string]any{"projectId": a.ID})
	}
	if rep, err := f.svc.VerifyAudit(bg); err != nil || rep.BrokenAt != 0 || rep.Entries != 5 {
		t.Fatalf("a fresh trail verifies: %+v %v", rep, err)
	}
	raw, err := sql.Open("sqlite", f.path)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	// Even straight on the database the trail refuses to be changed...
	if _, err := raw.Exec(`DELETE FROM assistant_audit WHERE seq = 3`); err == nil {
		t.Fatal("the database allowed a line to be deleted")
	}
	// ...so to test the check, take the guard away, as someone with a copy of the file could.
	for _, q := range []string{`DROP TRIGGER assistant_audit_no_update`, `DROP TRIGGER assistant_audit_no_delete`, `UPDATE assistant_audit SET outcome = 'denied' WHERE seq = 2`} {
		if _, err := raw.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	rep, _ := f.svc.VerifyAudit(bg)
	if rep.BrokenAt != 2 || !strings.Contains(rep.Problem, "changed") {
		t.Fatalf("an edited line: %+v", rep)
	}
	if _, err := raw.Exec(`UPDATE assistant_audit SET outcome = 'ok' WHERE seq = 2`); err != nil {
		t.Fatal(err)
	}
	if rep, _ = f.svc.VerifyAudit(bg); rep.BrokenAt != 0 {
		t.Fatalf("restored: %+v", rep)
	}
	if _, err := raw.Exec(`DELETE FROM assistant_audit WHERE seq = 3`); err != nil {
		t.Fatal(err)
	}
	if rep, _ = f.svc.VerifyAudit(bg); rep.BrokenAt != 4 || !strings.Contains(rep.Problem, "removed") {
		t.Fatalf("a removed line: %+v", rep)
	}
}

func TestWhenTheAuditCannotBeWrittenNothingHappens(t *testing.T) {
	f := newFx(t)
	p := f.session("ast_1")
	a := f.project("Alpha")
	res := f.mustCall(p, "create_ticket", map[string]any{"projectId": a.ID, "title": "Audited"})

	f.st.broken.Store(true)
	// A read is refused rather than answered off the record.
	_, err := f.call(p, "list_projects", map[string]any{})
	wantCode(t, err, CodeAuditUnavailable)
	// A proposal cannot be made.
	_, err = f.call(p, "create_ticket", map[string]any{"projectId": a.ID, "title": "Unrecorded"})
	wantCode(t, err, CodeAuditUnavailable)
	// And a confirmation cannot be acted on, because its intent cannot be recorded first.
	_, err = f.confirm(p, res.Pending.ActionID, true)
	wantCode(t, err, CodeAuditUnavailable)
	if len(f.tickets(a.ID)) != 0 {
		t.Fatal("a change was carried out without an audit line")
	}
	f.st.broken.Store(false)

	// Once the audit works again the same proposal is still waiting and can be confirmed.
	if _, err := f.confirm(p, res.Pending.ActionID, true); err != nil {
		t.Fatalf("the proposal survived: %v", err)
	}
	if len(f.tickets(a.ID)) != 1 {
		t.Fatal("not carried out")
	}
}

// ---- recovery ----

func TestRecoverSettlesWhatARestartLeftInDoubt(t *testing.T) {
	f := newFx(t)
	p := f.session("ast_1")
	a := f.project("Alpha")
	keep := f.mustCall(p, "create_ticket", map[string]any{"projectId": a.ID, "title": "Still wanted"})
	stale := f.mustCall(p, "create_ticket", map[string]any{"projectId": a.ID, "title": "Went stale"})
	f.advance(9 * time.Minute)
	keep2 := f.mustCall(p, "create_ticket", map[string]any{"projectId": a.ID, "title": "Newer"})
	f.advance(2 * time.Minute) // stale is now past its time; keep2 is not

	// A change that was claimed and then the controller died.
	if err := f.db.Update(bg, func(tx store.Tx) error {
		return tx.Assistant().TransitionAction(bg, keep.Pending.ActionID, domain.ActionPending, domain.ActionExecuting, "", f.now())
	}); err != nil {
		t.Fatal(err)
	}

	expired, interrupted, err := f.svc.Recover(bg)
	if err != nil || expired != 1 || interrupted != 1 {
		t.Fatalf("expired %d, interrupted %d, %v", expired, interrupted, err)
	}
	state := func(id string) *domain.AssistantAction {
		var act *domain.AssistantAction
		_ = f.db.View(bg, func(tx store.Tx) (err error) { act, err = tx.Assistant().GetAction(bg, id); return })
		return act
	}
	if a := state(keep.Pending.ActionID); a.State != domain.ActionFailed || !strings.Contains(a.Outcome, "may or may not have been applied") {
		t.Errorf("an interrupted change is failed with a warning: %+v", a)
	}
	if a := state(stale.Pending.ActionID); a.State != domain.ActionExpired {
		t.Errorf("a stale proposal expires: %+v", a)
	}
	if a := state(keep2.Pending.ActionID); a.State != domain.ActionPending {
		t.Errorf("a proposal still within its time survives the restart: %+v", a)
	}
	if _, err := f.confirm(p, keep2.Pending.ActionID, true); err != nil {
		t.Fatalf("and can be confirmed after it: %v", err)
	}
	if got := f.tickets(a.ID); len(got) != 1 || got[0].Title != "Newer" {
		t.Fatalf("only the surviving proposal was carried out: %+v", got)
	}
	// Recovery is idempotent.
	if e, i, err := f.svc.Recover(bg); err != nil || e != 0 || i != 0 {
		t.Fatalf("second recovery: %d %d %v", e, i, err)
	}
}

func TestWithdrawDropsEverythingASessionLeftWaiting(t *testing.T) {
	f := newFx(t)
	p := f.session("ast_1")
	other := f.session("ast_2")
	a := f.project("Alpha")
	r1 := f.mustCall(p, "create_ticket", map[string]any{"projectId": a.ID, "title": "one"})
	f.mustCall(p, "create_ticket", map[string]any{"projectId": a.ID, "title": "two"})
	r3 := f.mustCall(other, "create_ticket", map[string]any{"projectId": a.ID, "title": "not mine"})

	n, err := f.svc.Withdraw(bg, "ast_1", "the conversation was restarted", p)
	if err != nil || n != 2 {
		t.Fatalf("withdrew %d, %v", n, err)
	}
	_, err = f.confirm(p, r1.Pending.ActionID, true)
	wantCode(t, err, CodeNotPending)
	if len(f.tickets(a.ID)) != 0 {
		t.Fatal("a withdrawn change was carried out")
	}
	if _, err := f.confirm(other, r3.Pending.ActionID, true); err != nil {
		t.Fatalf("another session's proposal is untouched: %v", err)
	}
}

// ---- results ----

func TestAnAnswerThatIsTooLargeIsRefusedNotTruncatedSilently(t *testing.T) {
	f := newFx(t)
	p := f.session("ast_1")
	f.svc.ops["huge"] = &operation{
		Spec: Spec{Name: "huge", Kind: KindRead, Permission: PermProjectsRead, Input: Object(map[string]*Schema{})},
		run: func(context.Context, *env, map[string]any) (any, error) {
			return strings.Repeat("x", maxResultBytes+1), nil
		},
	}
	_, err := f.call(p, "huge", map[string]any{})
	wantCode(t, err, CodeTooLarge)
}

func TestServiceErrorsAreReportedWithoutInternals(t *testing.T) {
	f := newFx(t)
	p := f.session("ast_1")
	f.svc.ops["boom"] = &operation{
		Spec: Spec{Name: "boom", Kind: KindRead, Permission: PermProjectsRead, Input: Object(map[string]*Schema{})},
		run: func(context.Context, *env, map[string]any) (any, error) {
			return nil, errors.New("sqlite: table tasks is locked at /Users/x/db")
		},
	}
	_, err := f.call(p, "boom", map[string]any{})
	wantCode(t, err, CodeFailed)
	if strings.Contains(err.Error(), "/Users") || strings.Contains(err.Error(), "sqlite") {
		t.Errorf("an unexpected error must not leak its internals: %v", err)
	}
	if e := f.lastAudit(); e.Outcome != domain.AuditOutcomeFailed || strings.Contains(e.Detail, "/Users") {
		t.Errorf("audit = %+v", e)
	}
}

func ptr[T any](v T) *T { return &v }

// ---- retention ----

func TestOldAuditIsPrunedAsAPrefixAndWhatRemainsStillVerifies(t *testing.T) {
	f := newFx(t)
	p := f.session("ast_1")
	a := f.project("Alpha")
	for i := 0; i < 4; i++ {
		f.mustCall(p, "list_projects", map[string]any{})
	}
	f.advance(40 * 24 * time.Hour)
	for i := 0; i < 3; i++ {
		f.mustCall(p, "list_tickets", map[string]any{"projectId": a.ID})
	}
	before := len(f.audit())

	// Within the retention period nothing goes, and a period shorter than the floor is raised to it.
	if n, err := f.svc.PruneAudit(bg, 100*24*time.Hour); err != nil || n != 0 || len(f.audit()) != before {
		t.Fatalf("n=%d err=%v", n, err)
	}
	n, err := f.svc.PruneAudit(bg, time.Hour)
	if err != nil || n != 4 { // four reads, all 40 days old; the floor is 30 days
		t.Fatalf("pruned %d, %v", n, err)
	}
	rest := f.audit()
	if rest[0].Operation != "list_tickets" || rest[len(rest)-1].Operation != "audit_pruned" || !strings.Contains(rest[len(rest)-1].Detail, "4 entries") {
		var ops []string
		for _, e := range rest {
			ops = append(ops, e.Operation)
		}
		t.Fatalf("remaining = %v", ops)
	}
	rep, err := f.svc.VerifyAudit(bg)
	if err != nil || rep.BrokenAt != 0 || rep.Pruned != 4 || rep.Entries != len(rest) {
		t.Fatalf("a pruned trail still verifies: %+v %v", rep, err)
	}
	// It goes on growing, still chained.
	f.mustCall(p, "list_projects", map[string]any{})
	if rep, _ = f.svc.VerifyAudit(bg); rep.BrokenAt != 0 {
		t.Fatalf("%+v", rep)
	}
	// Pruning again has nothing to do.
	if n, _ := f.svc.PruneAudit(bg, time.Hour); n != 0 {
		t.Fatalf("pruned %d again", n)
	}
}

func TestPruningEverythingLeavesTheChainToContinue(t *testing.T) {
	f := newFx(t)
	p := f.session("ast_1")
	f.mustCall(p, "list_projects", map[string]any{})
	f.advance(60 * 24 * time.Hour)
	// Every entry is old: all go, and the next one follows the checkpoint, not nothing.
	if n, err := f.svc.PruneAudit(bg, MinAuditRetention); err != nil || n != 1 {
		t.Fatalf("pruned %d, %v", n, err)
	}
	f.mustCall(p, "list_projects", map[string]any{})
	rep, _ := f.svc.VerifyAudit(bg)
	if rep.BrokenAt != 0 || rep.Pruned != 1 || rep.Entries != 2 {
		t.Fatalf("%+v", rep)
	}
}

func TestPruningDoesNotHideTamperingWithWhatRemains(t *testing.T) {
	f := newFx(t)
	p := f.session("ast_1")
	for i := 0; i < 3; i++ {
		f.mustCall(p, "list_projects", map[string]any{})
	}
	f.advance(45 * 24 * time.Hour)
	for i := 0; i < 4; i++ {
		f.mustCall(p, "list_projects", map[string]any{})
	}
	if _, err := f.svc.PruneAudit(bg, MinAuditRetention); err != nil {
		t.Fatal(err)
	}
	raw, err := sql.Open("sqlite", f.path)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()

	// The database refuses to remove anything that retention has not been asked to.
	if _, err := raw.Exec(`DELETE FROM assistant_audit`); err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("a delete outside a pruning must be refused: %v", err)
	}
	// Someone with the file can still take the guard away; the chain notices.
	mustExec := func(q string, a ...any) {
		t.Helper()
		if _, err := raw.Exec(q, a...); err != nil {
			t.Fatal(err)
		}
	}
	mustExec(`DROP TRIGGER assistant_audit_no_delete`)
	mustExec(`DROP TRIGGER assistant_audit_no_update`)
	var first, second int64
	_ = raw.QueryRow(`SELECT MIN(seq) FROM assistant_audit`).Scan(&first)
	_ = raw.QueryRow(`SELECT MIN(seq) FROM assistant_audit WHERE seq > ?`, first).Scan(&second)

	mustExec(`DELETE FROM assistant_audit WHERE seq = ?`, first) // the oldest remaining one
	if rep, _ := f.svc.VerifyAudit(bg); rep.BrokenAt != second || !strings.Contains(rep.Problem, "where retention stopped") {
		t.Fatalf("removing the oldest remaining entry: %+v", rep)
	}
	mustExec(`UPDATE assistant_audit_checkpoint SET hash = 'forged'`)
	if rep, _ := f.svc.VerifyAudit(bg); rep.BrokenAt == 0 {
		t.Fatalf("a forged checkpoint: %+v", rep)
	}
}
