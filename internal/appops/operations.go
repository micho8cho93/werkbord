package appops

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"devboard/internal/domain"
	"devboard/internal/planning"
	"devboard/internal/service"
)

// The words the operations use for the things on the board are the ones the person uses: a ticket is a task on a
// project's board.

const (
	idMax    = 80
	titleMax = 200
	// descMax is the longest description an operation accepts; the board accepts more, but a model has no business
	// writing a quarter of a megabyte.
	descMax = 20000
	answMax = 4000
)

var (
	projectID = Str("The project's id (from list_projects).", idMax)
	ticketID  = Str("The ticket's id (from list_tickets).", idMax)
	states    = []string{string(domain.TaskBacklog), string(domain.TaskDoing), string(domain.TaskReview), string(domain.TaskDone)}
	modes     = []string{string(planning.ModeHuman), string(planning.ModeAgent), string(planning.ModeHybrid)}
)

func operations() []*operation {
	return []*operation{
		{
			Spec: Spec{Name: "get_overview", Title: "What needs attention", Kind: KindRead, Permission: PermProjectsRead,
				Description: "Counts per project of what is running, waiting for an answer, blocked, failed or waiting for review. Start here to see where things stand.",
				Input:       Object(map[string]*Schema{})},
			run: opOverview,
		},
		{
			Spec: Spec{Name: "list_projects", Title: "List projects", Kind: KindRead, Permission: PermProjectsRead,
				Description: "The projects on the board: id, name, and whether it is backed by a Git repository.",
				Input:       Object(map[string]*Schema{})},
			run: opListProjects,
		},
		{
			Spec: Spec{Name: "list_labels", Title: "List labels", Kind: KindRead, Permission: PermTicketsRead,
				Description: "The reusable labels a ticket can carry (needed to set labelIds).",
				Input:       Object(map[string]*Schema{})},
			run: opListLabels,
		},
		{
			Spec: Spec{Name: "list_tickets", Title: "List tickets", Kind: KindRead, Permission: PermTicketsRead,
				Description: "A project's tickets, with state, work mode, labels and planned dates. Descriptions are shortened; use get_ticket for one in full.",
				Input: Object(map[string]*Schema{
					"projectId":       projectID,
					"state":           Enum("Only tickets in this column.", states...),
					"workMode":        Enum("Only tickets of this work mode.", modes...),
					"labelId":         Str("Only tickets carrying this label.", idMax),
					"query":           Str("Only tickets whose title contains this text.", 100),
					"includeArchived": Bool("Include archived tickets. Default false."),
					"limit":           Int("How many to return, 1 to 100. Default 50.", 1, 100),
				}, "projectId")},
			run: opListTickets,
		},
		{
			Spec: Spec{Name: "get_ticket", Title: "Get a ticket", Kind: KindRead, Permission: PermTicketsRead,
				Description: "One ticket in full, with its most recent runs.",
				Input:       Object(map[string]*Schema{"projectId": projectID, "ticketId": ticketID}, "projectId", "ticketId")},
			run: opGetTicket,
		},
		{
			Spec: Spec{Name: "get_schedule", Title: "Schedule and timeline", Kind: KindRead, Permission: PermScheduleRead,
				Description: "A project's planned dates, scheduled starts, deadlines and dependencies, with the problems found in them (a dependency planned to end after its dependent starts, a cycle, a missing ticket) and the tickets waiting on others.",
				Input: Object(map[string]*Schema{
					"projectId":   projectID,
					"includeDone": Bool("Include tickets that are done. Default false."),
				}, "projectId")},
			run: opGetSchedule,
		},
		{
			Spec: Spec{Name: "list_blockers", Title: "What is blocked", Kind: KindRead, Permission: PermRunsRead,
				Description: "Runs that stopped rather than guess (with what is in the way), runs that failed on a ticket still waiting on them, and tickets waiting on a dependency. Optionally for one project.",
				Input:       Object(map[string]*Schema{"projectId": projectID})},
			run: opListBlockers,
		},
		{
			Spec: Spec{Name: "list_agent_questions", Title: "Questions from agents", Kind: KindRead, Permission: PermQuestionsRead,
				Description: "Questions agents are waiting on. A question with answerableHere false (a request for permission) can only be answered by the person.",
				Input:       Object(map[string]*Schema{"projectId": projectID})},
			run: opListQuestions,
		},
		{
			Spec: Spec{Name: "list_runs", Title: "List runs", Kind: KindRead, Permission: PermRunsRead,
				Description: "A project's recent runs, newest first, optionally for one ticket or only those still going.",
				Input: Object(map[string]*Schema{
					"projectId":  projectID,
					"ticketId":   ticketID,
					"activeOnly": Bool("Only runs that have not ended."),
					"limit":      Int("How many to return, 1 to 50. Default 20.", 1, 50),
				}, "projectId")},
			run: opListRuns,
		},
		{
			Spec: Spec{Name: "get_run_status", Title: "Run status", Kind: KindRead, Permission: PermRunsRead,
				Description: "One run's state, what it is doing, why it ended or what blocks it.",
				Input:       Object(map[string]*Schema{"projectId": projectID, "runId": Str("The run's id.", idMax)}, "projectId", "runId")},
			run: opGetRun,
		},
		{
			Spec: Spec{Name: "create_ticket", Title: "Create a ticket", Kind: KindMutation, Permission: PermTicketsCreate, RequiresConfirmation: true,
				Description: "Add a ticket to the bottom of a project's Backlog. The person must confirm before it is created. It does not start any work.",
				Input: Object(map[string]*Schema{
					"projectId":    projectID,
					"title":        Str("A short title.", titleMax),
					"description":  Str("What needs doing.", descMax),
					"workMode":     Enum("Who is to do it. Default: agent in a repository project, human in a work project.", modes...),
					"labelIds":     List("Labels to put on it.", Str("A label id.", idMax), 20),
					"plannedStart": Day("First planned day, like 2026-03-31."),
					"plannedEnd":   Day("Last planned day."),
					"milestone":    Bool("Whether it is a single-day milestone."),
				}, "projectId", "title")},
			plan: planCreateTicket, run: runCreateTicket,
		},
		{
			Spec: Spec{Name: "update_ticket", Title: "Change a ticket", Kind: KindMutation, Permission: PermTicketsUpdate, RequiresConfirmation: true,
				Description: "Change a ticket's title, description, column, work mode, labels or planned dates. The person must confirm before it is changed. Moving a ticket never starts or stops a run. Give only what changes; an empty planned date clears it.",
				Input: Object(map[string]*Schema{
					"projectId":       projectID,
					"ticketId":        ticketID,
					"expectedVersion": Int("The ticket's version you last read. Leave out to use the current one.", 1, 1<<30),
					"title":           Str("A new title.", titleMax),
					"description":     Str("A new description (replaces the old one).", descMax),
					"state":           Enum("Move it to this column.", states...),
					"workMode":        Enum("Who is to do it.", modes...),
					"labelIds":        List("The labels it should carry (replaces the old set).", Str("A label id.", idMax), 20),
					"plannedStart":    Day("First planned day; empty clears."),
					"plannedEnd":      Day("Last planned day; empty clears."),
					"milestone":       Bool("Whether it is a single-day milestone."),
				}, "projectId", "ticketId")},
			plan: planUpdateTicket, run: runUpdateTicket,
		},
		{
			Spec: Spec{Name: "answer_question", Title: "Answer an agent's question", Kind: KindMutation, Permission: PermQuestionsAnswer, RequiresConfirmation: true,
				Description: "Give an agent the answer to a question it is waiting on, through the runner it is on. The person must confirm first. A request for permission to run something cannot be answered this way.",
				Input: Object(map[string]*Schema{
					"projectId":  projectID,
					"questionId": Str("The question's id (from list_agent_questions).", idMax),
					"answer":     Str("The answer. For a question with options, one of them.", answMax),
				}, "projectId", "questionId", "answer")},
			plan: planAnswer, run: runAnswer,
		},
	}
}

