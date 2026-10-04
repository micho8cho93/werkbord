package sqlite

import (
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"devboard/internal/domain"
	"devboard/internal/store"
)

func TestTaskExecutionDefaultsAndPersists(t *testing.T) {
	db, _ := openTemp(t)
	p := seedProject(t, db)

	// A task created with nothing set overrides nothing: it inherits everything.
	plain := mustTask(t, db, p.ID)
	if got := mustGetTask(t, db, plain.ID); !got.Execution.IsZero() {
		t.Fatalf("default execution = %+v", got.Execution)
	}

	// What is chosen is stored, and can be changed or cleared.
	task := &domain.Task{ID: domain.NewID(domain.PrefixTask), ProjectID: p.ID, Title: "auto", State: domain.TaskBacklog, Position: 2,
		Execution: domain.ExecutionConfig{Agent: "codex", Model: "gpt-x", Reasoning: "high", Interaction: domain.InteractionAutonomousStopIfBlocked, Priority: domain.PriorityHigh},
		CreatedAt: now(), UpdatedAt: now()}
	mustUpdate(t, db, func(tx store.Tx) error { return tx.Tasks().Create(ctx, task) })
	if got := mustGetTask(t, db, task.ID); got.Execution != task.Execution {
		t.Fatalf("stored execution = %+v", got.Execution)
	}
	task.Execution = domain.ExecutionConfig{Interaction: domain.InteractionAutonomous}
	mustUpdate(t, db, func(tx store.Tx) error { return tx.Tasks().Update(ctx, task) })
	if got := mustGetTask(t, db, task.ID); got.Execution != task.Execution || got.Version != 2 {
		t.Fatalf("changed execution = %+v", got)
	}

	// An unknown value is refused before it reaches the database, and the database
	// refuses one that gets there by another way.
	for name, cfg := range map[string]domain.ExecutionConfig{
		"interaction":         {Interaction: "reckless"},
		"priority":            {Priority: "yesterday"},
		"model without agent": {Model: "x"},
		"model like a flag":   {Agent: "codex", Model: "--dangerous"},
	} {
		bad := &domain.Task{ID: domain.NewID(domain.PrefixTask), ProjectID: p.ID, Title: "x", State: domain.TaskBacklog, Position: 3,
			Execution: cfg, CreatedAt: now(), UpdatedAt: now()}
		if err := db.Update(ctx, func(tx store.Tx) error { return tx.Tasks().Create(ctx, bad) }); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	for _, v := range []string{`{"interaction":"reckless"}`, `{"priority":"yesterday"}`, `not json`} {
		if _, err := db.writer.ExecContext(ctx, `UPDATE tasks SET execution = ? WHERE id = ?`, v, plain.ID); err == nil {
			t.Errorf("the database accepted execution %s", v)
		}
	}
}

func TestProjectExecutionAndSettingsPersist(t *testing.T) {
	db, _ := openTemp(t)
	p := seedProject(t, db)
	if got := mustGetProject(t, db, p.ID); !got.Execution.IsZero() {
		t.Fatalf("a new project overrides %+v", got.Execution)
	}
	cfg := domain.ExecutionConfig{Agent: "claude-code", Model: "opus", Reasoning: "high", Priority: domain.PriorityLow}
	mustUpdate(t, db, func(tx store.Tx) error { return tx.Projects().SetExecution(ctx, p.ID, cfg, now()) })
	if got := mustGetProject(t, db, p.ID); got.Execution != cfg {
		t.Fatalf("project execution = %+v", got.Execution)
	}
	if err := db.Update(ctx, func(tx store.Tx) error {
		return tx.Projects().SetExecution(ctx, "prj_missing", cfg, now())
	}); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown project: err = %v", err)
	}
	if err := db.Update(ctx, func(tx store.Tx) error {
		return tx.Projects().SetExecution(ctx, p.ID, domain.ExecutionConfig{Priority: "soon"}, now())
	}); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("bad priority: err = %v", err)
	}

	// Settings: unset is not found, set round-trips, set again replaces.
	var got domain.ExecutionConfig
	if err := db.View(ctx, func(tx store.Tx) error { return tx.Settings().Get(ctx, domain.SettingExecution, &got) }); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unset setting: err = %v", err)
	}
	mustUpdate(t, db, func(tx store.Tx) error { return tx.Settings().Set(ctx, domain.SettingExecution, cfg, now()) })
	mustUpdate(t, db, func(tx store.Tx) error {
		return tx.Settings().Set(ctx, domain.SettingExecution, domain.ExecutionConfig{Interaction: domain.InteractionAutonomous}, now())
	})
	if err := db.View(ctx, func(tx store.Tx) error { return tx.Settings().Get(ctx, domain.SettingExecution, &got) }); err != nil || got.Interaction != domain.InteractionAutonomous || got.Agent != "" {
		t.Fatalf("setting = %+v, %v", got, err)
	}
}

