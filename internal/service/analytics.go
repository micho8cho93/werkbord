package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"werkbord/internal/domain"
	"werkbord/internal/store"
)

const analyticsLimit = 20000

type Analytics struct{ Deps }
type AnalyticsFilter struct {
	ProjectID string
	Provider  string
	Days      int
}
type AnalyticsDay struct {
	Date      string `json:"date"`
	Created   int    `json:"created"`
	Completed int    `json:"completed"`
}
type AnalyticsMeasurements struct {
	InputTokens       *int64   `json:"inputTokens"`
	OutputTokens      *int64   `json:"outputTokens"`
	CachedTokens      *int64   `json:"cachedTokens"`
	InputRuns         int      `json:"inputRuns"`
	OutputRuns        int      `json:"outputRuns"`
	CachedRuns        int      `json:"cachedRuns"`
	PartialRuns       int      `json:"partialRuns"`
	ActualCostUSD     *float64 `json:"actualCostUsd"`
	EstimatedCostUSD  *float64 `json:"estimatedCostUsd"`
	ActualCostRuns    int      `json:"actualCostRuns"`
	EstimatedCostRuns int      `json:"estimatedCostRuns"`
}
type AnalyticsExecution struct {
	ProjectID   string                  `json:"projectId"`
	RunnerID    string                  `json:"runnerId"`
	Provider    string                  `json:"provider"`
	Model       string                  `json:"model"`
	Attempts    int                     `json:"attempts"`
	Retries     int                     `json:"retries"`
	Outcomes    map[domain.RunState]int `json:"outcomes"`
	LimitEvents int                     `json:"limitEvents"`
	Accepted    int                     `json:"accepted"`
	Rejected    int                     `json:"rejected"`
	AnalyticsMeasurements
}
type AnalyticsCategory struct {
	LabelID   string `json:"labelId"`
	Name      string `json:"name"`
	Created   int    `json:"created"`
	Completed int    `json:"completed"`
}
type AnalyticsReport struct {
	Start                    time.Time               `json:"start"`
	End                      time.Time               `json:"end"`
	ProjectID                string                  `json:"projectId"`
	Provider                 string                  `json:"provider"`
	Providers                []string                `json:"providers"`
	Created                  int                     `json:"created"`
	Completed                int                     `json:"completed"`
	DurationSamples          int                     `json:"durationSamples"`
	AverageTaskSeconds       *float64                `json:"averageTaskSeconds"`
	MedianTaskSeconds        *float64                `json:"medianTaskSeconds"`
	Blocked                  int                     `json:"blocked"`
	Stopped                  int                     `json:"stopped"`
	AgentReportedCompletion  *int                    `json:"agentReportedCompletion"` // no distinct agent claim is persisted
	NormalSessionCompletions int                     `json:"normalSessionCompletions"`
	VerifiedCompletion       *int                    `json:"verifiedCompletion"` // no existing correctness verifier
	VerifiedMerges           int                     `json:"verifiedMerges"`
	Accepted                 int                     `json:"accepted"`
	Rejected                 int                     `json:"rejected"`
	Attempts                 int                     `json:"attempts"`
	Retries                  int                     `json:"retries"`
	LimitEvents              int                     `json:"limitEvents"`
	Outcomes                 map[domain.RunState]int `json:"outcomes"`
	AnalyticsMeasurements
	Days               []AnalyticsDay       `json:"days"`
	Executions         []AnalyticsExecution `json:"executions"`
	Categories         []AnalyticsCategory  `json:"categories"`
	Truncated          bool                 `json:"truncated"`
	RetentionRemoved   bool                 `json:"retentionRemoved"`
	MissingTaskHistory int                  `json:"missingTaskHistory"`
}

