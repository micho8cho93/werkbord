package service

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"werkbord/internal/domain"
	"werkbord/internal/planning"
	"werkbord/internal/store"
	"werkbord/internal/store/sqlite"
)

func TestDeckPersistsAcrossRestartAndSeparatesOwners(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "deck.db")
	db, err := sqlite.Open(ctx, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	s := &Settings{Deps: Deps{Store: db}}
	first, err := s.Deck(ctx, "phone:a")
	if err != nil {
		t.Fatal(err)
	}
	for i, id := range []string{"needs", "calendar", "projects", "mywork", "agents", "analytics"} {
		if first.Slots[i] == nil || first.Slots[i].Button != id {
			t.Fatalf("default %d = %+v", i, first)
		}
	}
	if first.Slots[6] != nil || first.Slots[7] != nil || first.Slots[8] != nil {
		t.Fatal("default sockets occupied")
	}

	projects := &Projects{Deps: s.Deps, Git: fakeGit{}}
	project, err := projects.Register(ctx, "/repos/deck", "Deck")
	if err != nil {
		t.Fatal(err)
	}
	first.Slots[6] = &DeckSlot{ID: "workflow", Button: "sprint-summary", Target: "selected", ProjectIDs: []string{project.ID}, Agent: "codex", Model: "configured-model", Reasoning: "high", Output: "report", Approval: "changes", MaxRepositories: 3}
	first.Slots[0], first.Slots[8] = nil, first.Slots[0]
	saved, err := s.SetDeck(ctx, "phone:a", first)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetDeck(ctx, "phone:a", first); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("stale save: %v", err)
	}

	original, err := s.PrepareDeckTask(ctx, &Tasks{Deps: s.Deps}, "phone:a", "workflow", "restart-request-1234")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = sqlite.Open(ctx, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s = &Settings{Deps: Deps{Store: db}}
	got, err := s.Deck(ctx, "phone:a")
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != saved.Version || got.Slots[0] != nil || got.Slots[8].Button != "needs" || got.Slots[6].Reasoning != "high" || got.Slots[6].MaxRepositories != 3 || got.Slots[6].Target != "selected" {
		t.Fatalf("restart: %+v", got)
	}
	other, _ := s.Deck(ctx, "phone:b")
	if other.Version != 0 || other.Slots[0].Button != "needs" {
		t.Fatal("layout leaked between phones")
	}
	empty := DeckLayout{Version: got.Version}
	if _, err = s.SetDeck(ctx, "phone:a", empty); err != nil {
		t.Fatal(err)
	}
	got, _ = s.Deck(ctx, "phone:a")
	for _, slot := range got.Slots {
		if slot != nil {
			t.Fatal("empty deck reset to defaults")
		}
	}
	// An accepted invocation keeps its target after restart and removal of its button.
	replay, err := s.PrepareDeckTask(ctx, &Tasks{Deps: s.Deps}, "phone:a", "workflow", "restart-request-1234")
	if err != nil || replay.ID != original.ID {
		t.Fatalf("restart replay = %+v, %v", replay, err)
	}
	history, err := s.DeckHistory(ctx, "phone:a")
	if err != nil || len(history) != 1 || history[0].TaskID != original.ID {
		t.Fatalf("history = %+v, %v", history, err)
	}
}

func TestDeckRejectsInvalidConfiguration(t *testing.T) {
	db, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := &Settings{Deps: Deps{Store: db}, Catalog: catalog{"codex": {}}}
	for _, slot := range []DeckSlot{
		{ID: "x", Button: "arbitrary-command"},
		{ID: "x", Button: "git", Agent: "codex"},
		{ID: "x", Button: "find-bugs", Target: "selected", Agent: "codex"},
		{ID: "x", Button: "find-bugs", ProjectID: "missing", Agent: "unknown"},
		{ID: "x", Button: "find-bugs", ProjectID: "missing", Agent: "codex", Model: "--unsafe"},
	} {
		in := DefaultDeck()
		in.Slots[6] = &slot
		if _, err := s.SetDeck(context.Background(), "owner", in); err == nil {
			t.Fatalf("accepted %+v", slot)
		}
	}
	in := DefaultDeck()
	in.Slots[6] = in.Slots[0]
	if _, err := s.SetDeck(context.Background(), "owner", in); err == nil {
		t.Fatal("duplicate slot IDs accepted")
	}
}

