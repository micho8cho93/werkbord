package teamlink

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// A closed metadata projection, fetched afresh through the approved Team bridge.
// Related scopes also enforce Salie's narrower session project selection.
func (l *Link) assistantInitiatives(ctx context.Context, base string, in BoardArgs) ([]map[string]any, error) {
	var views []struct {
		Initiative struct {
			ID         string    `json:"id"`
			Title      string    `json:"title"`
			Objective  string    `json:"objective"`
			Status     string    `json:"status"`
			ProjectIDs []string  `json:"projectIds"`
			UpdatedAt  time.Time `json:"updatedAt"`
		} `json:"initiative"`
		Tickets []struct {
			ID          string    `json:"id"`
			ProjectID   string    `json:"projectId"`
			Key         string    `json:"key"`
			Status      string    `json:"status"`
			UpdatedAt   time.Time `json:"updatedAt"`
			PullRequest *struct {
				State      string    `json:"state"`
				ReportedAt time.Time `json:"reportedAt"`
			} `json:"pullRequest"`
		} `json:"tickets"`
		Activity []struct {
			TicketID  string    `json:"ticketId"`
			Kind      string    `json:"kind"`
			CreatedAt time.Time `json:"createdAt"`
		} `json:"activity"`
		Done        int       `json:"done"`
		OpenPRs     int       `json:"openPRs"`
		Blockers    []string  `json:"blockers"`
		NextActions []string  `json:"nextActions"`
		GeneratedAt time.Time `json:"generatedAt"`
	}
	if err := l.call(ctx, http.MethodGet, base+"/"+url.PathEscape(in.ProjectID)+"/initiatives", nil, &views); err != nil {
		return nil, err
	}
	items := []map[string]any{}
	for _, v := range views {
		related := []map[string]string{}
		for _, pid := range v.Initiative.ProjectIDs {
			related = append(related, map[string]string{"projectId": pid})
		}
		var summary strings.Builder
		fmt.Fprintf(&summary, "%s (%s). %s\n%d/%d tickets Done; %d PRs last reported open.\n", v.Initiative.Title, v.Initiative.Status, v.Initiative.Objective, v.Done, len(v.Tickets), v.OpenPRs)
		for _, k := range v.Tickets {
			fmt.Fprintf(&summary, "%s [%s]: %s; updated %s", k.Key, k.ID, k.Status, k.UpdatedAt.Format(time.RFC3339))
			if k.PullRequest != nil {
				fmt.Fprintf(&summary, "; PR %s reported %s", k.PullRequest.State, k.PullRequest.ReportedAt.Format(time.RFC3339))
			}
			summary.WriteByte('\n')
		}
		for _, event := range v.Activity {
			fmt.Fprintf(&summary, "%s: %s at %s\n", event.TicketID, event.Kind, event.CreatedAt.Format(time.RFC3339))
		}
		for _, blocker := range v.Blockers {
			fmt.Fprintf(&summary, "Revalidate: %s\n", blocker)
		}
		for _, next := range v.NextActions {
			fmt.Fprintf(&summary, "Suggestion: %s\n", next)
		}
		items = append(items, map[string]any{"id": v.Initiative.ID, "kind": "suggestion", "type": "initiative", "summary": summary.String(), "verification": "observed", "confidence": "medium", "freshness": "reported", "updatedAt": v.GeneratedAt, "relatedScopes": related, "source": map[string]string{"type": "import", "reference": "initiative:" + v.Initiative.ID}, "scope": map[string]string{"projectId": in.ProjectID}})
	}
	return items, nil
}