// Changed is what a mutation reports once the person has confirmed it and it has been carried out.
type Changed struct {
	Message  string        `json:"message"`
	Ticket   *TicketView   `json:"ticket,omitempty"`
	Question *QuestionView `json:"question,omitempty"`
}

// Outcome is the short account kept with the action.
func (c *Changed) Outcome() string { return c.Message }

// ---- reads ----

func opOverview(ctx context.Context, e *env, _ map[string]any) (any, error) {
	ov, err := e.b.Overview(ctx)
	if err != nil {
		return nil, err
	}
	type out struct {
		Projects          []service.ProjectActivity `json:"projects"`
		QuestionsWaiting  int                       `json:"questionsWaiting"`
		RunsBlocked       int                       `json:"runsBlocked"`
		RunsFailed        int                       `json:"runsFailed"`
		TicketsInReview   int                       `json:"ticketsInReview"`
		WaitingDependency int                       `json:"ticketsWaitingOnDependencies"`
	}
	res := out{Projects: []service.ProjectActivity{}}
	for _, p := range ov.Projects {
		if !e.p.CanSee(p.ProjectID) {
			continue
		}
		res.Projects = append(res.Projects, p)
		res.QuestionsWaiting += p.NeedsInput
		res.RunsBlocked += p.Blocked
		res.RunsFailed += p.Failed
		res.TicketsInReview += p.Review
		res.WaitingDependency += p.WaitingDependency
	}
	return res, nil
}

