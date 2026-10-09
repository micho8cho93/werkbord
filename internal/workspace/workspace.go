// Package workspace is the contract between the Werkbord desktop shell and the places a person's work lives.
//
// A person has one Personal workspace (their own computer's Werkbord) and any number of Team workspaces. The shell shows
// them side by side, and shows what needs the person across all of them: their work, what waits for them, what is
// scheduled. It does that without knowing what a Team is or how a controller stores a task. Each place answers one
// question in the words below, and the shell puts the answers together.
//
// The package holds no behaviour of either product: no storage, no network, no credential, nothing that executes. It is
// plumbing, in the sense of docs/PRODUCTS.md, and so both products may import it. What it does hold is the rule for
// what the shell will believe. A summary comes from another program, so the shell reads it strictly: unknown fields are
// refused, sizes are bounded, text is plain, and a link can point only inside the workspace it came from (a fragment or a
// query on that workspace's own address), never to another address or another workspace.
package workspace

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// Schema names this version of the contract. A summary that says anything else is refused, not guessed at.
const Schema = "werkbord.workspace/v1"

// Bounds. A provider that exceeds one is not trusted with the rest.
const (
	MaxBytes     = 512 << 10
	MaxProjects  = 200
	MaxItems     = 200
	MaxAttention = 100
	MaxSchedule  = 200
	MaxWarnings  = 20
	MaxText      = 300
	MaxName      = 120
	MaxHref      = 300
)

// Kind is what sort of workspace it is.
type Kind string

const (
	// KindPersonal is a person's own Werkbord on their own computer. There is exactly one, and it is always there.
	KindPersonal Kind = "personal"
	// KindTeam is a shared workspace that coordinates a team.
	KindTeam Kind = "team"
)

// PersonalID is the identity of the Personal workspace.
const PersonalID = "personal"

// TeamID names a Team workspace by the slot it occupies on this computer.
func TeamID(slot string) string { return "team:" + slot }

// State is how far a workspace is from being usable, in one word the switcher can show.
type State string

const (
	// StateReady: usable now.
	StateReady State = "ready"
	// StateConnecting: starting or reconnecting; it usually settles by itself.
	StateConnecting State = "connecting"
	// StateOffline: its service runs but cannot reach what it needs (for Team, a Workspace Host).
	StateOffline State = "offline"
	// StateSetup: nothing is connected yet; the person is creating or joining.
	StateSetup State = "setup"
	// StateLeaving: the person is leaving it; it is being detached.
	StateLeaving State = "leaving"
	// StateUnavailable: its service is not installed or not running.
	StateUnavailable State = "unavailable"
)

// Valid reports whether s is a state.
func (s State) Valid() bool {
	switch s {
	case StateReady, StateConnecting, StateOffline, StateSetup, StateLeaving, StateUnavailable:
		return true
	}
	return false
}

// Entry is one workspace as the switcher lists it.
type Entry struct {
	ID   string `json:"id"`
	Kind Kind   `json:"kind"`
	Name string `json:"name"`
	// Role is the person's role in a Team workspace (owner, admin or member). Empty for Personal.
	Role   string `json:"role,omitempty"`
	State  State  `json:"state"`
	Detail string `json:"detail,omitempty"`
	// DeviceRoles are what this computer does for the workspace: runner, workspace_host, connectivity_host.
	DeviceRoles []string `json:"deviceRoles,omitempty"`
}

// Project is a project in a workspace, with where in the workspace's own interface it is.
type Project struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Href string `json:"href"`
}

// Execution is where a piece of the person's work stands as an agent run, if there is one. The words are the same in
// both products so the shell can show them the same way.
type Execution string

const (
	ExecNone       Execution = ""
	ExecQueued     Execution = "queued"
	ExecRunning    Execution = "running"
	ExecNeedsInput Execution = "needs_input"
	ExecBlocked    Execution = "blocked"
	ExecCompleted  Execution = "completed"
	ExecFailed     Execution = "failed"
	ExecCanceled   Execution = "canceled"
)

