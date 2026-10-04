package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"

	"devboard/internal/agent"
	"devboard/internal/domain"
)

var _ agent.Optioner = (*Adapter)(nil)

// optionsTTL is how long what Codex said about its models is remembered.
// Models change rarely and asking costs a process, but a user who has just
// updated Codex should not wait long to see the new ones.
const optionsTTL = 10 * time.Minute

// optionsTimeout bounds asking Codex for its models.
var optionsTimeout = 10 * time.Second

// Options implements agent.Optioner. Codex's app-server lists its own models
// with the reasoning levels each supports, so the list is whatever the
// installed Codex says it is; the user's configured list replaces it.
func (a *Adapter) Options(ctx context.Context) domain.AgentOptions {
	a.optMu.Lock()
	defer a.optMu.Unlock()
	if !a.optAt.IsZero() && time.Since(a.optAt) < optionsTTL {
		return a.optCach
	}
	opts := a.fetchOptions(ctx)
	ttl := optionsTTL
	if opts.ModelsSource != domain.OptionsFromAgent {
		ttl = time.Minute // try again soon: it may have been a passing failure
	}
	a.optCach, a.optAt = opts, time.Now().Add(ttl-optionsTTL)
	return opts
}

func (a *Adapter) fetchOptions(ctx context.Context) domain.AgentOptions {
	opts := domain.AgentOptions{AgentID: ID, CustomModels: true, ModelsSource: domain.OptionsBuiltIn, ReasoningSource: domain.OptionsBuiltIn}
	if len(a.cfg.Models) > 0 || len(a.cfg.Reasoning) > 0 {
		opts.Models, opts.ModelsSource = a.cfg.Models, domain.OptionsConfigured
		for _, r := range a.cfg.Reasoning {
			opts.Reasoning = append(opts.Reasoning, domain.AgentOption{ID: r, Name: r})
		}
		if len(opts.Reasoning) > 0 {
			opts.ReasoningSource = domain.OptionsConfigured
		}
		return opts
	}
	if info := a.Detect(ctx); !info.Installed {
		opts.Note = "Codex is not installed, so its models cannot be listed."
		return opts
	}
	models, err := listModels(ctx, a.cfg.Command)
	if err != nil {
		opts.Note = "Could not ask Codex for its models: " + err.Error()
		return opts
	}
	opts.ModelsSource, opts.ReasoningSource = domain.OptionsFromAgent, domain.OptionsFromAgent
	seen := map[string]bool{}
	for _, m := range models {
		if m.Hidden {
			continue
		}
		id := m.Model
		if id == "" {
			id = m.ID
		}
		o := domain.AgentOption{ID: id, Name: m.DisplayName, Description: m.Description, Default: m.IsDefault}
		if o.Name == "" {
			o.Name = id
		}
		for _, r := range m.SupportedReasoningEfforts {
			o.Reasoning = append(o.Reasoning, r.ReasoningEffort)
			if !seen[r.ReasoningEffort] {
				seen[r.ReasoningEffort] = true
				opts.Reasoning = append(opts.Reasoning, domain.AgentOption{ID: r.ReasoningEffort, Name: r.ReasoningEffort, Description: r.Description})
			}
		}
		opts.Models = append(opts.Models, o)
	}
	return opts
}

// codexModel is the part of the app-server's model/list entry that matters here.
type codexModel struct {
	ID                        string `json:"id"`
	Model                     string `json:"model"`
	DisplayName               string `json:"displayName"`
	Description               string `json:"description"`
	Hidden                    bool   `json:"hidden"`
	IsDefault                 bool   `json:"isDefault"`
	SupportedReasoningEfforts []struct {
		ReasoningEffort string `json:"reasoningEffort"`
		Description     string `json:"description"`
	} `json:"supportedReasoningEfforts"`
}

// listModels starts a short-lived `codex app-server`, asks it for its models and
// stops it. It is a separate process from any run's: listing models needs no
// worktree and must never disturb a session.
func listModels(ctx context.Context, command string) ([]codexModel, error) {
	ctx, cancel := context.WithTimeout(ctx, optionsTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, command, "app-server")
	cmd.Env = agent.SanitizedEnv(os.Environ())
	cmd.WaitDelay = time.Second
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	defer func() {
		_ = in.Close()
		cancel()
		_ = cmd.Wait()
	}()

	send := func(v map[string]any) error {
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		_, err = in.Write(append(b, '\n'))
		return err
	}
	lines := bufio.NewReaderSize(out, 1<<20)
	// await reads until the response to request id arrives.
	await := func(id int) (json.RawMessage, error) {
		for {
			line, err := lines.ReadBytes('\n')
			if err != nil {
				if ctx.Err() != nil {
					return nil, fmt.Errorf("timed out")
				}
				if err == io.EOF {
					return nil, fmt.Errorf("codex exited")
				}
				return nil, err
			}
			var msg rpcMessage
			if json.Unmarshal(line, &msg) != nil || msg.Method != "" {
				continue // not JSON, or a notification or request from the server
			}
			var got int
			if json.Unmarshal(msg.ID, &got) != nil || got != id {
				continue
			}
			if msg.Error != nil {
				return nil, msg.Error
			}
			return msg.Result, nil
		}
	}

	if err := send(map[string]any{"id": 1, "method": "initialize", "params": map[string]any{
		"clientInfo": map[string]any{"name": "devboard", "title": "Devboard", "version": "1"},
	}}); err != nil {
		return nil, err
	}
	if _, err := await(1); err != nil {
		return nil, err
	}
	if err := send(map[string]any{"method": "initialized"}); err != nil {
		return nil, err
	}

	var all []codexModel
	cursor := ""
	for page := 0; page < 10; page++ {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		if err := send(map[string]any{"id": 2 + page, "method": "model/list", "params": params}); err != nil {
			return nil, err
		}
		raw, err := await(2 + page)
		if err != nil {
			return nil, fmt.Errorf("model/list: %w", err)
		}
		var resp struct {
			Data       []codexModel `json:"data"`
			NextCursor *string      `json:"nextCursor"`
		}
		if err := json.Unmarshal(raw, &resp); err != nil {
			return nil, fmt.Errorf("model/list: %w", err)
		}
		all = append(all, resp.Data...)
		if resp.NextCursor == nil || *resp.NextCursor == "" {
			break
		}
		cursor = *resp.NextCursor
	}
	return all, nil
}
