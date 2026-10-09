package workspaces

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"devboard/internal/workspace"
)

// Registry is the list of workspaces on this computer and the person's choice among them.
//
// Choosing a workspace changes what the window shows and nothing else. It does not start, stop, authorize or re-authorize
// anything: an agent that is running keeps the authority it was started with in the program that runs it, whichever
// workspace is on the screen, and a Team workspace's service keeps working for a workspace the person is not looking at.
// That is why Select only reads and writes the shell's own state.
type Registry struct {
	sources []Source
	state   *State
	log     *slog.Logger

	mu    sync.Mutex
	known map[string]known
}

type known struct {
	src    Source
	listed workspace.Listed
}

// NewRegistry makes a registry over sources. The first source is Personal's.
func NewRegistry(state *State, log *slog.Logger, sources ...Source) *Registry {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Registry{sources: sources, state: state, log: log, known: map[string]known{}}
}

// Item is a workspace as the switcher shows it.
type Item struct {
	workspace.Entry
	// Source names the program that holds it.
	Source string `json:"source"`
}

// Problem is something wrong with a source, in words for the person.
type Problem struct {
	Source string `json:"source"`
	// Kind is not_running, refused, outdated, not_set_up or failed.
	Kind   string `json:"kind"`
	Detail string `json:"detail"`
}

// View is everything the switcher needs.
type View struct {
	Items    []Item    `json:"items"`
	Selected string    `json:"selected"`
	Problems []Problem `json:"problems"`
}

func problemOf(source string, err error) Problem {
	p := Problem{Source: source, Kind: "failed", Detail: err.Error()}
	switch {
	case errors.Is(err, ErrNotRunning):
		p.Kind = "not_running"
	case errors.Is(err, ErrRefused):
		p.Kind = "refused"
	case errors.Is(err, ErrOutdated):
		p.Kind = "outdated"
	case errors.Is(err, ErrNoCredential):
		p.Kind = "not_set_up"
	}
	return p
}

// Accessible says whether a workspace can be opened: it exists, and the person is not leaving it or locked out.
func Accessible(e workspace.Entry) bool {
	switch e.State {
	case workspace.StateReady, workspace.StateConnecting, workspace.StateOffline:
		return true
	}
	return false
}

// Refresh asks every source what it holds. A source that is not answering contributes no workspaces and a problem; the
// others are unaffected, which is what lets the person keep working in Personal while a Team workspace's service is down.
func (r *Registry) Refresh(ctx context.Context) View {
	type result struct {
		src Source
		l   []workspace.Listed
		err error
	}
	results := make([]result, len(r.sources))
	var wg sync.WaitGroup
	for i, s := range r.sources {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			l, err := s.List(c)
			results[i] = result{s, l, err}
		}()
	}
	wg.Wait()

	next := map[string]known{}
	var v View
	for _, res := range results {
		if res.err != nil {
			r.log.Debug("source not usable", "source", res.src.Name(), "err", res.err)
			v.Problems = append(v.Problems, problemOf(res.src.Name(), res.err))
			// A source that needs updating may still list what it holds: Personal is always listed, so that the window can
			// say it needs updating instead of showing nothing. Any other failure lists nothing from that source.
			if len(res.l) == 0 || !errors.Is(res.err, ErrOutdated) {
				continue
			}
		}
		for _, l := range res.l {
			if _, dup := next[l.Entry.ID]; dup {
				continue // the first source to name a workspace keeps it
			}
			next[l.Entry.ID] = known{res.src, l}
			v.Items = append(v.Items, Item{Entry: l.Entry, Source: res.src.Name()})
		}
	}
	r.mu.Lock()
	r.known = next
	r.mu.Unlock()
	v.Selected = r.initial(v.Items)
	return v
}

// initial is the workspace to open: the one last used if it is still there and the person can still get into it,
// otherwise Personal.
func (r *Registry) initial(items []Item) string {
	last := r.state.Last()
	for _, it := range items {
		if it.ID == last && Accessible(it.Entry) && it.State != workspace.StateSetup {
			return last
		}
	}
	return workspace.PersonalID
}