// Valid reports whether e is one of the words.
func (e Execution) Valid() bool {
	switch e {
	case ExecNone, ExecQueued, ExecRunning, ExecNeedsInput, ExecBlocked, ExecCompleted, ExecFailed, ExecCanceled:
		return true
	}
	return false
}

// Status is where a task or ticket is on its board, in four words that mean the same everywhere.
type Status string

const (
	StatusTodo   Status = "todo"
	StatusDoing  Status = "doing"
	StatusReview Status = "review"
	StatusDone   Status = "done"
)

// Valid reports whether s is one of the words.
func (s Status) Valid() bool {
	switch s {
	case StatusTodo, StatusDoing, StatusReview, StatusDone:
		return true
	}
	return false
}

// Item is a piece of the person's own work: a task of theirs, or a ticket they hold.
type Item struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Project   string    `json:"project"`
	Status    Status    `json:"status"`
	Execution Execution `json:"execution,omitempty"`
	Href      string    `json:"href"`
	UpdatedAt time.Time `json:"updatedAt,omitempty"`
}

// Attention kinds.
const (
	AttentionNeedsInput       = "needs_input"
	AttentionBlocked          = "blocked"
	AttentionFailed           = "failed"
	AttentionReview           = "review"
	AttentionChangesRequested = "changes_requested"
	AttentionConflict         = "conflict"
	AttentionHost             = "host"
	AttentionRepository       = "repository"
	AttentionRunner           = "runner"
	AttentionOther            = "other"
)

// Severity orders attention.
type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityWarning  Severity = "warning"
	SeverityCritical Severity = "critical"
)

// Valid reports whether s is a severity.
func (s Severity) Valid() bool {
	return s == SeverityInfo || s == SeverityWarning || s == SeverityCritical
}

// Attention is something that waits for the person, or that they should know about.
type Attention struct {
	ID       string   `json:"id"`
	Kind     string   `json:"kind"`
	Severity Severity `json:"severity"`
	Title    string   `json:"title"`
	Detail   string   `json:"detail,omitempty"`
	Href     string   `json:"href,omitempty"`
	// Project names the project it is about, when it is about one.
	Project string    `json:"project,omitempty"`
	At      time.Time `json:"at,omitempty"`
}

// Scheduled is work that is set to start at a time.
type Scheduled struct {
	ID      string    `json:"id"`
	Title   string    `json:"title"`
	Project string    `json:"project"`
	At      time.Time `json:"at"`
	// Zone is the time zone the schedule was written in, for display.
	Zone string `json:"zone,omitempty"`
	// State is a short word or two from the workspace: scheduled, waiting, claimed, blocked, missed, started.
	State string `json:"state"`
	Href  string `json:"href"`
}

// Check is one line of a health report.
type Check struct {
	Label string `json:"label"`
	Value string `json:"value"`
	// State is ok, warn, bad or unknown.
	State string `json:"state"`
}

// Infra is what a person who shares a workspace needs to know about the machines that keep it running: only Team
// workspaces have it.
type Infra struct {
	// Writable: changes can be saved now.
	Writable        bool    `json:"writable"`
	HostsConfigured int     `json:"hostsConfigured"`
	HostsOnline     int     `json:"hostsOnline"`
	Checks          []Check `json:"checks,omitempty"`
	// Warnings are sentences, worst first.
	Warnings []string `json:"warnings,omitempty"`
}

// Listed is a workspace as a provider lists it: the workspace, and where its own interface and its summary are on the
// provider's address. The paths are the provider's own and are checked: a provider can point the shell only at itself.
type Listed struct {
	Entry Entry `json:"entry"`
	// Root is where the workspace's own interface is, ending in a slash: "/" for Personal, "/w/<slot>/" for a Team workspace.
	Root string `json:"root"`
	// SummaryPath is where its Summary is.
	SummaryPath string `json:"summary"`
}

// Listing is what a provider that holds several workspaces answers when asked which it has.
type Listing struct {
	Schema     string   `json:"schema"`
	Workspaces []Listed `json:"workspaces"`
}

// MaxListed bounds a listing.
const MaxListed = 32

var pathPattern = regexp.MustCompile(`^/[A-Za-z0-9_/.-]{0,100}$`)

