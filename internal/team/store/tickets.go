package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"devboard/internal/team/domain"
)

// ---- project revision ----

// BumpRevision moves a project's revision and returns the new value. Every write
// that changes what a project's members see calls it in the same transaction.
func (t *sqlTx) BumpRevision(ctx context.Context, workspaceID, projectID string) (int64, error) {
	var rev int64
	err := t.q.QueryRowContext(ctx, `UPDATE projects SET revision = revision + 1 WHERE workspace_id = ? AND id = ? RETURNING revision`,
		workspaceID, projectID).Scan(&rev)
	return rev, notFound(err, "project")
}

// Revision returns a project's current revision.
func (t *sqlTx) Revision(ctx context.Context, workspaceID, projectID string) (int64, error) {
	var rev int64
	err := t.q.QueryRowContext(ctx, `SELECT revision FROM projects WHERE workspace_id = ? AND id = ?`, workspaceID, projectID).Scan(&rev)
	return rev, notFound(err, "project")
}

// ---- tickets ----

const ticketCols = `id, project_id, number, title, description, requirements, status, assignee_id, creator_id, reviewer_id, branch,
	pr_url, pr_number, pr_state, pr_draft, pr_mergeable, pr_base, pr_behind, pr_ahead, pr_reported_by, pr_reported_at, pr_created_at,
	version, created_at, updated_at, claimed_at, submitted_at, completed_at, archived_at, assignment, work_mode, plan_start, plan_end, milestone`

func optMS(v sql.NullInt64) *time.Time {
	if !v.Valid {
		return nil
	}
	t := fromMS(v.Int64)
	return &t
}

