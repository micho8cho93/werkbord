package api

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"devboard/internal/domain"
	"devboard/internal/service"
)

func savePhoneDeck(t *testing.T, p *phoned, token string) service.DeckLayout {
	t.Helper()
	layout := service.DefaultDeck()
	layout.Slots[6] = &service.DeckSlot{ID: "workflow-one", Button: "find-bugs", ProjectID: p.project.ID, Agent: "fake", Model: "test-model"}
	raw, _ := json.Marshal(layout)
	code, b := call(t, "PUT", p.url+"/api/deck", token, string(raw))
	if code != 200 {
		t.Fatalf("save: %d %s", code, b)
	}
	if err := json.Unmarshal(b, &layout); err != nil {
		t.Fatal(err)
	}
	return layout
}
func TestPhoneDeckPreferencesAreScopedAndValidated(t *testing.T) {
	p := newPhoned(t)
	a, _ := p.enrolled(t, "A")
	b, _ := p.enrolled(t, "B")
	layout := savePhoneDeck(t, p, a)
	for _, token := range []string{b, ownerToken} {
		code, raw := call(t, "GET", p.url+"/api/deck", token, "")
		var other service.DeckLayout
		_ = json.Unmarshal(raw, &other)
		if code != 200 || other.Version != 0 || other.Slots[6] != nil {
			t.Fatalf("layout leaked: %d %s", code, raw)
		}
	}
	raw, _ := json.Marshal(layout)
	if code, _ := call(t, "PUT", p.url+"/api/deck", a, strings.Replace(string(raw), `"version":1`, `"version":0`, 1)); code != 409 {
		t.Fatalf("stale save: %d", code)
	}
	for _, body := range []string{`{"version":1,"slots":[]}`, `{"version":1,"slots":[null,null,null,null,null,null,null,null,null,null]}`, `{"version":1,"slots":[{"id":"x","button":"find-bugs","policy":{"interaction":"autonomous"}},null,null,null,null,null,null,null,null]}`} {
		if code, _ := call(t, "PUT", p.url+"/api/deck", a, body); code < 400 {
			t.Fatalf("accepted %s", body)
		}
	}
	if code, _ := call(t, "POST", p.url+"/api/deck/workflows", a, `{"slotId":"workflow-one","requestId":"1234567890123456","agentId":"fake"}`); code != 403 {
		t.Fatalf("arbitrary execution override: %d", code)
	}
}
func TestPhoneWorkflowUsesNormalRunAndDeduplicatesConcurrentAndCompletedRetries(t *testing.T) {
	p := newPhoned(t)
	token, _ := p.enrolled(t, "A")
	layout := savePhoneDeck(t, p, token)
	body := `{"slotId":"workflow-one","requestId":"1234567890123456"}`
	var wg sync.WaitGroup
	results := make(chan []byte, 2)
	codes := make(chan int, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			code, raw := call(t, "POST", p.url+"/api/deck/workflows", token, body)
			codes <- code
			results <- raw
		}()
	}
	wg.Wait()
	close(results)
	close(codes)
	for code := range codes {
		if code != 200 {
			t.Fatalf("start: %d", code)
		}
	}
	var first struct {
		Task domain.Task
		Run  domain.Run
	}
	for raw := range results {
		var result struct {
			Task domain.Task
			Run  domain.Run
		}
		if err := json.Unmarshal(raw, &result); err != nil {
			t.Fatal(err)
		}
		if first.Task.ID != "" && first.Task.ID != result.Task.ID {
			t.Fatal("duplicate task")
		}
		if first.Run.ID != "" && first.Run.ID != result.Run.ID {
			t.Fatal("duplicate run")
		}
		first = result
	}
	if first.Run.AgentID != "fake" || first.Run.Model != "test-model" || first.Run.WorktreeID == "" {
		t.Fatalf("wrong execution: %+v", first.Run)
	}
	if first.Task.Title != "Find Bugs" || !strings.Contains(first.Task.Description, "Do not modify files") {
		t.Fatal("not a predefined workflow")
	}
	// Config changes do not change the meaning of an already accepted request.
	layout.Slots[6] = nil
	raw, _ := json.Marshal(layout)
	if code, b := call(t, "PUT", p.url+"/api/deck", token, string(raw)); code != 200 {
		t.Fatalf("remove: %d %s", code, b)
	}
	p.finish(t, first.Run)
	code, raw := call(t, "POST", p.url+"/api/deck/workflows", token, body)
	if code != 200 {
		t.Fatalf("replay: %d %s", code, raw)
	}
	var replay struct {
		Task domain.Task
		Run  domain.Run
	}
	_ = json.Unmarshal(raw, &replay)
	if replay.Run.ID != first.Run.ID || replay.Task.ID != first.Task.ID {
		t.Fatal("completed invocation ran again")
	}
	code, raw = call(t, "GET", p.url+"/api/deck/history", token, "")
	var history []service.DeckExecution
	_ = json.Unmarshal(raw, &history)
	if code != 200 || len(history) != 1 || history[0].TaskID != first.Task.ID {
		t.Fatalf("history: %d %s", code, raw)
	}
	other, _ := p.enrolled(t, "B")
	if code, raw := call(t, "GET", p.url+"/api/deck/history", other, ""); code != 200 || strings.TrimSpace(string(raw)) != "[]" {
		t.Fatalf("history leaked: %d %s", code, raw)
	}
}