// ValidPath reports whether s is a path on the provider's own address: absolute, plain, and with nothing in it that climbs.
func ValidPath(s string) bool {
	return pathPattern.MatchString(s) && !strings.Contains(s, "//") && !strings.Contains(s, "..")
}

// DecodeListing reads a listing strictly: bounded, unknown fields refused, every entry valid, every path the provider's own,
// and no workspace named twice.
func DecodeListing(r io.Reader) (Listing, error) {
	raw, err := io.ReadAll(io.LimitReader(r, MaxBytes+1))
	if err != nil {
		return Listing{}, err
	}
	if len(raw) > MaxBytes {
		return Listing{}, errors.New("workspace: the listing is too large")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var l Listing
	if err := dec.Decode(&l); err != nil {
		return Listing{}, fmt.Errorf("workspace: the listing is unreadable: %w", err)
	}
	if dec.More() {
		return Listing{}, errors.New("workspace: the listing has more than one document")
	}
	if l.Schema != Schema {
		return Listing{}, fmt.Errorf("workspace: schema %q is not %q", l.Schema, Schema)
	}
	if len(l.Workspaces) > MaxListed {
		return Listing{}, errors.New("workspace: too many workspaces")
	}
	seen := map[string]bool{}
	for i := range l.Workspaces {
		e := &l.Workspaces[i]
		e.Entry.Name, e.Entry.Detail = clip(e.Entry.Name, MaxName), clip(e.Entry.Detail, 500)
		if err := e.Entry.validate(); err != nil {
			return Listing{}, err
		}
		if !ValidPath(e.Root) || !strings.HasSuffix(e.Root, "/") || !ValidPath(e.SummaryPath) {
			return Listing{}, fmt.Errorf("workspace: %q has a path that is not its provider's own", e.Entry.ID)
		}
		if seen[e.Entry.ID] {
			return Listing{}, fmt.Errorf("workspace: %q is listed twice", e.Entry.ID)
		}
		seen[e.Entry.ID] = true
	}
	return l, nil
}

// Summary is everything the shell shows of one workspace outside the workspace's own interface.
type Summary struct {
	Schema    string      `json:"schema"`
	Workspace Entry       `json:"workspace"`
	Projects  []Project   `json:"projects"`
	Work      []Item      `json:"work"`
	Attention []Attention `json:"attention"`
	Schedule  []Scheduled `json:"schedule"`
	Infra     *Infra      `json:"infra,omitempty"`
	At        time.Time   `json:"at"`
}

var (
	idPattern   = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,80}$`)
	hrefPattern = regexp.MustCompile(`^[#?][A-Za-z0-9/_.:=&%?#-]{0,299}$`)
)

// ValidID reports whether s can be an identity: short, with nothing in it that means something to a path or a page.
func ValidID(s string) bool { return idPattern.MatchString(s) }

// ValidHref reports whether s is a place inside a workspace's own interface: a fragment (#/p/…) or a query (?tab=…) on the
// workspace's address. It cannot name another address, a scheme, a host or a path, so a link in a summary can move
// the workspace's own page and nothing else.
func ValidHref(s string) bool {
	if !hrefPattern.MatchString(s) {
		return false
	}
	return !strings.Contains(s, "//") && !strings.Contains(s, "..")
}

func plain(s string, max int) bool {
	if !utf8.ValidString(s) || utf8.RuneCountInString(s) > max {
		return false
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f || r == 0x2028 || r == 0x2029 {
			return false
		}
	}
	return true
}

func clip(s string, max int) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || r == 0x2028 || r == 0x2029 {
			return ' '
		}
		return r
	}, strings.ToValidUTF8(s, ""))
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) > max {
		rs := []rune(s)
		s = strings.TrimSpace(string(rs[:max-1])) + "…"
	}
	return s
}

// Clip makes text acceptable: single-line, valid, trimmed, and no longer than max characters. Providers use it on
// whatever a person typed (a task's title) before putting it in a summary.
func Clip(s string, max int) string { return clip(s, max) }

