package service

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"devboard/internal/domain"
	"devboard/internal/runnerwire"
	"devboard/internal/store"
)

// Runners is controller-owned. A lease expiring changes availability, never
// ownership: only an authenticated terminal report releases a remote task.
type Runners struct {
	Deps
	Runs              *Runs
	LocalID           string
	LocalCapabilities func(context.Context) domain.RunnerCapabilities
}

func contains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}
func (s *Runners) List(ctx context.Context) ([]domain.Runner, error) {
	var out []domain.Runner
	err := s.Store.View(ctx, func(tx store.Tx) error {
		var e error
		out, e = tx.Runners().List(ctx)
		if e != nil {
			return e
		}
		active, e := tx.Runs().ListActive(ctx)
		if e != nil {
			return e
		}
		for i := range out {
			r := &out[i]
			r.CurrentRuns = 0
			r.Online = !r.Removed && (r.Kind == domain.RunnerLocal || !r.LastSeenAt.IsZero() && s.now().Sub(r.LastSeenAt) <= runnerwire.OnlineWindow)
			for _, run := range active {
				if run.RunnerID == r.ID || run.RunnerID == "" && r.Kind == domain.RunnerLocal {
					r.CurrentRuns++
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	filtered := []domain.Runner{}
	for _, r := range out {
		if r.Removed {
			continue
		}
		if r.Kind == domain.RunnerLocal && s.LocalCapabilities != nil {
			r.Capabilities = s.LocalCapabilities(ctx)
		}
		filtered = append(filtered, r)
	}
	return filtered, nil
}

// Pair generates a 192-bit short-lived secret; only its hash is persisted.
func (s *Runners) Pair(ctx context.Context, address string, projects []string, allowClone bool) (runnerwire.Pairing, error) {
	b := make([]byte, 24)
	if _, e := rand.Read(b); e != nil {
		return runnerwire.Pairing{}, e
	}
	secret := base64.RawURLEncoding.EncodeToString(b)
	out := runnerwire.Pairing{Code: runnerwire.EncodeCode(address, secret), ExpiresAt: s.now().Add(5 * time.Minute), Projects: projects, AllowClone: allowClone}
	if _, _, err := runnerwire.DecodeCode(out.Code); err != nil {
		return out, fmt.Errorf("%w: %v", domain.ErrInvalid, err)
	}
	err := s.update(ctx, func(tx store.Tx, em *emitter) error {
		if len(projects) == 0 || len(projects) > 100 {
			return fmt.Errorf("%w: select 1–100 projects", domain.ErrInvalid)
		}
		for _, id := range projects {
			if _, e := tx.Projects().Get(ctx, id); e != nil {
				return e
			}
		}
		saved := out
		saved.Code = ""
		return tx.Settings().Set(ctx, "pair:"+runnerwire.SecretHash(secret), saved, s.now())
	})
	return out, err
}
func (s *Runners) Join(ctx context.Context, in runnerwire.Join) (*domain.Runner, error) {
	key, e := base64.RawURLEncoding.DecodeString(in.PublicKey)
	if e != nil || len(key) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("%w: invalid identity key", domain.ErrInvalid)
	}
	name, e := domain.ValidateProjectName(in.Name)
	if e != nil {
		return nil, e
	}
	if len(in.Hostname) > 200 || len(in.OS) > 30 || len(in.Arch) > 30 || len(in.Version) > 100 || len(in.Secret) > 100 {
		return nil, domain.ErrInvalid
	}
	var out *domain.Runner
	e = s.update(ctx, func(tx store.Tx, em *emitter) error {
		var p runnerwire.Pairing
		if e := tx.Settings().Get(ctx, "pair:"+runnerwire.SecretHash(in.Secret), &p); e != nil || !s.now().Before(p.ExpiresAt) {
			return fmt.Errorf("%w: pairing code expired, used, or invalid", domain.ErrInvalid)
		}
		if p.Used {
			r, e := tx.Runners().Get(ctx, p.RunnerID)
			if e == nil && !r.Removed && r.PublicKey == in.PublicKey {
				out = r
				return nil
			}
			return fmt.Errorf("%w: pairing code already used", domain.ErrInvalid)
		}
		existing, e := tx.Runners().List(ctx)
		if e != nil {
			return e
		}
		for _, r := range existing {
			if r.PublicKey == in.PublicKey {
				return fmt.Errorf("%w: runner identity already paired; use devboard runner", domain.ErrDuplicate)
			}
		}
		// New devices cannot automatically receive work until the owner enables it.
		out = &domain.Runner{ID: domain.NewID(domain.PrefixRunner), Name: name, Kind: domain.RunnerRemote, Hostname: in.Hostname, OS: in.OS, Arch: in.Arch, Version: in.Version, PublicKey: in.PublicKey, Capacity: 1, Projects: p.Projects, AllowClone: p.AllowClone, CreatedAt: s.now()}
		if e := tx.Runners().Save(ctx, out); e != nil {
			return e
		}
		p.Used = true
		p.RunnerID = out.ID
		if e := tx.Settings().Set(ctx, "pair:"+runnerwire.SecretHash(in.Secret), p, s.now()); e != nil {
			return e
		}
		return em.emit(newEvent(domain.EventSettingsUpdated, map[string]string{"key": "runners"}))
	})
	return out, e
}

// Manage permits only owner-defined fields. Remove is a tombstone that revokes
// access and retains audit history; active ownership must be resolved first.
func (s *Runners) Manage(ctx context.Context, id string, in domain.Runner, remove bool) (*domain.Runner, error) {
	var out *domain.Runner
	err := s.update(ctx, func(tx store.Tx, em *emitter) error {
		r, e := tx.Runners().Get(ctx, id)
		if e != nil {
			return e
		}
		if r.Removed {
			return domain.ErrNotFound
		}
		if remove {
			if r.Kind == domain.RunnerLocal {
				return fmt.Errorf("%w: disable this computer instead", domain.ErrInvalid)
			}
			active, e := tx.Runs().ListActive(ctx)
			if e != nil {
				return e
			}
			for _, run := range active {
				if run.RunnerID == id {
					return fmt.Errorf("%w: runner still owns active runs; reconnect and stop them first", domain.ErrConflict)
				}
			}
			r.Removed = true
			r.Disabled = true
		} else {
			name, e := domain.ValidateProjectName(in.Name)
			if e != nil {
				return e
			}
			if in.Capacity < 1 || in.Capacity > 128 {
				return fmt.Errorf("%w: capacity must be 1–128", domain.ErrInvalid)
			}
			if len(in.Projects) > 100 {
				return domain.ErrInvalid
			}
			for _, p := range in.Projects {
				if _, e := tx.Projects().Get(ctx, p); e != nil {
					return e
				}
			}
			r.Name, r.Capacity, r.Automatic, r.Disabled, r.Projects, r.AllowClone = name, in.Capacity, in.Automatic, in.Disabled, in.Projects, in.AllowClone
		}
		if e := tx.Runners().Save(ctx, r); e != nil {
			return e
		}
		out = r
		return em.emit(newEvent(domain.EventSettingsUpdated, map[string]string{"key": "runners"}))
	})
	return out, err
}

// Route ranks by project affinity, free capacity, RAM, CPU, and finally ID.
// Priority/order are applied by the durable scheduler before this decision.
func (s *Runners) Route(ctx context.Context, task *domain.Task, resolved domain.Resolved) (*domain.Runner, domain.Resolved, error) {
	list, e := s.List(ctx)
	if e != nil {
		return nil, resolved, e
	}
	rules, e := s.Rules(ctx)
	if e != nil {
		return nil, resolved, e
	}
	prior, e := s.Runs.ListByTask(ctx, task.ID)
	if e != nil {
		return nil, resolved, e
	}
	minCPU := 0
	var minRAM int64
	for _, rule := range rules {
		if rule.Retry && len(prior) == 0 {
			continue
		}
		if rule.Contains != "" && !strings.Contains(strings.ToLower(task.Title+"\n"+task.Description), strings.ToLower(rule.Contains)) {
			continue
		}
		// Rules fill defaults; explicit run/task choices retain precedence.
		explicit := func(src domain.Source) bool { return src == domain.SourceRun || src == domain.SourceTask }
		if rule.Agent != "" && !explicit(resolved.Sources.Agent) {
			if resolved.Agent != rule.Agent {
				resolved.Model = ""
				resolved.Reasoning = ""
			}
			resolved.Agent = rule.Agent
		}
		if rule.Agent == "" || rule.Agent == resolved.Agent {
			if rule.Model != "" && !explicit(resolved.Sources.Model) {
				resolved.Model = rule.Model
				if resolved.Model == domain.AgentDefault {
					resolved.Model = ""
				}
			}
			if rule.Reasoning != "" && !explicit(resolved.Sources.Reasoning) {
				resolved.Reasoning = rule.Reasoning
			}
		}
		if rule.MinCPU > minCPU {
			minCPU = rule.MinCPU
		}
		if rule.MinRAMBytes > minRAM {
			minRAM = rule.MinRAMBytes
		}
	}
	cloneRemote := false
	if e := s.Store.View(ctx, func(tx store.Tx) error {
		repo, e := tx.Repositories().Get(ctx, task.ProjectID)
		if e != nil {
			return e
		}
		for _, remote := range repo.Remotes {
			if remote.Name == "origin" && runnerwire.SafeRemote(remote.URL) {
				cloneRemote = true
			}
		}
		return nil
	}); e != nil {
		return nil, resolved, e
	}
	candidates := []domain.Runner{}
	reasons := []string{}
	for _, r := range list {
		if resolved.Runner != "" && resolved.Runner != "automatic" && r.ID != resolved.Runner {
			continue
		}
		reason := ""
		switch {
		case r.Disabled:
			reason = "disabled"
		case !r.Online:
			reason = "offline; ownership retained"
		case (resolved.Runner == "" || resolved.Runner == "automatic") && !r.Automatic:
			reason = "automatic routing is off"
		case r.CurrentRuns >= r.Capacity:
			reason = "at capacity"
		case r.Kind == domain.RunnerRemote && !contains(r.Projects, task.ProjectID):
			reason = "project not authorized"
		case r.Kind == domain.RunnerRemote && !contains(r.Capabilities.Repositories, task.ProjectID) && !(r.AllowClone && r.Capabilities.CloneEnabled && cloneRemote):
			reason = "repository unavailable"
		case r.Capabilities.CPU < minCPU:
			reason = "not enough CPU"
		case minRAM > 0 && (r.Capabilities.AvailableRAMBytes == nil || *r.Capabilities.AvailableRAMBytes < minRAM):
			reason = "available RAM unknown or insufficient"
		}
		agentID := resolved.Agent
		if reason == "" {
			found := false
			for _, a := range r.Capabilities.Agents {
				if a.Available && (agentID == "" || a.ID == agentID) {
					if agentID == "" {
						agentID = a.ID
					}
					found = true
					break
				}
			}
			if !found {
				reason = "required agent unavailable"
				for _, a := range r.Capabilities.Agents {
					if a.ID == agentID && a.Detail != "" {
						reason += ": " + a.Detail
					}
				}
			}
			for _, opts := range r.Capabilities.Options {
				if opts.AgentID != agentID {
					continue
				}
				if resolved.Model != "" && !opts.CustomModels {
					has := false
					for _, m := range opts.Models {
						if m.ID == resolved.Model {
							has = true
						}
					}
					if !has {
						reason = "model unavailable"
					}
				}
				if !opts.HasReasoning(resolved.Reasoning) {
					reason = "reasoning unavailable"
				}
			}
		}
		if reason != "" {
			reasons = append(reasons, r.Name+": "+reason)
			continue
		}
		candidates = append(candidates, r)
	}
	if len(candidates) == 0 {
		return nil, resolved, fmt.Errorf("%w: no eligible runner (%s)", domain.ErrConflict, strings.Join(reasons, "; "))
	}
	sort.Slice(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		aa, bb := contains(a.Capabilities.Repositories, task.ProjectID), contains(b.Capabilities.Repositories, task.ProjectID)
		if aa != bb {
			return aa
		}
		if a.CurrentRuns*b.Capacity != b.CurrentRuns*a.Capacity {
			return a.CurrentRuns*b.Capacity < b.CurrentRuns*a.Capacity
		}
		ram := func(r domain.Runner) int64 {
			if r.Capabilities.AvailableRAMBytes != nil {
				return *r.Capabilities.AvailableRAMBytes
			}
			return 0
		}
		if ram(a) != ram(b) {
			return ram(a) > ram(b)
		}
		if a.Capabilities.CPU != b.Capabilities.CPU {
			return a.Capabilities.CPU > b.Capabilities.CPU
		}
		return a.ID < b.ID
	})
	r := candidates[0]
	if resolved.Agent == "" {
		for _, a := range r.Capabilities.Agents {
			if a.Available {
				resolved.Agent = a.ID
				break
			}
		}
	}
	return &r, resolved, nil
}

// Claim is called inside the same transaction that creates the Run/schedule claim.
func (s *Runners) Claim(selected *domain.Runner, job *runnerwire.Job) func(context.Context, store.Tx, *domain.Run) error {
	return func(ctx context.Context, tx store.Tx, run *domain.Run) error {
		r, e := tx.Runners().Get(ctx, selected.ID)
		if e != nil {
			return e
		}
		if r.Disabled || r.Removed || r.Kind == domain.RunnerRemote && (s.now().Sub(r.LastSeenAt) > runnerwire.OnlineWindow || !contains(r.Projects, run.ProjectID)) {
			return fmt.Errorf("%w: selected runner is unavailable", domain.ErrConflict)
		}
		active, e := tx.Runs().ListActive(ctx)
		if e != nil {
			return e
		}
		count := 0
		for _, a := range active {
			if a.RunnerID == r.ID || a.RunnerID == "" && r.Kind == domain.RunnerLocal {
				count++
			}
		}
		if count >= r.Capacity {
			return fmt.Errorf("%w: selected runner is at capacity", domain.ErrConflict)
		}
		if job != nil {
			job.Run = *run
			job.Questions = map[string]string{}
			job.Commands = []runnerwire.Command{}
			return saveJob(ctx, tx, job, s.now())
		}
		return nil
	}
}

func jobKey(id string) string { return "runner-job:" + id }
func loadJob(ctx context.Context, tx store.Tx, id string) (*runnerwire.Job, error) {
	var j runnerwire.Job
	e := tx.Settings().Get(ctx, jobKey(id), &j)
	if j.Questions == nil {
		j.Questions = map[string]string{}
	}
	return &j, e
}
func saveJob(ctx context.Context, tx store.Tx, j *runnerwire.Job, now time.Time) error {
	return tx.Settings().Set(ctx, jobKey(j.Run.ID), j, now)
}

func (s *Runners) Rules(ctx context.Context) ([]domain.RoutingRule, error) {
	out := []domain.RoutingRule{}
	err := s.Store.View(ctx, func(tx store.Tx) error {
		e := tx.Settings().Get(ctx, "routing-rules", &out)
		if errors.Is(e, domain.ErrNotFound) {
			return nil
		}
		return e
	})
	return out, err
}
func (s *Runners) SetRules(ctx context.Context, rules []domain.RoutingRule) error {
	if len(rules) > 50 {
		return domain.ErrInvalid
	}
	for _, r := range rules {
		if r.Name == "" || len(r.Name) > 120 || len(r.Contains) > 200 || r.MinCPU < 0 || r.MinCPU > 1024 || r.MinRAMBytes < 0 {
			return domain.ErrInvalid
		}
		if e := (domain.ExecutionConfig{Agent: r.Agent, Model: r.Model, Reasoning: r.Reasoning}).Validate(); e != nil {
			return e
		}
	}
	return s.update(ctx, func(tx store.Tx, em *emitter) error {
		if e := tx.Settings().Set(ctx, "routing-rules", rules, s.now()); e != nil {
			return e
		}
		return em.emit(newEvent(domain.EventSettingsUpdated, map[string]string{"key": "routing-rules"}))
	})
}

// Sync authenticates body bytes, prevents replays using a persisted counter,
// applies reports and their acknowledgments in one transaction, then returns
// only this identity's already-authorized jobs. Duplicate observations are inert.
func (s *Runners) Sync(ctx context.Context, body []byte, signature string) (runnerwire.SyncReply, error) {
	var in runnerwire.Sync
	out := runnerwire.SyncReply{Jobs: []runnerwire.Job{}, Acks: map[string]int64{}, LeaseSeconds: int(runnerwire.Lease.Seconds())}
	if e := json.Unmarshal(body, &in); e != nil {
		return out, domain.ErrInvalid
	}
	if len(in.Reports) > 128 || len(in.Capabilities.Agents) > 10 || len(in.Capabilities.Options) > 10 || len(in.Capabilities.Repositories) > 100 || len(in.Capabilities.Diagnostics) > 2000 {
		return out, domain.ErrInvalid
	}
	err := s.update(ctx, func(tx store.Tx, em *emitter) error {
		r, e := tx.Runners().Get(ctx, in.RunnerID)
		if e != nil {
			return domain.ErrNotFound
		}
		if r.Removed || !runnerwire.Verify(r.PublicKey, signature, body) {
			return fmt.Errorf("%w: invalid runner authentication", domain.ErrNotFound)
		}
		if in.Sequence <= r.LastSequence || in.At.Before(s.now().Add(-2*time.Minute)) || in.At.After(s.now().Add(2*time.Minute)) {
			return fmt.Errorf("%w: stale runner request", domain.ErrConflict)
		}
		for _, report := range in.Reports {
			if len(report.Observations) > 200 {
				return domain.ErrInvalid
			}
			run, e := tx.Runs().Get(ctx, report.RunID)
			if e != nil {
				return e
			}
			if !run.Remote || run.RunnerID != r.ID {
				return fmt.Errorf("%w: run belongs to another runner", domain.ErrNotFound)
			}
			job, e := loadJob(ctx, tx, run.ID)
			if e != nil {
				return e
			}
			for _, ob := range report.Observations {
				if ob.Seq <= job.Ack {
					continue
				}
				if ob.Seq != job.Ack+1 {
					return fmt.Errorf("%w: observation gap", domain.ErrConflict)
				}
				if e := s.observe(ctx, tx, em, run, job, ob); e != nil {
					return e
				}
				job.Ack = ob.Seq
			}
			job.Run = *run
			if e := saveJob(ctx, tx, job, s.now()); e != nil {
				return e
			}
			out.Acks[run.ID] = job.Ack
		}
		r.LastSequence = in.Sequence
		r.LastSeenAt = s.now()
		r.Capabilities = in.Capabilities
		// Capability reports are observations, not permission grants.
		filtered := []string{}
		for _, p := range r.Capabilities.Repositories {
			if contains(r.Projects, p) {
				filtered = append(filtered, p)
			}
		}
		r.Capabilities.Repositories = filtered
		if e := tx.Runners().Save(ctx, r); e != nil {
			return e
		}
		active, e := tx.Runs().ListActive(ctx)
		if e != nil {
			return e
		}
		for _, run := range active {
			if run.RunnerID != r.ID || !run.Remote {
				continue
			}
			job, e := loadJob(ctx, tx, run.ID)
			if e != nil {
				return e
			}
			job.Run = run
			if r.Disabled || !contains(r.Projects, run.ProjectID) {
				enqueue(job, "stop", "", "", "")
				if e := saveJob(ctx, tx, job, s.now()); e != nil {
					return e
				}
			}
			out.Jobs = append(out.Jobs, *job)
			out.Acks[run.ID] = job.Ack
		}
		out.Projects = r.Projects
		out.AllowClone = r.AllowClone
		out.Capacity = r.Capacity
		out.Disabled = r.Disabled
		return nil
	})
	return out, err
}

func enqueue(j *runnerwire.Job, kind, ref, text, question string) {
	for _, c := range j.Commands {
		if c.Kind == kind && (kind == "stop" || kind == "finish" || kind == "respond" && c.Ref == ref) {
			return
		}
	}
	j.Commands = append(j.Commands, runnerwire.Command{ID: domain.NewID("cmd"), Kind: kind, Ref: ref, Text: text, QuestionID: question})
}
