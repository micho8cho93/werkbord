package sqlite

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"devboard/internal/domain"
	"devboard/internal/store"
)

func newAssistantSession(t *testing.T, db *DB) *domain.AssistantSession {
	t.Helper()
	s := &domain.AssistantSession{ID: domain.NewID(domain.PrefixAssistantSession), Provider: "claude-code", State: domain.AssistantIdle, CreatedAt: now(), UpdatedAt: now()}
	if err := db.Update(ctx, func(tx store.Tx) error { return tx.Assistant().CreateSession(ctx, s) }); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestAssistantSessionIsCompareAndSwap(t *testing.T) {
	db, _ := openTemp(t)
	s := newAssistantSession(t, db)

	stale := *s
	if err := db.Update(ctx, func(tx store.Tx) error {
		s.ProviderRef, s.Turns, s.State = "ref-1", 1, domain.AssistantRunning
		s.UpdatedAt = now()
		return tx.Assistant().UpdateSession(ctx, s)
	}); err != nil {
		t.Fatal(err)
	}
	if s.Version != 2 {
		t.Fatalf("version %d, want 2", s.Version)
	}
	err := db.Update(ctx, func(tx store.Tx) error { return tx.Assistant().UpdateSession(ctx, &stale) })
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("a stale write must conflict, got %v", err)
	}
	var got *domain.AssistantSession
	_ = db.View(ctx, func(tx store.Tx) (err error) { got, err = tx.Assistant().GetSession(ctx, s.ID); return })
	if got.ProviderRef != "ref-1" || got.Turns != 1 || got.State != domain.AssistantRunning {
		t.Fatalf("got %+v", got)
	}
	err = db.View(ctx, func(tx store.Tx) error { _, err := tx.Assistant().GetSession(ctx, "ast_missing"); return err })
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestAssistantActionIsClaimedAndSettledExactlyOnce(t *testing.T) {
	db, _ := openTemp(t)
	s := newAssistantSession(t, db)
	args := json.RawMessage(`{"projectId":"prj_1","title":"x"}`)
	a := &domain.AssistantAction{ID: domain.NewID(domain.PrefixAssistantAction), SessionID: s.ID, Principal: "assistant:" + s.ID,
		Operation: "create_ticket", Args: args, ArgsHash: domain.ArgsDigest(args), Summary: "Create a ticket", State: domain.ActionPending,
		CreatedAt: now(), ExpiresAt: now().Add(1)}
	if err := db.Update(ctx, func(tx store.Tx) error { return tx.Assistant().CreateAction(ctx, a) }); err != nil {
		t.Fatal(err)
	}
	move := func(from, to domain.AssistantActionState, outcome string) error {
		return db.Update(ctx, func(tx store.Tx) error { return tx.Assistant().TransitionAction(ctx, a.ID, from, to, outcome, now()) })
	}
	if err := move(domain.ActionPending, domain.ActionExecuting, ""); err != nil {
		t.Fatal(err)
	}
	// A second confirmation finds the claim taken.
	if err := move(domain.ActionPending, domain.ActionExecuting, ""); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("claiming twice must conflict, got %v", err)
	}
	if err := move(domain.ActionPending, domain.ActionRejected, ""); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("an executing action cannot be rejected, got %v", err)
	}
	var open []domain.AssistantAction
	_ = db.View(ctx, func(tx store.Tx) (err error) { open, err = tx.Assistant().ListOpenActions(ctx); return })
	if len(open) != 1 || open[0].ResolvedAt != nil {
		t.Fatalf("an executing action is still open and not resolved: %+v", open)
	}
	if err := move(domain.ActionExecuting, domain.ActionExecuted, "created tsk_1"); err != nil {
		t.Fatal(err)
	}
	for _, to := range []domain.AssistantActionState{domain.ActionFailed, domain.ActionExecuted} {
		if err := move(domain.ActionExecuting, to, "again"); !errors.Is(err, domain.ErrConflict) {
			t.Fatalf("settling twice (%s) must conflict, got %v", to, err)
		}
	}
	if err := db.Update(ctx, func(tx store.Tx) error {
		return tx.Assistant().TransitionAction(ctx, "act_missing", domain.ActionPending, domain.ActionRejected, "", now())
	}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("got %v", err)
	}
	for _, bad := range [][2]domain.AssistantActionState{{domain.ActionPending, domain.ActionPending}, {domain.ActionExecuted, domain.ActionFailed}, {domain.ActionPending, "bogus"}} {
		if err := move(bad[0], bad[1], ""); !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("%v is not a move: %v", bad, err)
		}
	}
	var got *domain.AssistantAction
	_ = db.View(ctx, func(tx store.Tx) (err error) { got, err = tx.Assistant().GetAction(ctx, a.ID); return })
	if got.State != domain.ActionExecuted || got.Outcome != "created tsk_1" || got.ResolvedAt == nil || string(got.Args) != string(args) {
		t.Fatalf("got %+v", got)
	}
}