// Report is on-demand and read-only. Lookback ranges end at the same snapshot
// time as lifetime usage counters, so spend is never assigned to a historical
// day which a cumulative snapshot cannot substantiate.
func (s *Analytics) Report(ctx context.Context, f AnalyticsFilter) (out AnalyticsReport, err error) {
	if f.Days == 0 {
		f.Days = 30
	}
	if f.Days != 7 && f.Days != 30 && f.Days != 90 {
		return out, fmt.Errorf("%w: analytics range must be 7, 30 or 90 days", domain.ErrInvalid)
	}
	now := s.now()
	start := now.Add(-time.Duration(f.Days) * 24 * time.Hour)
	err = s.Store.View(ctx, func(tx store.Tx) error {
		if f.ProjectID != "" {
			if _, e := tx.Projects().Get(ctx, f.ProjectID); e != nil {
				return e
			}
		}
		// Persisted timestamps have millisecond precision. Include the snapshot's
		// stored millisecond by querying up to the following exclusive tick.
		data, e := tx.Analytics().Read(ctx, f.ProjectID, start, now.Add(time.Millisecond), analyticsLimit)
		if e != nil {
			return e
		}
		labels, e := tx.Labels().List(ctx)
		if e != nil {
			return e
		}
		out = aggregateAnalytics(data, labels, f, start, now)
		floor, e := tx.Events().ReplayFloor(ctx)
		out.RetentionRemoved = floor > 0
		return e
	})
	return out, err
}

