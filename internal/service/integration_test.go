package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"devboard/internal/domain"
	"devboard/internal/integration"
	"devboard/internal/store"
)

func TestIntegrationImportIsAtomicAndPreservesLocalWork(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	p, err := f.projects.Register(ctx, "/repos/sync", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.deps.Store.Update(ctx, func(tx store.Tx) error {
		r, e := tx.Repositories().Get(ctx, p.ID)
		if e != nil {
			return e
		}
		r.Remotes = []domain.GitRemote{{URL: "git@host:owner/repo"}}
		return tx.Repositories().Upsert(ctx, r)
	}); err != nil {
		t.Fatal(err)
	}
	in := integration.Import{Schema: integration.Schema, ProjectID: p.ID, Repository: "https://host/owner/repo.git", SourceRef: "source-one", Title: "ticket", Description: "initial", WorkBranch: "ticket-one"}
	var wg sync.WaitGroup
	ids := make(chan string, 12)
	errs := make(chan error, 12)
	for range 12 {
		wg.Add(1)
		go func() { defer wg.Done(); out, e := f.tasks.Import(ctx, in); ids <- out.TaskID; errs <- e }()
	}
	wg.Wait()
	close(ids)
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	id := ""
	for got := range ids {
		if id != "" && id != got {
			t.Fatal("duplicate import")
		}
		id = got
	}
	all, _ := f.tasks.List(ctx, p.ID)
	if len(all) != 1 {
		t.Fatalf("%d tasks", len(all))
	}
	previous := integration.Text{Title: in.Title, Description: in.Description, WorkBranch: in.WorkBranch}
	in.Previous = &previous
	in.Description = "Team edit"
	if out, e := f.tasks.Import(ctx, in); e != nil || out.Conflict {
		t.Fatalf("untouched import: %+v %v", out, e)
	}
	task, _ := f.tasks.Get(ctx, id)
	local := "local work"
	if _, e := f.tasks.Update(ctx, id, TaskPatch{Version: task.Version, Description: &local}); e != nil {
		t.Fatal(e)
	}
	in.Previous = &integration.Text{Title: in.Title, Description: "Team edit", WorkBranch: in.WorkBranch}
	in.Description = "conflicting Team edit"
	if out, e := f.tasks.Import(ctx, in); e != nil || !out.Conflict || out.TaskID != id {
		t.Fatalf("conflict: %+v %v", out, e)
	}
	task, _ = f.tasks.Get(ctx, id)
	if task.Description != local {
		t.Fatal("local edit overwritten")
	}
	in.Repository = "https://host/another/repo"
	if _, e := f.tasks.Import(ctx, in); !errors.Is(e, domain.ErrConflict) {
		t.Fatalf("repository mismatch: %v", e)
	}
}

func TestIntegrationEventsProjectOnlyReviewedFieldsAndRetentionReset(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	p, _ := f.projects.Register(ctx, "/repos/feed", "")
	task, _ := f.tasks.Create(ctx, p.ID, "ticket", "local prompt secret")
	secret := "RAW_PRIVATE_TERMINAL_TOKEN"
	now := time.Now().UTC()
	if err := f.deps.Store.Update(ctx, func(tx store.Tx) error {
		r := domain.Run{ID: "run_one", ProjectID: p.ID, TaskID: task.ID, State: domain.RunRunning, CreatedAt: now, Prompt: secret, Activity: secret, SessionRef: secret, Reason: secret, Blocker: &domain.Blocker{Summary: secret}}
		for _, typeName := range []domain.EventType{domain.EventAgentOutput, domain.EventRunStateChanged} {
			e, _ := domain.NewEvent(typeName, map[string]any{"run": r, "compact": true})
			e.ProjectID, e.TaskID, e.RunID = p.ID, task.ID, r.ID
			if err := tx.Events().Append(ctx, &e); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	feed, err := f.tasks.IntegrationEvents(ctx, p.ID, task.ID, 0)
	if err != nil || len(feed.Events) != 1 {
		t.Fatalf("feed %+v %v", feed, err)
	}
	raw, _ := json.Marshal(feed)
	if strings.Contains(string(raw), secret) || strings.Contains(string(raw), "payload") || strings.Contains(string(raw), "prompt") {
		t.Fatalf("leak: %s", raw)
	}
	if err := f.deps.Store.Update(ctx, func(tx store.Tx) error { return tx.Settings().Set(ctx, "events:replay-floor", feed.Cursor, now) }); err != nil {
		t.Fatal(err)
	}
	reset, err := f.tasks.IntegrationEvents(ctx, p.ID, task.ID, 0)
	if err != nil || !reset.Reset || len(reset.Events) != 0 {
		t.Fatalf("reset %+v %v", reset, err)
	}
	if _, err := f.tasks.IntegrationEvents(ctx, "other", task.ID, 0); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("project scope lost")
	}
}

func TestIntegrationLegacyProvenanceAliasesPreserveHandoffs(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	p, err := f.projects.Register(ctx, "/repos/legacy", "")
	if err != nil {
		t.Fatal(err)
	}
	err = f.deps.Store.Update(ctx, func(tx store.Tx) error {
		r, e := tx.Repositories().Get(ctx, p.ID)
		if e != nil {
			return e
		}
		r.Remotes = []domain.GitRemote{{URL: "git@host:owner/repo"}}
		return tx.Repositories().Upsert(ctx, r)
	})
	if err != nil {
		t.Fatal(err)
	}
	old := "http://127.0.0.1:7430/?tab=board&project=p&ticket=t"
	legacy, err := f.tasks.CreateTask(ctx, NewTask{ProjectID: p.ID, Title: "original", Description: "private changes", SourceRef: old, WorkBranch: "wb-1-ticket"})
	if err != nil {
		t.Fatal(err)
	}
	in := integration.Import{Schema: integration.Schema, SourceRef: "stable-source", SourceAliases: []string{old}, ProjectID: p.ID, Repository: "https://host/owner/repo", Title: "Team title", WorkBranch: "wb/workspace/wb-1-ticket"}
	out, err := f.tasks.Import(ctx, in)
	if err != nil || out.TaskID != legacy.ID || !out.Conflict {
		t.Fatal(out, err)
	}
	replay, err := f.tasks.CreateTask(ctx, NewTask{ProjectID: p.ID, Title: "manual replay", SourceRef: old})
	if err != nil || replay.ID != legacy.ID || replay.Description != "private changes" {
		t.Fatal(replay, err)
	}
	in.SourceRef = "second-stable"
	in.SourceAliases = []string{"new-legacy"}
	in.WorkBranch = "other-branch"
	fresh, err := f.tasks.Import(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	replay, err = f.tasks.CreateTask(ctx, NewTask{ProjectID: p.ID, Title: "manual replay", SourceRef: "new-legacy"})
	if err != nil || replay.ID != fresh.TaskID {
		t.Fatal("manual handoff duplicated connector task", err)
	}
}
func TestIntegrationExecutionStateProjection(t *testing.T) {
	for _, tc := range []struct {
		state domain.RunState
		want  string
	}{{domain.RunStarting, "queued"}, {domain.RunRunning, "running"}, {domain.RunWaitingForUser, "needs_input"}, {domain.RunBlocked, "blocked"}, {domain.RunCompleted, "completed"}, {domain.RunFailed, "failed"}, {domain.RunStopped, "canceled"}} {
		t.Run(tc.want, func(t *testing.T) {
			r := domain.Run{State: tc.state, CreatedAt: time.Now(), Prompt: "secret", Reason: "secret", Activity: "secret"}
			out := executionMetadata(r)
			if !out.Valid() || out.State != tc.want {
				t.Fatal(out)
			}
			raw, _ := json.Marshal(out)
			if strings.Contains(string(raw), "secret") {
				t.Fatal("projection leaked local text")
			}
		})
	}
}

func TestIntegrationDiscoveryOmitsPrivatePathsSettingsAndCredentialRemotes(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	p, err := f.projects.Register(ctx, "/repos/PRIVATE_REPOSITORY_PATH", "")
	if err != nil {
		t.Fatal(err)
	}
	err = f.deps.Store.Update(ctx, func(tx store.Tx) error {
		r, e := tx.Repositories().Get(ctx, p.ID)
		if e != nil {
			return e
		}
		r.Remotes = []domain.GitRemote{{URL: "https://user:PRIVATE_TOKEN@host/owner/repo"}, {URL: "git@host:owner/repo"}}
		return tx.Repositories().Upsert(ctx, r)
	})
	if err != nil {
		t.Fatal(err)
	}
	out, err := f.tasks.IntegrationProjects(ctx)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(out)
	if strings.Contains(string(raw), "/repos/") || strings.Contains(string(raw), "PRIVATE_TOKEN") || strings.Contains(string(raw), "repoPath") {
		t.Fatal("discovery exposed private metadata")
	}
	if len(out.Projects) != 1 || len(out.Projects[0].Remotes) != 1 || out.Projects[0].Remotes[0] != "git@host:owner/repo" {
		t.Fatal(out)
	}
}

func TestIntegrationCompletionSummaryContainsOnlyMeasuredFacts(t *testing.T) {
	start := time.Now().UTC()
	end := start.Add(12 * time.Second)
	r := domain.Run{State: domain.RunCompleted, CreatedAt: start, EndedAt: &end, Handoff: &domain.Handoff{Objective: "PRIVATE_RESULT"}, Prompt: "PRIVATE_RESULT", Activity: "PRIVATE_RESULT"}
	out := executionMetadata(r)
	if out.Summary == nil || out.Summary.ElapsedMillis == nil || *out.Summary.ElapsedMillis != 12000 || !out.Summary.HandoffAvailable || out.Summary.Outcome != "completed" {
		t.Fatal(out)
	}
	raw, _ := json.Marshal(out)
	if strings.Contains(string(raw), "PRIVATE_RESULT") {
		t.Fatal("summary leaked local content")
	}
}

func TestIntegrationWaitingIsReplacedPerSourceAndClearedByImport(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	src := integration.SourcePrefix + "w1:"
	a := integration.Waiting{SourceRef: src + "p:t1:m", Repository: "https://github.com/acme/billing", Title: "WB-1: Retry", From: "Northstar"}
	b := integration.Waiting{SourceRef: src + "p:t2:m", Repository: "git@example.com:acme/other.git", Title: "WB-2: Other", From: "Northstar"}
	other := integration.Waiting{SourceRef: integration.SourcePrefix + "w2:p:t9:m", Repository: "https://github.com/acme/billing", Title: "X-9", From: "Second"}
	if err := f.tasks.SetWaiting(ctx, integration.WaitingSet{Schema: integration.Schema, Source: src, Items: []integration.Waiting{a, b}}); err != nil {
		t.Fatal(err)
	}
	if err := f.tasks.SetWaiting(ctx, integration.WaitingSet{Schema: integration.Schema, Source: integration.SourcePrefix + "w2:", Items: []integration.Waiting{other}}); err != nil {
		t.Fatal(err)
	}
	items, err := f.tasks.Waiting(ctx)
	if err != nil || len(items) != 3 || items[0].GitHub != "acme/billing" || items[1].GitHub != "" {
		t.Fatalf("%+v %v", items, err)
	}
	first := items[0].Since
	// Replacing one source keeps the other's items and the time an unchanged item started waiting.
	if err := f.tasks.SetWaiting(ctx, integration.WaitingSet{Schema: integration.Schema, Source: src, Items: []integration.Waiting{a}}); err != nil {
		t.Fatal(err)
	}
	items, _ = f.tasks.Waiting(ctx)
	if len(items) != 2 || items[0].SourceRef != other.SourceRef && items[1].SourceRef != other.SourceRef {
		t.Fatalf("other source lost: %+v", items)
	}
	for _, it := range items {
		if it.SourceRef == a.SourceRef && !it.Since.Equal(first) {
			t.Fatal("waiting time reset")
		}
	}
	// An item from outside the source, a bad repository or a foreign schema is refused whole.
	for _, bad := range []integration.WaitingSet{
		{Schema: integration.Schema, Source: src, Items: []integration.Waiting{other}},
		{Schema: integration.Schema, Source: src, Items: []integration.Waiting{{SourceRef: src + "x", Repository: "/local/path", Title: "t"}}},
		{Schema: "other", Source: src},
		{Schema: integration.Schema, Source: "not-a-source:"},
	} {
		if err := f.tasks.SetWaiting(ctx, bad); !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("accepted %+v: %v", bad, err)
		}
	}
	// Once the repository is here and the ticket is imported, it no longer waits, and a later report cannot bring it back.
	p, err := f.projects.Register(ctx, "/repos/billing", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.deps.Store.Update(ctx, func(tx store.Tx) error {
		r, e := tx.Repositories().Get(ctx, p.ID)
		if e != nil {
			return e
		}
		r.Remotes = []domain.GitRemote{{URL: "git@github.com:acme/billing.git"}}
		return tx.Repositories().Upsert(ctx, r)
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.tasks.Import(ctx, integration.Import{Schema: integration.Schema, ProjectID: p.ID, Repository: a.Repository, SourceRef: a.SourceRef, Title: a.Title, Description: "d"}); err != nil {
		t.Fatal(err)
	}
	if err := f.tasks.SetWaiting(ctx, integration.WaitingSet{Schema: integration.Schema, Source: src, Items: []integration.Waiting{a}}); err != nil {
		t.Fatal(err)
	}
	items, _ = f.tasks.Waiting(ctx)
	if len(items) != 1 || items[0].SourceRef != other.SourceRef {
		t.Fatalf("imported ticket still waiting: %+v", items)
	}
}

func TestRegisterForRefusesAFolderOfAnotherRepository(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if _, err := f.projects.RegisterFor(ctx, "/repos/elsewhere", "", "https://github.com/acme/billing"); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("registered a folder without the repository: %v", err)
	}
	if ps, _ := f.projects.List(ctx); len(ps) != 0 {
		t.Fatal("refused folder was registered")
	}
	if _, err := f.projects.RegisterFor(ctx, "/repos/elsewhere", "", "/not/a/repository"); !errors.Is(err, domain.ErrInvalid) {
		t.Fatal(err)
	}
}
