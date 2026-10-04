package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"devboard/internal/domain"
	"devboard/internal/store"
)

func mustRun(t *testing.T, db *DB, r *domain.Run) *domain.Run {
	t.Helper()
	if err := db.Update(ctx, func(tx store.Tx) error { return tx.Runs().Create(ctx, r) }); err != nil {
		t.Fatal(err)
	}
	return r
}

func newRun(projectID, taskID string, state domain.RunState) *domain.Run {
	r := &domain.Run{ID: domain.NewID(domain.PrefixRun), TaskID: taskID, ProjectID: projectID, AgentID: "fake", State: state, CreatedAt: now(), UpdatedAt: now()}
	if state == domain.RunWaitingForUser {
		r.Waiting = domain.WaitIdle
	}
	return r
}

func TestRunRuntimeFieldsRoundTrip(t *testing.T) {
	db, _ := openTemp(t)
	p := seedProject(t, db)
	task := mustTask(t, db, p.ID)

	code := 3
	at := now()
	r := newRun(p.ID, task.ID, domain.RunRunning)
	r.Prompt, r.Activity, r.ActivityAt, r.PID, r.ProcessID = "fix it", "editing main.go", &at, 4242, "Sat Oct  4 09:00:00 2026"
	mustRun(t, db, r)

	got := mustGetRun(t, db, r.ID)
	if got.Prompt != "fix it" || got.Activity != "editing main.go" || got.ActivityAt == nil || !got.ActivityAt.Equal(at) ||
		got.PID != 4242 || got.ProcessID != r.ProcessID || got.ExitCode != nil {
		t.Fatalf("round trip lost fields: %+v", got)
	}

	// Waiting, then ended with an exit code.
	if err := db.Update(ctx, func(tx store.Tx) error {
		if err := got.WaitFor(domain.WaitQuestion, now()); err != nil {
			return err
		}
		return tx.Runs().Update(ctx, got)
	}); err != nil {
		t.Fatal(err)
	}
	if w := mustGetRun(t, db, r.ID); w.State != domain.RunWaitingForUser || w.Waiting != domain.WaitQuestion {
		t.Fatalf("waiting not stored: %+v", w)
	}
	got = mustGetRun(t, db, r.ID)
	got.ExitCode = &code
	if err := db.Update(ctx, func(tx store.Tx) error {
		if err := got.Transition(domain.RunFailed, "exit 3", now()); err != nil {
			return err
		}
		return tx.Runs().Update(ctx, got)
	}); err != nil {
		t.Fatal(err)
	}
	end := mustGetRun(t, db, r.ID)
	if end.ExitCode == nil || *end.ExitCode != 3 || end.PID != 0 || end.Waiting != domain.WaitNone || end.EndedAt == nil {
		t.Fatalf("ended run: %+v", end)
	}
}

func mustGetRun(t *testing.T, db *DB, id string) *domain.Run {
	t.Helper()
	var r *domain.Run
	if err := db.View(ctx, func(tx store.Tx) error {
		var err error
		r, err = tx.Runs().Get(ctx, id)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestDatabaseRefusesInconsistentRuns(t *testing.T) {
	db, _ := openTemp(t)
	p := seedProject(t, db)
	task := mustTask(t, db, p.ID)
	create := func(r *domain.Run) error {
		return db.Update(ctx, func(tx store.Tx) error { return tx.Runs().Create(ctx, r) })
	}

	// Waiting without saying what for, and saying what for without waiting.
	bad := newRun(p.ID, task.ID, domain.RunWaitingForUser)
	bad.Waiting = domain.WaitNone
	if err := create(bad); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("waiting run without a kind: err = %v", err)
	}
	bad = newRun(p.ID, task.ID, domain.RunRunning)
	bad.Waiting = domain.WaitIdle
	if err := create(bad); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("running run with a waiting kind: err = %v", err)
	}

	// An ended run cannot still own a process, however the row is written.
	live := mustRun(t, db, func() *domain.Run { r := newRun(p.ID, task.ID, domain.RunRunning); r.PID = 99; return r }())
	live.State, live.UpdatedAt = domain.RunFailed, now() // PID deliberately left set
	err := db.Update(ctx, func(tx store.Tx) error { return tx.Runs().Update(ctx, live) })
	if !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("ended run keeping its pid: err = %v", err)
	}
}

