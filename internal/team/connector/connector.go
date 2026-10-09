// Package connector runs on a member's own computer as that user. It imports
// context and reports metadata. Optional scheduling dispatches an exact local
// approval through Individual; the connector has no agent runtime or command API.
package connector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"devboard/internal/integration"
	"devboard/internal/team/devicestate"
	"devboard/internal/team/domain"
	"devboard/internal/team/hostclient"
	"devboard/internal/team/localwerkbord"
	"devboard/internal/team/service"
)

type Local interface {
	IntegrationProjects(context.Context) ([]localwerkbord.Project, error)
	Import(context.Context, integration.Import) (integration.Imported, error)
	Snapshot(context.Context, string, string) (integration.Snapshot, error)
	Events(context.Context, string, string, int64) (integration.Feed, error)
}
type Workspace interface {
	Me(context.Context) (hostclient.Me, error)
	Do(context.Context, string, string, any, any) error
	Handoff(context.Context, string, string) (hostclient.Handoff, error)
}
type Connector struct {
	WorkspaceID, MemberID, DeviceID string
	Host                            Workspace
	Local                           Local
	Journal                         *devicestate.SyncJournal
	// Projects is the locally opted-in Team project → Individual project map.
	// An empty value selects the unique canonical repository match.
	Execution ExecutionLocal
	Projects  map[string]string
	Now       func() time.Time
}

func (c *Connector) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}
func Source(w, p, t, m string) string { return "werkbord-source:v1:" + w + ":" + p + ":" + t + ":" + m }
func (c *Connector) choose(ctx context.Context, p service.ProjectRef) (string, error) {
	want, err := integration.RepositoryIdentity(p.Repository)
	if err != nil {
		return "", err
	}
	ps, err := c.Local.IntegrationProjects(ctx)
	if err != nil {
		return "", err
	}
	var matches []string
	for _, local := range ps {
		for _, r := range local.Remotes {
			got, e := integration.RepositoryIdentity(r)
			if e == nil && got == want {
				matches = append(matches, local.ID)
				break
			}
		}
	}
	if selected := c.Projects[p.ID]; selected != "" {
		for _, id := range matches {
			if id == selected {
				return id, nil
			}
		}
		return "", errors.New("selected local project does not match the canonical repository")
	}
	if len(matches) != 1 {
		return "", fmt.Errorf("project %s has %d repository matches; select a matching local project explicitly", p.ID, len(matches))
	}
	return matches[0], nil
}

