package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"devboard/internal/planning"
)

// Label is a name and a colour the workspace chose and shares: "Design", "Q4 launch", "Waiting on legal". Any
// ticket in any of the workspace's projects may carry any number of them. Nothing about labels is built in, and
// they are not a ticket's status, nor who does the work (Ticket.WorkMode), so renaming or deleting one changes
// how work is described and never how it moves.
//
// Defining labels is the workspace's to decide (PermLabelsManage); using one is a ticket edit.
type Label struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspaceId"`
	Name        string    `json:"name"`
	Color       string    `json:"color"`
	Description string    `json:"description,omitempty"`
	Version     int64     `json:"version"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// LabelUse is a label with how many open tickets carry it in the projects the viewer may see.
type LabelUse struct {
	Label
	Tickets int `json:"tickets"`
}

// FromPlanning turns an error of the shared planning package into this domain's invalid-input error, so that a
// caller matches it with errors.Is(err, ErrInvalid) like any other.
func FromPlanning(err error) error {
	if err == nil || !errors.Is(err, planning.ErrInvalid) {
		return err
	}
	return fmt.Errorf("%w: %s", ErrInvalid, strings.TrimPrefix(err.Error(), "invalid: "))
}

// CleanLabel validates the parts of a label a person types, returning them normalised.
func CleanLabel(name, color, description string) (n, c, d string, err error) {
	if n, err = planning.CleanLabelName(name); err != nil {
		return "", "", "", FromPlanning(err)
	}
	if c, err = planning.CleanLabelColor(color); err != nil {
		return "", "", "", FromPlanning(err)
	}
	if d, err = planning.CleanLabelDescription(description); err != nil {
		return "", "", "", FromPlanning(err)
	}
	return n, c, d, nil
}
