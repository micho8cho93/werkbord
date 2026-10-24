package acp

import (
	"context"
	agentapi "devboard/internal/agent"
	"devboard/internal/domain"
	"fmt"
	acp "github.com/caelis-labs/acp-go-sdk"
	"github.com/caelis-labs/acp-go-sdk/transport/stdio"
	"io"
	"os"
	"time"
)

// A discovery session never sends a prompt, grants a permission or advertises filesystem/terminal access.
type probeClient struct{}

func (*probeClient) SessionUpdate(context.Context, acp.SessionNotification) error { return nil }
func (*probeClient) RequestPermission(context.Context, acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error) {
	return acp.RequestPermissionResponse{Outcome: acp.NewRequestPermissionOutcomeCancelled()}, nil
}
func (a *Adapter) discover(parent context.Context) (domain.AgentOptions, error) {
	var out domain.AgentOptions
	ctx, cancel := context.WithTimeout(parent, 12*time.Second)
	defer cancel()
	dir, err := os.MkdirTemp("", "werkbord-agent-probe-")
	if err != nil {
		return out, err
	}
	defer os.RemoveAll(dir)
	proc, err := stdio.StartClient(ctx, &probeClient{}, stdio.Command{Executable: a.cfg.Command, Args: a.cfg.Args, Dir: dir, Env: agentapi.SanitizedEnv(os.Environ()), Stderr: io.Discard}, acp.ConnectionOptions{})
	if err != nil {
		return out, err
	}
	defer func() {
		stop, c := context.WithTimeout(context.Background(), time.Second)
		defer c()
		_ = proc.Shutdown(stop)
	}()
	_, err = proc.Connection.Initialize(ctx, acp.InitializeRequest{ProtocolVersion: acp.ProtocolVersion(acp.WireProtocolVersion), ClientInfo: &acp.Implementation{Name: "Werkbord capability probe", Version: "1"}})
	if err != nil {
		return out, err
	}
	session, err := proc.Connection.NewSession(ctx, acp.NewSessionRequest{Cwd: dir, McpServers: []acp.McpServer{}})
	if err != nil {
		return out, err
	}
	out = parseSessionOptions(session.ConfigOptions)
	state := session.ConfigOptions
	// Config options can change with the model. Never apply one model's effort
	// choices to another model, and bound work for very large catalogues.
	seen := map[string]bool{}
	for _, level := range out.Reasoning {
		seen[level.ID] = true
	}
	for i := range out.Models {
		model := &out.Models[i]
		model.ReasoningKnown = true
		if i >= 64 || ctx.Err() != nil {
			continue
		}
		if err := setOption(ctx, proc.Connection, session.SessionId, &state, "model", model.ID); err != nil {
			continue
		}
		selected := parseSessionOptions(state)
		for _, level := range selected.Reasoning {
			model.Reasoning = append(model.Reasoning, level.ID)
			if !seen[level.ID] {
				out.Reasoning = append(out.Reasoning, level)
				seen[level.ID] = true
			}
		}
	}
	if len(out.Models) == 0 && len(out.Reasoning) == 0 {
		return out, fmt.Errorf("the CLI did not advertise model or reasoning options")
	}
	return out, nil
}

func parseSessionOptions(options []acp.SessionConfigOption) domain.AgentOptions {
	var out domain.AgentOptions
	for _, o := range options {
		v := o.Select
		if v == nil {
			continue
		}
		category := string(v.Id)
		if v.Category != nil {
			category = string(*v.Category)
		}
		var values []acp.SessionConfigSelectOption
		if v.Options.Ungrouped != nil {
			values = append(values, (*v.Options.Ungrouped)...)
		}
		if v.Options.Grouped != nil {
			for _, g := range *v.Options.Grouped {
				values = append(values, g.Options...)
			}
		}
		for _, x := range values {
			id := string(x.Value)
			item := domain.AgentOption{ID: id, Name: x.Name, Default: x.Value == v.CurrentValue}
			if x.Description != nil {
				item.Description = *x.Description
			}
			if category == "model" && domain.ValidModelName(id) {
				out.Models = append(out.Models, item)
			}
			if (category == "thought_level" || category == "effort" || category == "reasoning" || category == "reasoning_effort") && domain.ValidReasoning(id) {
				out.Reasoning = append(out.Reasoning, item)
			}
		}
	}
	return out
}