func TestLocalRunnerIsRegisteredOnce(t *testing.T) {
	db, _ := openTemp(t)
	first := &domain.Runner{ID: "rnr_a", Name: "mac", Kind: domain.RunnerLocal, Hostname: "mac", OS: "darwin", Arch: "arm64", Version: "1", CreatedAt: now(), LastSeenAt: now()}
	var a, b *domain.Runner
	mustUpdate(t, db, func(tx store.Tx) (err error) { a, err = tx.Runners().UpsertLocal(ctx, first); return })
	// A later start refreshes the record but is the same runner.
	second := &domain.Runner{ID: "rnr_b", Name: "mac-2", Kind: domain.RunnerLocal, Hostname: "mac-2", OS: "darwin", Arch: "arm64", Version: "2", CreatedAt: now().Add(5 * time.Second), LastSeenAt: now().Add(5 * time.Second)}
	mustUpdate(t, db, func(tx store.Tx) (err error) { b, err = tx.Runners().UpsertLocal(ctx, second); return })
	if a.ID != "rnr_a" || b.ID != "rnr_a" || b.Name != "mac-2" || b.Version != "2" || !b.CreatedAt.Equal(a.CreatedAt) || !b.LastSeenAt.After(a.LastSeenAt) {
		t.Fatalf("runner = %+v then %+v", a, b)
	}
	var all []domain.Runner
	if err := db.View(ctx, func(tx store.Tx) (err error) { all, err = tx.Runners().List(ctx); return }); err != nil || len(all) != 1 {
		t.Fatalf("runners = %+v, %v", all, err)
	}
}

