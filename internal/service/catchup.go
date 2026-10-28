package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"
	"werkbord/internal/domain"
	"werkbord/internal/store"
	"werkbord/internal/workspace"
)

func recentWorkspaceChanges(ctx context.Context, tx store.Tx, ov *Overview, now time.Time) (*workspace.RecentChanges, error) {
	events, err := tx.Events().RecentChanges(ctx)
	if err != nil {
		return nil, err
	}
	cursor, err := tx.Events().LatestSeq(ctx)
	if err != nil {
		return nil, err
	}
	out := &workspace.RecentChanges{Since: now.AddDate(0, 0, -30), Through: now, Cursor: cursor, Items: []workspace.Change{}}
	allowed := map[string]bool{}
	for _, p := range ov.Projects {
		allowed[p.ProjectID] = true
	}
	if len(events) > 200 {
		out.Incomplete = true
		out.Since = events[199].CreatedAt
		events = events[:200]
	}
	seen := map[string]bool{}
	for _, e := range events {
		if !allowed[e.ProjectID] || e.CreatedAt.Before(out.Since) || e.CreatedAt.After(now) {
			continue
		}
		if e.Type == domain.EventGitPRObserved {
			var st domain.GitHubState
			if json.Unmarshal(e.Payload, &st) != nil || !st.Available {
				continue
			}
			for _, pr := range st.PullRequests {
				if pr.State != "merged" || pr.MergedAt == nil || pr.MergedAt.Before(out.Since) || pr.MergedAt.After(now) || pr.URL == "" {
					continue
				}
				key := "pr:" + pr.URL
				if seen[key] {
					continue
				}
				seen[key] = true
				out.Items = append(out.Items, workspace.Change{ID: fmt.Sprintf("pr-%d-%d", e.Seq, pr.Number), Key: key, Kind: "pr_merged", Title: workspace.Clip(pr.Title, workspace.MaxText), Href: "#/p/" + e.ProjectID + "/git", At: *pr.MergedAt})
			}
		} else {
			var t domain.Task
			if json.Unmarshal(e.Payload, &t) != nil {
				continue
			}
			key := "task:" + e.TaskID
			if seen[key] {
				continue
			}
			seen[key] = true
			out.Items = append(out.Items, workspace.Change{ID: fmt.Sprintf("event-%d", e.Seq), Key: key, Kind: "completed", Title: workspace.Clip(t.Title, workspace.MaxText), Href: taskHref(e.ProjectID, e.TaskID), Origin: t.SourceRef, At: e.CreatedAt})
		}
	}
	sort.Slice(out.Items, func(i, j int) bool { return out.Items[i].At.After(out.Items[j].At) })
	if len(out.Items) > workspace.MaxItems {
		out.Items = out.Items[:workspace.MaxItems]
		out.Incomplete = true
		out.Since = out.Items[len(out.Items)-1].At
	}
	return out, nil
}