// Tick reconciles authoritative Team ownership before importing or delivering.
// Local observations are journaled even when a workspace is offline.
func (c *Connector) Tick(ctx context.Context) error {
	for _, id := range []string{c.WorkspaceID, c.MemberID, c.DeviceID} {
		if !integration.Identifier(id) {
			return errors.New("invalid connector identity")
		}
	}
	as, err := c.Journal.Associations(ctx, c.WorkspaceID)
	if err != nil {
		return err
	}
	for _, a := range as {
		if a.MemberID != c.MemberID || a.DeviceID != c.DeviceID {
			return errors.New("connector journal belongs to a different identity")
		}
	}
	me, teamErr := c.Host.Me(ctx)
	if teamErr == nil && (me.Workspace.ID != c.WorkspaceID || me.Member.ID != c.MemberID) {
		return errors.New("workspace answered for a different identity")
	}
	var work service.MyWork
	if teamErr == nil {
		teamErr = c.Host.Do(ctx, "GET", "/my-work", nil, &work)
	}
	if hostclient.IsStatus(teamErr, 401) || hostclient.IsStatus(teamErr, 403) {
		for i := range as {
			if err := c.Journal.Suspend(ctx, &as[i], "membership or device access revoked"); err != nil {
				return err
			}
		}
		return teamErr
	}
	if teamErr != nil {
		for i := range as {
			if as[i].Suspended == "" {
				if err := c.observe(ctx, &as[i]); err != nil {
					return err
				}
			}
		}
		return teamErr
	}
	held := map[string]service.WorkItem{}
	for _, item := range append(work.InProgress, work.Submitted...) {
		if !integration.Identifier(item.Project.ID) || !integration.Identifier(item.Ticket.ID) || item.Ticket.ProjectID != item.Project.ID {
			return errors.New("workspace returned an invalid ticket identity")
		}
		if item.Ticket.AssigneeID == c.MemberID && item.Ticket.Status.Held() && item.Ticket.ArchivedAt == nil && !item.Project.Archived {
			held[item.Project.ID+":"+item.Ticket.ID] = item
		}
	}
	known := map[string]int{}
	for i := range as {
		known[as[i].TeamProjectID+":"+as[i].TicketID] = i
	}
	var problems []error
	for _, item := range held {
		if _, enabled := c.Projects[item.Project.ID]; !enabled {
			continue
		}
		id := item.Project.ID + ":" + item.Ticket.ID
		index, exists := known[id]
		if !exists {
			if len(as) >= devicestate.MaxAssociations {
				problems = append(problems, errors.New("connector association limit reached"))
				continue
			}
			pid, err := c.choose(ctx, item.Project)
			if err != nil {
				problems = append(problems, err)
				continue
			}
			a := devicestate.Association{WorkspaceID: c.WorkspaceID, TeamProjectID: item.Project.ID, TicketID: item.Ticket.ID, MemberID: c.MemberID, DeviceID: c.DeviceID, LocalProjectID: pid, Repository: item.Project.Repository}
			as = append(as, a)
			index = len(as) - 1
			known[id] = index
		}
		a := &as[index]
		if item.Ticket.ClaimedAt == nil {
			continue
		}
		if a.Assignment != item.Ticket.Assignment || !a.ClaimAt.Equal(*item.Ticket.ClaimedAt) {
			if err := c.Journal.Suspend(ctx, a, "assignment changed"); err != nil {
				return err
			}
			a.ClaimAt = *item.Ticket.ClaimedAt
			a.Assignment = item.Ticket.Assignment
			a.LastObservation = ""
			a.Suspended = ""
		}
		if a.Repository != item.Project.Repository {
			old, e1 := integration.RepositoryIdentity(a.Repository)
			next, e2 := integration.RepositoryIdentity(item.Project.Repository)
			if e1 != nil || e2 != nil || old != next {
				if err := c.Journal.Suspend(ctx, a, "project repository changed; review local association"); err != nil {
					return err
				}
				continue
			}
		}
		contextRaw, _ := json.Marshal(struct{ Title, Description, Requirements, Repository string }{item.Ticket.Title, item.Ticket.Description, item.Ticket.Requirements, item.Project.Repository})
		contextKey := string(contextRaw)
		if a.Suspended == "ticket unavailable, reassigned, archived or project disconnected" {
			a.Suspended = ""
		}
		if a.Suspended != "" {
			continue
		}
		if a.TaskID == "" || a.TeamContext != contextKey {
			h, err := c.Host.Handoff(ctx, item.Project.ID, item.Ticket.ID)
			if err != nil {
				problems = append(problems, err)
				continue
			}
			if h.Project.ID != a.TeamProjectID || h.Ticket.ID != a.TicketID || h.For.ID != c.MemberID {
				problems = append(problems, errors.New("workspace returned a handoff for a different ticket or member"))
				continue
			}
			repo1, e1 := integration.RepositoryIdentity(h.Git.Repository)
			repo2, e2 := integration.RepositoryIdentity(item.Project.Repository)
			if e1 != nil || e2 != nil || repo1 != repo2 {
				problems = append(problems, errors.New("handoff repository does not match selected project"))
				continue
			}
			title := []rune(h.Ticket.Key + ": " + h.Ticket.Title)
			if len(title) > 200 {
				title = title[:200]
			}
			branch := h.Git.Branch
			prefix := "wb/" + c.WorkspaceID + "/"
			if !strings.HasPrefix(branch, prefix) {
				branch = prefix + branch
			}
			description := "Local working branch: " + branch + ". Use this task's working branch in place of the Team branch suggestion below.\n\n" + h.Prompt
			text := integration.Text{Title: string(title), Description: description, WorkBranch: branch, BaseBranch: h.Git.BaseBranch}
			in := integration.Import{Schema: integration.Schema, SourceRef: Source(c.WorkspaceID, a.TeamProjectID, a.TicketID, c.MemberID), ProjectID: a.LocalProjectID, Repository: item.Project.Repository, Title: text.Title, Description: text.Description, WorkBranch: text.WorkBranch, BaseBranch: text.BaseBranch}
			if hosts, ok := c.Host.(interface{ Bases() []string }); ok {
				for _, base := range hosts.Bases() {
					in.SourceAliases = append(in.SourceAliases, base+"/?tab=board&project="+url.QueryEscape(a.TeamProjectID)+"&ticket="+url.QueryEscape(a.TicketID))
				}
			}
			if a.TaskID != "" {
				in.Previous = &a.Imported
			}
			out, err := c.Local.Import(ctx, in)
			if err != nil {
				problems = append(problems, err)
				continue
			}
			a.TaskID, a.Conflict = out.TaskID, out.Conflict
			if !out.Conflict {
				a.Imported = text
			}
			a.TeamContext = contextKey
		}
		if err := c.Journal.Save(ctx, *a); err != nil {
			return err
		}
	}
	for i := range as {
		a := &as[i]
		item, ok := held[a.TeamProjectID+":"+a.TicketID]
		_, enabled := c.Projects[a.TeamProjectID]
		if !ok || !enabled || item.Ticket.ClaimedAt == nil || a.Assignment != item.Ticket.Assignment || !a.ClaimAt.Equal(*item.Ticket.ClaimedAt) {
			if err := c.Journal.Suspend(ctx, a, "ticket unavailable, reassigned, archived or project disconnected"); err != nil {
				return err
			}
			continue
		}
		if a.Suspended != "" || a.TaskID == "" {
			continue
		}
		if err := c.deliver(ctx, a); err != nil {
			problems = append(problems, err)
			// Preserve new local observations during transient outbound failure.
			if a.Suspended != "" {
				continue
			}
		}
		if err := c.observe(ctx, a); err != nil {
			problems = append(problems, err)
			continue
		}
		if err := c.deliver(ctx, a); err != nil {
			problems = append(problems, err)
		}
	}
	if c.Execution != nil {
		for pid := range c.Projects {
			var schedules []domain.Schedule
			if err := c.Host.Do(ctx, "GET", "/projects/"+pid+"/schedules", nil, &schedules); err != nil {
				problems = append(problems, err)
				continue
			}
			for _, v := range schedules {
				if v.MemberID != c.MemberID {
					continue
				}
				for _, a := range as {
					if a.TeamProjectID == pid && a.TicketID == v.TicketID && a.Suspended == "" && !a.Conflict && a.Assignment == v.Assignment && a.TaskID != "" {
						err := CoordinateSchedule(ctx, c.Host, c.Execution, c.DeviceID, v, func(req integration.ExecutionRequest) bool {
							return req.ProjectID == a.LocalProjectID && req.TaskID == a.TaskID
						})
						if err != nil {
							problems = append(problems, err)
						}
					}
				}
			}
		}
	}
	return errors.Join(problems...)
}
func (c *Connector) observe(ctx context.Context, a *devicestate.Association) error {
	if a.TaskID == "" {
		return nil
	}
	snap, err := c.Local.Snapshot(ctx, a.LocalProjectID, a.TaskID)
	if err != nil {
		return err
	}
	feed, err := c.Local.Events(ctx, a.LocalProjectID, a.TaskID, a.Cursor)
	if err != nil {
		return err
	}
	next := *a
	if snap.Schema != integration.Schema || feed.Schema != integration.Schema {
		return errors.New("unsupported integration schema")
	}
	var observations []domain.Progress
	lastSeq := a.Cursor
	if feed.Cursor < 0 || !feed.Reset && feed.Cursor < a.Cursor || snap.Cursor < 0 || !snap.Execution.Valid() {
		return errors.New("invalid integration snapshot")
	}
	if !feed.Reset {
		for _, e := range feed.Events {
			if e.Seq <= lastSeq || e.Seq > feed.Cursor || e.Execution == nil || !e.Execution.Valid() {
				return errors.New("invalid or out-of-order controller event")
			}
			lastSeq = e.Seq
			observations = append(observations, domain.Progress{Execution: *e.Execution, GitUnavailable: true})
			if len(observations) == 128 {
				feed.Cursor = e.Seq
				break
			}
		}
	}
	// Snapshot precedes replay: unrelated output/heartbeat writes may advance the
	// global cursor while Git is inspected. Once replay catches up, append the
	// snapshot unless a newer lifecycle event for this task would be regressed.
	// A retention reset checkpoints the snapshot itself, so later events replay.
	if feed.Reset {
		feed.Cursor = snap.Cursor
	}
	if feed.Reset || feed.Cursor >= snap.Cursor && lastSeq <= snap.Cursor {
		o := domain.Progress{Execution: snap.Execution, Git: snap.Git, GitUnavailable: snap.GitUnavailable}
		b, _ := json.Marshal(o)
		if string(b) != a.LastObservation || c.now().Sub(a.LastSnapshotAt) >= time.Minute {
			observations = append(observations, o)
			next.LastObservation = string(b)
			next.LastSnapshotAt = c.now()
		}
	}
	if len(observations) > 128 {
		observations = observations[:128]
		feed.Cursor = feed.Events[127].Seq
		next.LastObservation = a.LastObservation
		next.LastSnapshotAt = a.LastSnapshotAt
	}
	if err := c.Journal.Enqueue(ctx, &next, feed.Cursor, observations); err != nil {
		return err
	}
	*a = next
	return nil
}
func (c *Connector) deliver(ctx context.Context, a *devicestate.Association) error {
	ps, err := c.Journal.Pending(ctx, a.Key(), c.now())
	if err != nil {
		return err
	}
	for _, p := range ps {
		var ack domain.ProgressAck
		path := "/projects/" + a.TeamProjectID + "/tickets/" + a.TicketID + "/progress"
		err := c.Host.Do(ctx, "PUT", path, p.Progress, &ack)
		if err != nil {
			if hostclient.IsStatus(err, 401) || hostclient.IsStatus(err, 403) || hostclient.IsStatus(err, 404) || hostclient.IsStatus(err, 409) {
				return c.Journal.Suspend(ctx, a, "workspace refused progress; reconcile authority and review association")
			}
			if e := c.Journal.Retry(ctx, p, c.now()); e != nil {
				return e
			}
			return err
		}
		if ack.Sequence < p.Progress.Sequence || ack.Sequence > a.NextSequence {
			return errors.New("workspace returned an invalid acknowledgment")
		}
		if err := c.Journal.Ack(ctx, a.Key(), ack.Sequence); err != nil {
			return err
		}
	}
	return nil
}
