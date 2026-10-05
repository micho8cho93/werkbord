package agent

import (
	"encoding/json"
	"strings"

	"devboard/internal/domain"
)

// This file is where an execution policy becomes agent behaviour. It is the one
// place that knows what each policy means, so that the adapters (which translate
// it into their agent's own channel) and the runner (which enforces it) agree,
// and no other code appends policy text to anything.
//
// Two mechanisms, deliberately:
//
//   - Instructions tell the agent how to behave. They are a request.
//   - HandleQuestion and ParseBlocker are what the controller does when the agent
//     behaves otherwise, or when it reports that it cannot go on. They are the
//     authority.
//
// Neither widens what an agent may do. A policy only changes who answers
// conversational questions; permission requests are the user's under every policy.

// BlockerMarker begins the line in which an agent reports that it is blocked.
// Instructions tell it to end its turn with one; ParseBlocker reads it back.
const BlockerMarker = "DEVBOARD_BLOCKED"

// Instructions is the text that is added to an agent's own instructions for a
// run with policy p. It is empty for an interactive run, which is how agents
// behave without Werkbord's help.
func Instructions(p domain.ExecutionPolicy) string {
	switch p.Normalized().Interaction {
	case domain.InteractionAutonomous:
		return autonomousInstructions
	case domain.InteractionAutonomousStopIfBlocked:
		return autonomousInstructions + "\n\n" + stopIfBlockedInstructions
	}
	return ""
}

const autonomousInstructions = `Werkbord execution policy: AUTONOMOUS.
The user has asked not to be interrupted with routine questions during this task.
- Investigate the repository yourself before deciding that something is unknown: read the code, the tests, the documentation and the Git history.
- Make reasonable implementation decisions yourself, and carry on until you consider the task complete. Do not ask the user to pick between ordinary implementation options or to confirm your plan.
- When a decision matters, note the assumption you made in a sentence, so the user can review it afterwards.
- This policy does not widen what you may do. Stay within your sandbox and permissions, make no destructive Git or filesystem changes (force pushes, hard resets, deleting branches or files the task does not call for), and when a permission prompt appears it is for the user to answer, as usual.
- When you are done, end your turn with a short summary of what you did and what you assumed.`

const stopIfBlockedInstructions = `Werkbord execution policy: STOP IF BLOCKED.
Work independently whenever a reasonable decision can be made. If you reach a decision you cannot safely infer (information only the user has, such as credentials or product intent; a choice where a wrong guess is costly or hard to undo), do not guess and do not ask the user a question. Stop instead, and end your turn with a blocker report as the last line of your message, in exactly this form (one line of JSON):
` + BlockerMarker + ` {"summary": "<what is blocking you, in one sentence>", "detail": "<what you found, what you tried, and what you would do with each answer>", "options": ["<a choice you would offer>", "..."]}
"options" is optional. Report a blocker only for a real decision; ordinary choices you make yourself.`

// Reply is what the controller says to a question that its run's policy does not
// put to the user. It is delivered through the session's normal question channel,
// so it reaches the agent as the user's answer would.
const (
	autonomousReply = "The user is not available to answer questions during this run: it is set to work autonomously. " +
		"Decide this yourself. Check the repository first if the answer could be found there; otherwise choose the most reasonable " +
		"and least destructive option, note your assumption in a sentence, and continue until the task is complete. " +
		"This does not authorise anything your permissions do not already allow."
	stopReply = "This run is set to stop if blocked, so this question was not put to the user. " +
		"Do not guess at it and do not carry on past it. End your turn now with a short summary of what you have done so far and " +
		"what needs deciding. Werkbord has recorded the blocker; the user will settle it."
)

// Action is what the controller does with a question from an agent.
type Action int

const (
	// ActionAsk puts the question to the user: the run waits for an answer.
	ActionAsk Action = iota
	// ActionReply answers the agent on the user's behalf with Handling.Reply, and
	// the run carries on.
	ActionReply
	// ActionBlock stops the run as blocked, records Handling.Blocker, and gives the
	// agent Handling.Reply so that it ends its turn.
	ActionBlock
)

// MaxAutoReplies is how many questions the controller answers for the user in
// one run before it stops doing so and asks them instead. An agent that keeps
// asking after being told to decide is not complying, and answering forever
// would hide a loop from the person paying for it.
const MaxAutoReplies = 8

// Handling is the decision HandleQuestion makes.
type Handling struct {
	Action  Action
	Reply   string          // for ActionReply and ActionBlock
	Blocker *domain.Blocker // for ActionBlock; RaisedAt is set by the service
}

// HandleQuestion decides what the controller does with a question from an agent
// whose run has policy p. autoReplies is how many it has already answered itself
// in this run.
//
// An approval is never answered or swallowed, whatever the policy: it is a
// request for permission, and whether to grant it is the user's decision.
// Autonomous means "do not interrupt me with routine questions", not "I have
// pre-approved whatever you ask for". Everything else follows the policy.
func HandleQuestion(p domain.ExecutionPolicy, q Question, autoReplies int) Handling {
	if q.Kind == domain.QuestionApproval {
		return Handling{Action: ActionAsk}
	}
	switch p.Normalized().Interaction {
	case domain.InteractionAutonomous:
		if autoReplies >= MaxAutoReplies {
			return Handling{Action: ActionAsk}
		}
		return Handling{Action: ActionReply, Reply: autonomousReply}
	case domain.InteractionAutonomousStopIfBlocked:
		return Handling{Action: ActionBlock, Reply: stopReply, Blocker: &domain.Blocker{
			Summary: blockerSummary(q),
			Detail:  strings.TrimSpace(q.Context),
			Options: append([]string(nil), q.Options...),
			Source:  domain.BlockerQuestion,
			Kind:    q.Kind,
		}}
	}
	return Handling{Action: ActionAsk}
}

// StopReply is the reply given to a further question from an agent that is
// already blocked: the question is not asked, and the agent is told again to stop.
func StopReply() string { return stopReply }

func blockerSummary(q Question) string {
	s := strings.Join(strings.Fields(q.Prompt), " ")
	if s == "" {
		s = "The agent needs a decision it could not make itself."
	}
	return s
}

// ParseBlocker finds a blocker report in what an agent said at the end of a
// turn: the last line that begins with BlockerMarker, followed by a JSON object.
// A line that has the marker but not valid JSON still counts, with the rest of
// the line as its summary, because the agent clearly meant to stop. It reports
// false if there is no such line.
func ParseBlocker(text string) (domain.Blocker, bool) {
	lines := strings.Split(text, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		rest, ok := strings.CutPrefix(line, BlockerMarker)
		if !ok {
			continue
		}
		rest = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(rest), ":"))
		var in struct {
			Summary string   `json:"summary"`
			Detail  string   `json:"detail"`
			Options []string `json:"options"`
		}
		b := domain.Blocker{Source: domain.BlockerReport}
		if err := json.Unmarshal([]byte(rest), &in); err == nil && strings.TrimSpace(in.Summary) != "" {
			b.Summary, b.Detail, b.Options = strings.TrimSpace(in.Summary), strings.TrimSpace(in.Detail), in.Options
			return b, true
		}
		b.Summary = rest
		if b.Summary == "" {
			b.Summary = "The agent reported that it is blocked."
		}
		return b, true
	}
	return domain.Blocker{}, false
}