func TestDeckPresetsCaptureScopeAndConfiguration(t *testing.T) {
	for _, preset := range DeckWorkflows {
		t.Run(preset.ID, func(t *testing.T) {
			ctx := context.Background()
			db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "db"), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			s := &Settings{Deps: Deps{Store: db}}
			projects := &Projects{Deps: s.Deps, Git: fakeGit{}}
			tasks := &Tasks{Deps: s.Deps}
			p, _ := projects.Register(ctx, "/repos/first", "First")
			q, _ := projects.Register(ctx, "/repos/second", "Second")
			if _, err := s.SetExecution(ctx, domain.ExecutionConfig{Agent: "codex", Model: "inherited-model", Reasoning: "high"}); err != nil {
				t.Fatal(err)
			}
			deck := DefaultDeck()
			deck.Slots[6] = &DeckSlot{ID: "preset", Button: preset.ID, Target: "selected", ProjectIDs: []string{p.ID, q.ID, p.ID}, Output: "drafts", Approval: "changes", MaxRepositories: 2}
			if _, err := s.SetDeck(ctx, "phone:a", deck); err != nil {
				t.Fatal(err)
			}
			first, err := s.PrepareDeckTasks(ctx, tasks, "phone:a", "preset", "request-capture-1234", DeckStart{})
			if err != nil || len(first) != 2 {
				t.Fatalf("prepare %v %v", first, err)
			}
			for _, task := range first {
				if task.Execution.Model != "inherited-model" || task.Execution.Reasoning != "high" || task.Execution.Interaction != domain.InteractionInteractive || !strings.Contains(task.Description, preset.Prompt) || !strings.Contains(task.Description, "WERKBORD_DRAFT") {
					t.Fatalf("wrong snapshot %+v", task)
				}
			}
			_, _ = s.SetExecution(ctx, domain.ExecutionConfig{Agent: "other", Model: "new-default"})
			retry, err := s.PrepareDeckTasks(ctx, tasks, "phone:a", "preset", "request-capture-1234", DeckStart{CurrentProjectID: q.ID})
			if err != nil || retry[0].ID != first[0].ID || retry[0].Execution != first[0].Execution {
				t.Fatalf("retry changed: %v", err)
			}
			saved, _ := s.Deck(ctx, "phone:a")
			if saved.Slots[6].MaxRepositories != 2 || len(saved.Slots[6].ProjectIDs) != 3 {
				t.Fatal("configuration lost")
			}
			history, _ := s.DeckHistory(ctx, "phone:a")
			if len(history) != 2 || history[0].Configuration.Target != "selected" {
				t.Fatal("history lost scope")
			}
		})
	}
}

func TestDeckTargetsOverridesAndApproval(t *testing.T) {
	ctx := context.Background()
	db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := &Settings{Deps: Deps{Store: db}}
	projects := &Projects{Deps: s.Deps, Git: fakeGit{}}
	tasks := &Tasks{Deps: s.Deps}
	p, _ := projects.Register(ctx, "/repos/first", "First")
	q, _ := projects.Register(ctx, "/repos/second", "Second")
	for _, mode := range []string{"current", "all", "ask"} {
		t.Run(mode, func(t *testing.T) {
			d, _ := s.Deck(ctx, "owner")
			d.Slots[6] = &DeckSlot{ID: "preset", Button: "refactor-codebase", Target: mode}
			if _, err := s.SetDeck(ctx, "owner", d); err != nil {
				t.Fatal(err)
			}
			input := DeckStart{CurrentProjectID: p.ID}
			if mode == "ask" {
				input.ProjectIDs = []string{q.ID}
			}
			out, err := s.PrepareDeckTasks(ctx, tasks, "owner", "preset", "request-target-"+mode, input)
			want := 1
			if mode == "all" {
				want = 2
			}
			if err != nil || len(out) != want {
				t.Fatalf("targets: %v %v", out, err)
			}
			if mode == "ask" && out[0].ProjectID != q.ID {
				t.Fatal("picker ignored")
			}
		})
	}
	d, _ := s.Deck(ctx, "owner")
	override := *d.Slots[6]
	override.Target = "selected"
	override.ProjectIDs = []string{q.ID}
	override.Output = "changes"
	input := DeckStart{Override: &override}
	if _, err := s.PrepareDeckTasks(ctx, tasks, "owner", "preset", "request-approval-1234", input); err == nil {
		t.Fatal("changes lacked approval")
	}
	input.Approved = true
	out, err := s.PrepareDeckTasks(ctx, tasks, "owner", "preset", "request-approval-1234", input)
	if err != nil || out[0].ProjectID != q.ID {
		t.Fatalf("override: %v", err)
	}
	unchanged, _ := s.Deck(ctx, "owner")
	if unchanged.Slots[6].Target != "ask" || unchanged.Slots[6].Output != "" {
		t.Fatal("one-off settings persisted")
	}
	override.MaxRepositories = 1
	override.Target = "all"
	if _, err := s.PrepareDeckTasks(ctx, tasks, "owner", "preset", "request-limit-12345", input); err == nil {
		t.Fatal("scope limit ignored")
	}
	override.Target = "selected"
	override.ProjectIDs = []string{"not-authorised"}
	if _, err := s.PrepareDeckTasks(ctx, tasks, "owner", "preset", "request-missing-1234", input); err == nil {
		t.Fatal("unknown repository accepted")
	}
}

