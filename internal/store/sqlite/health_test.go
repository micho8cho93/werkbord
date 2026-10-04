package sqlite

import (
	"errors"
	"testing"
	"time"

	"devboard/internal/domain"
	"devboard/internal/store"
)

func finding(projectID, key string, sev domain.HealthSeverity) *domain.HealthFinding {
	t := now()
	return &domain.HealthFinding{
		ID: domain.HealthFindingID(projectID, domain.FindStaleBranch, key), ProjectID: projectID,
		Type: domain.FindStaleBranch, Category: domain.CatBranch, Severity: sev, Basis: domain.BasisDeterministic,
		Title: "title " + key, Explanation: "why", Subject: domain.HealthSubject{Branch: key, Related: []string{"x"}},
		Evidence: []domain.HealthEvidence{{Label: "a", Value: "b"}},
		Action:   domain.HealthAction{Kind: domain.ActReviewChanges, Label: "Review", CanPerform: true, Branch: key},
		State:    domain.HealthOpen, DetectedAt: t, UpdatedAt: t,
	}
}

func upsert(t *testing.T, db *DB, f *domain.HealthFinding) error {
	t.Helper()
	return db.Update(ctx, func(tx store.Tx) error { return tx.Health().Upsert(ctx, f) })
}

func getFinding(t *testing.T, db *DB, projectID, id string) *domain.HealthFinding {
	t.Helper()
	var f *domain.HealthFinding
	if err := db.View(ctx, func(tx store.Tx) error {
		var err error
		f, err = tx.Health().Get(ctx, projectID, id)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestHealthFindingRoundTrips(t *testing.T) {
	db, _ := openTemp(t)
	p := seedProject(t, db)
	f := finding(p.ID, "devboard/x", domain.HealthRisk)
	if err := upsert(t, db, f); err != nil {
		t.Fatal(err)
	}
	got := getFinding(t, db, p.ID, f.ID)
	if got.Title != f.Title || got.Severity != domain.HealthRisk || got.Subject.Branch != "devboard/x" || len(got.Subject.Related) != 1 ||
		len(got.Evidence) != 1 || got.Action.Kind != domain.ActReviewChanges || !got.Action.CanPerform || !got.DetectedAt.Equal(f.DetectedAt) {
		t.Fatalf("got %+v", got)
	}
}

func TestHealthUpsertReplacesTheSameFinding(t *testing.T) {
	db, _ := openTemp(t)
	p := seedProject(t, db)
	f := finding(p.ID, "devboard/x", domain.HealthAttention)
	_ = upsert(t, db, f)
	f.Severity, f.Title = domain.HealthRisk, "worse"
	if err := upsert(t, db, f); err != nil {
		t.Fatal(err)
	}
	var all []domain.HealthFinding
	_ = db.View(ctx, func(tx store.Tx) error {
		all, _ = tx.Health().ListByProject(ctx, p.ID)
		return nil
	})
	if len(all) != 1 || all[0].Severity != domain.HealthRisk || all[0].Title != "worse" {
		t.Fatalf("all = %+v", all)
	}
}

func TestHealthLifecycleIsEnforcedByTheDatabase(t *testing.T) {
	db, _ := openTemp(t)
	p := seedProject(t, db)
	at := now()

	open := finding(p.ID, "a", domain.HealthAttention)
	open.ResolvedAt = &at
	resolvedWithoutState := finding(p.ID, "b", domain.HealthAttention)
	resolvedWithoutState.State = domain.HealthResolved
	dismissedWithoutSeverity := finding(p.ID, "c", domain.HealthAttention)
	dismissedWithoutSeverity.State, dismissedWithoutSeverity.DismissedAt = domain.HealthDismissed, &at
	badSeverity := finding(p.ID, "d", "alarming")
	badState := finding(p.ID, "e", domain.HealthRisk)
	badState.State = "snoozed"

	for name, f := range map[string]*domain.HealthFinding{
		"an open finding cannot carry a resolution time": open,
		"a resolved finding must say when":               resolvedWithoutState,
		"a dismissed finding must say how severe it was": dismissedWithoutSeverity,
		"an unknown severity":                            badSeverity,
		"an unknown state":                               badState,
	} {
		if err := upsert(t, db, f); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", name, err)
		}
	}

	// And the valid shapes are accepted.
	good := finding(p.ID, "ok", domain.HealthRisk)
	good.State, good.DismissedAt, good.DismissedSeverity = domain.HealthDismissed, &at, domain.HealthRisk
	if err := upsert(t, db, good); err != nil {
		t.Fatalf("a dismissed finding: %v", err)
	}
	res := finding(p.ID, "res", domain.HealthRisk)
	res.State, res.ResolvedAt = domain.HealthResolved, &at
	if err := upsert(t, db, res); err != nil {
		t.Fatalf("a resolved finding: %v", err)
	}
}

func TestHealthFindingNeedsAProject(t *testing.T) {
	db, _ := openTemp(t)
	if err := upsert(t, db, finding("proj_missing", "x", domain.HealthRisk)); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestHealthListsByStateAndSeverity(t *testing.T) {
	db, _ := openTemp(t)
	p1, p2 := seedProject(t, db), seedProject(t, db)
	at := now()
	mk := func(p *domain.Project, key string, sev domain.HealthSeverity, state domain.HealthState) {
		f := finding(p.ID, key, sev)
		f.State = state
		switch state {
		case domain.HealthResolved:
			f.ResolvedAt = &at
		case domain.HealthDismissed:
			f.DismissedAt, f.DismissedSeverity = &at, sev
		}
		if err := upsert(t, db, f); err != nil {
			t.Fatal(err)
		}
	}
	mk(p1, "info", domain.HealthInfo, domain.HealthOpen)
	mk(p1, "attention", domain.HealthAttention, domain.HealthOpen)
	mk(p1, "critical", domain.HealthCritical, domain.HealthOpen)
	mk(p1, "gone", domain.HealthRisk, domain.HealthResolved)
	mk(p1, "known", domain.HealthRisk, domain.HealthDismissed)
	mk(p2, "risk", domain.HealthRisk, domain.HealthOpen)

	_ = db.View(ctx, func(tx store.Tx) error {
		h := tx.Health()
		all, _ := h.ListByProject(ctx, p1.ID)
		if len(all) != 5 || all[0].Severity != domain.HealthCritical {
			t.Errorf("all of p1, worst first: %+v", titles(all))
		}
		open, _ := h.ListByProject(ctx, p1.ID, domain.HealthOpen)
		if len(open) != 3 {
			t.Errorf("open of p1: %v", titles(open))
		}
		both, _ := h.ListByProject(ctx, p1.ID, domain.HealthResolved, domain.HealthDismissed)
		if len(both) != 2 {
			t.Errorf("resolved and dismissed: %v", titles(both))
		}
		risky, _ := h.ListOpen(ctx, domain.HealthRisk)
		if len(risky) != 2 || risky[0].Severity != domain.HealthCritical || risky[1].Severity != domain.HealthRisk {
			t.Errorf("risk and above across projects: %v", titles(risky))
		}
		everything, _ := h.ListOpen(ctx, domain.HealthInfo)
		if len(everything) != 4 {
			t.Errorf("every open finding: %v", titles(everything))
		}
		return nil
	})
}

func titles(fs []domain.HealthFinding) []string {
	var out []string
	for _, f := range fs {
		out = append(out, string(f.Severity)+":"+f.Title+":"+string(f.State))
	}
	return out
}

func TestHealthForgetsOldResolvedFindings(t *testing.T) {
	db, _ := openTemp(t)
	p := seedProject(t, db)
	old, recent := now().Add(-40*24*time.Hour), now().Add(-time.Hour)
	mk := func(key string, resolved time.Time) {
		f := finding(p.ID, key, domain.HealthRisk)
		f.State, f.ResolvedAt = domain.HealthResolved, &resolved
		if err := upsert(t, db, f); err != nil {
			t.Fatal(err)
		}
	}
	mk("old", old)
	mk("recent", recent)
	_ = upsert(t, db, finding(p.ID, "open", domain.HealthRisk))
	var n int
	_ = db.Update(ctx, func(tx store.Tx) error {
		n, _ = tx.Health().DeleteResolvedBefore(ctx, now().Add(-30*24*time.Hour))
		return nil
	})
	var left []domain.HealthFinding
	_ = db.View(ctx, func(tx store.Tx) error {
		left, _ = tx.Health().ListByProject(ctx, p.ID)
		return nil
	})
	if n != 1 || len(left) != 2 {
		t.Fatalf("deleted %d, left %v", n, titles(left))
	}
}

func TestHealthFindingsGoWithTheirProject(t *testing.T) {
	db, _ := openTemp(t)
	p := seedProject(t, db)
	_ = upsert(t, db, finding(p.ID, "x", domain.HealthRisk))
	if _, err := db.writer.ExecContext(ctx, `DELETE FROM projects WHERE id = ?`, p.ID); err != nil {
		t.Fatal(err)
	}
	var n int
	_ = db.reader.QueryRowContext(ctx, `SELECT COUNT(*) FROM health_findings`).Scan(&n)
	if n != 0 {
		t.Fatalf("%d findings survived their project", n)
	}
}

func TestHealthCheckRoundTrip(t *testing.T) {
	db, _ := openTemp(t)
	p := seedProject(t, db)
	err := db.View(ctx, func(tx store.Tx) error {
		_, err := tx.Health().GetCheck(ctx, p.ID)
		return err
	})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("a project never checked: %v", err)
	}
	at := now()
	set := func(c domain.HealthCheck) {
		if err := db.Update(ctx, func(tx store.Tx) error { return tx.Health().SetCheck(ctx, c) }); err != nil {
			t.Fatal(err)
		}
	}
	set(domain.HealthCheck{ProjectID: p.ID, CheckedAt: at, DurationMS: 12})
	set(domain.HealthCheck{ProjectID: p.ID, CheckedAt: at.Add(time.Minute), DurationMS: 30, Error: "boom"})
	var c *domain.HealthCheck
	_ = db.View(ctx, func(tx store.Tx) error {
		c, _ = tx.Health().GetCheck(ctx, p.ID)
		return nil
	})
	if c == nil || c.DurationMS != 30 || c.Error != "boom" || !c.CheckedAt.Equal(at.Add(time.Minute)) {
		t.Fatalf("check = %+v", c)
	}
	if err := db.Update(ctx, func(tx store.Tx) error {
		return tx.Health().SetCheck(ctx, domain.HealthCheck{ProjectID: "proj_missing", CheckedAt: at})
	}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("a check of an unknown project: %v", err)
	}
}
