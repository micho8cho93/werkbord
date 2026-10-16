package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"

	"devboard/internal/domain"
	"devboard/internal/store"
)

// Deck preferences belong to a controller owner or one enrolled phone. They never
// grant access to settings, runners, repositories or another product.
type DeckSlot struct {
	ID        string `json:"id"`
	Button    string `json:"button"`
	ProjectID string `json:"projectId,omitempty"`
	Agent     string `json:"agent,omitempty"`
	Model     string `json:"model,omitempty"`
}
type DeckLayout struct {
	Version int64        `json:"version"`
	Slots   [9]*DeckSlot `json:"slots"`
}
type DeckExecution struct {
	RequestID string `json:"requestId"`
	SlotID    string `json:"slotId"`
	ProjectID string `json:"projectId"`
	TaskID    string `json:"taskId"`
}
type Workflow struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Prompt      string `json:"-"`
}

// These are task briefs, never commands, runtime flags or Assistant tools. The
// existing agent permissions and repository review flow remain authoritative.
var DeckWorkflows = []Workflow{
	{"find-bugs", "Find Bugs", "Inspect the repository and report reproducible bugs without editing files.", "Inspect this repository for bugs. Report actionable findings with file locations, impact, reproduction steps and suggested fixes. Prioritise correctness over style. Do not modify files."},
	{"improve-tests", "Improve Tests", "Add focused tests for gaps in existing behaviour.", "Inspect the existing tests and add focused regression tests for important uncovered behaviour. Follow the repository's conventions. Run relevant tests and report results and limitations. Avoid unrelated production changes."},
	{"security-audit", "Security Audit", "Inspect security risks and report findings without editing files.", "Audit this repository for security vulnerabilities, trust-boundary violations, unsafe input handling and credential exposure. Report evidence, severity and suggested remediation. Never print secrets. Do not modify files."},
	{"dependency-check", "Dependency Check", "Inspect dependency risks and report recommendations without updating packages.", "Inspect dependency manifests and lockfiles for outdated, vulnerable or unnecessary dependencies. Use the project's existing tooling where available. Report evidence, compatibility implications and recommended updates. Clearly distinguish verified findings from unknowns. Do not change files or install updates."},
	{"refactor-codebase", "Refactor Codebase", "Make a small, behaviour-preserving refactor and run relevant tests.", "Identify and implement one small, valuable refactor that improves clarity or removes duplication. Preserve behaviour and public interfaces. Follow repository instructions, avoid broad rewrites and run relevant tests. Explain the change and its validation."},
	{"sprint-summary", "Sprint Summary", "Summarise repository activity from the last two weeks without editing files.", "Summarise this repository's Git activity over the last 14 days: delivered changes, fixes, tests, risks and follow-up work supported by the evidence. Cite commits or files. State the time range and limitations. Do not infer task completion from commits alone. Do not modify files."},
}