func TestPhoneCanConfigureAndRunEveryPresetWithoutAdministrativeAccess(t *testing.T) {
	p := newPhoned(t)
	token, _ := p.enrolled(t, "Preset phone")
	for i, preset := range service.DeckWorkflows {
		t.Run(preset.ID, func(t *testing.T) {
			code, raw := call(t, "GET", p.url+"/api/deck", token, "")
			if code != 200 {
				t.Fatal(code)
			}
			var layout service.DeckLayout
			_ = json.Unmarshal(raw, &layout)
			layout.Slots[6] = &service.DeckSlot{ID: "preset", Button: preset.ID, Target: "current", Agent: "fake", Output: "report", Approval: "changes", MaxRepositories: 1}
			body, _ := json.Marshal(layout)
			if code, raw = call(t, "PUT", p.url+"/api/deck", token, string(body)); code != 200 {
				t.Fatalf("save: %d %s", code, raw)
			}
			body, _ = json.Marshal(map[string]any{"slotId": "preset", "requestId": fmt.Sprintf("preset-request-%04d", i), "currentProjectId": p.project.ID})
			code, raw = call(t, "POST", p.url+"/api/deck/workflows", token, string(body))
			if code != 200 {
				t.Fatalf("start: %d %s", code, raw)
			}
			var result struct {
				Task domain.Task
				Run  domain.Run
			}
			_ = json.Unmarshal(raw, &result)
			if result.Task.Title != preset.Name || result.Run.AgentID != "fake" || result.Run.WorktreeID == "" {
				t.Fatalf("wrong preset %s", raw)
			}
			p.finish(t, result.Run)
		})
	}
	for _, body := range []string{
		`{"slotId":"preset","requestId":"unsafe-request-1234","override":{"id":"preset","button":"sprint-summary","target":"selected","projectIds":["unknown"]}}`,
		`{"slotId":"preset","requestId":"unsafe-request-1234","override":{"id":"preset","button":"sprint-summary","target":"all","interaction":"autonomous"}}`,
		`{"slotId":"preset","requestId":"unsafe-request-1234","override":{"id":"preset","button":"arbitrary-command","target":"all"}}`,
	} {
		if code, raw := call(t, "POST", p.url+"/api/deck/workflows", token, body); code < 400 {
			t.Fatalf("accepted: %s", raw)
		}
	}
	if code, _ := call(t, "PUT", p.url+"/api/settings/execution", token, `{"agent":"fake"}`); code != 403 {
		t.Fatal("phone gained settings access")
	}
}
