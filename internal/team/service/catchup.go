package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"werkbord/internal/team/domain"
	"werkbord/internal/workspace"
)

// CatchUp uses the same project and activity permissions as the console.
func (s *Service) CatchUp(ctx context.Context, a Actor) (*workspace.RecentChanges, error) {
	ctx = AuthenticatedContext(ctx, a)
	now := s.stamp()
	out := &workspace.RecentChanges{Since: now.AddDate(0, 0, -30), Through: now, Items: []workspace.Change{}}
	projects, err := s.ListProjects(ctx, a)
	if err != nil {
		return nil, err
	}
	for i, p := range projects {
		if p.Archived {
			continue
		}
		if i >= workspace.MaxProjects {
			out.Incomplete = true
			break
		}
		acts, err := s.ListActivity(ctx, a, p.ID, 0, MaxActivityPage)
		if err != nil {
			out.Incomplete = true
			continue
		}
		if len(acts) == MaxActivityPage {
			out.Incomplete = true
			if acts[len(acts)-1].CreatedAt.After(out.Since) {
				out.Since = acts[len(acts)-1].CreatedAt
			}
		}
		for _, e := range acts {
			if e.ID > out.Cursor {
				out.Cursor = e.ID
			}
			if e.CreatedAt.Before(out.Since) || e.CreatedAt.After(now) {
				continue
			}
			kind, key := "", ""
			switch e.Kind {
			case domain.ActTicketCompleted:
				kind = "completed"
				key = "ticket:" + a.Workspace.ID + ":" + p.ID + ":" + e.TicketID
			case domain.ActPullRequestMerged:
				if !strings.HasPrefix(e.Detail, "https://") {
					continue
				}
				kind = "pr_merged"
				key = "pr:" + e.Detail
			default:
				continue
			}
			title := e.TicketKey
			if title == "" {
				title = p.Name
			}
			out.Items = append(out.Items, workspace.Change{ID: fmt.Sprintf("activity-%d", e.ID), Key: key, Kind: kind, Title: title, Href: "?tab=board&project=" + p.ID + "&ticket=" + e.TicketID, At: e.CreatedAt})
		}
	}
	filtered := out.Items[:0]
	for _, it := range out.Items {
		if !it.At.Before(out.Since) {
			filtered = append(filtered, it)
		}
	}
	out.Items = filtered
	sort.Slice(out.Items, func(i, j int) bool { return out.Items[i].At.After(out.Items[j].At) })
	if len(out.Items) > workspace.MaxItems {
		out.Items = out.Items[:workspace.MaxItems]
		out.Incomplete = true
		out.Since = out.Items[len(out.Items)-1].At
	}
	return out, nil
}
