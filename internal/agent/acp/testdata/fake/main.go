package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	acp "github.com/caelis-labs/acp-go-sdk"
	"github.com/caelis-labs/acp-go-sdk/transport/stdio"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Println("opencode 1.0.0")
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "models" {
		fmt.Println("openai/gpt-test")
		return
	}
	agent := &fakeAgent{}
	conn, err := stdio.NewAgentConnection(agent, acp.ConnectionOptions{})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	agent.conn = conn
	if err := conn.Wait(context.Background()); err != nil && !errors.Is(err, acp.ErrPeerClosed) && !errors.Is(err, acp.ErrConnectionClosed) {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

type fakeAgent struct {
	conn *acp.AgentSideConnection
	cwd  string
}

func (a *fakeAgent) Initialize(_ context.Context, req acp.InitializeRequest) (acp.InitializeResponse, error) {
	if path := os.Getenv("ACP_TEST_CAPS_FILE"); path != "" {
		data, err := json.Marshal(req.ClientCapabilities)
		if err != nil {
			return acp.InitializeResponse{}, err
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			return acp.InitializeResponse{}, err
		}
	}
	if path := os.Getenv("ACP_TEST_ENV_FILE"); path != "" {
		content := "gemini=" + os.Getenv("GEMINI_CLI_SYSTEM_SETTINGS_PATH") + "\nopencode=" + os.Getenv("OPENCODE_CONFIG_CONTENT")
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			return acp.InitializeResponse{}, err
		}
	}
	return acp.InitializeResponse{ProtocolVersion: req.ProtocolVersion, AgentCapabilities: acp.AgentCapabilities{LoadSession: true}}, nil
}

func (a *fakeAgent) NewSession(_ context.Context, req acp.NewSessionRequest) (acp.NewSessionResponse, error) {
	a.cwd = req.Cwd
	return acp.NewSessionResponse{SessionId: "new-session", ConfigOptions: configOptions()}, nil
}

func (a *fakeAgent) LoadSession(_ context.Context, req acp.LoadSessionRequest) (acp.LoadSessionResponse, error) {
	a.cwd = req.Cwd
	return acp.LoadSessionResponse{ConfigOptions: configOptions()}, nil
}

func (a *fakeAgent) SetSessionConfigOption(_ context.Context, req acp.SetSessionConfigOptionRequest) (acp.SetSessionConfigOptionResponse, error) {
	if req.ValueId == nil {
		return acp.SetSessionConfigOptionResponse{}, nil
	}
	line := fmt.Sprintf("%s=%s\n", req.ValueId.ConfigId, req.ValueId.Value)
	f, err := os.OpenFile(os.Getenv("ACP_TEST_OPTIONS_FILE"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return acp.SetSessionConfigOptionResponse{}, err
	}
	_, writeErr := f.WriteString(line)
	closeErr := f.Close()
	if writeErr != nil {
		return acp.SetSessionConfigOptionResponse{}, writeErr
	}
	return acp.SetSessionConfigOptionResponse{ConfigOptions: configOptions()}, closeErr
}

func (a *fakeAgent) Cancel(context.Context, acp.CancelNotification) error { return nil }

func (a *fakeAgent) Prompt(ctx context.Context, req acp.PromptRequest) (acp.PromptResponse, error) {
	if len(req.Prompt) == 0 || req.Prompt[0].Text == nil {
		return acp.PromptResponse{StopReason: acp.StopReasonEndTurn}, nil
	}
	prompt := req.Prompt[0].Text.Text
	if strings.Contains(prompt, "wait-for-cancel") {
		<-ctx.Done()
		return acp.PromptResponse{StopReason: acp.StopReasonCancelled}, nil
	}
	if strings.Contains(prompt, "approval-test") {
		name, title, kind := "write_file", "Write a test file", acp.ToolKindEdit
		permission, err := a.conn.RequestPermission(ctx, acp.RequestPermissionRequest{
			SessionId: req.SessionId,
			ToolCall:  acp.ToolCallUpdate{ToolCallId: "test-call", Name: &name, Title: &title, Kind: &kind},
			Options: []acp.PermissionOption{
				{OptionId: "allow", Name: "Allow once", Kind: acp.PermissionOptionKindAllowOnce},
				{OptionId: "reject", Name: "Reject", Kind: acp.PermissionOptionKindRejectOnce},
			},
		})
		if err != nil {
			return acp.PromptResponse{}, err
		}
		if permission.Outcome.Selected != nil {
			if _, err := a.conn.WriteTextFile(ctx, acp.WriteTextFileRequest{Path: filepath.Join(a.cwd, "from-acp.txt"), Content: "approved"}); err != nil {
				return acp.PromptResponse{}, err
			}
		}
		if err := a.send(ctx, req.SessionId, "approval complete"); err != nil {
			return acp.PromptResponse{}, err
		}
	} else {
		if err := a.send(ctx, req.SessionId, "Hello "); err != nil {
			return acp.PromptResponse{}, err
		}
		if err := a.send(ctx, req.SessionId, "ACP"); err != nil {
			return acp.PromptResponse{}, err
		}
	}
	return acp.PromptResponse{StopReason: acp.StopReasonEndTurn}, nil
}

func (a *fakeAgent) send(ctx context.Context, session acp.SessionId, text string) error {
	return a.conn.SessionUpdate(ctx, acp.SessionNotification{SessionId: session, Update: acp.SessionUpdate{AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{SessionUpdate: "agent_message_chunk", Content: acp.ContentBlock{Text: &acp.ContentBlockText{Type: "text", Text: text}}}}})
}

func configOptions() []acp.SessionConfigOption {
	var options []acp.SessionConfigOption
	if err := json.Unmarshal([]byte(`[
		{"type":"select","id":"model","name":"Model","currentValue":"old/model","options":[{"name":"New model","value":"new/model"}]},
		{"type":"select","id":"effort","name":"Effort","currentValue":"low","options":[{"name":"High","value":"high"}]}
	]`), &options); err != nil {
		panic(err)
	}
	return options
}
