package appops

import (
	"context"

	"devboard/internal/domain"
	"devboard/internal/service"
)

// Backend is everything the operations may use, and nothing else. It is the existing domain services, not a store:
// there is no method here that reads or writes a table, runs Git, or starts, stops or configures a run. An operation
// that needs more has to say so here, where it can be seen and reviewed.
type Backend interface {
	Projects(ctx context.Context) ([]service.ProjectDetail, error)
	Project(ctx context.Context, projectID string) (*service.ProjectDetail, error)
	Labels(ctx context.Context) ([]domain.LabelUse, error)
	// Tickets are the project's tasks, archived ones included.
	Tickets(ctx context.Context, projectID string) ([]domain.Task, error)
	Ticket(ctx context.Context, projectID, ticketID string) (*domain.Task, error)
	CreateTicket(ctx context.Context, in service.NewTask) (*domain.Task, error)
	UpdateTicket(ctx context.Context, ticketID string, patch service.TaskPatch) (*domain.Task, error)
	Runs(ctx context.Context, projectID string, limit int) ([]domain.Run, error)
	TicketRuns(ctx context.Context, ticketID string) ([]domain.Run, error)
	Run(ctx context.Context, projectID, runID string) (*domain.Run, error)
	Overview(ctx context.Context) (*service.Overview, error)
	PendingQuestions(ctx context.Context, projectID string) ([]domain.Question, error)
	Question(ctx context.Context, projectID, questionID string) (*domain.Question, error)
	// AnswerQuestion records the answer and hands it to the agent through the runner the run is on, the same way
	// answering in the app does.
	AnswerQuestion(ctx context.Context, questionID, answer string) (*domain.Question, error)
}

// Services is the set of domain services a Backend is made of.
type Services struct {
	Projects *service.Projects
	Tasks    *service.Tasks
	Labels   *service.Labels
	Runs     *service.Runs
	Control  *service.ControlCenter
	// Answer delivers an answer to the agent that asked: the runner manager's Answer, which is also what the app calls.
	Answer func(ctx context.Context, questionID, answer string) (*domain.Question, error)
}

// NewBackend is the Backend over the controller's domain services.
func NewBackend(s Services) Backend { return servicesBackend{s} }

type servicesBackend struct{ s Services }

func (b servicesBackend) Projects(ctx context.Context) ([]service.ProjectDetail, error) {
	return b.s.Projects.List(ctx)
}

func (b servicesBackend) Project(ctx context.Context, id string) (*service.ProjectDetail, error) {
	return b.s.Projects.Get(ctx, id)
}

func (b servicesBackend) Labels(ctx context.Context) ([]domain.LabelUse, error) {
	return b.s.Labels.List(ctx)
}

func (b servicesBackend) Tickets(ctx context.Context, projectID string) ([]domain.Task, error) {
	return b.s.Tasks.List(ctx, projectID)
}

func (b servicesBackend) Ticket(ctx context.Context, projectID, ticketID string) (*domain.Task, error) {
	return b.s.Tasks.GetIn(ctx, projectID, ticketID)
}

func (b servicesBackend) CreateTicket(ctx context.Context, in service.NewTask) (*domain.Task, error) {
	return b.s.Tasks.CreateTask(ctx, in)
}

func (b servicesBackend) UpdateTicket(ctx context.Context, id string, patch service.TaskPatch) (*domain.Task, error) {
	return b.s.Tasks.Update(ctx, id, patch)
}

func (b servicesBackend) Runs(ctx context.Context, projectID string, limit int) ([]domain.Run, error) {
	return b.s.Runs.History(ctx, projectID, limit)
}

func (b servicesBackend) TicketRuns(ctx context.Context, ticketID string) ([]domain.Run, error) {
	return b.s.Runs.ListByTask(ctx, ticketID)
}

func (b servicesBackend) Run(ctx context.Context, projectID, runID string) (*domain.Run, error) {
	return b.s.Runs.GetIn(ctx, projectID, runID)
}

func (b servicesBackend) Overview(ctx context.Context) (*service.Overview, error) {
	return b.s.Control.Overview(ctx)
}

func (b servicesBackend) PendingQuestions(ctx context.Context, projectID string) ([]domain.Question, error) {
	if projectID == "" {
		return b.s.Runs.ListPendingQuestions(ctx)
	}
	return b.s.Runs.ListPendingQuestionsIn(ctx, projectID)
}

func (b servicesBackend) Question(ctx context.Context, projectID, questionID string) (*domain.Question, error) {
	return b.s.Runs.GetQuestionIn(ctx, projectID, questionID)
}

func (b servicesBackend) AnswerQuestion(ctx context.Context, questionID, answer string) (*domain.Question, error) {
	if b.s.Answer == nil {
		return nil, errorf(CodeUnavailable, "agent execution is not enabled, so a question cannot be answered")
	}
	return b.s.Answer(ctx, questionID, answer)
}