func TestListLatestByProject(t *testing.T) {
	db, _ := openTemp(t)
	p := seedProject(t, db)
	other := seedProject(t, db)
	a, b, never := mustTask(t, db, p.ID), mustTask(t, db, p.ID), mustTask(t, db, p.ID)
	_ = never
	o := mustTask(t, db, other.ID)

	mustRun(t, db, newRun(p.ID, a.ID, domain.RunFailed))
	second := mustRun(t, db, newRun(p.ID, a.ID, domain.RunRunning))
	only := mustRun(t, db, newRun(p.ID, b.ID, domain.RunCompleted))
	mustRun(t, db, newRun(other.ID, o.ID, domain.RunRunning))

	var got []domain.Run
	if err := db.View(ctx, func(tx store.Tx) error {
		var err error
		got, err = tx.Runs().ListLatestByProject(ctx, p.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != second.ID || got[1].ID != only.ID {
		t.Fatalf("latest runs = %+v; want the newest run of each task that has one, in this project only", got)
	}
}

func TestListEventsByRunPagesBackwards(t *testing.T) {
	db, _ := openTemp(t)
	p := seedProject(t, db)
	task := mustTask(t, db, p.ID)
	run := mustRun(t, db, newRun(p.ID, task.ID, domain.RunRunning))
	otherRun := mustRun(t, db, newRun(p.ID, mustTask(t, db, p.ID).ID, domain.RunRunning))

	var seqs []int64
	if err := db.Update(ctx, func(tx store.Tx) error {
		for i := 0; i < 7; i++ {
			e, _ := domain.NewEvent(domain.EventAgentOutput, domain.AgentOutput{Stream: domain.StreamAssistant, Text: "x"})
			e.RunID = run.ID
			if i%2 == 1 { // interleave another run's events
				o := e
				o.RunID = otherRun.ID
				if err := tx.Events().Append(ctx, &o); err != nil {
					return err
				}
			}
			if err := tx.Events().Append(ctx, &e); err != nil {
				return err
			}
			seqs = append(seqs, e.Seq)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	page := func(before int64, limit int) []domain.Event {
		var out []domain.Event
		if err := db.View(ctx, func(tx store.Tx) error {
			var err error
			out, err = tx.Events().ListByRun(ctx, run.ID, before, limit)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return out
	}
	newest := page(0, 3)
	if len(newest) != 3 || newest[0].Seq != seqs[4] || newest[2].Seq != seqs[6] {
		t.Fatalf("newest page = %+v; want the last three of this run, ascending", newest)
	}
	older := page(newest[0].Seq, 3)
	if len(older) != 3 || older[0].Seq != seqs[1] || older[2].Seq != seqs[3] {
		t.Fatalf("older page = %+v", older)
	}
	oldest := page(older[0].Seq, 3)
	if len(oldest) != 1 || oldest[0].Seq != seqs[0] {
		t.Fatalf("oldest page = %+v", oldest)
	}
	for _, e := range append(append(newest, older...), oldest...) {
		if e.RunID != run.ID {
			t.Fatalf("event of another run leaked into the page: %+v", e)
		}
	}
}

func TestQuestionRoundTripsEveryField(t *testing.T) {
	db, _ := openTemp(t)
	p := seedProject(t, db)
	task := mustTask(t, db, p.ID)
	run := mustRun(t, db, newRun(p.ID, task.ID, domain.RunRunning))
	asked := now()
	q := &domain.Question{
		ID: domain.NewID(domain.PrefixQuestion), RunID: run.ID, TaskID: task.ID, ProjectID: p.ID,
		Kind: domain.QuestionApproval, Prompt: "Run this?", Context: "$ rm -rf build\nbecause", Options: []string{"Allow", "Deny"},
		State: domain.QuestionPending, AskedAt: asked,
	}
	free := &domain.Question{
		ID: domain.NewID(domain.PrefixQuestion), RunID: run.ID, TaskID: task.ID, ProjectID: p.ID,
		Kind: domain.QuestionClarification, Prompt: "Why?", AllowFreeText: true, State: domain.QuestionPending, AskedAt: asked.Add(time.Second),
	}
	if err := db.Update(ctx, func(tx store.Tx) error {
		return errors.Join(tx.Questions().Create(ctx, q), tx.Questions().Create(ctx, free))
	}); err != nil {
		t.Fatal(err)
	}

	read := func(id string) *domain.Question {
		var got *domain.Question
		if err := db.View(ctx, func(tx store.Tx) error {
			var err error
			got, err = tx.Questions().Get(ctx, id)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return got
	}
	got := read(q.ID)
	if got.Kind != domain.QuestionApproval || got.Context != q.Context || got.AllowFreeText || len(got.Options) != 2 ||
		got.TaskID != task.ID || got.ProjectID != p.ID || !got.AskedAt.Equal(asked) || !got.Pending() {
		t.Fatalf("question = %+v", got)
	}
	if !read(free.ID).AllowFreeText {
		t.Fatal("allowFreeText lost")
	}

	// answered, then delivered
	answered := asked.Add(time.Minute)
	got.State, got.Answer, got.AnsweredAt = domain.QuestionAnswered, "Allow", &answered
	if err := db.Update(ctx, func(tx store.Tx) error { return tx.Questions().Update(ctx, got) }); err != nil {
		t.Fatal(err)
	}
	got = read(q.ID)
	if got.State != domain.QuestionAnswered || got.Answer != "Allow" || got.AnsweredAt == nil || got.DeliveredAt != nil {
		t.Fatalf("answered = %+v", got)
	}
	delivered := answered.Add(time.Second)
	got.DeliveredAt = &delivered
	if err := db.Update(ctx, func(tx store.Tx) error { return tx.Questions().Update(ctx, got) }); err != nil {
		t.Fatal(err)
	}
	if got = read(q.ID); got.DeliveredAt == nil || !got.DeliveredAt.Equal(delivered) {
		t.Fatalf("delivered = %+v", got)
	}

	// the pending list is oldest first and holds only what is pending
	var pending []domain.Question
	_ = db.View(ctx, func(tx store.Tx) error { pending, _ = tx.Questions().ListPending(ctx); return nil })
	if len(pending) != 1 || pending[0].ID != free.ID {
		t.Fatalf("pending = %+v", pending)
	}
}

// The database refuses a question that contradicts its own state, whatever wrote it.
func TestQuestionStatesAreEnforcedByTheDatabase(t *testing.T) {
	db, _ := openTemp(t)
	p := seedProject(t, db)
	task := mustTask(t, db, p.ID)
	run := mustRun(t, db, newRun(p.ID, task.ID, domain.RunRunning))
	at := now()
	base := func() *domain.Question {
		return &domain.Question{
			ID: domain.NewID(domain.PrefixQuestion), RunID: run.ID, TaskID: task.ID, ProjectID: p.ID,
			Kind: domain.QuestionClarification, Prompt: "?", AllowFreeText: true, State: domain.QuestionPending, AskedAt: at,
		}
	}
	for name, mutate := range map[string]func(q *domain.Question){
		"unknown kind":                 func(q *domain.Question) { q.Kind = "ask" },
		"pending with an answer":       func(q *domain.Question) { q.Answer = "x" },
		"pending and already closed":   func(q *domain.Question) { q.ClosedAt = &at },
		"answered without a time":      func(q *domain.Question) { q.State, q.Answer = domain.QuestionAnswered, "x" },
		"answered without an answer":   func(q *domain.Question) { q.State, q.AnsweredAt = domain.QuestionAnswered, &at },
		"cancelled without a reason":   func(q *domain.Question) { q.State, q.ClosedAt = domain.QuestionCancelled, &at },
		"cancelled without a time":     func(q *domain.Question) { q.State, q.CancelReason = domain.QuestionCancelled, domain.CancelWithdrawn },
		"unknown reason":               func(q *domain.Question) { q.State, q.ClosedAt, q.CancelReason = domain.QuestionCancelled, &at, "other" },
		"delivered but never answered": func(q *domain.Question) { q.DeliveredAt = &at },
		"cancelled yet delivered": func(q *domain.Question) {
			q.State, q.ClosedAt, q.CancelReason, q.AnsweredAt, q.DeliveredAt = domain.QuestionCancelled, &at, domain.CancelRunEnded, &at, &at
		},
		"unknown state": func(q *domain.Question) { q.State = "closed" },
	} {
		q := base()
		mutate(q)
		if err := db.Update(ctx, func(tx store.Tx) error { return tx.Questions().Create(ctx, q) }); err == nil {
			t.Errorf("%s: the database accepted %+v", name, q)
		}
	}
	// What is allowed, is accepted.
	for name, mutate := range map[string]func(q *domain.Question){
		"answered and delivered": func(q *domain.Question) {
			q.State, q.Answer, q.AnsweredAt, q.DeliveredAt = domain.QuestionAnswered, "x", &at, &at
		},
		"cancelled, answer kept": func(q *domain.Question) {
			q.State, q.Answer, q.AnsweredAt, q.ClosedAt, q.CancelReason = domain.QuestionCancelled, "x", &at, &at, domain.CancelRunEnded
		},
	} {
		q := base()
		mutate(q)
		if err := db.Update(ctx, func(tx store.Tx) error { return tx.Questions().Create(ctx, q) }); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// A database written by the previous build must upgrade in place without
// losing its runs.
func TestMigration0004KeepsExistingRuns(t *testing.T) {
	ms, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "old.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	raw.SetMaxOpenConns(1)
	if _, err := raw.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		t.Fatal(err)
	}
	if _, err := migrate(context.Background(), raw, ms[:3]); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO projects VALUES ('p', 'p', '/r', 1, 1)`,
		`INSERT INTO tasks (id, project_id, title, state, position, created_at, updated_at) VALUES ('t', 'p', 't', 'doing', 1, 1, 1)`,
		`INSERT INTO runs (id, task_id, project_id, agent_id, state, created_at, updated_at) VALUES ('r', 't', 'p', 'claude-code', 'waiting_for_user', 1, 1)`,
		`INSERT INTO questions (id, run_id, prompt, status, created_at) VALUES ('q', 'r', 'ok?', 'pending', 1)`,
	} {
		if _, err := raw.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	if _, err := migrate(context.Background(), raw, ms); err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	var state, kind string
	if err := raw.QueryRow(`SELECT state FROM runs WHERE id = 'r'`).Scan(&state); err != nil || state != "waiting_for_user" {
		t.Fatalf("run lost: %q, %v", state, err)
	}
	if err := raw.QueryRow(`SELECT kind FROM questions WHERE id = 'q'`).Scan(&kind); err != nil || kind != "clarification" {
		t.Fatalf("question kind = %q, %v (0005 renames the old 'ask')", kind, err)
	}
}

// A database from the build before first-class questions must upgrade in place:
// every question keeps its text and outcome, and learns its task and project.
func TestMigration0005KeepsExistingQuestions(t *testing.T) {
	ms, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "old.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	raw.SetMaxOpenConns(1)
	if _, err := raw.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		t.Fatal(err)
	}
	if _, err := migrate(context.Background(), raw, ms[:4]); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO projects VALUES ('p', 'p', '/r', 1, 1)`,
		`INSERT INTO tasks (id, project_id, title, state, position, created_at, updated_at) VALUES ('t', 'p', 't', 'doing', 1, 1, 1)`,
		`INSERT INTO runs (id, task_id, project_id, agent_id, state, waiting, created_at, updated_at) VALUES ('r', 't', 'p', 'claude-code', 'waiting_for_user', 'idle', 1, 1)`,
		`INSERT INTO runs (id, task_id, project_id, agent_id, state, waiting, created_at, updated_at) VALUES ('r2', 't', 'p', 'claude-code', 'waiting_for_user', 'question', 2, 2)`,
		`INSERT INTO questions (id, run_id, prompt, options, status, kind, created_at) VALUES ('pend', 'r2', 'Run it?', '["Allow","Deny"]', 'pending', 'approval', 10)`,
		`INSERT INTO questions (id, run_id, prompt, options, status, kind, created_at) VALUES ('ask', 'r', 'Which?', '["a","b"]', 'pending', 'ask', 11)`,
		`INSERT INTO questions (id, run_id, prompt, status, answer, created_at, answered_at) VALUES ('done', 'r', 'ok?', 'answered', 'yes', 12, 13)`,
		`INSERT INTO questions (id, run_id, prompt, status, created_at, answered_at) VALUES ('gone', 'r', 'still?', 'cancelled', 14, 15)`,
	} {
		if _, err := raw.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	if _, err := migrate(context.Background(), raw, ms); err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	type row struct {
		kind, state, answer, reason, task, project string
		free                                       int
		delivered, closed                          sql.NullInt64
	}
	get := func(id string) row {
		var r row
		if err := raw.QueryRow(`SELECT kind, state, answer, cancel_reason, task_id, project_id, allow_free_text, delivered_at, closed_at FROM questions WHERE id = ?`, id).
			Scan(&r.kind, &r.state, &r.answer, &r.reason, &r.task, &r.project, &r.free, &r.delivered, &r.closed); err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		return r
	}
	if r := get("pend"); r.kind != "approval" || r.state != "pending" || r.free != 0 || r.task != "t" || r.project != "p" {
		t.Errorf("a pending approval stays an approval that takes only its options: %+v", r)
	}
	if r := get("ask"); r.kind != "clarification" || r.free != 1 {
		t.Errorf("an old question becomes a clarification that takes text: %+v", r)
	}
	if r := get("done"); r.state != "answered" || r.answer != "yes" || !r.delivered.Valid {
		t.Errorf("an answered question counts as delivered: %+v", r)
	}
	if r := get("gone"); r.state != "cancelled" || r.reason != "run_ended" || !r.closed.Valid || r.delivered.Valid {
		t.Errorf("a cancelled question says why: %+v", r)
	}
	var n int
	if err := raw.QueryRow(`SELECT count(*) FROM questions`).Scan(&n); err != nil || n != 4 {
		t.Fatalf("%d questions after the upgrade, %v", n, err)
	}
	if err := raw.QueryRow(`SELECT count(*) FROM pragma_index_list('questions') WHERE name IN ('questions_by_run', 'questions_pending')`).Scan(&n); err != nil || n != 2 {
		t.Fatalf("indexes after the rebuild: %d, %v", n, err)
	}
}
