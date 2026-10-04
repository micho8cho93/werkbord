package domain

import "time"

// QuestionStatus tracks whether an agent's question still needs the user.
type QuestionStatus string

const (
	QuestionPending   QuestionStatus = "pending"
	QuestionAnswered  QuestionStatus = "answered"
	QuestionCancelled QuestionStatus = "cancelled" // the run ended before an answer arrived
)

// Question is something an agent asked the user during a run. While a
// question is pending its run is normally in RunWaitingForUser.
type Question struct {
	ID         string         `json:"id"`
	RunID      string         `json:"runId"`
	Prompt     string         `json:"prompt"`
	Options    []string       `json:"options,omitempty"` // suggested one-tap answers
	Status     QuestionStatus `json:"status"`
	Answer     string         `json:"answer,omitempty"`
	CreatedAt  time.Time      `json:"createdAt"`
	AnsweredAt *time.Time     `json:"answeredAt,omitempty"`
}
