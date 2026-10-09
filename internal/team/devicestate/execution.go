package devicestate

import "time"

// ExecutionRef is only an index to an approval stored/enforced by Individual.
// It carries no permission and changing it cannot authorize execution.
type ExecutionRef struct {
	ExecutionID   string    `json:"executionId"`
	TeamProjectID string    `json:"teamProjectId"`
	TicketID      string    `json:"ticketId"`
	Fence         string    `json:"fence"`
	Context       string    `json:"context"`
	Title         string    `json:"title"`
	ExpiresAt     time.Time `json:"expiresAt"`
}

func (s *State) NoteExecution(ref ExecutionRef) error {
	return s.update(func(d *filedata) error {
		out := []ExecutionRef{}
		for _, v := range d.Executions {
			if v.ExecutionID != ref.ExecutionID && s.now().Before(v.ExpiresAt) {
				out = append(out, v)
			}
		}
		if len(out) >= 200 {
			out = out[len(out)-199:]
		}
		d.Executions = append(out, ref)
		return nil
	})
}
func (s *State) Executions() []ExecutionRef {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]ExecutionRef(nil), s.d.Executions...)
}