// TestMigrationToExecutionConfig upgrades a version-7 database: a task's chosen
// interaction policy becomes its override, one that was only ever the default
// becomes "inherit", and nothing else about the task changes.
func TestMigrationToExecutionConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v7.db")
	raw, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	raw.SetMaxOpenConns(1)
	ms, err := Migrations()
	if err != nil || len(ms) < 8 {
		t.Fatalf("migrations: %d, %v", len(ms), err)
	}
	if _, err := migrate(ctx, raw, ms[:7]); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO projects (id, name, repo_path, created_at, updated_at) VALUES ('prj_1', 'old', '/repos/old', 1, 1)`,
		`INSERT INTO tasks (id, project_id, title, description, state, position, version, created_at, updated_at, policy) VALUES
		 ('tsk_plain', 'prj_1', 'Plain', '', 'backlog', 1, 1, 1, 1, '{"interaction":"interactive"}'),
		 ('tsk_auto', 'prj_1', 'Auto', 'd', 'doing', 2, 4, 1, 1, '{"interaction":"autonomous"}'),
		 ('tsk_stop', 'prj_1', 'Stop', '', 'review', 3, 1, 1, 1, '{"interaction":"autonomous_stop_if_blocked"}')`,
		`INSERT INTO runs (id, task_id, project_id, agent_id, state, version, created_at, updated_at) VALUES ('run_1', 'tsk_auto', 'prj_1', 'codex', 'completed', 1, 1, 1)`,
	} {
		if _, err := raw.ExecContext(ctx, q); err != nil {
			t.Fatalf("seed v7: %v\n%s", err, q)
		}
	}
	_ = raw.Close()

	db, err := Open(ctx, path, nil)
	if err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	defer db.Close()
	want := map[string]domain.InteractionPolicy{"tsk_plain": "", "tsk_auto": domain.InteractionAutonomous, "tsk_stop": domain.InteractionAutonomousStopIfBlocked}
	for id, interaction := range want {
		tk := mustGetTask(t, db, id)
		if tk.Execution != (domain.ExecutionConfig{Interaction: interaction}) {
			t.Errorf("%s: execution = %+v, want interaction %q only", id, tk.Execution, interaction)
		}
	}
	if tk := mustGetTask(t, db, "tsk_auto"); tk.Title != "Auto" || tk.Description != "d" || tk.Version != 4 || tk.State != domain.TaskDoing {
		t.Errorf("task changed by the migration: %+v", tk)
	}
	if err := db.View(ctx, func(tx store.Tx) error {
		r, err := tx.Runs().Get(ctx, "run_1")
		if err != nil || r.Model != "" || r.Reasoning != "" || r.AgentID != "codex" {
			t.Errorf("run after the migration = %+v, %v", r, err)
		}
		p, err := tx.Projects().Get(ctx, "prj_1")
		if err != nil || !p.Execution.IsZero() {
			t.Errorf("project after the migration = %+v, %v", p, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var col int
	if err := db.writer.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info('tasks') WHERE name = 'policy'`).Scan(&col); err != nil || col != 0 {
		t.Errorf("tasks.policy still exists: %d, %v", col, err)
	}
}

func TestRunKeepsItsPolicyAndBlocker(t *testing.T) {
	db, _ := openTemp(t)
	p := seedProject(t, db)
	task := mustTask(t, db, p.ID)

	r := newRun(p.ID, task.ID, domain.RunRunning)
	r.Policy = domain.ExecutionPolicy{Interaction: domain.InteractionAutonomousStopIfBlocked}
	mustRun(t, db, r)
	got := mustGetRun(t, db, r.ID)
	if got.Policy.Interaction != domain.InteractionAutonomousStopIfBlocked || got.Blocker != nil {
		t.Fatalf("run = %+v", got)
	}

	// Blocking stores a structured blocker.
	if err := got.Block(domain.Blocker{Summary: "Which database?", Detail: "Both are plausible.", Options: []string{"sqlite", "postgres"},
		Source: domain.BlockerQuestion, Kind: domain.QuestionSelection}, now()); err != nil {
		t.Fatal(err)
	}
	mustUpdate(t, db, func(tx store.Tx) error { return tx.Runs().Update(ctx, got) })
	b := mustGetRun(t, db, r.ID)
	if b.State != domain.RunBlocked || b.Waiting != domain.WaitNone || b.Blocker == nil ||
		b.Blocker.Summary != "Which database?" || len(b.Blocker.Options) != 2 || b.Blocker.Source != domain.BlockerQuestion ||
		b.Blocker.Kind != domain.QuestionSelection || b.Blocker.RaisedAt.IsZero() {
		t.Fatalf("blocked run = %+v / %+v", b, b.Blocker)
	}
	if b.Policy.Interaction != domain.InteractionAutonomousStopIfBlocked {
		t.Fatalf("the run lost its policy: %+v", b.Policy)
	}

	// A blocked run is still an active one.
	var active []domain.Run
	if err := db.View(ctx, func(tx store.Tx) error { var err error; active, err = tx.Runs().ListActive(ctx); return err }); err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 || active[0].ID != r.ID {
		t.Fatalf("active = %+v", active)
	}

	// Resuming clears the blocker; the policy stays.
	if err := b.Transition(domain.RunRunning, "", now()); err != nil {
		t.Fatal(err)
	}
	mustUpdate(t, db, func(tx store.Tx) error { return tx.Runs().Update(ctx, b) })
	if again := mustGetRun(t, db, r.ID); again.State != domain.RunRunning || again.Blocker != nil {
		t.Fatalf("resumed run = %+v", again)
	}
}

