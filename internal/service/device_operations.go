package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"werkbord/internal/devicewire"
	"werkbord/internal/domain"
	"werkbord/internal/runnerwire"
	"werkbord/internal/store"
)

// DeviceOperations is an authoritative command queue, carried by Runners.Sync.
// Only the executor on the owning machine resolves paths or starts processes.
type DeviceOperations struct {
	Deps
	Runners      *Runners
	LocalID      string
	Execute      func(context.Context, devicewire.Operation) devicewire.Result
	ActorAllowed func(string) bool
	paused       atomic.Bool
	localActive  atomic.Int64
}
type terminalGrant struct {
	Runner, Project, Session string
	Clients                  []string
}

func operationID(actor string, r devicewire.Request) string {
	sum := sha256.Sum256([]byte(actor + ":" + r.RequestID))
	return "op_" + hex.EncodeToString(sum[:16])
}
func requestHash(r devicewire.Request) string {
	raw, _ := json.Marshal(r)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func (d *DeviceOperations) actorAllowed(actor string) bool {
	return actor == "owner" || d.ActorAllowed != nil && d.ActorAllowed(actor)
}
func (d *DeviceOperations) Submit(ctx context.Context, actor string, r devicewire.Request) (*devicewire.Record, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	if !d.actorAllowed(actor) {
		return nil, domain.ErrForbidden
	}
	id, hash := operationID(actor, r), requestHash(r)
	var out *devicewire.Record
	err := d.Store.Update(ctx, func(tx store.Tx) error {
		prior, err := tx.DeviceOperations().Get(ctx, id)
		if err == nil {
			if prior.Hash != hash {
				return domain.ErrConflict
			}
			out = prior
			return d.authorize(ctx, tx, actor, prior.Operation.Request)
		}
		if !errors.Is(err, domain.ErrNotFound) {
			return err
		}
		if d.paused.Load() {
			return fmt.Errorf("%w: controller is switching", domain.ErrConflict)
		}
		if err = d.authorize(ctx, tx, actor, r); err != nil {
			return err
		}
		runner, err := tx.Runners().Get(ctx, r.RunnerID)
		if err != nil {
			return err
		}
		if runner.Kind != domain.RunnerLocal && (runner.Removed || runner.Disabled || !runner.Capabilities.DeviceOperations || d.now().Sub(runner.LastSeenAt) > 30*time.Second) {
			return fmt.Errorf("%w: owning runner is unavailable or requires an update", domain.ErrConflict)
		}
		pending, err := tx.DeviceOperations().Pending(ctx, r.RunnerID)
		if err != nil {
			return err
		}
		if len(pending) >= devicewire.MaxPending {
			return domain.ErrConflict
		}
		repo, err := tx.Repositories().Get(ctx, r.ProjectID)
		if err != nil {
			return err
		}
		remote := ""
		for _, value := range repo.Remotes {
			if value.Name == "origin" {
				remote = value.URL
			}
		}
		if remote != "" && !runnerwire.SafeRemote(remote) {
			return fmt.Errorf("%w: origin must use a credential-free SSH or HTTPS address", domain.ErrInvalid)
		}
		out = &devicewire.Record{Operation: devicewire.Operation{ID: id, Actor: actor, Request: r, RemoteURL: remote, CreatedAt: d.now(), ExpiresAt: d.now().Add(2 * time.Minute)}, Hash: hash, Result: devicewire.Result{ID: id, State: "pending"}, UpdatedAt: d.now()}
		return tx.DeviceOperations().Save(ctx, out)
	})
	return out, err
}
func (d *DeviceOperations) authorize(ctx context.Context, tx store.Tx, actor string, r devicewire.Request) error {
	if !d.actorAllowed(actor) {
		return domain.ErrForbidden
	}
	p, err := tx.Projects().Get(ctx, r.ProjectID)
	if err != nil {
		return err
	}
	if p.Inactive() || !p.HasRepository() {
		return domain.ErrForbidden
	}
	runner, err := tx.Runners().Get(ctx, r.RunnerID)
	if err != nil {
		return err
	}
	if runner.Removed || runner.Disabled {
		return domain.ErrForbidden
	}
	if runner.Kind != domain.RunnerLocal && !contains(runner.Projects, r.ProjectID) {
		return domain.ErrForbidden
	}
	if r.RunID != "" {
		run, err := tx.Runs().Get(ctx, r.RunID)
		if err != nil {
			return err
		}
		if run.ProjectID != r.ProjectID || (run.RunnerID != r.RunnerID && !(run.RunnerID == "" && !run.Remote && runner.Kind == domain.RunnerLocal)) {
			return domain.ErrNotFound
		}
		if !run.State.Terminal() && strings.HasPrefix(r.Kind, "git.") {
			return fmt.Errorf("%w: wait for the run to finish before Git operations", domain.ErrConflict)
		}
	}
	if strings.HasPrefix(r.Kind, "terminal.") {
		if r.Kind == "terminal.create" {
			if actor != "owner" {
				return domain.ErrForbidden
			}
			return nil
		}
		if actor != "owner" {
			var grant terminalGrant
			if err = tx.Settings().Get(ctx, terminalGrantKey(r.RunnerID, r.SessionID), &grant); err != nil {
				return domain.ErrForbidden
			}
			if grant.Runner != r.RunnerID || grant.Project != r.ProjectID || !contains(grant.Clients, actor) {
				return domain.ErrForbidden
			}
		}
	}
	return nil
}
func (d *DeviceOperations) Get(ctx context.Context, actor, id string) (*devicewire.Record, error) {
	var out *devicewire.Record
	err := d.Store.Update(ctx, func(tx store.Tx) error {
		var err error
		out, err = tx.DeviceOperations().Get(ctx, id)
		if err != nil {
			return err
		}
		if actor != "owner" && out.Operation.Actor != actor {
			return domain.ErrNotFound
		}
		if err := d.authorize(ctx, tx, actor, out.Operation.Request); err != nil {
			return err
		}
		if out.Result.State == "pending" && !d.now().Before(out.Operation.ExpiresAt) {
			out.Result = devicewire.Result{ID: out.Operation.ID, State: "uncertain", Code: "coordination_ended", Error: "coordination expired; inspect the owning machine before issuing another command"}
			out.UpdatedAt = d.now()
			return tx.DeviceOperations().Save(ctx, out)
		}
		return nil
	})
	return out, err
}
func (d *DeviceOperations) Wait(ctx context.Context, actor, id string) (*devicewire.Record, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	timer := time.NewTicker(100 * time.Millisecond)
	defer timer.Stop()
	for {
		record, err := d.Get(ctx, actor, id)
		if err != nil {
			return nil, err
		}
		if record.Result.State != "pending" {
			return record, nil
		}
		select {
		case <-ctx.Done():
			return record, nil
		case <-timer.C:
		}
	}
}
func (d *DeviceOperations) applyResult(ctx context.Context, tx store.Tx, runner string, result devicewire.Result) error {
	raw, err := json.Marshal(result)
	if err != nil || len(raw) > devicewire.MaxResultBytes {
		return domain.ErrInvalid
	}
	if result.State != "done" && result.State != "failed" && result.State != "uncertain" {
		return domain.ErrInvalid
	}
	item, err := tx.DeviceOperations().Get(ctx, result.ID)
	if err != nil {
		return err
	}
	if item.Operation.Request.RunnerID != runner {
		return domain.ErrForbidden
	}
	if item.Result.State != "pending" && !(item.Result.State == "uncertain" && item.Result.Code == "coordination_ended") {
		return nil
	}
	// Results are observations. Actor/session revocation prevents delivery and new
	// dispatch, but the originating write's receipt is still retained for recovery.
	item.Result = result
	item.UpdatedAt = d.now()
	return tx.DeviceOperations().Save(ctx, item)
}
func (d *DeviceOperations) sync(ctx context.Context, tx store.Tx, runner *domain.Runner, in []devicewire.Result, out *[]devicewire.Operation, acks *[]string) error {
	if len(in) > 8 {
		return domain.ErrInvalid
	}
	for _, result := range in {
		if err := d.applyResult(ctx, tx, runner.ID, result); err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				*acks = append(*acks, result.ID)
				continue
			}
			return err
		}
		*acks = append(*acks, result.ID)
	}
	pending, err := tx.DeviceOperations().Pending(ctx, runner.ID)
	if err != nil {
		return err
	}
	for _, item := range pending {
		if !d.now().Before(item.Operation.ExpiresAt) || d.authorize(ctx, tx, item.Operation.Actor, item.Operation.Request) != nil {
			item.Result = devicewire.Result{ID: item.Operation.ID, State: "uncertain", Code: "coordination_ended", Error: "operation permission or coordination expired; inspect its outcome on the owning machine before repeating it"}
			item.UpdatedAt = d.now()
			if err = tx.DeviceOperations().Save(ctx, &item); err != nil {
				return err
			}
			continue
		}
		if len(*out) < 8 {
			*out = append(*out, item.Operation)
		}
	}
	return tx.DeviceOperations().Prune(ctx, d.now().Add(-10*time.Minute))
}
func (d *DeviceOperations) RunLocal(ctx context.Context) {
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if d.Execute == nil || d.paused.Load() {
			continue
		}
		var work []devicewire.Operation
		_ = d.Store.Update(ctx, func(tx store.Tx) error {
			runner, err := tx.Runners().Get(ctx, d.LocalID)
			if err != nil {
				return err
			}
			acks := []string{}
			return d.sync(ctx, tx, runner, nil, &work, &acks)
		})
		// One controller executor loop serialises its own writes; the remote worker
		// keeps the same durable operation ID when the heartbeat is retried.
		for _, op := range work {
			if ctx.Err() != nil {
				return
			}
			d.localActive.Add(1)
			result := d.Execute(ctx, op)
			if result.State == "pending" {
				d.localActive.Add(-1)
				continue
			}
			_ = d.Store.Update(context.Background(), func(tx store.Tx) error { return d.applyResult(context.Background(), tx, d.LocalID, result) })
			d.localActive.Add(-1)
		}
	}
}
func (d *DeviceOperations) Pause(ctx context.Context) error {
	d.paused.Store(true)
	err := d.Store.Update(ctx, func(tx store.Tx) error {
		if d.localActive.Load() > 0 {
			return fmt.Errorf("%w: a device operation is executing on this computer", domain.ErrConflict)
		}
		runners, err := tx.Runners().List(ctx)
		if err != nil {
			return err
		}
		for _, r := range runners {
			pending, err := tx.DeviceOperations().Pending(ctx, r.ID)
			if err != nil {
				return err
			}
			if len(pending) > 0 {
				return fmt.Errorf("%w: settle device operations before switching controller", domain.ErrConflict)
			}
		}
		return nil
	})
	if err != nil {
		d.paused.Store(false)
	}
	return err
}
func (d *DeviceOperations) Resume() { d.paused.Store(false) }
func terminalGrantKey(runner, session string) string {
	return "terminal-client:" + runner + ":" + session
}
func (d *DeviceOperations) GrantTerminal(ctx context.Context, runner, project, session, client string, enabled bool) error {
	return d.Store.Update(ctx, func(tx store.Tx) error {
		var grant terminalGrant
		err := tx.Settings().Get(ctx, terminalGrantKey(runner, session), &grant)
		if err != nil && !errors.Is(err, domain.ErrNotFound) {
			return err
		}
		grant.Runner, grant.Project, grant.Session = runner, project, session
		clients := []string{}
		for _, id := range grant.Clients {
			if id != client {
				clients = append(clients, id)
			}
		}
		if enabled {
			clients = append(clients, client)
		}
		grant.Clients = clients
		return tx.Settings().Set(ctx, terminalGrantKey(runner, session), grant, d.now())
	})
}
func (d *DeviceOperations) TerminalAllowed(ctx context.Context, actor, runner, project, session string) bool {
	if actor == "owner" {
		return true
	}
	return d.Store.View(ctx, func(tx store.Tx) error {
		return d.authorize(ctx, tx, actor, devicewire.Request{Kind: "terminal.output", RunnerID: runner, ProjectID: project, SessionID: session})
	}) == nil
}
func (d *DeviceOperations) RemoteTerminals(ctx context.Context) ([]devicewire.Terminal, error) {
	out := []devicewire.Terminal{}
	err := d.Store.View(ctx, func(tx store.Tx) error {
		runners, err := tx.Runners().List(ctx)
		if err != nil {
			return err
		}
		for _, r := range runners {
			if r.Kind == domain.RunnerLocal || r.Removed {
				continue
			}
			var terminals []devicewire.Terminal
			if e := tx.Settings().Get(ctx, "runner-terminals:"+r.ID, &terminals); e != nil && !errors.Is(e, domain.ErrNotFound) {
				return e
			}
			out = append(out, terminals...)
		}
		return nil
	})
	return out, err
}

func (d *DeviceOperations) AuthorizeOperation(ctx context.Context, op devicewire.Operation) error {
	return d.Store.View(ctx, func(tx store.Tx) error { return d.authorize(ctx, tx, op.Actor, op.Request) })
}
func (d *DeviceOperations) PendingCount(ctx context.Context) (int, error) {
	total := int(d.localActive.Load())
	err := d.Store.View(ctx, func(tx store.Tx) error {
		runners, err := tx.Runners().List(ctx)
		if err != nil {
			return err
		}
		for _, r := range runners {
			pending, err := tx.DeviceOperations().Pending(ctx, r.ID)
			if err != nil {
				return err
			}
			total += len(pending)
		}
		return nil
	})
	return total, err
}