var deckID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,80}$`)

func DeckOwnerKey(owner string) string {
	sum := sha256.Sum256([]byte(owner))
	return "phone-deck." + hex.EncodeToString(sum[:])
}
func DefaultDeck() DeckLayout {
	var out DeckLayout
	for i, id := range []string{"needs", "calendar", "projects", "mywork", "agents", "analytics"} {
		out.Slots[i] = &DeckSlot{ID: id, Button: id}
	}
	return out
}
func deckWorkflow(id string) (Workflow, bool) {
	for _, w := range DeckWorkflows {
		if w.ID == id {
			return w, true
		}
	}
	return Workflow{}, false
}
func navigationButton(id string) bool {
	switch id {
	case "needs", "calendar", "projects", "mywork", "agents", "analytics", "reviews", "git", "runners":
		return true
	}
	return false
}

func (s *Settings) Deck(ctx context.Context, owner string) (DeckLayout, error) {
	out := DefaultDeck()
	err := s.Store.View(ctx, func(tx store.Tx) error {
		err := tx.Settings().Get(ctx, DeckOwnerKey(owner), &out)
		if errors.Is(err, domain.ErrNotFound) {
			return nil
		}
		return err
	})
	return out, err
}
func (s *Settings) SetDeck(ctx context.Context, owner string, in DeckLayout) (DeckLayout, error) {
	seen := map[string]bool{}
	for _, slot := range in.Slots {
		if slot == nil {
			continue
		}
		if !deckID.MatchString(slot.ID) || seen[slot.ID] {
			return in, invalid("each deck button needs a unique identifier")
		}
		seen[slot.ID] = true
		if navigationButton(slot.Button) {
			if slot.ProjectID != "" || slot.Agent != "" || slot.Model != "" {
				return in, invalid("navigation buttons have no execution settings")
			}
		} else if _, ok := deckWorkflow(slot.Button); !ok {
			return in, invalid("unknown deck button")
		}
		if slot.ProjectID == "" && (slot.Agent != "" || slot.Model != "") {
			return in, invalid("choose a repository first")
		}
		if err := validateExecution(ctx, s.Catalog, domain.ExecutionConfig{Agent: slot.Agent, Model: slot.Model}); err != nil {
			return in, err
		}
	}
	err := s.update(ctx, func(tx store.Tx, em *emitter) error {
		current := DefaultDeck()
		err := tx.Settings().Get(ctx, DeckOwnerKey(owner), &current)
		if err != nil && !errors.Is(err, domain.ErrNotFound) {
			return err
		}
		if current.Version != in.Version {
			return fmt.Errorf("%w: this deck changed elsewhere; reload it before saving", domain.ErrConflict)
		}
		for _, slot := range in.Slots {
			if slot != nil && slot.ProjectID != "" {
				p, err := tx.Projects().Get(ctx, slot.ProjectID)
				if err != nil {
					return err
				}
				if !p.HasRepository() {
					return invalid("workflow targets must be registered Git repositories")
				}
			}
		}
		in.Version++
		return tx.Settings().Set(ctx, DeckOwnerKey(owner), in, s.now())
	})
	return in, err
}

func (s *Settings) DeckHistory(ctx context.Context, owner string) ([]DeckExecution, error) {
	out := []DeckExecution{}
	err := s.Store.View(ctx, func(tx store.Tx) error {
		err := tx.Settings().Get(ctx, DeckOwnerKey(owner)+".history", &out)
		if errors.Is(err, domain.ErrNotFound) {
			return nil
		}
		return err
	})
	return out, err
}

// PrepareDeckTask uses the existing source-reference deduplication to survive a
// lost response or restart without creating a second task. A request ID is scoped
// to its authenticated owner; the client cannot select another phone's owner key.
func (s *Settings) PrepareDeckTask(ctx context.Context, tasks *Tasks, owner, slotID, requestID string) (*domain.Task, error) {
	if !deckID.MatchString(slotID) || len(requestID) < 16 || !deckID.MatchString(requestID) {
		return nil, invalid("invalid workflow request identifier")
	}

	var slot DeckSlot
	err := s.update(ctx, func(tx store.Tx, em *emitter) error {
		key := DeckOwnerKey(owner) + ".request." + requestID
		err := tx.Settings().Get(ctx, key, &slot)
		if err == nil {
			return nil
		}
		if !errors.Is(err, domain.ErrNotFound) {
			return err
		}
		layout := DefaultDeck()
		err = tx.Settings().Get(ctx, DeckOwnerKey(owner), &layout)
		if err != nil && !errors.Is(err, domain.ErrNotFound) {
			return err
		}
		for _, candidate := range layout.Slots {
			if candidate != nil && candidate.ID == slotID {
				if _, ok := deckWorkflow(candidate.Button); !ok || candidate.ProjectID == "" {
					return invalid("configure a workflow and its repository first")
				}
				slot = *candidate
				return tx.Settings().Set(ctx, key, slot, s.now())
			}
		}
		return invalid("that workflow is no longer on this deck")
	})
	if err != nil {
		return nil, err
	}
	if slot.ID != slotID {
		return nil, fmt.Errorf("%w: this request belongs to another button", domain.ErrConflict)
	}
	workflow, ok := deckWorkflow(slot.Button)
	if !ok || slot.ProjectID == "" {
		return nil, invalid("configure a workflow and its repository first")
	}
	exec := domain.ExecutionConfig{Agent: slot.Agent, Model: slot.Model}
	task, err := tasks.CreateTask(ctx, NewTask{ProjectID: slot.ProjectID, Title: workflow.Name, Description: workflow.Prompt, Execution: exec, SourceRef: DeckOwnerKey(owner) + ":" + requestID})
	if err != nil {
		return nil, err
	}
	if task.Title != workflow.Name || task.Description != workflow.Prompt || task.Execution != exec {
		return nil, fmt.Errorf("%w: this request already belongs to a different workflow configuration; open its task", domain.ErrConflict)
	}
	err = s.update(ctx, func(tx store.Tx, em *emitter) error {
		history := []DeckExecution{}
		e := tx.Settings().Get(ctx, DeckOwnerKey(owner)+".history", &history)
		if e != nil && !errors.Is(e, domain.ErrNotFound) {
			return e
		}
		for _, h := range history {
			if h.RequestID == requestID {
				if h.TaskID != task.ID {
					return fmt.Errorf("%w: request already used for another repository", domain.ErrConflict)
				}
				return nil
			}
		}
		history = append([]DeckExecution{{RequestID: requestID, SlotID: slotID, ProjectID: task.ProjectID, TaskID: task.ID}}, history...)
		if len(history) > 32 {
			history = history[:32]
		}
		return tx.Settings().Set(ctx, DeckOwnerKey(owner)+".history", history, s.now())
	})
	return task, err
}
