package envelope

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
)

// Action is what an envelope asks for, by meaning. There is no action that carries
// a command, a script, a path or an environment: every request is one of this
// closed list, each with a payload of fixed, typed fields that name things by ID,
// and the device that receives it decides under its own policy whether to do it.
// A new capability is a new Action added here, reviewed as such, and not a field
// on an old one.
type Action string

const (
	// OpenTicketOnRunner: show a ticket the sender holds in the target runner's
	// own Werkbord, as a task to start there (what "Open in my runner" does).
	ActionOpenTicketOnRunner Action = "open_ticket_on_runner"
	// StartApprovedRun: start a run the target runner's owner has already approved.
	ActionStartApprovedRun Action = "start_approved_run"
	// CancelRun: stop a run on the target runner.
	ActionCancelRun Action = "cancel_run"
	// RespondToAgentQuestion: answer a question an agent on the target runner asked.
	ActionRespondToAgentQuestion Action = "respond_to_agent_question"
	// FetchRunnerStatus: ask the target runner what it is doing.
	ActionFetchRunnerStatus Action = "fetch_runner_status"
)

// spec describes one action.
type spec struct {
	// NeedsTarget: the action is for one device, which the envelope must name.
	NeedsTarget bool
	// Payload is a zero value of the payload's type.
	Payload any
}

var specs = map[Action]spec{
	ActionOpenTicketOnRunner:     {NeedsTarget: true, Payload: OpenTicketOnRunner{}},
	ActionStartApprovedRun:       {NeedsTarget: true, Payload: StartApprovedRun{}},
	ActionCancelRun:              {NeedsTarget: true, Payload: CancelRun{}},
	ActionRespondToAgentQuestion: {NeedsTarget: true, Payload: RespondToAgentQuestion{}},
	ActionFetchRunnerStatus:      {NeedsTarget: true, Payload: FetchRunnerStatus{}},
}

// Actions lists every action, in a stable order.
func Actions() []Action {
	return []Action{ActionOpenTicketOnRunner, ActionStartApprovedRun, ActionCancelRun, ActionRespondToAgentQuestion, ActionFetchRunnerStatus}
}

// Known reports whether a is an action that exists.
func Known(a Action) bool { _, ok := specs[a]; return ok }

// NeedsTarget reports whether a must name the device it is for.
func (a Action) NeedsTarget() bool { return specs[a].NeedsTarget }

// PayloadFor returns a pointer to a new zero payload of the type a defines.
func PayloadFor(a Action) (any, bool) {
	s, ok := specs[a]
	if !ok {
		return nil, false
	}
	return reflect.New(reflect.TypeOf(s.Payload)).Interface(), true
}

// The payloads. Each is a closed, typed record that names things by ID. None has a
// free-form field that could be run (a command, a script, an argument list, a
// path, an environment): TestNoPayloadCarriesAnythingExecutable keeps it so.

// OpenTicketOnRunner names a ticket to open in the target's Werkbord.
type OpenTicketOnRunner struct {
	ProjectID string `json:"projectId"`
	TicketID  string `json:"ticketId"`
}

// StartApprovedRun names a task and the approval that allows its run. The target
// checks the approval is its own owner's; the sender cannot supply one.
type StartApprovedRun struct {
	TaskID     string `json:"taskId"`
	ApprovalID string `json:"approvalId"`
}

// CancelRun names a run.
type CancelRun struct {
	RunID string `json:"runId"`
}

// RespondToAgentQuestion answers one question a run asked, by choosing among the
// options the question offered, or with a short reply in words for the agent's
// person to read. It is an answer to a question that was asked, never an
// instruction to the machine.
type RespondToAgentQuestion struct {
	RunID      string `json:"runId"`
	QuestionID string `json:"questionId"`
	// OptionID is the option chosen, when the question offered options.
	OptionID string `json:"optionId,omitempty"`
	// Reply is a short answer in words, when it did not.
	Reply string `json:"reply,omitempty"`
}

// MaxReply is the longest reply, in bytes.
const MaxReply = 2000

// FetchRunnerStatus asks for the runner's status. It has no arguments.
type FetchRunnerStatus struct{}

// EncodePayload checks that payload is the type a defines and returns its JSON.
func EncodePayload(a Action, payload any) ([]byte, error) {
	s, ok := specs[a]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrAction, a)
	}
	v := reflect.ValueOf(payload)
	for v.IsValid() && v.Kind() == reflect.Pointer && !v.IsNil() {
		v = v.Elem()
	}
	if payload == nil || !v.IsValid() || v.Type() != reflect.TypeOf(s.Payload) {
		return nil, fmt.Errorf("%w: %s takes a %T", ErrPayload, a, s.Payload)
	}
	if err := checkPayload(a, v.Interface()); err != nil {
		return nil, err
	}
	return json.Marshal(v.Interface())
}

// DecodePayload reads an envelope's payload into the type its action defines. It
// is strict: an unknown field or trailing data is an error. Call it only on an
// envelope that has been verified.
func DecodePayload(e Envelope) (any, error) {
	p, ok := PayloadFor(e.Action)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrAction, e.Action)
	}
	dec := json.NewDecoder(bytes.NewReader(e.Payload))
	dec.DisallowUnknownFields()
	if err := dec.Decode(p); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPayload, err)
	}
	if dec.Decode(new(any)) != io.EOF {
		return nil, fmt.Errorf("%w: trailing data", ErrPayload)
	}
	v := reflect.ValueOf(p).Elem().Interface()
	if err := checkPayload(e.Action, v); err != nil {
		return nil, err
	}
	return v, nil
}

const maxID = 128

func checkPayload(a Action, v any) error {
	id := func(name, s string) error {
		if s == "" || len(s) > maxID {
			return fmt.Errorf("%w: %s is required and at most %d bytes", ErrPayload, name, maxID)
		}
		for _, r := range s {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
				return fmt.Errorf("%w: %s is not an opaque identifier", ErrPayload, name)
			}
		}
		return nil
	}
	switch p := v.(type) {
	case OpenTicketOnRunner:
		if err := id("projectId", p.ProjectID); err != nil {
			return err
		}
		return id("ticketId", p.TicketID)
	case StartApprovedRun:
		if err := id("taskId", p.TaskID); err != nil {
			return err
		}
		return id("approvalId", p.ApprovalID)
	case CancelRun:
		return id("runId", p.RunID)
	case RespondToAgentQuestion:
		if err := id("runId", p.RunID); err != nil {
			return err
		}
		if err := id("questionId", p.QuestionID); err != nil {
			return err
		}
		if (p.OptionID == "") == (p.Reply == "") {
			return fmt.Errorf("%w: answer with an option or a reply, not both and not neither", ErrPayload)
		}
		if len(p.OptionID) > maxID || len(p.Reply) > MaxReply {
			return fmt.Errorf("%w: the answer is too long", ErrPayload)
		}
		if p.OptionID != "" {
			return id("optionId", p.OptionID)
		}
	case FetchRunnerStatus:
	default:
		return fmt.Errorf("%w: %s", ErrPayload, a)
	}
	return nil
}