func opListProjects(ctx context.Context, e *env, _ map[string]any) (any, error) {
	ps, err := e.b.Projects(ctx)
	if err != nil {
		return nil, err
	}
	out := []ProjectView{}
	for _, p := range ps {
		if e.p.CanSee(p.ID) {
			out = append(out, projectView(p))
		}
	}
	return map[string]any{"projects": out}, nil
}

func opListLabels(ctx context.Context, e *env, _ map[string]any) (any, error) {
	ls, err := e.b.Labels(ctx)
	if err != nil {
		return nil, err
	}
	type label struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Description string `json:"description,omitempty"`
		OpenTickets int    `json:"openTickets"`
	}
	out := []label{}
	for _, l := range ls {
		out = append(out, label{ID: l.ID, Name: clip(l.Name, 80), Description: clip(l.Description, 200), OpenTickets: l.Tasks})
	}
	return map[string]any{"labels": out}, nil
}

func opListTickets(ctx context.Context, e *env, a map[string]any) (any, error) {
	pid := str(a, "projectId")
	if _, err := e.b.Project(ctx, pid); err != nil {
		return nil, err
	}
	all, err := e.b.Tickets(ctx, pid)
	if err != nil {
		return nil, err
	}
	limit := intArg(a, "limit", 50)
	query := strings.ToLower(str(a, "query"))
	includeArchived, _ := a["includeArchived"].(bool)
	out := []TicketView{}
	total := 0
	for _, t := range all {
		if t.ArchivedAt != nil && !includeArchived {
			continue
		}
		if s := str(a, "state"); s != "" && string(t.State) != s {
			continue
		}
		if m := str(a, "workMode"); m != "" && string(t.Mode()) != m {
			continue
		}
		if l := str(a, "labelId"); l != "" && !contains(t.LabelIDs, l) {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(t.Title), query) {
			continue
		}
		total++
		if len(out) < limit {
			out = append(out, ticketView(t, listDescriptionChars))
		}
	}
	return map[string]any{"tickets": out, "matching": total, "truncated": total > len(out)}, nil
}

