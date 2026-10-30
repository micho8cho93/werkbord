package domain

import "time"

// Initiative references existing work; it never owns tickets or runner assignments.
type Initiative struct {
	Decisions    string                 `json:"decisions"`
	Questions    string                 `json:"questions"`
	ID           string                 `json:"id"`
	Title        string                 `json:"title"`
	Objective    string                 `json:"objective"`
	Status       string                 `json:"status"`
	OwnerID      string                 `json:"ownerId"`
	ProjectIDs   []string               `json:"projectIds"`
	Tickets      []InitiativeTicket     `json:"tickets"`
	Dependencies []InitiativeDependency `json:"dependencies"`
	Memories     []InitiativeMemory     `json:"memories"`
	Version      int64                  `json:"version"`
	CreatedAt    time.Time              `json:"createdAt"`
	UpdatedAt    time.Time              `json:"updatedAt"`
	History      []InitiativeEvent      `json:"history"`
}
type InitiativeTicket struct {
	ProjectID string `json:"projectId"`
	TicketID  string `json:"ticketId"`
}
type InitiativeDependency struct {
	Before InitiativeTicket `json:"before"`
	After  InitiativeTicket `json:"after"`
}

// Decisions and repository relationships are linked through their approved memory records.
type InitiativeMemory struct {
	ProjectID string `json:"projectId"`
	MemoryID  string `json:"memoryId"`
}
type InitiativeEvent struct {
	ActorID string    `json:"actorId"`
	Kind    string    `json:"kind"`
	At      time.Time `json:"at"`
}
