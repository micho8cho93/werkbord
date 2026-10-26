package service

import (
	"context"
	"fmt"
	"sort"
	"time"

	"werkbord/internal/team/domain"
	"werkbord/internal/team/store"
)

type CoordinationDay struct {
	Date      string `json:"date"`
	Created   int    `json:"created"`
	Completed int    `json:"completed"`
}
type CoordinationCategory struct {
	LabelID   string `json:"labelId"`
	Name      string `json:"name"`
	Created   int    `json:"created"`
	Completed int    `json:"completed"`
}
type CoordinationAnalytics struct {
	Projects                 []AnalyticsProject     `json:"projects"`
	Start                    time.Time              `json:"start"`
	End                      time.Time              `json:"end"`
	Created                  int                    `json:"created"`
	Completed                int                    `json:"completed"`
	HumanCompletionDecisions int                    `json:"humanCompletionDecisions"`
	DurationSamples          int                    `json:"durationSamples"`
	AverageTicketSeconds     *float64               `json:"averageTicketSeconds"`
	MedianTicketSeconds      *float64               `json:"medianTicketSeconds"`
	Days                     []CoordinationDay      `json:"days"`
	Categories               []CoordinationCategory `json:"categories"`
	Truncated                bool                   `json:"truncated"`
}

type AnalyticsProject struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// CoordinationAnalytics authorises within the same consistent snapshot used
// to read metrics. Project visibility alone does not grant ticket/activity access.
func (s *Service) CoordinationAnalytics(ctx context.Context, a Actor, project string, days int) (out CoordinationAnalytics, err error) {
	if days == 0 {
		days = 30
	}
	if days != 7 && days != 30 && days != 90 {
		return out, fmt.Errorf("%w: analytics range must be 7, 30 or 90 days", domain.ErrInvalid)
	}
	end := s.stamp()
	start := end.Add(-time.Duration(days) * 24 * time.Hour)
	err = s.db.View(ctx, func(tx store.Tx) error {
		var authorised []domain.Project
		if project != "" {
			x, e := s.access(ctx, tx, a, project)
			if e != nil {
				return e
			}
			if e = x.require(domain.PPTicketsView, "see ticket analytics"); e != nil {
				return e
			}
			if e = x.require(domain.PPActivityView, "see ticket completion history"); e != nil {
				return e
			}
		}
		only := a.Member.ID
		if a.Member.Can(domain.PermProjectsViewAll) {
			only = ""
		}
		ps, e := tx.Projects(ctx, a.Workspace.ID, only)
		if e != nil {
			return e
		}
		roles, e := tx.ProjectRoles(ctx, a.Workspace.ID, a.Member.ID)
		if e != nil {
			return e
		}
		for _, p := range ps {
			role := roles[p.ID]
			if a.Member.Can(domain.PermProjectsManage) {
				role = domain.ProjectOwner
			}
			if role.Can(domain.PPTicketsView) && role.Can(domain.PPActivityView) {
				authorised = append(authorised, p)
			}
		}
		capped := len(authorised) > 200
		if capped {
			authorised = authorised[:200]
			if project != "" {
				for _, p := range ps {
					if p.ID != project {
						continue
					}
					included := false
					for _, listed := range authorised {
						included = included || listed.ID == project
					}
					if !included {
						authorised[199] = p
					}
					break
				}
			}
		}
		ids := make([]string, 0, len(authorised))
		if project != "" {
			ids = append(ids, project)
		} else {
			for _, p := range authorised {
				ids = append(ids, p.ID)
			}
		}
		// Persisted timestamps have millisecond precision. Include the snapshot's
		// stored millisecond by querying up to the following exclusive tick.
		data, e := tx.AnalyticsData(ctx, a.Workspace.ID, ids, start, end.Add(time.Millisecond), 20000)
		if e != nil {
			return e
		}
		labels, e := tx.Labels(ctx, a.Workspace.ID)
		if e != nil {
			return e
		}
		out = aggregateCoordination(data, labels, start, end)
		for _, p := range authorised {
			out.Projects = append(out.Projects, AnalyticsProject{ID: p.ID, Name: p.Name})
		}
		out.Truncated = out.Truncated || capped
		return nil
	})
	return out, err
}
func aggregateCoordination(data store.AnalyticsData, labels []domain.Label, start, end time.Time) CoordinationAnalytics {
	out := CoordinationAnalytics{Start: start, End: end, Days: []CoordinationDay{}, Categories: []CoordinationCategory{}, Truncated: data.Truncated}
	periodEnd := end.Add(time.Millisecond)
	idx := map[string]int{}
	for d := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, time.UTC); d.Before(periodEnd); d = d.AddDate(0, 0, 1) {
		date := d.Format("2006-01-02")
		idx[date] = len(out.Days)
		out.Days = append(out.Days, CoordinationDay{Date: date})
	}
	names := map[string]string{}
	for _, l := range labels {
		names[l.ID] = l.Name
	}
	categories := map[string]*CoordinationCategory{}
	count := func(k domain.Ticket, done bool) {
		ids := k.LabelIDs
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
				c = &CoordinationCategory{LabelID: id, Name: name}
				categories[id] = c
			}
			if done {
				c.Completed++
			} else {
				c.Created++
			}
		}
	}
	tickets := map[string]domain.Ticket{}
	for _, k := range data.Tickets {
		tickets[k.ID] = k
		if !k.CreatedAt.Before(start) && k.CreatedAt.Before(periodEnd) {
			out.Created++
			out.Days[idx[k.CreatedAt.UTC().Format("2006-01-02")]].Created++
			count(k, false)
		}
	}
	done := map[string]bool{}
	durations := []float64{}
	for _, a := range data.Completions {
		if a.CreatedAt.Before(start) || !a.CreatedAt.Before(periodEnd) {
			continue
		}
		out.HumanCompletionDecisions++
		k, ok := tickets[a.TicketID]
		if !ok || k.ProjectID != a.ProjectID || done[k.ID] {
			continue
		}
		done[k.ID] = true
		out.Completed++
		out.Days[idx[a.CreatedAt.UTC().Format("2006-01-02")]].Completed++
		count(k, true)
		if !k.CreatedAt.IsZero() && !a.CreatedAt.Before(k.CreatedAt) {
			durations = append(durations, a.CreatedAt.Sub(k.CreatedAt).Seconds())
		}
	}
	out.DurationSamples = len(durations)
	if len(durations) > 0 {
		sort.Float64s(durations)
		sum := 0.0
		for _, d := range durations {
			sum += d
		}
		mean := sum / float64(len(durations))
		median := durations[len(durations)/2]
		if len(durations)%2 == 0 {
			median = (median + durations[len(durations)/2-1]) / 2
		}
		out.AverageTicketSeconds = &mean
		out.MedianTicketSeconds = &median
	}
	for _, c := range categories {
		out.Categories = append(out.Categories, *c)
	}
	sort.Slice(out.Categories, func(i, j int) bool { return out.Categories[i].Name < out.Categories[j].Name })
	return out
}