func TestDatabaseRefusesABlockedRunWithoutABlocker(t *testing.T) {
	db, _ := openTemp(t)
	p := seedProject(t, db)
	task := mustTask(t, db, p.ID)
	r := newRun(p.ID, task.ID, domain.RunBlocked)
	if err := db.Update(ctx, func(tx store.Tx) error { return tx.Runs().Create(ctx, r) }); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("blocked run without a blocker: err = %v", err)
	}
}

func TestABlockedRunHoldsItsWorktree(t *testing.T) {
	db, _ := openTemp(t)
	p := seedProject(t, db)
	task := mustTask(t, db, p.ID)
	root := t.TempDir()
	w := mustWT(t, db, p.ID, filepath.Join(root, "a"), "devboard/a")

	r := newRun(p.ID, task.ID, domain.RunBlocked)
	r.WorktreeID = w.ID
	r.Blocker = &domain.Blocker{Summary: "stuck", Source: domain.BlockerReport, RaisedAt: now()}
	mustRun(t, db, r)

	// No second active run may use it, and it cannot be taken away from under it.
	second := newRun(p.ID, task.ID, domain.RunRunning)
	second.WorktreeID = w.ID
	if err := db.Update(ctx, func(tx store.Tx) error { return tx.Runs().Create(ctx, second) }); !errors.Is(err, domain.ErrDuplicate) {
		t.Errorf("second run on a blocked run's worktree: err = %v", err)
	}
	if err := beginRemoval(db, w); err == nil {
		t.Error("a worktree used by a blocked run could be removed")
	}
}

func TestQuestionRecordsWhoAnsweredIt(t *testing.T) {
	db, _ := openTemp(t)
	p := seedProject(t, db)
	task := mustTask(t, db, p.ID)
	r := mustRun(t, db, newRun(p.ID, task.ID, domain.RunRunning))

	ask := func(by domain.AnsweredBy) *domain.Question {
		q := &domain.Question{ID: domain.NewID(domain.PrefixQuestion), RunID: r.ID, TaskID: task.ID, ProjectID: p.ID, Kind: domain.QuestionClarification,
			Prompt: "which?", AllowFreeText: true, State: domain.QuestionPending, AskedAt: now()}
		mustUpdate(t, db, func(tx store.Tx) error { return tx.Questions().Create(ctx, q) })
		if by == "" {
			return q
		}
		if by == domain.AnsweredByPolicy {
			if err := q.AcceptFromPolicy("decide it yourself", now()); err != nil {
				t.Fatal(err)
			}
		} else if err := q.Accept("sqlite", now()); err != nil {
			t.Fatal(err)
		}
		mustUpdate(t, db, func(tx store.Tx) error { return tx.Questions().Update(ctx, q) })
		return q
	}
	pending, byUser, byPolicy := ask(""), ask(domain.AnsweredByUser), ask(domain.AnsweredByPolicy)

	get := func(id string) *domain.Question {
		var q *domain.Question
		if err := db.View(ctx, func(tx store.Tx) error { var err error; q, err = tx.Questions().Get(ctx, id); return err }); err != nil {
			t.Fatal(err)
		}
		return q
	}
	if q := get(pending.ID); q.AnsweredBy != "" {
		t.Errorf("a pending question has no answerer: %q", q.AnsweredBy)
	}
	if q := get(byUser.ID); q.AnsweredBy != domain.AnsweredByUser {
		t.Errorf("answered by = %q", q.AnsweredBy)
	}
	if q := get(byPolicy.ID); q.AnsweredBy != domain.AnsweredByPolicy || q.Answer != "decide it yourself" {
		t.Errorf("answered by = %q, answer %q", q.AnsweredBy, q.Answer)
	}
}