func opGetTicket(ctx context.Context, e *env, a map[string]any) (any, error) {
	t, err := e.b.Ticket(ctx, str(a, "projectId"), str(a, "ticketId"))
	if err != nil {
		return nil, err
	}
	runs, err := e.b.TicketRuns(ctx, t.ID)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(runs, func(i, j int) bool { return runs[i].CreatedAt.After(runs[j].CreatedAt) })
	views := []RunView{}
	for _, r := range runs {
		if len(views) == 5 {
			break
		}
		views = append(views, runView(r))
	}
	return map[string]any{"ticket": ticketView(*t, fullDescriptionChars), "recentRuns": views}, nil
}

// ScheduleItem is a ticket's place in time.
type ScheduleItem struct {
	TicketID     string     `json:"ticketId"`
	Title        string     `json:"title"`
	State        string     `json:"state"`
	WorkMode     string     `json:"workMode"`
	PlannedStart string     `json:"plannedStart,omitempty"`
	PlannedEnd   string     `json:"plannedEnd,omitempty"`
	Milestone    bool       `json:"milestone,omitempty"`
	ScheduledAt  *time.Time `json:"scheduledAt,omitempty"`
	NotBefore    *time.Time `json:"notBefore,omitempty"`
	Deadline     *time.Time `json:"deadline,omitempty"`
	DependsOn    []string   `json:"dependsOn"`
	AutoStart    bool       `json:"autoStart"`
}

// WaitingView is a ticket the scheduler is holding back, and why.
type WaitingView struct {
	TicketID    string `json:"ticketId"`
	TicketTitle string `json:"ticketTitle"`
	ProjectID   string `json:"projectId"`
	ProjectName string `json:"projectName"`
	State       string `json:"state"`
	Reason      string `json:"reason"`
}

func opGetSchedule(ctx context.Context, e *env, a map[string]any) (any, error) {
	pid := str(a, "projectId")
	if _, err := e.b.Project(ctx, pid); err != nil {
		return nil, err
	}
	all, err := e.b.Tickets(ctx, pid)
	if err != nil {
		return nil, err
	}
	includeDone, _ := a["includeDone"].(bool)
	items := []ScheduleItem{}
	for _, t := range all {
		if t.ArchivedAt != nil || (t.State == domain.TaskDone && !includeDone) {
			continue
		}
		o := t.Orchestration
		if t.Plan.IsZero() && o.ScheduledAt == nil && o.NotBefore == nil && o.Deadline == nil && len(o.Dependencies) == 0 {
			continue
		}
		if len(items) == 100 {
			break
		}
		items = append(items, ScheduleItem{TicketID: t.ID, Title: clip(t.Title, 200), State: string(t.State), WorkMode: string(t.Mode()),
			PlannedStart: t.Plan.Start, PlannedEnd: t.Plan.End, Milestone: t.Plan.Milestone, ScheduledAt: o.ScheduledAt, NotBefore: o.NotBefore,
			Deadline: o.Deadline, DependsOn: nonNil(o.Dependencies), AutoStart: o.Enabled})
	}
	warnings := service.TimelineWarnings(all)
	if warnings == nil {
		warnings = []planning.Warning{}
	}
	ov, err := e.b.Overview(ctx)
	if err != nil {
		return nil, err
	}
	return map[string]any{"projectId": pid, "tickets": items, "warnings": warnings, "waiting": waiting(ov, pid)}, nil
}

// waiting lists the tickets the scheduler holds back for a reason that is not simply "not yet due".
func waiting(ov *service.Overview, projectID string) []WaitingView {
	out := []WaitingView{}
	for _, s := range ov.Orchestration {
		if projectID != "" && s.Task.ProjectID != projectID {
			continue
		}
		if s.Decision.State != "waiting_dependency" && s.Decision.State != "blocked" {
			continue
		}
		out = append(out, WaitingView{TicketID: s.Task.ID, TicketTitle: clip(s.Task.Title, 200), ProjectID: s.Task.ProjectID,
			ProjectName: s.ProjectName, State: s.Decision.State, Reason: clip(s.Decision.Reason, 300)})
	}
	return out
}