// Select makes a workspace the one that is open next time, and returns the view. Only a workspace that is there and can be
// opened can be chosen. It touches no workspace.
func (r *Registry) Select(ctx context.Context, id string) (View, error) {
	v := r.Refresh(ctx)
	for _, it := range v.Items {
		if it.ID == id {
			if !Accessible(it.Entry) && it.State != workspace.StateSetup {
				return v, fmt.Errorf("%s cannot be opened now: %s", it.Name, it.Detail)
			}
			if err := r.state.SetLast(id); err != nil {
				return v, err
			}
			v.Selected = id
			return v, nil
		}
	}
	return v, errors.New("that workspace is not on this computer")
}

func (r *Registry) lookup(id string) (known, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	k, ok := r.known[id]
	return k, ok
}

// Target is where to load a workspace's interface, and where in it the person last was.
type Target struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	URL    string `json:"url"`
	Origin string `json:"origin"`
	// Place is where the person was in it last time: a link inside the workspace, or empty.
	Place string `json:"place,omitempty"`
}

// Target returns how to open a workspace. join is an invitation to hand to a Team workspace being set up.
func (r *Registry) Target(ctx context.Context, id, join string) (Target, error) {
	k, ok := r.lookup(id)
	if !ok {
		r.Refresh(ctx)
		if k, ok = r.lookup(id); !ok {
			return Target{}, errors.New("that workspace is not on this computer")
		}
	}
	f, err := k.src.Frame(ctx, k.listed, join)
	if err != nil {
		return Target{}, err
	}
	return Target{ID: id, Kind: string(k.listed.Entry.Kind), URL: f.URL, Origin: f.Origin, Place: r.state.Place(id)}, nil
}

// Kind says what a known workspace is, for deciding what its pages may ask the app. It is the registry's knowledge, never a
// page's claim.
func (r *Registry) Kind(id string) (workspace.Kind, bool) {
	k, ok := r.lookup(id)
	if !ok {
		return "", false
	}
	return k.listed.Entry.Kind, true
}

// Remember records where in a workspace the person is. The place is checked (a link inside a workspace); the workspace must
// be a known one.
func (r *Registry) Remember(id, place string) error {
	if _, ok := r.lookup(id); !ok {
		return errors.New("that workspace is not on this computer")
	}
	return r.state.SetPlace(id, place)
}

// Forget drops what the shell remembers about a workspace that is no longer on this computer.
func (r *Registry) Forget(id string) { r.state.Forget(id) }

// Entry is one workspace's summary, or why there is none.
type Entry struct {
	Workspace workspace.Entry    `json:"workspace"`
	Summary   *workspace.Summary `json:"summary,omitempty"`
	// Error is why the summary could not be read.
	Error string `json:"error,omitempty"`
}

// Overview is what needs the person across every workspace they can open, one entry per workspace.
type Overview struct {
	Entries []Entry   `json:"entries"`
	At      time.Time `json:"at"`
}

// Overview reads every accessible workspace's summary at the same time. A workspace that does not answer is reported as
// not answering; it never makes another's summary wait for it or disappear.
func (r *Registry) Overview(ctx context.Context) Overview {
	v := r.Refresh(ctx)
	out := Overview{Entries: make([]Entry, len(v.Items)), At: time.Now().UTC()}
	var wg sync.WaitGroup
	for i, it := range v.Items {
		out.Entries[i] = Entry{Workspace: it.Entry}
		if it.State != workspace.StateReady && it.State != workspace.StateOffline {
			continue
		}
		k, ok := r.lookup(it.ID)
		if !ok {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, cancel := context.WithTimeout(ctx, 6*time.Second)
			defer cancel()
			s, err := k.src.Summary(c, k.listed)
			if err != nil {
				out.Entries[i].Error = summaryProblem(err)
				return
			}
			out.Entries[i].Summary = &s
			// The workspace may know more than the list did (it is offline, or it just came up).
			out.Entries[i].Workspace = s.Workspace
		}()
	}
	wg.Wait()
	return out
}

func summaryProblem(err error) string {
	switch {
	case errors.Is(err, ErrNotRunning):
		return "Its service is not running."
	case errors.Is(err, ErrRefused):
		return "Its service did not accept this app."
	case errors.Is(err, ErrOutdated):
		return "Its service needs updating."
	case errors.Is(err, context.DeadlineExceeded):
		return "It did not answer in time."
	}
	return "It could not be read."
}
