package workspace

import "time"

// RunContext is bounded display evidence from the person's own execution service. It grants no authority to act.
type RunContext struct {
	RunID    string   `json:"runId"`
	Agent    string   `json:"agent,omitempty"`
	RunnerID string   `json:"runnerId,omitempty"`
	Runner   string   `json:"runner,omitempty"`
	Activity string   `json:"activity,omitempty"`
	Choices  []string `json:"choices,omitempty"`
	Stale    bool     `json:"stale,omitempty"`
	Retry    bool     `json:"retry,omitempty"`
}

func (r *RunContext) valid() bool {
	if r == nil {
		return true
	}
	if !ValidID(r.RunID) || (r.Agent != "" && !agentIDPattern.MatchString(r.Agent)) || (r.RunnerID != "" && !ValidID(r.RunnerID)) || !plain(r.Runner, MaxName) || !plain(r.Activity, MaxText) || len(r.Choices) > 10 {
		return false
	}
	for _, c := range r.Choices {
		if c == "" || !plain(c, 200) {
			return false
		}
	}
	return true
}

type Machine struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// PendingRepository is an incoming work item awaiting a local clone. No credential or local path is projected.
type PendingRepository struct {
	SourceRef  string `json:"sourceRef"`
	Repository string `json:"repository"`
	Title      string `json:"title"`
	From       string `json:"from"`
	GitHub     string `json:"github,omitempty"`
}

func (p PendingRepository) valid() bool {
	return p.SourceRef != "" && plain(p.SourceRef, 2000) && p.Repository != "" && plain(p.Repository, 1000) && p.Title != "" && plain(p.Title, MaxText) && plain(p.From, MaxName) && plain(p.GitHub, 200)
}

type Readiness struct {
	ID      string     `json:"id"`
	Title   string     `json:"title"`
	Project string     `json:"project"`
	State   string     `json:"state"`
	Reason  string     `json:"reason,omitempty"`
	At      *time.Time `json:"at,omitempty"`
	Href    string     `json:"href"`
}

func (r Readiness) valid() bool {
	return ValidID(r.ID) && r.Title != "" && plain(r.Title, MaxText) && plain(r.Project, MaxName) && plain(r.State, 40) && plain(r.Reason, MaxText) && ValidHref(r.Href) && (r.At == nil || !r.At.IsZero())
}
func (s Summary) validActivity() bool {
	if len(s.PendingRepositories) > MaxItems || len(s.Runners) > MaxItems || len(s.Readiness) > MaxSchedule {
		return false
	}
	if s.Workspace.Kind != KindIndividual && (len(s.PendingRepositories) > 0 || len(s.Runners) > 0 || len(s.Readiness) > 0) {
		return false
	}
	for _, p := range s.PendingRepositories {
		if !p.valid() {
			return false
		}
	}
	for _, r := range s.Readiness {
		if !r.valid() {
			return false
		}
	}
	for _, m := range s.Runners {
		if !ValidID(m.ID) || m.Name == "" || !plain(m.Name, MaxName) {
			return false
		}
	}
	for _, w := range s.Work {
		if !w.Run.valid() || (w.Run != nil && s.Workspace.Kind != KindIndividual) {
			return false
		}
	}
	for _, a := range s.Attention {
		if !a.Run.valid() || (a.Run != nil && s.Workspace.Kind != KindIndividual) {
			return false
		}
	}
	return true
}
func (s *Summary) cleanActivity() {
	if s.Workspace.Kind != KindIndividual {
		s.PendingRepositories = nil
		s.Runners = nil
		s.Readiness = nil
		return
	}
	pending := s.PendingRepositories
	s.PendingRepositories = nil
	for _, p := range pending {
		if p.valid() && len(s.PendingRepositories) < MaxItems {
			s.PendingRepositories = append(s.PendingRepositories, p)
		}
	}
	readiness := s.Readiness
	s.Readiness = nil
	for _, r := range readiness {
		r.Title = clip(r.Title, MaxText)
		r.Project = clip(r.Project, MaxName)
		r.Reason = clip(r.Reason, MaxText)
		if r.valid() && len(s.Readiness) < MaxSchedule {
			s.Readiness = append(s.Readiness, r)
		}
	}
	machines := s.Runners
	s.Runners = nil
	for _, m := range machines {
		m.Name = clip(m.Name, MaxName)
		if ValidID(m.ID) && m.Name != "" && len(s.Runners) < MaxItems {
			s.Runners = append(s.Runners, m)
		}
	}
}