func TestDeckDraftFindingsAreScopedHumanOwnedAndIdempotent(t *testing.T) {
	f := newRunFixture(t)
	ctx := context.Background()
	r := f.running(t)
	slot := DeckSlot{ID: "bugs", Button: "find-bugs", Output: "drafts"}
	if err := f.runs.Store.Update(ctx, func(tx store.Tx) error { return tx.Settings().Set(ctx, "deck-task."+f.task.ID, slot, f.runs.now()) }); err != nil {
		t.Fatal(err)
	}
	line := `WERKBORD_DRAFT {"key":"nil-request","title":"Nil request panics","description":"Reproduce with a nil request; add a regression test.","references":["internal/api/server.go:12"]}` + "\n"
	// Tool output and malformed or path-escaping references must not create tasks.
	_ = f.runs.AppendOutput(ctx, r.ID, []OutputItem{{Stream: domain.StreamTool, Text: strings.ReplaceAll(line, "nil-request", "tool-finding")}, {Stream: domain.StreamAssistant, Text: line[:20]}, {Stream: domain.StreamAssistant, Text: line[20:] + line + `WERKBORD_DRAFT {"key":"bad","title":"Bad","description":"Unsupported","references":["../secret:1"]}`}})
	if _, err := f.runs.MarkIdle(ctx, r.ID); err != nil {
		t.Fatal(err)
	}
	f.runs.collectDeckDrafts(ctx, r)
	settings := &Settings{Deps: f.runs.Deps}
	owner := "report-owner"
	if err := f.runs.Store.Update(ctx, func(tx store.Tx) error {
		return tx.Settings().Set(ctx, DeckOwnerKey(owner)+".history", []DeckExecution{{SlotID: "bugs", TaskID: f.task.ID, ProjectID: f.project.ID}}, f.runs.now())
	}); err != nil {
		t.Fatal(err)
	}
	history, err := settings.DeckHistory(ctx, owner)
	if err != nil || len(history) != 1 || len(history[0].Findings) != 1 {
		t.Fatalf("report findings: %+v %v", history, err)
	}
	other, err := settings.DeckHistory(ctx, "another-phone")
	if err != nil || len(other) != 0 {
		t.Fatalf("another owner's findings: %+v %v", other, err)
	}
	// A later execution of the same workflow links the existing finding too.
	repeatedTask, err := f.tasks.CreateTask(ctx, NewTask{ProjectID: f.project.ID, Title: "Repeat scan"})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.runs.Store.Update(ctx, func(tx store.Tx) error {
		return tx.Settings().Set(ctx, "deck-task."+repeatedTask.ID, slot, f.runs.now())
	}); err != nil {
		t.Fatal(err)
	}
	repeatedRun := *r
	repeatedRun.TaskID = repeatedTask.ID
	f.runs.collectDeckDrafts(ctx, &repeatedRun)
	var linked []string
	if err := f.runs.Store.View(ctx, func(tx store.Tx) error { return tx.Settings().Get(ctx, "deck-findings."+repeatedTask.ID, &linked) }); err != nil || len(linked) != 1 || linked[0] != history[0].Findings[0].ID {
		t.Fatalf("repeated report lost its deduplicated finding: %v %v", linked, err)
	}
	all, err := f.tasks.List(ctx, f.project.ID)
	if err != nil || len(all) != 3 {
		t.Fatalf("drafts: %d %v", len(all), err)
	}
	for _, task := range all {
		if task.ID != f.task.ID && task.ID != repeatedTask.ID && (task.State != domain.TaskBacklog || task.WorkMode != planning.ModeHuman || !strings.Contains(task.Description, "internal/api/server.go:12")) {
			t.Fatalf("unsafe draft: %+v", task)
		}
	}
}