// Validate checks a whole summary as a strict reader would. It reports the first problem.
func (s Summary) Validate() error {
	if s.Schema != Schema {
		return fmt.Errorf("workspace: schema %q is not %q", s.Schema, Schema)
	}
	if err := s.Workspace.validate(); err != nil {
		return err
	}
	if len(s.Projects) > MaxProjects || len(s.Work) > MaxItems || len(s.Attention) > MaxAttention || len(s.Schedule) > MaxSchedule {
		return errors.New("workspace: too many entries")
	}
	for _, p := range s.Projects {
		if !ValidID(p.ID) || !plain(p.Name, MaxName) || p.Name == "" || !ValidHref(p.Href) {
			return fmt.Errorf("workspace: project %q is not valid", p.ID)
		}
	}
	for _, it := range s.Work {
		if !ValidID(it.ID) || !plain(it.Title, MaxText) || it.Title == "" || !plain(it.Project, MaxName) || !it.Status.Valid() || !it.Execution.Valid() || !ValidHref(it.Href) {
			return fmt.Errorf("workspace: work item %q is not valid", it.ID)
		}
	}
	for _, a := range s.Attention {
		if !ValidID(a.ID) || !ValidID(a.Kind) || !a.Severity.Valid() || !plain(a.Title, MaxText) || a.Title == "" || !plain(a.Detail, MaxText) || !plain(a.Project, MaxName) || (a.Href != "" && !ValidHref(a.Href)) {
			return fmt.Errorf("workspace: attention %q is not valid", a.ID)
		}
	}
	for _, c := range s.Schedule {
		if !ValidID(c.ID) || !plain(c.Title, MaxText) || c.Title == "" || !plain(c.Project, MaxName) || c.At.IsZero() || !plain(c.Zone, 64) || !plain(c.State, 40) || !ValidHref(c.Href) {
			return fmt.Errorf("workspace: scheduled item %q is not valid", c.ID)
		}
	}
	if in := s.Infra; in != nil {
		if len(in.Warnings) > MaxWarnings || len(in.Checks) > MaxWarnings || in.HostsConfigured < 0 || in.HostsOnline < 0 || in.HostsOnline > in.HostsConfigured {
			return errors.New("workspace: infrastructure report is not valid")
		}
		for _, w := range in.Warnings {
			if !plain(w, 500) || w == "" {
				return errors.New("workspace: infrastructure warning is not valid")
			}
		}
		for _, c := range in.Checks {
			if !plain(c.Label, MaxName) || !plain(c.Value, MaxText) || (c.State != "ok" && c.State != "warn" && c.State != "bad" && c.State != "unknown") {
				return errors.New("workspace: infrastructure check is not valid")
			}
		}
	}
	return nil
}

func (e Entry) validate() error {
	if !ValidID(e.ID) || (e.Kind != KindPersonal && e.Kind != KindTeam) || !plain(e.Name, MaxName) || e.Name == "" || !e.State.Valid() || !plain(e.Detail, 500) || !plain(e.Role, 20) {
		return fmt.Errorf("workspace: workspace %q is not valid", e.ID)
	}
	if (e.Kind == KindPersonal) != (e.ID == PersonalID) {
		return fmt.Errorf("workspace: %q cannot be a %s workspace", e.ID, e.Kind)
	}
	if e.Kind == KindTeam && !strings.HasPrefix(e.ID, "team:") {
		return fmt.Errorf("workspace: team workspace %q is not named team:<slot>", e.ID)
	}
	for _, r := range e.DeviceRoles {
		switch r {
		case "runner", "workspace_host", "connectivity_host":
		default:
			return fmt.Errorf("workspace: device role %q is not one", r)
		}
	}
	return nil
}

