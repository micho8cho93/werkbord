package assistant

import (
	"context"
	"devboard/internal/appops"
	"encoding/json"
	"strings"
	"testing"

	"devboard/internal/assistant/provider"
	"devboard/internal/assistant/provider/fake"
)

type operationProvider struct {
	*fake.Provider
	answer func(context.Context, provider.Turn, func(provider.Event)) (provider.Result, error)
}

func (*operationProvider) UsesOperationTools() bool { return true }
func (p *operationProvider) Run(ctx context.Context, turn provider.Turn, emit func(provider.Event)) (provider.Result, error) {
	return p.answer(ctx, turn, emit)
}

func nativeFixture(t *testing.T) (*efx, *operationProvider) {
	t.Helper()
	p := &operationProvider{Provider: fake.New("fake")}
	f := newEfx(t, func(c *Config) {
		c.Providers = provider.NewRegistry()
		if err := c.Providers.Register(p); err != nil {
			t.Fatal(err)
		}
	})
	return f, p
}

func TestApplicationToolStillRequiresConfirmationAndAudit(t *testing.T) {
	f, p := nativeFixture(t)
	project := f.project("portfolio")
	p.answer = func(ctx context.Context, turn provider.Turn, emit func(provider.Event)) (provider.Result, error) {
		args, _ := json.Marshal(map[string]string{"projectId": project.ID, "title": "Create the website"})
		result, err := turn.CallOperation(ctx, "create1", "create_ticket", args)
		if err != nil {
			return provider.Result{}, err
		}
		if !strings.Contains(result, "pending_confirmation") {
			t.Errorf("result = %s", result)
		}
		emit(provider.Event{Kind: provider.EventText, Text: "Waiting for confirmation."})
		return provider.Result{}, nil
	}
	s := f.session()
	evs := f.send(s.ID, "add a task")
	if end(evs).Type != EventCompleted || has(evs, EventConfirmationRequired).Proposal == nil {
		t.Fatalf("events = %v", types(evs))
	}
	if len(f.tickets(project.ID)) != 0 {
		t.Fatal("native tool bypassed confirmation")
	}
	v, _ := f.eng.Session(bg, s.ID)
	if len(v.Pending) != 1 {
		t.Fatalf("pending = %+v", v.Pending)
	}
	if _, err := f.eng.Resolve(bg, s.ID, v.Pending[0].ID, true); err != nil {
		t.Fatal(err)
	}
	if len(f.tickets(project.ID)) != 1 {
		t.Fatal("confirmed card was not saved")
	}
}

func TestApplicationToolStopsOnAuditFailure(t *testing.T) {
	f, p := nativeFixture(t)
	f.project("portfolio")
	p.answer = func(ctx context.Context, turn provider.Turn, _ func(provider.Event)) (provider.Result, error) {
		f.st.broken.Store(true)
		_, err := turn.CallOperation(ctx, "lookup", "list_projects", json.RawMessage(`{}`))
		return provider.Result{}, err
	}
	s := f.session()
	evs := f.send(s.ID, "list projects")
	if end(evs).Type != EventFailed || end(evs).Error.Code != "audit_unavailable" {
		t.Fatalf("end = %+v", end(evs).Error)
	}
}

func TestApplicationToolLoopIsBounded(t *testing.T) {
	f, p := nativeFixture(t)
	p.answer = func(ctx context.Context, turn provider.Turn, _ func(provider.Event)) (provider.Result, error) {
		for i := 0; i < 100; i++ {
			if _, err := turn.CallOperation(ctx, "lookup", "list_projects", json.RawMessage(`{}`)); err != nil {
				return provider.Result{}, err
			}
		}
		t.Fatal("unbounded native calls")
		return provider.Result{}, nil
	}
	s := f.session()
	evs := f.send(s.ID, "list projects")
	if end(evs).Type != EventFailed || end(evs).Error.Code != "too_many_steps" {
		t.Fatalf("end = %+v", end(evs).Error)
	}
}

func TestApplicationToolCannotBypassGrants(t *testing.T) {
	f, p := nativeFixture(t)
	f.eng.cfg.Grants = []appops.Permission{appops.Permission("projects:read")}
	project := f.project("portfolio")
	p.answer = func(ctx context.Context, turn provider.Turn, emit func(provider.Event)) (provider.Result, error) {
		args, _ := json.Marshal(map[string]string{"projectId": project.ID, "title": "Create the website"})
		result, err := turn.CallOperation(ctx, "create1", "create_ticket", args)
		if err != nil {
			return provider.Result{}, err
		}
		if !strings.Contains(result, "permission_denied") {
			t.Errorf("result = %s", result)
		}
		emit(provider.Event{Kind: provider.EventText, Text: "Permission denied."})
		return provider.Result{}, nil
	}
	s := f.session()
	evs := f.send(s.ID, "add a task")
	if end(evs).Type != EventCompleted || has(evs, EventConfirmationRequired) != nil || len(f.tickets(project.ID)) != 0 {
		t.Fatal("native operation bypassed grants")
	}
}