func TestAssistantAuditIsChainedAndAppendOnly(t *testing.T) {
	db, path := openTemp(t)
	for i, outcome := range []string{domain.AuditOutcomeOK, domain.AuditOutcomeProposed, domain.AuditOutcomeDenied} {
		e := &domain.AssistantAuditEntry{At: now(), SessionID: "ast_1", Actor: "assistant:claude-code", Operation: "list_tickets", Kind: domain.AuditRead, Outcome: outcome, Detail: strings.Repeat("d", i)}
		if err := db.Update(ctx, func(tx store.Tx) error { return tx.Assistant().AppendAudit(ctx, e) }); err != nil {
			t.Fatal(err)
		}
		if e.Seq != int64(i+1) || e.Hash == "" || e.Hash != domain.AuditHash(*e) {
			t.Fatalf("entry %d: %+v", i, e)
		}
	}
	var all []domain.AssistantAuditEntry
	_ = db.View(ctx, func(tx store.Tx) (err error) { all, err = tx.Assistant().AllAudit(ctx); return })
	if len(all) != 3 || all[0].PrevHash != "" || all[1].PrevHash != all[0].Hash || all[2].PrevHash != all[1].Hash {
		t.Fatalf("the entries are not chained: %+v", all)
	}
	for _, e := range all {
		if domain.AuditHash(e) != e.Hash {
			t.Fatalf("the hash of %d cannot be recomputed from what was stored", e.Seq)
		}
	}

	// Nothing, not even a statement run straight on the database, may change or remove a line.
	raw, err := openRaw(path)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	for _, q := range []string{`UPDATE assistant_audit SET outcome = 'ok'`, `DELETE FROM assistant_audit WHERE seq = 2`, `DELETE FROM assistant_audit`} {
		if _, err := raw.ExecContext(ctx, q); err == nil || !strings.Contains(err.Error(), "append-only") {
			t.Errorf("%q must be refused as append-only, got %v", q, err)
		}
	}

	var page []domain.AssistantAuditEntry
	_ = db.View(ctx, func(tx store.Tx) (err error) { page, err = tx.Assistant().ListAudit(ctx, "", 3, 10); return })
	if len(page) != 2 || page[0].Seq != 2 {
		t.Fatalf("paging back from seq 3 must give 2 then 1: %+v", page)
	}
	_ = db.View(ctx, func(tx store.Tx) (err error) { page, err = tx.Assistant().ListAudit(ctx, "ast_other", 0, 10); return })
	if len(page) != 0 {
		t.Fatalf("another session's trail is not this one's: %+v", page)
	}
}

func TestDeletingASessionKeepsItsAudit(t *testing.T) {
	db, _ := openTemp(t)
	s := newAssistantSession(t, db)
	args := json.RawMessage(`{}`)
	if err := db.Update(ctx, func(tx store.Tx) error {
		if err := tx.Assistant().CreateAction(ctx, &domain.AssistantAction{ID: "act_1", SessionID: s.ID, Principal: "p", Operation: "create_ticket",
			Args: args, ArgsHash: domain.ArgsDigest(args), State: domain.ActionPending, CreatedAt: now(), ExpiresAt: now()}); err != nil {
			return err
		}
		return tx.Assistant().AppendAudit(ctx, &domain.AssistantAuditEntry{At: now(), SessionID: s.ID, Actor: "a", Operation: "create_ticket", Kind: domain.AuditMutation, Outcome: domain.AuditOutcomeProposed})
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Update(ctx, func(tx store.Tx) error { return tx.Assistant().DeleteSession(ctx, s.ID) }); err != nil {
		t.Fatal(err)
	}
	var acts []domain.AssistantAction
	var trail []domain.AssistantAuditEntry
	_ = db.View(ctx, func(tx store.Tx) (err error) {
		acts, _ = tx.Assistant().ListActions(ctx, s.ID)
		trail, err = tx.Assistant().ListAudit(ctx, s.ID, 0, 10)
		return
	})
	if len(acts) != 0 || len(trail) != 1 {
		t.Fatalf("a deleted session leaves no actions but keeps its trail: %v %v", acts, trail)
	}
	err := db.Update(ctx, func(tx store.Tx) error { return tx.Assistant().DeleteSession(ctx, s.ID) })
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestAuditRetentionRemovesOnlyAnUnbrokenPrefixAndKeepsTheChain(t *testing.T) {
	db, path := openTemp(t)
	base := now().Add(-100 * 24 * time.Hour)
	for i := 0; i < 6; i++ {
		at := base
		if i >= 3 {
			at = now() // three old entries, then three new
		}
		e := &domain.AssistantAuditEntry{At: at, Actor: "a", Operation: "op", Kind: domain.AuditRead, Outcome: domain.AuditOutcomeOK}
		if err := db.Update(ctx, func(tx store.Tx) error { return tx.Assistant().AppendAudit(ctx, e) }); err != nil {
			t.Fatal(err)
		}
	}
	var n int64
	var all []domain.AssistantAuditEntry
	var cp *domain.AuditCheckpoint
	if err := db.Update(ctx, func(tx store.Tx) (err error) {
		n, err = tx.Assistant().PruneAudit(ctx, now().Add(-30*24*time.Hour), now())
		return
	}); err != nil || n != 3 {
		t.Fatalf("pruned %d, %v", n, err)
	}
	_ = db.View(ctx, func(tx store.Tx) (err error) {
		all, _ = tx.Assistant().AllAudit(ctx)
		cp, err = tx.Assistant().AuditCheckpoint(ctx)
		return
	})
	if len(all) != 3 || all[0].Seq != 4 || cp == nil || cp.ThroughSeq != 3 || cp.Pruned != 3 || all[0].PrevHash != cp.Hash {
		t.Fatalf("all=%v cp=%+v", all, cp)
	}
	// The window is closed again: nothing else can be removed.
	raw, _ := openRaw(path)
	defer raw.Close()
	if _, err := raw.ExecContext(ctx, `DELETE FROM assistant_audit WHERE seq = 5`); err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("a middle entry must not go: %v", err)
	}
	// A prune that finds nothing old changes nothing.
	_ = db.Update(ctx, func(tx store.Tx) (err error) {
		n, err = tx.Assistant().PruneAudit(ctx, now().Add(-30*24*time.Hour), now())
		return
	})
	if n != 0 {
		t.Fatalf("pruned %d", n)
	}
}
