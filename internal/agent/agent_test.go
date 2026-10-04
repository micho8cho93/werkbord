package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"devboard/internal/domain"
)

type fakeAdapter struct{ id string }

func (f fakeAdapter) ID() string { return f.id }
func (f fakeAdapter) Detect(context.Context) domain.Agent {
	return domain.Agent{ID: f.id, Name: f.id, Available: true}
}
func (f fakeAdapter) Start(context.Context, StartRequest) (Session, error) {
	return nil, errors.New("not implemented")
}

type unavailable struct{ fakeAdapter }

func (u unavailable) Detect(context.Context) domain.Agent {
	return domain.Agent{ID: u.id, Name: u.id, Detail: "not installed"}
}

func TestRegistry(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(fakeAdapter{"zeta"}); err != nil {
		t.Fatal(err)
	}
	if err := r.Register(fakeAdapter{"alpha"}); err != nil {
		t.Fatal(err)
	}
	if err := r.Register(fakeAdapter{"alpha"}); !errors.Is(err, domain.ErrDuplicate) {
		t.Fatalf("duplicate register: err = %v", err)
	}
	if _, err := r.Get("missing"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Get missing: err = %v", err)
	}
	got := r.Detect(context.Background())
	if len(got) != 2 || got[0].ID != "alpha" || got[1].ID != "zeta" {
		t.Fatalf("Detect = %+v", got)
	}
}

func TestRegistryAvailable(t *testing.T) {
	r := NewRegistry()
	_ = r.Register(fakeAdapter{"ok"})
	_ = r.Register(unavailable{fakeAdapter{"missing"}})
	ctx := context.Background()
	if a, err := r.Available(ctx, "ok"); err != nil || a.ID() != "ok" {
		t.Fatalf("Available(ok) = %v, %v", a, err)
	}
	if _, err := r.Available(ctx, "missing"); !errors.Is(err, domain.ErrConflict) || !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("Available(missing): err = %v; want a conflict that says why", err)
	}
	if _, err := r.Available(ctx, "nope"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Available(nope): err = %v", err)
	}
}
