package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"werkbord/internal/domain"
	"werkbord/internal/planning"
	"werkbord/internal/store"
)

// Only registered deck tasks with draft output can materialise findings. The
// agent supplies content, never project IDs, execution settings or task states.
// Source references deduplicate a stable finding across turns, runs and retries.
func (s *Runs) collectDeckDrafts(ctx context.Context, run *domain.Run) {
	var slot DeckSlot
	if err := s.Store.View(ctx, func(tx store.Tx) error { return tx.Settings().Get(ctx, "deck-task."+run.TaskID, &slot) }); err != nil {
		return
	}
	if slot.Output != "drafts" && !(slot.Output == "" && slot.Button == "find-bugs") {
		return
	}
	events, err := s.Events(ctx, run.ID, 0, 1000)
	if err != nil {
		return
	}
	var text strings.Builder
	for _, ev := range events {
		if ev.Type != domain.EventAgentOutput {
			continue
		}
		var output domain.AgentOutput
		if json.Unmarshal(ev.Payload, &output) != nil || output.Stream != domain.StreamAssistant {
			continue
		}
		if text.Len()+len(output.Text) > 2<<20 {
			break
		}
		text.WriteString(output.Text)
	}
	tasks := &Tasks{Deps: s.Deps}
	count := 0
	for _, line := range strings.Split(text.String(), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "WERKBORD_DRAFT ") {
			continue
		}
		var finding struct {
			Key         string   `json:"key"`
			Title       string   `json:"title"`
			Description string   `json:"description"`
			References  []string `json:"references"`
		}
		if json.Unmarshal([]byte(strings.TrimPrefix(line, "WERKBORD_DRAFT ")), &finding) != nil || !deckID.MatchString(finding.Key) || len(finding.References) == 0 || len(finding.References) > 20 || strings.TrimSpace(finding.Description) == "" {
			continue
		}
		valid := true
		for _, ref := range finding.References {
			if len(ref) > 500 || !strings.Contains(ref, ":") || strings.ContainsAny(ref, "\r\n") || strings.HasPrefix(ref, "/") || strings.Contains(ref, "..") {
				valid = false
			}
		}
		if !valid {
			continue
		}
		count++
		if count > 50 {
			break
		}
		sum := sha256.Sum256([]byte(run.ProjectID + ":" + slot.Button + ":" + finding.Key))
		task, err := tasks.CreateTask(ctx, NewTask{ProjectID: run.ProjectID, Title: finding.Title, Description: "Draft finding — review before scheduling.\n\n" + finding.Description + "\n\nReferences:\n" + strings.Join(finding.References, "\n") + "\n\nWorkflow task: " + run.TaskID, SourceRef: "deck-finding:" + hex.EncodeToString(sum[:]), WorkMode: planning.ModeHuman})
		if err != nil {
			s.log().Warn("cannot create workflow draft", "run", run.ID, "err", err)
			continue
		}
		// Link even a deduplicated finding to this execution, so its report opens
		// the existing task rather than losing the next action on a repeated scan.
		if err := s.Store.Update(ctx, func(tx store.Tx) error {
			key := "deck-findings." + run.TaskID
			var ids []string
			if err := tx.Settings().Get(ctx, key, &ids); err != nil && !errors.Is(err, domain.ErrNotFound) {
				return err
			}
			if slices.Contains(ids, task.ID) || len(ids) >= 50 {
				return nil
			}
			return tx.Settings().Set(ctx, key, append(ids, task.ID), s.now())
		}); err != nil {
			s.log().Warn("cannot link workflow draft", "run", run.ID, "err", err)
		}
	}
}