// BlockedRunView is a run that cannot go on, with where it belongs.
type BlockedRunView struct {
	Run         RunView `json:"run"`
	TicketTitle string  `json:"ticketTitle"`
	ProjectName string  `json:"projectName"`
}

func opListBlockers(ctx context.Context, e *env, a map[string]any) (any, error) {
	pid := str(a, "projectId")
	if pid != "" {
		if _, err := e.b.Project(ctx, pid); err != nil {
			return nil, err
		}
	}
	ov, err := e.b.Overview(ctx)
	if err != nil {
		return nil, err
	}
	keep := func(project string) bool { return e.p.CanSee(project) && (pid == "" || pid == project) }
	blocked, failed := []BlockedRunView{}, []BlockedRunView{}
	for _, r := range ov.Runs {
		if r.Run.State == domain.RunBlocked && keep(r.Run.ProjectID) {
			blocked = append(blocked, BlockedRunView{Run: runView(r.Run), TicketTitle: clip(r.TaskTitle, 200), ProjectName: r.ProjectName})
		}
	}
	for _, r := range ov.Failed {
		if keep(r.Run.ProjectID) {
			failed = append(failed, BlockedRunView{Run: runView(r.Run), TicketTitle: clip(r.TaskTitle, 200), ProjectName: r.ProjectName})
		}
	}
	held := []WaitingView{}
	for _, w := range waiting(ov, pid) {
		if keep(w.ProjectID) {
			held = append(held, w)
		}
	}
	questions := 0
	for _, q := range ov.Questions {
		if keep(q.Question.ProjectID) {
			questions++
		}
	}
	return map[string]any{"blockedRuns": blocked, "failedRuns": failed, "waitingOnDependencies": held, "questionsWaiting": questions}, nil
}

func opListQuestions(ctx context.Context, e *env, a map[string]any) (any, error) {
	pid := str(a, "projectId")
	if pid != "" {
		if _, err := e.b.Project(ctx, pid); err != nil {
			return nil, err
		}
	}
	ov, err := e.b.Overview(ctx)
	if err != nil {
		return nil, err
	}
	out := []QuestionView{}
	for _, q := range ov.Questions {
		if !e.p.CanSee(q.Question.ProjectID) || pid != "" && q.Question.ProjectID != pid {
			continue
		}
		out = append(out, questionView(q.Question, q.ProjectName, q.TaskTitle))
	}
	return map[string]any{"questions": out}, nil
}

func opListRuns(ctx context.Context, e *env, a map[string]any) (any, error) {
	pid := str(a, "projectId")
	limit := intArg(a, "limit", 20)
	var runs []domain.Run
	var err error
	if tid := str(a, "ticketId"); tid != "" {
		t, terr := e.b.Ticket(ctx, pid, tid) // the ticket must be in this project
		if terr != nil {
			return nil, terr
		}
		runs, err = e.b.TicketRuns(ctx, t.ID)
		sort.SliceStable(runs, func(i, j int) bool { return runs[i].CreatedAt.After(runs[j].CreatedAt) })
	} else {
		runs, err = e.b.Runs(ctx, pid, 200)
	}
	if err != nil {
		return nil, err
	}
	activeOnly, _ := a["activeOnly"].(bool)
	out := []RunView{}
	for _, r := range runs {
		if activeOnly && r.EndedAt != nil {
			continue
		}
		if len(out) == limit {
			break
		}
		out = append(out, runView(r))
	}
	return map[string]any{"runs": out}, nil
}

func opGetRun(ctx context.Context, e *env, a map[string]any) (any, error) {
	r, err := e.b.Run(ctx, str(a, "projectId"), str(a, "runId"))
	if err != nil {
		return nil, err
	}
	return map[string]any{"run": runView(*r)}, nil
}

// ---- mutations ----

