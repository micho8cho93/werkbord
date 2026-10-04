package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

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

func TestQuestionKindDefaultsToAsk(t *testing.T) {
	db, _ := openTemp(t)
	p := seedProject(t, db)
	run := mustRun(t, db, newRun(p.ID, mustTask(t, db, p.ID).ID, domain.RunRunning))
	q := &domain.Question{ID: domain.NewID(domain.PrefixQuestion), RunID: run.ID, Prompt: "ok?", Status: domain.QuestionPending, CreatedAt: now()}
	ap := &domain.Question{ID: domain.NewID(domain.PrefixQuestion), RunID: run.ID, Kind: domain.QuestionApproval, Prompt: "run ls?", Status: domain.QuestionPending, CreatedAt: now()}
	if err := db.Update(ctx, func(tx store.Tx) error {
		return errors.Join(tx.Questions().Create(ctx, q), tx.Questions().Create(ctx, ap))
	}); err != nil {
		t.Fatal(err)
	}
	var got []domain.Question
	_ = db.View(ctx, func(tx store.Tx) error {
		got, _ = tx.Questions().ListByRun(ctx, run.ID)
		return nil
	})
	if len(got) != 2 || got[0].Kind != domain.QuestionAsk || got[1].Kind != domain.QuestionApproval {
		t.Fatalf("questions = %+v", got)
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
	if err := raw.QueryRow(`SELECT kind FROM questions WHERE id = 'q'`).Scan(&kind); err != nil || kind != "ask" {
		t.Fatalf("question kind = %q, %v", kind, err)
	}
}