func TestDeckValidatesProviderModelAndReasoning(t *testing.T) {
	s := &Settings{Catalog: catalog{"agent": {Models: []domain.AgentOption{{ID: "available", Reasoning: []string{"low"}}}, Reasoning: []domain.AgentOption{{ID: "low"}, {ID: "high"}}}}}
	for _, slot := range []DeckSlot{
		{ID: "x", Button: "find-bugs", Agent: "agent", Model: "unavailable"},
		{ID: "x", Button: "find-bugs", Agent: "agent", Model: "available", Reasoning: "high"},
		{ID: "x", Button: "find-bugs", Agent: "agent", Runner: "--command"},
		{ID: "x", Button: "find-bugs", Output: "changes"},
	} {
		if err := s.validateDeckSlot(context.Background(), slot); err == nil {
			t.Fatalf("accepted %+v", slot)
		}
	}
	if err := s.validateDeckSlot(context.Background(), DeckSlot{ID: "x", Button: "find-bugs", Agent: "agent", Model: "available", Reasoning: "low"}); err != nil {
		t.Fatal(err)
	}
}

func TestDeckUpgradeReplaysPreviouslyAcceptedRequest(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	p, err := f.projects.Register(ctx, "/repos/upgrade", "")
	if err != nil {
		t.Fatal(err)
	}
	owner, request := "phone:upgrade", "upgrade-request-1234"
	old := DeckSlot{ID: "old-slot", Button: "find-bugs", ProjectID: p.ID}
	original, err := f.tasks.CreateTask(ctx, NewTask{ProjectID: p.ID, Title: "Find Bugs", Description: "Stage 2 brief", SourceRef: DeckOwnerKey(owner) + ":" + request})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.tasks.Store.Update(ctx, func(tx store.Tx) error {
		return tx.Settings().Set(ctx, DeckOwnerKey(owner)+".request."+request, old, f.tasks.now())
	}); err != nil {
		t.Fatal(err)
	}
	s := &Settings{Deps: f.tasks.Deps}
	replayed, err := s.PrepareDeckTask(ctx, f.tasks, owner, old.ID, request)
	if err != nil || replayed.ID != original.ID {
		t.Fatalf("upgrade duplicated task: %+v %v", replayed, err)
	}
}

func TestDeckHistoryRetainsActiveAndPartiallyStartedExecutions(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	p, err := f.projects.Register(ctx, "/repos/history", "History")
	if err != nil {
		t.Fatal(err)
	}
	makeTask := func(title string) *domain.Task {
		t.Helper()
		task, err := f.tasks.CreateTask(ctx, NewTask{ProjectID: p.ID, Title: title})
		if err != nil {
			t.Fatal(err)
		}
		return task
	}
	active, failed, partial := makeTask("Active"), makeTask("Failed"), makeTask("Partial")
	if _, err := f.runs.Create(ctx, NewRun{TaskID: active.ID, AgentID: "fake", Prompt: "Inspect"}); err != nil {
		t.Fatal(err)
	}
	run, err := f.runs.Create(ctx, NewRun{TaskID: failed.ID, AgentID: "fake", Prompt: "Inspect"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.runs.End(ctx, run.ID, Ended{State: domain.RunFailed, Reason: "setup failed"}); err != nil {
		t.Fatal(err)
	}
	history := make([]DeckExecution, 99)
	for i := range history {
		history[i] = DeckExecution{RequestID: "recent", TaskID: failed.ID, ProjectID: p.ID}
	}
	history = append(history, DeckExecution{RequestID: "old-active", TaskID: active.ID, ProjectID: p.ID}, DeckExecution{RequestID: "old-failed", TaskID: failed.ID, ProjectID: p.ID}, DeckExecution{RequestID: "old-partial", TaskID: partial.ID, ProjectID: p.ID})
	s := &Settings{Deps: f.deps}
	layout := DefaultDeck()
	layout.Slots[6] = &DeckSlot{ID: "workflow", Button: "find-bugs", ProjectID: p.ID}
	if _, err := s.SetDeck(ctx, "owner", layout); err != nil {
		t.Fatal(err)
	}
	if err := f.deps.Store.Update(ctx, func(tx store.Tx) error {
		return tx.Settings().Set(ctx, DeckOwnerKey("owner")+".history", history, s.now())
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PrepareDeckTask(ctx, f.tasks, "owner", "workflow", "history-request-1234"); err != nil {
		t.Fatal(err)
	}
	got, err := s.DeckHistory(ctx, "owner")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, execution := range got {
		seen[execution.RequestID] = true
	}
	if len(got) != 102 || !seen["old-active"] || !seen["old-partial"] || seen["old-failed"] {
		t.Fatalf("lost active/partial execution or retained old failure: %d %+v", len(got), seen)
	}
}