func planCreateTicket(ctx context.Context, e *env, a map[string]any) (*plan, error) {
	pid := str(a, "projectId")
	project, err := e.b.Project(ctx, pid)
	if err != nil {
		return nil, err
	}
	title, err := domain.ValidateTaskTitle(str(a, "title"))
	if err != nil {
		return nil, err
	}
	desc := str(a, "description")
	if err := domain.ValidateTaskDescription(desc); err != nil {
		return nil, err
	}
	mode := str(a, "workMode")
	if mode != "" {
		if _, err := planning.ParseExecutionMode(mode); err != nil {
			return nil, fmt.Errorf("%w: %s", domain.ErrInvalid, err)
		}
	}
	labels, err := labelNames(ctx, e, strList(a, "labelIds"))
	if err != nil {
		return nil, err
	}
	rng, err := planning.CleanRange(planning.Range{Start: str(a, "plannedStart"), End: str(a, "plannedEnd"), Milestone: boolArg(a, "milestone")})
	if err != nil {
		return nil, fmt.Errorf("%w: %s", domain.ErrInvalid, err)
	}
	norm := map[string]any{"projectId": pid, "title": title}
	if desc != "" {
		norm["description"] = desc
	}
	if mode != "" {
		norm["workMode"] = mode
	}
	if ids := strList(a, "labelIds"); len(ids) > 0 {
		norm["labelIds"] = ids
	}
	if rng.Start != "" {
		norm["plannedStart"] = rng.Start
	}
	if rng.End != "" {
		norm["plannedEnd"] = rng.End
	}
	if rng.Milestone {
		norm["milestone"] = true
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "Create the ticket %q in project %q", title, project.Name)
	if mode != "" {
		fmt.Fprintf(&sb, " as %s work", mode)
	}
	if len(labels) > 0 {
		fmt.Fprintf(&sb, ", labelled %s", strings.Join(labels, ", "))
	}
	if rng.Start != "" || rng.End != "" {
		fmt.Fprintf(&sb, ", planned %s", rangeText(rng))
	}
	if desc != "" {
		fmt.Fprintf(&sb, ". Description: %q", clip(oneLine(desc), 200))
	}
	return &plan{Args: norm, Summary: sb.String()}, nil
}

func runCreateTicket(ctx context.Context, e *env, a map[string]any) (any, error) {
	t, err := e.b.CreateTicket(ctx, service.NewTask{
		ProjectID: str(a, "projectId"), Title: str(a, "title"), Description: str(a, "description"),
		WorkMode: planning.ExecutionMode(str(a, "workMode")), LabelIDs: strList(a, "labelIds"),
		Plan: domain.Plan{Start: str(a, "plannedStart"), End: str(a, "plannedEnd"), Milestone: boolArg(a, "milestone")},
	})
	if err != nil {
		return nil, err
	}
	v := ticketView(*t, listDescriptionChars)
	return &Changed{Message: fmt.Sprintf("Created ticket %s (%q) in the %s column", t.ID, clip(t.Title, 80), t.State), Ticket: &v}, nil
}

