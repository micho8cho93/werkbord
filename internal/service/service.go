// Package service holds the application use cases. Services are the only
// code that writes to the store: they validate input, apply domain rules,
// write state and its events in one transaction, then publish the events.
package service

import (
	"context"
	"log/slog"
	"time"

	"devboard/internal/domain"
	"devboard/internal/events"
	"devboard/internal/store"
)

// Deps are the shared dependencies of all services.
type Deps struct {
	Store store.Store
	Bus   events.Publisher
	Log   *slog.Logger
	Now   func() time.Time // defaults to time.Now; overridable in tests
}

func (d Deps) now() time.Time {
	if d.Now != nil {
		return d.Now().UTC().Truncate(time.Millisecond)
	}
	return time.Now().UTC().Truncate(time.Millisecond)
}

func (d Deps) log() *slog.Logger {
	if d.Log == nil {
		return slog.New(slog.DiscardHandler)
	}
	return d.Log
}

// emitter collects events produced inside a transaction.
type emitter struct {
	tx     store.Tx
	ctx    context.Context
	events []domain.Event
}

// emit appends an event to the log inside the current transaction.
func (e *emitter) emit(ev domain.Event) error {
	if err := e.tx.Events().Append(e.ctx, &ev); err != nil {
		return err
	}
	e.events = append(e.events, ev)
	return nil
}

// update runs fn in a write transaction and publishes the events it emitted
// once the transaction has committed. Nothing is published on failure.
func (d Deps) update(ctx context.Context, fn func(tx store.Tx, em *emitter) error) error {
	var em *emitter
	err := d.Store.Update(ctx, func(tx store.Tx) error {
		em = &emitter{tx: tx, ctx: ctx}
		return fn(tx, em)
	})
	if err != nil {
		return err
	}
	if d.Bus != nil && len(em.events) > 0 {
		d.Bus.Publish(em.events...)
	}
	return nil
}

func newEvent(t domain.EventType, payload any) domain.Event {
	ev, err := domain.NewEvent(t, payload)
	if err != nil {
		// Payloads are domain structs that always marshal.
		panic(err)
	}
	return ev
}
