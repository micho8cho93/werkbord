package device

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// RunningTerminal is a native terminal that is still open, described for a person.
type RunningTerminal struct {
	ID         string
	ProjectID  string
	Agent      string
	Repository string
	Since      time.Time
}

// TerminalsRunning is the refusal to replace the program while native terminals are open. It carries the terminals, so a
// caller can name them and offer to stop them instead of repeating a sentence.
type TerminalsRunning struct {
	Terminals []RunningTerminal
	now       func() time.Time
}

func (e *TerminalsRunning) Error() string {
	now := time.Now
	if e.now != nil {
		now = e.now
	}
	return DescribeTerminals(e.Terminals, now())
}

// DescribeTerminals says which terminals are open: "Codex in test-repo has been running since Oct 27 and is still open."
func DescribeTerminals(list []RunningTerminal, now time.Time) string {
	if len(list) == 1 {
		return fmt.Sprintf("%s has been running %s and is still open", terminalLabel(list[0]), since(list[0].Since, now))
	}
	parts := make([]string, 0, len(list))
	for _, t := range list {
		parts = append(parts, fmt.Sprintf("%s (%s)", terminalLabel(t), since(t.Since, now)))
	}
	return fmt.Sprintf("%d terminals are still open: %s", len(list), strings.Join(parts, ", "))
}

func terminalLabel(t RunningTerminal) string {
	switch {
	case t.Agent == "":
		return "A terminal"
	case t.Repository != "":
		return agentLabel(t.Agent) + " in " + t.Repository
	}
	return "A " + agentLabel(t.Agent) + " terminal"
}

func agentLabel(id string) string {
	switch id {
	case "codex":
		return "Codex"
	case "claude-code":
		return "Claude Code"
	}
	return id
}

// since is "since today", "since yesterday" or "since Oct 27", in the person's own time zone.
func since(t, now time.Time) string {
	if t.IsZero() {
		return "for some time"
	}
	t, now = t.Local(), now.Local()
	y, m, d := now.Date()
	today := time.Date(y, m, d, 0, 0, 0, 0, now.Location())
	switch {
	case !t.Before(today):
		return "since today"
	case !t.Before(today.AddDate(0, 0, -1)):
		return "since yesterday"
	}
	return "since " + t.Format("Jan 2")
}

// CheckOperationsSettled reads only local receipts; the desktop links no executor.
func CheckOperationsSettled(dataDir string) error {
	for _, dir := range []string{"runner", "device-operations"} {
		paths, err := filepath.Glob(filepath.Join(dataDir, dir, "operations", "*.json"))
		if err != nil {
			return err
		}
		for _, path := range paths {
			raw, err := os.ReadFile(path)
			var receipt struct{ Result struct{ State string } }
			if err != nil || json.Unmarshal(raw, &receipt) != nil || receipt.Result.State == "executing" || receipt.Result.State == "" {
				return fmt.Errorf("inspect interrupted device operation before continuing: %s", filepath.Base(path))
			}
		}
	}
	return nil
}

type terminalRecord struct {
	ID         string    `json:"id"`
	ProjectID  string    `json:"projectId"`
	AgentID    string    `json:"agentId"`
	Repository string    `json:"repository"`
	State      string    `json:"state"`
	CreatedAt  time.Time `json:"createdAt"`
}

func (r terminalRecord) running() RunningTerminal {
	return RunningTerminal{ID: r.ID, ProjectID: r.ProjectID, Agent: r.AgentID, Repository: r.Repository, Since: r.CreatedAt}
}

// RunningTerminalsOnDisk is what the metadata files say is running. A file is only as current as the last time its owner
// wrote it, so it is the answer of last resort: the controller's own list is better (see LiveTerminals). The runner's file
// belongs to a separate process that nothing else can ask, so it is always read from disk; the controller's file only when
// includeController is set.
func RunningTerminalsOnDisk(dataDir string, includeController bool) ([]RunningTerminal, error) {
	files := []string{filepath.Join(dataDir, "runner", "terminals.json")}
	if includeController {
		files = append(files, filepath.Join(dataDir, "terminals.json"))
	}
	var running []RunningTerminal
	for _, path := range files {
		raw, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		var sessions []terminalRecord
		if json.Unmarshal(raw, &sessions) != nil {
			return nil, fmt.Errorf("cannot inspect terminal sessions")
		}
		for _, s := range sessions {
			if s.State == "running" {
				running = append(running, s.running())
			}
		}
	}
	return running, nil
}

// LiveTerminals asks the running controller which of its native terminals are open. ok is false when it could not say (it
// is not answering, or terminals are not available to this caller), and then the caller falls back to the files.
func LiveTerminals(ctx context.Context, client *http.Client, base, token string) (running []RunningTerminal, ok bool) {
	req, err := http.NewRequestWithContext(ctx, "GET", strings.TrimSuffix(base, "/")+"/api/terminals", nil)
	if err != nil {
		return nil, false
	}
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := client.Do(req)
	if err != nil {
		return nil, false
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, false
	}
	var sessions []terminalRecord
	if json.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(&sessions) != nil {
		return nil, false
	}
	for _, s := range sessions {
		if s.State == "running" {
			running = append(running, s.running())
		}
	}
	return running, true
}

// OpenTerminals is every native terminal that blocks replacing the program, as a *TerminalsRunning, or nil. live is the
// controller's own answer (see LiveTerminals) and haveLive says it gave one: it is then the authority for the controller's
// terminals, because the controller reaps a record whose process is gone, which a file read cannot know.
func OpenTerminals(dataDir string, live []RunningTerminal, haveLive bool) error {
	running, err := RunningTerminalsOnDisk(dataDir, !haveLive)
	if err != nil {
		return err
	}
	running = append(live, running...)
	if len(running) == 0 {
		return nil
	}
	return &TerminalsRunning{Terminals: running}
}

// CheckExecutionSettled is CheckOperationsSettled and the terminal files together, for a caller that cannot ask the
// controller.
func CheckExecutionSettled(dataDir string) error {
	if err := CheckOperationsSettled(dataDir); err != nil {
		return err
	}
	return OpenTerminals(dataDir, nil, false)
}