// Everything the Control Center and the project views read is filtered by the
// database, not by the caller.
func TestListsAreScopedToTheirProject(t *testing.T) {
	db, _ := openTemp(t)
	a, b := seedProject(t, db), seedProject(t, db)
	ta, tb := mustTask(t, db, a.ID), mustTask(t, db, b.ID)
	ra := mustRun(t, db, newRun(a.ID, ta.ID, domain.RunRunning))
	rb := mustRun(t, db, newRun(b.ID, tb.ID, domain.RunRunning))
	for _, r := range []*domain.Run{ra, rb} {
		q := &domain.Question{ID: domain.NewID(domain.PrefixQuestion), RunID: r.ID, TaskID: r.TaskID, ProjectID: r.ProjectID,
			Kind: domain.QuestionClarification, Prompt: "p", AllowFreeText: true, State: domain.QuestionPending, AskedAt: now()}
		mustUpdate(t, db, func(tx store.Tx) error { return tx.Questions().Create(ctx, q) })
	}

	err := db.View(ctx, func(tx store.Tx) error {
		qa, err := tx.Questions().ListPendingByProject(ctx, a.ID)
		if err != nil {
			return err
		}
		if len(qa) != 1 || qa[0].ProjectID != a.ID || qa[0].RunID != ra.ID {
			t.Errorf("project A's questions = %+v", qa)
		}
		all, _ := tx.Questions().ListPending(ctx)
		if len(all) != 2 {
			t.Errorf("all pending = %d", len(all))
		}
		hist, err := tx.Runs().ListByProject(ctx, b.ID, 10)
		if err != nil {
			return err
		}
		if len(hist) != 1 || hist[0].ID != rb.ID {
			t.Errorf("project B's history = %+v", hist)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func mustGetTask(t *testing.T, db *DB, id string) *domain.Task {
	t.Helper()
	var task *domain.Task
	if err := db.View(ctx, func(tx store.Tx) error { var err error; task, err = tx.Tasks().Get(ctx, id); return err }); err != nil {
		t.Fatal(err)
	}
	return task
}

// TestMigrationToExecutionPolicyKeepsEverything upgrades a version-5 database that
// holds a running agent session, as a user's would. Rebuilding `runs` must not
// delete its questions, detach its worktree, or change a single run, and the
// rules that hang off `runs` must still hold afterwards.
func TestMigrationToExecutionPolicyKeepsEverything(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v5.db")
	raw, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	raw.SetMaxOpenConns(1)
	ms, err := Migrations()
	if err != nil || len(ms) < 6 {
		t.Fatalf("migrations: %d, %v", len(ms), err)
	}
	if _, err := migrate(ctx, raw, ms[:5]); err != nil {
		t.Fatal(err)
	}
	wtPath := filepath.Join(t.TempDir(), "wt")
	for _, q := range []string{
		`INSERT INTO projects VALUES ('prj_1', 'old', '/repos/old', 1, 1)`,
		`INSERT INTO tasks (id, project_id, title, description, state, position, version, created_at, updated_at)
		 VALUES ('tsk_1', 'prj_1', 'Old task', 'd', 'doing', 1, 3, 1, 1), ('tsk_2', 'prj_1', 'Other', '', 'review', 1, 1, 1, 1)`,
		`INSERT INTO worktrees (id, project_id, path, branch, base_ref, state, created_at, updated_at)
		 VALUES ('wt_1', 'prj_1', '` + wtPath + `', 'devboard/old', 'main', 'active', 1, 1)`,
		`INSERT INTO runs (id, task_id, project_id, agent_id, state, worktree_id, session_ref, prompt, waiting, activity, pid, process_id, version, created_at, updated_at)
		 VALUES ('run_1', 'tsk_1', 'prj_1', 'claude-code', 'waiting_for_user', 'wt_1', 'sess', 'do it', 'question', 'editing', 77, 'p77', 4, 10, 20),
		        ('run_2', 'tsk_2', 'prj_1', 'codex', 'completed', NULL, '', 'x', '', '', 0, '', 2, 11, 21)`,
		`INSERT INTO questions (id, run_id, task_id, project_id, kind, prompt, context, options, allow_free_text, state, asked_at)
		 VALUES ('qst_1', 'run_1', 'tsk_1', 'prj_1', 'selection', 'Which?', 'ctx', '["a","b"]', 1, 'pending', 12)`,
	} {
		if _, err := raw.ExecContext(ctx, q); err != nil {
			t.Fatalf("seed v5: %v\n%s", err, q)
		}
	}
	_ = raw.Close()

	db, err := Open(ctx, path, nil)
	if err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	defer db.Close()

	if err := db.View(ctx, func(tx store.Tx) error {
		r, err := tx.Runs().Get(ctx, "run_1")
		if err != nil {
			return err
		}
		if r.State != domain.RunWaitingForUser || r.Waiting != domain.WaitQuestion || r.WorktreeID != "wt_1" || r.SessionRef != "sess" ||
			r.Prompt != "do it" || r.Activity != "editing" || r.PID != 77 || r.ProcessID != "p77" || r.Version != 4 ||
			r.Policy.Interaction != domain.InteractionInteractive || r.Blocker != nil {
			t.Errorf("run_1 after the migration = %+v", r)
		}
		if r2, err := tx.Runs().Get(ctx, "run_2"); err != nil || r2.State != domain.RunCompleted || r2.WorktreeID != "" {
			t.Errorf("run_2 = %+v, %v", r2, err)
		}
		q, err := tx.Questions().Get(ctx, "qst_1")
		if err != nil {
			return err
		}
		if q.RunID != "run_1" || q.Prompt != "Which?" || q.State != domain.QuestionPending || q.AnsweredBy != "" || len(q.Options) != 2 {
			t.Errorf("question after the migration = %+v", q)
		}
		tk, err := tx.Tasks().Get(ctx, "tsk_1")
		if err != nil {
			return err
		}
		if !tk.Execution.IsZero() || tk.Version != 3 || tk.State != domain.TaskDoing {
			t.Errorf("task after the migration = %+v", tk)
		}
		w, err := tx.Worktrees().Get(ctx, "wt_1")
		if err != nil || w.State != domain.WorktreeActive {
			t.Errorf("worktree = %+v, %v", w, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// Foreign keys are enforced again, and none is broken.
	var on int
	if err := db.writer.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&on); err != nil || on != 1 {
		t.Errorf("foreign_keys = %d, %v", on, err)
	}
	rows, err := db.writer.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	if rows.Next() {
		t.Error("the migration left a dangling foreign key")
	}
	_ = rows.Close()

	// What hangs off `runs` still holds: cascade to questions, the in-use worktree
	// trigger, the one-active-run-per-worktree index, the process trigger.
	if _, err := db.writer.ExecContext(ctx, `UPDATE worktrees SET removing_since = 5 WHERE id = 'wt_1'`); err == nil || !strings.Contains(err.Error(), "in use by an active run") {
		t.Errorf("a worktree in use was released: %v", err)
	}
	if _, err := db.writer.ExecContext(ctx, `INSERT INTO runs (id, task_id, project_id, agent_id, state, worktree_id, version, created_at, updated_at)
		VALUES ('run_3', 'tsk_1', 'prj_1', 'x', 'running', 'wt_1', 1, 1, 1)`); err == nil {
		t.Error("two active runs share a worktree")
	}
	if _, err := db.writer.ExecContext(ctx, `UPDATE runs SET state = 'failed' WHERE id = 'run_1'`); err == nil {
		t.Error("an ended run kept its process")
	}
	if _, err := db.writer.ExecContext(ctx, `DELETE FROM runs WHERE id = 'run_1'`); err == nil {
		// the worktree is active, so deleting the run only detaches it; questions go with it
		var n int
		_ = db.writer.QueryRowContext(ctx, `SELECT COUNT(*) FROM questions`).Scan(&n)
		if n != 0 {
			t.Errorf("deleting a run left its question behind")
		}
	}
}

func mustGetProject(t *testing.T, db *DB, id string) *domain.Project {
	t.Helper()
	var p *domain.Project
	if err := db.View(ctx, func(tx store.Tx) (err error) { p, err = tx.Projects().Get(ctx, id); return }); err != nil {
		t.Fatal(err)
	}
	return p
}