// Clean makes a summary acceptable by dropping the entries that are not and clipping the text that is too long, rather
// than refusing everything for one bad row. The workspace entry itself must be valid: without it the summary says
// nothing the shell can trust. The count is how many entries were dropped.
func (s Summary) Clean() (Summary, int) {
	dropped := 0
	out := s
	out.Projects = make([]Project, 0, len(s.Projects))
	for _, p := range s.Projects {
		p.Name = clip(p.Name, MaxName)
		if ValidID(p.ID) && p.Name != "" && ValidHref(p.Href) && len(out.Projects) < MaxProjects {
			out.Projects = append(out.Projects, p)
		} else {
			dropped++
		}
	}
	out.Work = make([]Item, 0, len(s.Work))
	for _, it := range s.Work {
		it.Title, it.Project = clip(it.Title, MaxText), clip(it.Project, MaxName)
		if ValidID(it.ID) && it.Title != "" && it.Status.Valid() && it.Execution.Valid() && ValidHref(it.Href) && len(out.Work) < MaxItems {
			out.Work = append(out.Work, it)
		} else {
			dropped++
		}
	}
	out.Attention = make([]Attention, 0, len(s.Attention))
	for _, a := range s.Attention {
		a.Title, a.Detail, a.Project = clip(a.Title, MaxText), clip(a.Detail, MaxText), clip(a.Project, MaxName)
		if ValidID(a.ID) && ValidID(a.Kind) && a.Severity.Valid() && a.Title != "" && (a.Href == "" || ValidHref(a.Href)) && len(out.Attention) < MaxAttention {
			out.Attention = append(out.Attention, a)
		} else {
			dropped++
		}
	}
	out.Schedule = make([]Scheduled, 0, len(s.Schedule))
	for _, c := range s.Schedule {
		c.Title, c.Project, c.Zone, c.State = clip(c.Title, MaxText), clip(c.Project, MaxName), clip(c.Zone, 64), clip(c.State, 40)
		if ValidID(c.ID) && c.Title != "" && !c.At.IsZero() && ValidHref(c.Href) && len(out.Schedule) < MaxSchedule {
			out.Schedule = append(out.Schedule, c)
		} else {
			dropped++
		}
	}
	if in := s.Infra; in != nil {
		cp := *in
		cp.Warnings = nil
		for _, w := range in.Warnings {
			if w = clip(w, 500); w != "" && len(cp.Warnings) < MaxWarnings {
				cp.Warnings = append(cp.Warnings, w)
			}
		}
		cp.Checks = nil
		for _, c := range in.Checks {
			c.Label, c.Value = clip(c.Label, MaxName), clip(c.Value, MaxText)
			if c.State == "ok" || c.State == "warn" || c.State == "bad" || c.State == "unknown" {
				if len(cp.Checks) < MaxWarnings {
					cp.Checks = append(cp.Checks, c)
				}
			}
		}
		if cp.HostsConfigured < 0 {
			cp.HostsConfigured = 0
		}
		if cp.HostsOnline < 0 {
			cp.HostsOnline = 0
		}
		if cp.HostsOnline > cp.HostsConfigured {
			cp.HostsOnline = cp.HostsConfigured
		}
		out.Infra = &cp
	}
	return out, dropped
}

// Decode reads a summary from another program: strictly, bounded, and cleaned. The workspace entry must be valid and
// must be the one the caller asked about (want), so a provider cannot answer for another workspace.
func Decode(r io.Reader, want string) (Summary, int, error) {
	raw, err := io.ReadAll(io.LimitReader(r, MaxBytes+1))
	if err != nil {
		return Summary{}, 0, err
	}
	if len(raw) > MaxBytes {
		return Summary{}, 0, errors.New("workspace: the summary is too large")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var s Summary
	if err := dec.Decode(&s); err != nil {
		return Summary{}, 0, fmt.Errorf("workspace: the summary is unreadable: %w", err)
	}
	if dec.More() {
		return Summary{}, 0, errors.New("workspace: the summary has more than one document")
	}
	s.Workspace.Name = clip(s.Workspace.Name, MaxName)
	s.Workspace.Detail = clip(s.Workspace.Detail, 500)
	if s.Schema != Schema {
		return Summary{}, 0, fmt.Errorf("workspace: schema %q is not %q", s.Schema, Schema)
	}
	if err := s.Workspace.validate(); err != nil {
		return Summary{}, 0, err
	}
	if want != "" && s.Workspace.ID != want {
		return Summary{}, 0, fmt.Errorf("workspace: the answer is for %q, not %q", s.Workspace.ID, want)
	}
	cleaned, dropped := s.Clean()
	if err := cleaned.Validate(); err != nil {
		return Summary{}, 0, err
	}
	return cleaned, dropped, nil
}
