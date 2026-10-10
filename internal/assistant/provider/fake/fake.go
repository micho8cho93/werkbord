// Package fake is a provider that does what a test tells it to, in process. It is for tests of the engine; the real
// providers are tested against fake command lines in their own packages.
package fake

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"devboard/internal/assistant/provider"
)

// Step is what one turn does.
type Step struct {
	// Chunks are the pieces of the reply, in order.
	Chunks []string
	// Delay is waited before each chunk.
	Delay time.Duration
	// Err ends the turn with this error after the chunks.
	Err error
	// Hang makes the turn wait for its context after the chunks: it does not end by itself.
	Hang bool
	// Silent is Hang without the signs of life: nothing is emitted while it waits.
	Silent bool
	// Ref is the handle reported; default "fake-<n>" for a new conversation, the turn's own for a continued one.
	Ref string
	// NoRef makes the turn report no handle at all.
	NoRef bool
}

// Reply is a step that says text in the pieces given.
func Reply(chunks ...string) Step { return Step{Chunks: chunks} }

// Provider implements provider.Provider.
type Provider struct {
	id   string
	mu   sync.Mutex
	info provider.Info
	// steps are consumed one per turn; when they run out the provider says "ok".
	steps []Step
	// Turns records every turn it was asked to run.
	turns []provider.Turn
	// known are the conversations it can continue; a turn that continues another gets session_lost.
	known map[string]bool
	n     int
}

var _ provider.Provider = (*Provider)(nil)

// New returns a provider that is installed and signed in.
func New(id string) *Provider {
	yes := true
	return &Provider{id: id, known: map[string]bool{}, info: provider.Info{ID: id, Name: "Fake " + id, Installed: true, SignedIn: &yes, Available: true, Version: "0",
		Capabilities: provider.Capabilities{Streaming: provider.StreamTokens, Resume: true, Cancel: true, ModelChoice: true, NativeTools: "none", Voice: "none"}}}
}

// Queue adds steps for the next turns.
func (p *Provider) Queue(steps ...Step) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.steps = append(p.steps, steps...)
}

// Turns returns the turns run so far.
func (p *Provider) Turns() []provider.Turn {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]provider.Turn(nil), p.turns...)
}

// Forget drops a conversation, as if the provider had lost it.
func (p *Provider) Forget(ref string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.known, ref)
}

// SetInfo changes what Detect reports.
func (p *Provider) SetInfo(f func(*provider.Info)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	f(&p.info)
}

// ID implements provider.Provider.
func (p *Provider) ID() string { return p.id }

// Detect implements provider.Provider.
func (p *Provider) Detect(context.Context) provider.Info {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.info
}

// Models implements provider.Provider.
func (p *Provider) Models(context.Context) provider.Models {
	return provider.Models{ProviderID: p.id, Source: "builtin", Custom: true, Models: []provider.Model{
		{ID: "m1", Name: "M1", Default: true, Reasoning: []string{"low", "high"}},
		{ID: "m2", Name: "M2", Reasoning: []string{"medium"}},
	}}
}

// Run implements provider.Provider.
func (p *Provider) Run(ctx context.Context, t provider.Turn, emit func(provider.Event)) (provider.Result, error) {
	p.mu.Lock()
	p.turns = append(p.turns, t)
	ref := t.Ref
	if ref != "" && !p.known[ref] {
		p.mu.Unlock()
		return provider.Result{}, provider.Errorf(provider.KindSessionLost, false, "the provider no longer has this conversation")
	}
	var step Step
	if len(p.steps) > 0 {
		step, p.steps = p.steps[0], p.steps[1:]
	} else {
		step = Reply("ok")
	}
	if ref == "" {
		p.n++
		ref = fmt.Sprintf("fake-%d", p.n)
	}
	if step.Ref != "" {
		ref = step.Ref
	}
	p.known[ref] = true
	p.mu.Unlock()

	if !step.NoRef {
		emit(provider.Event{Kind: provider.EventRef, Ref: ref})
	}
	var text strings.Builder
	for _, c := range step.Chunks {
		if step.Delay > 0 {
			select {
			case <-time.After(step.Delay):
			case <-ctx.Done():
				return provider.Result{}, ctx.Err()
			}
		}
		if ctx.Err() != nil {
			return provider.Result{}, ctx.Err()
		}
		text.WriteString(c)
		emit(provider.Event{Kind: provider.EventText, Text: c})
	}
	if step.Hang || step.Silent {
		if step.Hang {
			ticker := time.NewTicker(20 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return provider.Result{}, ctx.Err()
				case <-ticker.C:
					emit(provider.Event{Kind: provider.EventAlive})
				}
			}
		}
		<-ctx.Done()
		return provider.Result{}, ctx.Err()
	}
	if step.Err != nil {
		return provider.Result{}, step.Err
	}
	return provider.Result{Text: text.String(), Ref: ref, Usage: provider.Usage{InputTokens: 10, OutputTokens: 2}}, nil
}