func nullMS(t *time.Time) any {
	if t == nil {
		return nil
	}
	return ms(*t)
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func scanTicket(s interface{ Scan(...any) error }) (domain.Ticket, error) {
	var t domain.Ticket
	var status, prState, prMergeable, prURL string
	var assignee, reviewer sql.NullString
	var prNumber, prBehind, prAhead int
	var prDraft int
	var prBy string
	var prReported, prCreated, created, updated int64
	var claimed, submitted, completed, archived sql.NullInt64
	var prBase string
	var milestone int
	err := s.Scan(&t.ID, &t.ProjectID, &t.Number, &t.Title, &t.Description, &t.Requirements, &status, &assignee, &t.CreatorID, &reviewer, &t.Branch,
		&prURL, &prNumber, &prState, &prDraft, &prMergeable, &prBase, &prBehind, &prAhead, &prBy, &prReported, &prCreated,
		&t.Version, &created, &updated, &claimed, &submitted, &completed, &archived, &t.Assignment, &t.WorkMode, &t.Plan.Start, &t.Plan.End, &milestone)
	if err != nil {
		return t, err
	}
	t.Plan.Milestone = milestone == 1
	t.LabelIDs, t.Dependencies = []string{}, []string{}
	t.Status, t.AssigneeID, t.ReviewerID = domain.TicketStatus(status), assignee.String, reviewer.String
	t.Key = domain.TicketKey(t.Number)
	t.CreatedAt, t.UpdatedAt = fromMS(created), fromMS(updated)
	t.ClaimedAt, t.SubmittedAt, t.CompletedAt = optMS(claimed), optMS(submitted), optMS(completed)
	t.ArchivedAt = optMS(archived)
	t.Commits = []domain.Commit{}
	if prURL != "" {
		t.PullRequest = &domain.PullRequest{Number: prNumber, URL: prURL, State: domain.PRState(prState), Draft: prDraft == 1,
			Mergeable: domain.Mergeable(prMergeable), BaseBranch: prBase, Behind: prBehind, Ahead: prAhead,
			ReportedBy: prBy, ReportedAt: fromMS(prReported), CreatedAt: fromMS(prCreated)}
	}
	return t, nil
}

// NextTicketNumber returns the number the workspace's next ticket takes.
func (t *sqlTx) NextTicketNumber(ctx context.Context, workspaceID string) (int, error) {
	var n int
	err := t.q.QueryRowContext(ctx, `SELECT COALESCE(MAX(number), 0) + 1 FROM tickets WHERE workspace_id = ?`, workspaceID).Scan(&n)
	return n, err
}

// InsertTicket stores a new ticket.
func (t *sqlTx) InsertTicket(ctx context.Context, workspaceID string, k domain.Ticket) error {
	_, err := t.q.ExecContext(ctx, `INSERT INTO tickets (id, workspace_id, project_id, number, title, description, requirements, status,
		assignee_id, creator_id, reviewer_id, branch, version, created_at, updated_at, work_mode, plan_start, plan_end, milestone)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?, ?, ?, ?, ?)`,
		k.ID, workspaceID, k.ProjectID, k.Number, k.Title, k.Description, k.Requirements, string(k.Status),
		nullStr(k.AssigneeID), k.CreatorID, nullStr(k.ReviewerID), k.Branch, ms(k.CreatedAt), ms(k.UpdatedAt),
		string(k.Mode()), k.Plan.Start, k.Plan.End, b2i(k.Plan.Milestone))
	if err != nil {
		return err
	}
	if err := t.SetTicketLabels(ctx, workspaceID, k.ID, k.LabelIDs); err != nil {
		return err
	}
	return t.SetTicketDependencies(ctx, workspaceID, k.ID, k.Dependencies)
}

// Ticket returns a ticket of a project, with its commits.
func (t *sqlTx) Ticket(ctx context.Context, workspaceID, projectID, id string) (domain.Ticket, error) {
	k, err := scanTicket(t.q.QueryRowContext(ctx, `SELECT `+ticketCols+` FROM tickets WHERE workspace_id = ? AND project_id = ? AND id = ?`, workspaceID, projectID, id))
	if err != nil {
		return k, notFound(err, "ticket")
	}
	k.Commits, err = t.commits(ctx, k.ID)
	if err != nil {
		return k, err
	}
	one := []domain.Ticket{k}
	if err := t.fillPlanning(ctx, workspaceID, one, `t.id = ?`, id); err != nil {
		return k, err
	}
	return one[0], nil
}

// Tickets lists a project's tickets, newest number first within a status; commits are not loaded.
func (t *sqlTx) Tickets(ctx context.Context, workspaceID, projectID string) ([]domain.Ticket, error) {
	rows, err := t.q.QueryContext(ctx, `SELECT `+ticketCols+` FROM tickets WHERE workspace_id = ? AND project_id = ? ORDER BY number`, workspaceID, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Ticket{}
	for rows.Next() {
		k, err := scanTicket(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := t.fillPlanning(ctx, workspaceID, out, `t.project_id = ?`, projectID); err != nil {
		return nil, err
	}
	return out, nil
}

// ClaimTicket takes an available ticket for a member. It is one guarded UPDATE:
// it changes the row only if the ticket is still available and held by nobody, so
// of any number of simultaneous claims exactly one affects a row. It reports
// whether this call was the one.
func (t *sqlTx) ClaimTicket(ctx context.Context, workspaceID, projectID, id, memberID, branch string, now time.Time) (bool, error) {
	res, err := t.q.ExecContext(ctx, `UPDATE tickets SET status = 'in_progress', assignee_id = ?, branch = CASE WHEN branch = '' THEN ? ELSE branch END,
		claimed_at = ?, submitted_at = NULL, completed_at = NULL, reviewer_id = NULL, version = version + 1, updated_at = ?
		WHERE workspace_id = ? AND project_id = ? AND id = ? AND status = 'available' AND assignee_id IS NULL AND archived_at IS NULL`,
		memberID, branch, ms(now), ms(now), workspaceID, projectID, id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// SaveTicket writes a ticket's changeable fields if its version is still the one
// the caller read (compare-and-swap), and bumps the version. It returns a
// conflict when someone else changed the ticket in between.
func (t *sqlTx) SaveTicket(ctx context.Context, workspaceID string, k domain.Ticket) (domain.Ticket, error) {
	pr := domain.PullRequest{State: domain.PROpen, Mergeable: domain.MergeUnknown, Behind: -1}
	if k.PullRequest != nil {
		pr = *k.PullRequest
	}
	res, err := t.q.ExecContext(ctx, `UPDATE tickets SET title = ?, description = ?, requirements = ?, status = ?, assignee_id = ?, reviewer_id = ?, branch = ?,
		pr_url = ?, pr_number = ?, pr_state = ?, pr_draft = ?, pr_mergeable = ?, pr_base = ?, pr_behind = ?, pr_ahead = ?, pr_reported_by = ?, pr_reported_at = ?, pr_created_at = ?,
		claimed_at = ?, submitted_at = ?, completed_at = ?, archived_at = ?, updated_at = ?,
		work_mode = ?, plan_start = ?, plan_end = ?, milestone = ?, version = version + 1
		WHERE workspace_id = ? AND project_id = ? AND id = ? AND version = ?`,
		k.Title, k.Description, k.Requirements, string(k.Status), nullStr(k.AssigneeID), nullStr(k.ReviewerID), k.Branch,
		pr.URL, pr.Number, string(pr.State), b2i(pr.Draft), string(pr.Mergeable), pr.BaseBranch, pr.Behind, pr.Ahead, pr.ReportedBy, ms(pr.ReportedAt), ms(pr.CreatedAt),
		nullMS(k.ClaimedAt), nullMS(k.SubmittedAt), nullMS(k.CompletedAt), nullMS(k.ArchivedAt), ms(k.UpdatedAt),
		string(k.Mode()), k.Plan.Start, k.Plan.End, b2i(k.Plan.Milestone),
		workspaceID, k.ProjectID, k.ID, k.Version)
	if err != nil {
		return k, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return k, fmt.Errorf("%w: the ticket was changed by someone else; reload it and try again", domain.ErrConflict)
	}
	k.Version++
	if err := t.SetTicketLabels(ctx, workspaceID, k.ID, k.LabelIDs); err != nil {
		return k, err
	}
	if err := t.SetTicketDependencies(ctx, workspaceID, k.ID, k.Dependencies); err != nil {
		return k, err
	}
	err = t.q.QueryRowContext(ctx, `SELECT assignment FROM tickets WHERE workspace_id=? AND project_id=? AND id=?`, workspaceID, k.ProjectID, k.ID).Scan(&k.Assignment)
	return k, err
}

// ReplaceCommits sets the commits reported for a ticket.
func (t *sqlTx) ReplaceCommits(ctx context.Context, ticketID string, commits []domain.Commit) error {
	if _, err := t.q.ExecContext(ctx, `DELETE FROM ticket_commits WHERE ticket_id = ?`, ticketID); err != nil {
		return err
	}
	for _, c := range commits {
		if _, err := t.q.ExecContext(ctx, `INSERT INTO ticket_commits (ticket_id, sha, subject, author, committed_at) VALUES (?, ?, ?, ?, ?)
			ON CONFLICT (ticket_id, sha) DO NOTHING`, ticketID, c.SHA, c.Subject, c.Author, ms(c.CommittedAt)); err != nil {
			return err
		}
	}
	return nil
}

func (t *sqlTx) commits(ctx context.Context, ticketID string) ([]domain.Commit, error) {
	rows, err := t.q.QueryContext(ctx, `SELECT sha, subject, author, committed_at FROM ticket_commits WHERE ticket_id = ? ORDER BY committed_at, sha`, ticketID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Commit{}
	for rows.Next() {
		var c domain.Commit
		var at int64
		if err := rows.Scan(&c.SHA, &c.Subject, &c.Author, &at); err != nil {
			return nil, err
		}
		c.CommittedAt = fromMS(at)
		out = append(out, c)
	}
	return out, rows.Err()
}

// ReleaseHeld puts back on the board every ticket in progress that a member holds
// on a project (a member who leaves a project must not keep tickets nobody is
// working on) and returns them.
func (t *sqlTx) ReleaseHeld(ctx context.Context, workspaceID, projectID, memberID string, now time.Time) ([]domain.Ticket, error) {
	rows, err := t.q.QueryContext(ctx, `UPDATE tickets SET status = 'available', assignee_id = NULL, version = version + 1, updated_at = ?
		WHERE workspace_id = ? AND project_id = ? AND assignee_id = ? AND status = 'in_progress' RETURNING `+ticketCols,
		ms(now), workspaceID, projectID, memberID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Ticket
	for rows.Next() {
		k, err := scanTicket(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// ProjectsOfMemberWithHeldTickets lists the projects in which a member holds a ticket in progress.
func (t *sqlTx) ProjectsOfMemberWithHeldTickets(ctx context.Context, workspaceID, memberID string) ([]string, error) {
	rows, err := t.q.QueryContext(ctx, `SELECT DISTINCT project_id FROM tickets WHERE workspace_id = ? AND assignee_id = ? AND status = 'in_progress'`, workspaceID, memberID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// ---- branches reported for a project ----

// maxBranchesPerProject bounds what members can make Team remember.
const maxBranchesPerProject = 500

// UpsertBranch records a branch a member reports. A later report replaces an earlier one.
func (t *sqlTx) UpsertBranch(ctx context.Context, workspaceID string, b domain.ProjectBranch) error {
	files, _ := json.Marshal(append([]string{}, b.Files...))
	_, err := t.q.ExecContext(ctx, `INSERT INTO project_branches (project_id, workspace_id, name, head_sha, base_branch, ahead, behind, last_commit_at, files, reported_by, reported_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (project_id, name) DO UPDATE SET head_sha = excluded.head_sha, base_branch = excluded.base_branch, ahead = excluded.ahead,
			behind = excluded.behind, last_commit_at = excluded.last_commit_at, files = excluded.files, reported_by = excluded.reported_by, reported_at = excluded.reported_at`,
		b.ProjectID, workspaceID, b.Name, b.HeadSHA, b.BaseBranch, b.Ahead, b.Behind, ms(b.LastCommitAt), string(files), b.ReportedBy, ms(b.ReportedAt))
	if err != nil {
		return err
	}
	// Keep the table bounded: forget the least recently reported beyond the cap.
	_, err = t.q.ExecContext(ctx, `DELETE FROM project_branches WHERE project_id = ? AND name IN
		(SELECT name FROM project_branches WHERE project_id = ? ORDER BY reported_at DESC, name LIMIT -1 OFFSET ?)`,
		b.ProjectID, b.ProjectID, maxBranchesPerProject)
	return err
}

// DeleteBranch forgets a reported branch (it was deleted or merged).
func (t *sqlTx) DeleteBranch(ctx context.Context, workspaceID, projectID, name string) (bool, error) {
	res, err := t.q.ExecContext(ctx, `DELETE FROM project_branches WHERE workspace_id = ? AND project_id = ? AND name = ?`, workspaceID, projectID, name)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// Branches lists the branches reported for a project, by name.
func (t *sqlTx) Branches(ctx context.Context, workspaceID, projectID string) ([]domain.ProjectBranch, error) {
	rows, err := t.q.QueryContext(ctx, `SELECT project_id, name, head_sha, base_branch, ahead, behind, last_commit_at, files, reported_by, reported_at
		FROM project_branches WHERE workspace_id = ? AND project_id = ? ORDER BY name`, workspaceID, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.ProjectBranch{}
	for rows.Next() {
		var b domain.ProjectBranch
		var last, reported int64
		var files string
		if err := rows.Scan(&b.ProjectID, &b.Name, &b.HeadSHA, &b.BaseBranch, &b.Ahead, &b.Behind, &last, &files, &b.ReportedBy, &reported); err != nil {
			return nil, err
		}
		b.LastCommitAt, b.ReportedAt = fromMS(last), fromMS(reported)
		if err := json.Unmarshal([]byte(files), &b.Files); err != nil {
			b.Files = nil
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// ---- activity ----

// AddActivity appends to a project's history.
func (t *sqlTx) AddActivity(ctx context.Context, workspaceID string, a domain.Activity) error {
	_, err := t.q.ExecContext(ctx, `INSERT INTO activity (workspace_id, project_id, ticket_id, ticket_key, actor_id, kind, detail, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		workspaceID, a.ProjectID, a.TicketID, a.TicketKey, a.ActorID, string(a.Kind), a.Detail, ms(a.CreatedAt))
	return err
}

// Activity lists a project's history, newest first. before > 0 pages: only entries with a smaller id.
func (t *sqlTx) Activity(ctx context.Context, workspaceID, projectID string, before int64, limit int) ([]domain.Activity, error) {
	q := `SELECT id, project_id, ticket_id, ticket_key, actor_id, kind, detail, created_at FROM activity WHERE workspace_id = ? AND project_id = ?`
	args := []any{workspaceID, projectID}
	if before > 0 {
		q += ` AND id < ?`
		args = append(args, before)
	}
	rows, err := t.q.QueryContext(ctx, q+` ORDER BY id DESC LIMIT ?`, append(args, limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanActivities(rows)
}

func scanActivities(rows interface {
	Next() bool
	Scan(...any) error
	Err() error
}) ([]domain.Activity, error) {
	out := []domain.Activity{}
	for rows.Next() {
		var a domain.Activity
		var kind string
		var at int64
		if err := rows.Scan(&a.ID, &a.ProjectID, &a.TicketID, &a.TicketKey, &a.ActorID, &kind, &a.Detail, &at); err != nil {
			return nil, err
		}
		a.Kind, a.CreatedAt = domain.ActivityKind(kind), fromMS(at)
		out = append(out, a)
	}
	return out, rows.Err()
}

// ---- invites ----

const inviteCols = `id, project_id, role, created_by, created_at, expires_at, max_uses, uses, revoked_at`

func scanInvite(s interface{ Scan(...any) error }) (domain.Invite, error) {
	var i domain.Invite
	var role string
	var created, expires int64
	var revoked sql.NullInt64
	err := s.Scan(&i.ID, &i.ProjectID, &role, &i.CreatedBy, &created, &expires, &i.MaxUses, &i.Uses, &revoked)
	i.Role, i.CreatedAt, i.ExpiresAt, i.RevokedAt = domain.ProjectRole(role), fromMS(created), fromMS(expires), optMS(revoked)
	return i, err
}

// InsertInvite stores a new invite with the hash of its code.
func (t *sqlTx) InsertInvite(ctx context.Context, workspaceID string, i domain.Invite, codeHash string) error {
	_, err := t.q.ExecContext(ctx, `INSERT INTO project_invites (id, workspace_id, project_id, code_hash, role, created_by, created_at, expires_at, max_uses, uses)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 0)`,
		i.ID, workspaceID, i.ProjectID, codeHash, string(i.Role), i.CreatedBy, ms(i.CreatedAt), ms(i.ExpiresAt), i.MaxUses)
	return err
}

// Invites lists a project's invites, newest first.
func (t *sqlTx) Invites(ctx context.Context, workspaceID, projectID string) ([]domain.Invite, error) {
	rows, err := t.q.QueryContext(ctx, `SELECT `+inviteCols+` FROM project_invites WHERE workspace_id = ? AND project_id = ? ORDER BY created_at DESC, id`, workspaceID, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Invite{}
	for rows.Next() {
		i, err := scanInvite(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

// RevokeInvite stops an invite from being used. Revoking one that is already revoked is a no-op.
func (t *sqlTx) RevokeInvite(ctx context.Context, workspaceID, projectID, id string, now time.Time) error {
	res, err := t.q.ExecContext(ctx, `UPDATE project_invites SET revoked_at = COALESCE(revoked_at, ?) WHERE workspace_id = ? AND project_id = ? AND id = ?`,
		ms(now), workspaceID, projectID, id)
	return affected(res, err, "invite")
}

// ErrInviteUnusable is returned when no invite can be used for a code: it never
// existed, expired, was revoked or is used up. The reasons are not told apart, so
// a code cannot be probed.
var ErrInviteUnusable = errors.New("this invite link or code is not valid any more")

// UseInvite redeems a code: it counts a use if, and only if, the invite is still
// valid (not revoked, not expired, uses left) and returns it. The check and the
// count are one UPDATE, so a single-use invite cannot be spent twice.
func (t *sqlTx) UseInvite(ctx context.Context, codeHash string, now time.Time) (workspaceID string, inv domain.Invite, err error) {
	row := t.q.QueryRowContext(ctx, `UPDATE project_invites SET uses = uses + 1
		WHERE code_hash = ? AND revoked_at IS NULL AND expires_at > ? AND uses < max_uses
		RETURNING workspace_id, `+inviteCols, codeHash, ms(now))
	var role string
	var created, expires int64
	var revoked sql.NullInt64
	err = row.Scan(&workspaceID, &inv.ID, &inv.ProjectID, &role, &inv.CreatedBy, &created, &expires, &inv.MaxUses, &inv.Uses, &revoked)
	if errors.Is(err, sql.ErrNoRows) {
		return "", inv, ErrInviteUnusable
	}
	inv.Role, inv.CreatedAt, inv.ExpiresAt, inv.RevokedAt = domain.ProjectRole(role), fromMS(created), fromMS(expires), optMS(revoked)
	return workspaceID, inv, err
}

// HasMemberNamed reports whether a name is taken in a workspace (ignoring case).
func (t *sqlTx) HasMemberNamed(ctx context.Context, workspaceID, name string) (bool, error) {
	var n int
	err := t.q.QueryRowContext(ctx, `SELECT COUNT(*) FROM members WHERE workspace_id = ? AND lower(name) = ?`, workspaceID, strings.ToLower(name)).Scan(&n)
	return n > 0, err
}