func planUpdateTicket(ctx context.Context, e *env, a map[string]any) (*plan, error) {
	pid, tid := str(a, "projectId"), str(a, "ticketId")
	t, err := e.b.Ticket(ctx, pid, tid)
	if err != nil {
		return nil, err
	}
	if t.ArchivedAt != nil {
		return nil, fmt.Errorf("%w: this ticket is archived; it has to be restored in Werkbord before it can be changed", domain.ErrConflict)
	}
	if v, ok := a["expectedVersion"]; ok && int64(v.(float64)) != t.Version {
		return nil, fmt.Errorf("%w: ticket %s is at version %d, not %d: read it again first", domain.ErrConflict, t.ID, t.Version, int64(v.(float64)))
	}
	norm := map[string]any{"projectId": pid, "ticketId": tid, "expectedVersion": t.Version}
	var changes []string
	if v, ok := a["title"]; ok {
		title, err := domain.ValidateTaskTitle(v.(string))
		if err != nil {
			return nil, err
		}
		if title != t.Title {
			norm["title"] = title
			changes = append(changes, fmt.Sprintf("title %q → %q", clip(t.Title, 60), clip(title, 60)))
		}
	}
	if v, ok := a["description"]; ok {
		if err := domain.ValidateTaskDescription(v.(string)); err != nil {
			return nil, err
		}
		if v.(string) != t.Description {
			norm["description"] = v
			changes = append(changes, fmt.Sprintf("description replaced (now %q)", clip(oneLine(v.(string)), 120)))
		}
	}
	if v, ok := a["state"]; ok && domain.TaskState(v.(string)) != t.State {
		norm["state"] = v
		changes = append(changes, fmt.Sprintf("moved %s → %s", t.State, v))
	}
	if v, ok := a["workMode"]; ok && planning.ExecutionMode(v.(string)) != t.Mode() {
		norm["workMode"] = v
		changes = append(changes, fmt.Sprintf("work mode %s → %s", t.Mode(), v))
	}
	if _, ok := a["labelIds"]; ok {
		ids := strList(a, "labelIds")
		if !sameSet(ids, t.LabelIDs) {
			names, err := labelNames(ctx, e, ids)
			if err != nil {
				return nil, err
			}
			norm["labelIds"] = ids
			changes = append(changes, "labels now "+nameList(names))
		}
	}
	_, hasStart := a["plannedStart"]
	_, hasEnd := a["plannedEnd"]
	_, hasMilestone := a["milestone"]
	if hasStart || hasEnd || hasMilestone {
		next := t.Plan
		if hasStart {
			next.Start = str(a, "plannedStart")
		}
		if hasEnd {
			next.End = str(a, "plannedEnd")
		}
		if hasMilestone {
			next.Milestone = boolArg(a, "milestone")
		}
		next, err := planning.CleanRange(next)
		if err != nil {
			return nil, fmt.Errorf("%w: %s", domain.ErrInvalid, err)
		}
		if next != t.Plan {
			// The whole resulting plan is carried, so what is confirmed is what it becomes.
			norm["plannedStart"], norm["plannedEnd"], norm["milestone"] = next.Start, next.End, next.Milestone
			changes = append(changes, fmt.Sprintf("planned %s → %s", rangeText(t.Plan), rangeText(next)))
		}
	}
	if len(changes) == 0 {
		return nil, fmt.Errorf("%w: nothing would change: the ticket already is as asked", domain.ErrInvalid)
	}
	return &plan{Args: norm, Summary: fmt.Sprintf("Change ticket %q (%s): %s", clip(t.Title, 80), t.ID, strings.Join(changes, "; "))}, nil
}

func runUpdateTicket(ctx context.Context, e *env, a map[string]any) (any, error) {
	patch := service.TaskPatch{Version: int64(a["expectedVersion"].(float64))}
	if v, ok := a["title"].(string); ok {
		patch.Title = &v
	}
	if v, ok := a["description"].(string); ok {
		patch.Description = &v
	}
	if v, ok := a["state"].(string); ok {
		st := domain.TaskState(v)
		patch.State = &st
	}
	if v, ok := a["workMode"].(string); ok {
		m := planning.ExecutionMode(v)
		patch.WorkMode = &m
	}
	if _, ok := a["labelIds"]; ok {
		ids := strList(a, "labelIds")
		patch.LabelIDs = &ids
	}
	if _, ok := a["plannedStart"]; ok {
		plan := domain.Plan{Start: str(a, "plannedStart"), End: str(a, "plannedEnd"), Milestone: boolArg(a, "milestone")}
		patch.Plan = &plan
	}
	// The ticket must still be in the project the person was shown.
	if _, err := e.b.Ticket(ctx, str(a, "projectId"), str(a, "ticketId")); err != nil {
		return nil, err
	}
	t, err := e.b.UpdateTicket(ctx, str(a, "ticketId"), patch)
	if err != nil {
		return nil, err
	}
	v := ticketView(*t, listDescriptionChars)
	return &Changed{Message: fmt.Sprintf("Changed ticket %s (%q); it is now in the %s column", t.ID, clip(t.Title, 80), t.State), Ticket: &v}, nil
}