func aggregateAnalytics(data store.AnalyticsData, labels []domain.Label, f AnalyticsFilter, start, end time.Time) AnalyticsReport {
	out := AnalyticsReport{Start: start, End: end, ProjectID: f.ProjectID, Provider: f.Provider, Providers: []string{}, Days: []AnalyticsDay{}, Executions: []AnalyticsExecution{}, Categories: []AnalyticsCategory{}, Outcomes: map[domain.RunState]int{}, Truncated: data.Truncated}
	periodEnd := end.Add(time.Millisecond)
	inPeriod := func(t time.Time) bool { return !t.Before(start) && t.Before(periodEnd) }
	dateIndex := map[string]int{}
	for d := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, time.UTC); d.Before(periodEnd); d = d.AddDate(0, 0, 1) {
		date := d.Format("2006-01-02")
		dateIndex[date] = len(out.Days)
		out.Days = append(out.Days, AnalyticsDay{Date: date})
	}
	tasks := map[string]domain.Task{}
	runs := map[string]domain.Run{}
	latest := map[string]domain.Run{}
	matchingTasks := map[string]bool{}
	providers := map[string]bool{}
	for _, t := range data.Tasks {
		if f.ProjectID == "" || t.ProjectID == f.ProjectID {
			tasks[t.ID] = t
		}
	}
	for _, r := range data.Runs {
		if f.ProjectID != "" && r.ProjectID != f.ProjectID {
			continue
		}
		runs[r.ID] = r
		providers[r.AgentID] = true
		old, ok := latest[r.TaskID]
		if !ok || r.CreatedAt.After(old.CreatedAt) {
			latest[r.TaskID] = r
		}
		if f.Provider == "" || r.AgentID == f.Provider {
			matchingTasks[r.TaskID] = true
		}
	}
	for p := range providers {
		out.Providers = append(out.Providers, p)
	}
	sort.Strings(out.Providers)
	taskMatches := func(id string) bool { _, ok := tasks[id]; return ok && (f.Provider == "" || matchingTasks[id]) }
	categories := map[string]*AnalyticsCategory{}
	names := map[string]string{}
	for _, l := range labels {
		names[l.ID] = l.Name
	}
	categoryCount := func(t domain.Task, completed bool) {
		ids := t.LabelIDs
		if len(ids) == 0 {
			ids = []string{""}
		}
		seen := map[string]bool{}
		for _, id := range ids {
			if seen[id] {
				continue
			}
			seen[id] = true
			c := categories[id]
			if c == nil {
				name := names[id]
				if id == "" {
					name = "Unlabelled"
				}
				if name == "" {
					name = "Unavailable label"
				}
				c = &AnalyticsCategory{LabelID: id, Name: name}
				categories[id] = c
			}
			if completed {
				c.Completed++
			} else {
				c.Created++
			}
		}
	}
	for id, t := range tasks {
		if !taskMatches(id) {
			continue
		}
		if inPeriod(t.CreatedAt) {
			out.Created++
			out.Days[dateIndex[t.CreatedAt.UTC().Format("2006-01-02")]].Created++
			categoryCount(t, false)
		}
		if t.ArchivedAt != nil || t.State == domain.TaskDone {
			continue
		}
		r, ok := latest[id]
		if !ok || (f.Provider != "" && r.AgentID != f.Provider) {
			continue
		}
		if r.State == domain.RunBlocked {
			out.Blocked++
		}
		if r.State == domain.RunStopped {
			out.Stopped++
		}
	}
	groups := map[[4]string]*AnalyticsExecution{}
	group := func(r domain.Run) *AnalyticsExecution {
		key := [4]string{r.ProjectID, r.RunnerID, r.AgentID, r.Model}
		g := groups[key]
		if g == nil {
			g = &AnalyticsExecution{ProjectID: r.ProjectID, RunnerID: r.RunnerID, Provider: r.AgentID, Model: r.Model, Outcomes: map[domain.RunState]int{}}
			groups[key] = g
		}
		return g
	}
	runMatches := func(id string) (domain.Run, bool) {
		r, ok := runs[id]
		return r, ok && (f.Provider == "" || r.AgentID == f.Provider)
	}
	for _, r := range runs {
		if _, ok := runMatches(r.ID); !ok || !inPeriod(r.CreatedAt) {
			continue
		}
		g := group(r)
		out.Attempts++
		g.Attempts++
		out.Outcomes[r.State]++
		g.Outcomes[r.State]++
		if r.Attempt > 1 {
			out.Retries++
			g.Retries++
		}
		addMeasurements(&out.AnalyticsMeasurements, r.Usage)
		addMeasurements(&g.AnalyticsMeasurements, r.Usage)
	}
	states := map[string]domain.TaskState{}
	done := map[string]bool{}
	reported := map[string]bool{}
	merged := map[string]bool{}
	assessments := map[string]string{}
	decisions := map[string]string{}
	durations := []float64{}
	// The store orders by sequence: repeated cumulative snapshots and edits while
	// Done never create extra completions or acceptance decisions.
	for _, ev := range data.Events {
		if f.ProjectID != "" && ev.ProjectID != f.ProjectID || !ev.CreatedAt.Before(periodEnd) {
			continue
		}
		var p struct {
			State      domain.TaskState `json:"state"`
			Acceptance string           `json:"acceptance"`
			OK         bool             `json:"ok"`
			Outcome    string           `json:"outcome"`
			Stream     string           `json:"stream"`
			Text       string           `json:"text"`
		}
		// SQLite emits booleans as 0/1; git's OK is decoded separately below.
		var raw map[string]json.RawMessage
		if json.Unmarshal(ev.Payload, &raw) != nil {
			continue
		}
		if b := raw["ok"]; string(b) == "1" {
			raw["ok"] = json.RawMessage("true")
		}
		normalized, _ := json.Marshal(raw)
		if json.Unmarshal(normalized, &p) != nil {
			continue
		}
		if ev.Type == domain.EventTaskCreated || ev.Type == domain.EventTaskUpdated {
			previous, known := states[ev.TaskID]
			if !p.State.Valid() {
				continue
			}
			states[ev.TaskID] = p.State
			if !inPeriod(ev.CreatedAt) || p.State != domain.TaskDone || known && previous == domain.TaskDone || done[ev.TaskID] || !taskMatches(ev.TaskID) {
				continue
			}
			if !known {
				out.MissingTaskHistory++
				continue
			}
			done[ev.TaskID] = true
			out.Completed++
			out.Days[dateIndex[ev.CreatedAt.UTC().Format("2006-01-02")]].Completed++
			t := tasks[ev.TaskID]
			categoryCount(t, true)
			if !t.CreatedAt.IsZero() && !ev.CreatedAt.Before(t.CreatedAt) {
				durations = append(durations, ev.CreatedAt.Sub(t.CreatedAt).Seconds())
			}
			continue
		}
		r, ok := runMatches(ev.RunID)
		if !ok || r.TaskID != ev.TaskID || r.ProjectID != ev.ProjectID {
			continue
		}
		if ev.Type == domain.EventRunStateChanged {
			old, known := assessments[r.ID]
			assessments[r.ID] = p.Acceptance
			if known && old != p.Acceptance && inPeriod(ev.CreatedAt) {
				decisions[r.ID] = p.Acceptance
			}
		}
		if !inPeriod(ev.CreatedAt) {
			continue
		}
		switch ev.Type {
		case domain.EventAgentCompleted:
			if !reported[r.ID] {
				reported[r.ID] = true
				out.NormalSessionCompletions++
			}
		case domain.EventGitMerged:
			if p.OK && p.Outcome == domain.OutcomeDone && !merged[r.ID] {
				merged[r.ID] = true
				out.VerifiedMerges++
			}
		case domain.EventAgentOutput:
			// Only exact adapter-origin system notices. User/assistant prose and
			// generic quota errors cannot establish a provider-limit event.
			if p.Stream == "system" && limitNotice(r.AgentID, p.Text) {
				out.LimitEvents++
				group(r).LimitEvents++
			}
		}
	}
	for id, decision := range decisions {
		r := runs[id]
		switch decision {
		case "accepted":
			out.Accepted++
			group(r).Accepted++
		case "rejected":
			out.Rejected++
			group(r).Rejected++
		}
	}
	out.DurationSamples = len(durations)
	if len(durations) > 0 {
		sort.Float64s(durations)
		var sum float64
		for _, d := range durations {
			sum += d
		}
		average := sum / float64(len(durations))
		median := durations[len(durations)/2]
		if len(durations)%2 == 0 {
			median = (median + durations[len(durations)/2-1]) / 2
		}
		out.AverageTaskSeconds = &average
		out.MedianTaskSeconds = &median
	}
	for _, g := range groups {
		out.Executions = append(out.Executions, *g)
	}
	sort.Slice(out.Executions, func(i, j int) bool {
		a, b := out.Executions[i], out.Executions[j]
		ka := [4]string{a.ProjectID, a.RunnerID, a.Provider, a.Model}
		kb := [4]string{b.ProjectID, b.RunnerID, b.Provider, b.Model}
		for n := range ka {
			if ka[n] != kb[n] {
				return ka[n] < kb[n]
			}
		}
		return false
	})
	for _, c := range categories {
		out.Categories = append(out.Categories, *c)
	}
	sort.Slice(out.Categories, func(i, j int) bool { return out.Categories[i].Name < out.Categories[j].Name })
	return out
}

func limitNotice(provider, text string) bool {
	if provider == "claude-code" {
		return text == "Rate limit: rejected"
	}
	if provider != "codex" || !strings.HasPrefix(text, "The turn failed: ") {
		return false
	}
	reason := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(text, "The turn failed: ")))
	return reason == "usage limit reached" || reason == "rate limit exceeded" || reason == "rate limit reached"
}
func addMeasurements(m *AnalyticsMeasurements, u domain.Usage) {
	// Reject malformed legacy measurements instead of inventing or poisoning sums.
	if u.Validate() != nil {
		return
	}
	add := func(dst **int64, n *int64, coverage *int) {
		if n == nil {
			return
		}
		if *dst == nil {
			v := int64(0)
			*dst = &v
		}
		**dst += *n
		*coverage++
	}
	add(&m.InputTokens, u.InputTokens, &m.InputRuns)
	add(&m.OutputTokens, u.OutputTokens, &m.OutputRuns)
	add(&m.CachedTokens, u.CachedTokens, &m.CachedRuns)
	if u.Partial {
		m.PartialRuns++
	}
	if u.CostUSD == nil {
		return
	}
	var dst **float64
	switch u.CostKind {
	case "actual_api":
		dst = &m.ActualCostUSD
		m.ActualCostRuns++
	case "estimated_api_equivalent":
		dst = &m.EstimatedCostUSD
		m.EstimatedCostRuns++
	default:
		return
	}
	if *dst == nil {
		v := float64(0)
		*dst = &v
	}
	**dst += *u.CostUSD
}