func planAnswer(ctx context.Context, e *env, a map[string]any) (*plan, error) {
	pid, qid := str(a, "projectId"), str(a, "questionId")
	q, err := e.b.Question(ctx, pid, qid)
	if err != nil {
		return nil, err
	}
	if q.Kind == domain.QuestionApproval {
		return nil, errorf(CodeApprovalIsPersons, "%s", approvalNote)
	}
	if !q.Pending() {
		return nil, fmt.Errorf("%w: this question is no longer waiting for an answer (%s)", domain.ErrConflict, q.State)
	}
	answer, err := q.CheckAnswer(str(a, "answer"))
	if err != nil {
		return nil, err
	}
	title := ""
	if t, err := e.b.Ticket(ctx, pid, q.TaskID); err == nil {
		title = t.Title
	}
	return &plan{
		Args:    map[string]any{"projectId": pid, "questionId": qid, "answer": answer},
		Summary: fmt.Sprintf("Answer the agent's question %q (ticket %q) with %q", clip(oneLine(q.Prompt), 160), clip(title, 80), clip(oneLine(answer), 300)),
	}, nil
}

func runAnswer(ctx context.Context, e *env, a map[string]any) (any, error) {
	pid, qid := str(a, "projectId"), str(a, "questionId")
	// Looked at again at the moment of answering, not trusted from when it was proposed.
	q, err := e.b.Question(ctx, pid, qid)
	if err != nil {
		return nil, err
	}
	if q.Kind == domain.QuestionApproval {
		return nil, errorf(CodeApprovalIsPersons, "%s", approvalNote)
	}
	done, err := e.b.AnswerQuestion(ctx, qid, str(a, "answer"))
	if err != nil {
		return nil, err
	}
	v := questionView(*done, "", "")
	return &Changed{Message: fmt.Sprintf("Answered question %s; the agent was given %q", done.ID, clip(oneLine(done.Answer), 120)), Question: &v}, nil
}

// ---- argument helpers ----

func str(a map[string]any, k string) string { s, _ := a[k].(string); return s }

func boolArg(a map[string]any, k string) bool { b, _ := a[k].(bool); return b }

func intArg(a map[string]any, k string, def int) int {
	if f, ok := a[k].(float64); ok {
		return int(f)
	}
	return def
}

func strList(a map[string]any, k string) []string {
	list, _ := a[k].([]any)
	out := make([]string, 0, len(list))
	for _, v := range list {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for _, v := range a {
		if !contains(b, v) {
			return false
		}
	}
	return true
}

// labelNames checks that every label exists and returns their names, for the summary.
func labelNames(ctx context.Context, e *env, ids []string) ([]string, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	ls, err := e.b.Labels(ctx)
	if err != nil {
		return nil, err
	}
	byID := map[string]string{}
	for _, l := range ls {
		byID[l.ID] = l.Name
	}
	names := make([]string, 0, len(ids))
	for _, id := range ids {
		name, ok := byID[id]
		if !ok {
			return nil, fmt.Errorf("%w: there is no label %q: use list_labels", domain.ErrInvalid, id)
		}
		names = append(names, clip(name, 40))
	}
	return names, nil
}

func nameList(names []string) string {
	if len(names) == 0 {
		return "none"
	}
	return strings.Join(names, ", ")
}

func rangeText(r planning.Range) string {
	switch {
	case r.IsZero():
		return "nothing"
	case r.Milestone:
		return "milestone " + r.Start
	case r.End == "":
		return "from " + r.Start
	case r.Start == "":
		return "until " + r.End
	}
	return r.Start + " to " + r.End
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }
