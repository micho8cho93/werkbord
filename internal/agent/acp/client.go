package acp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	agentapi "devboard/internal/agent"
	"devboard/internal/domain"

	acp "github.com/caelis-labs/acp-go-sdk"
	"github.com/caelis-labs/acp-go-sdk/transport/stdio"
)

const (
	maxFileBytes       = 4 << 20
	maxWriteBytes      = 2 << 20
	maxTerminalBytes   = 1 << 20
	maxTerminalRuntime = 30 * time.Minute
)

type permission struct {
	ref, category string
	options       []acp.PermissionOption
	answer        chan string
}

type terminal struct {
	id       acp.TerminalId
	proc     *stdio.Process
	cancel   context.CancelFunc
	output   tailBuffer
	done     chan struct{}
	exitCode *uint32
	signal   *string
	mu       sync.Mutex
}

type runClient struct {
	mu        sync.Mutex
	owner     *session
	root      string
	questions map[string]*permission
	terminals map[string]*terminal
	approvals map[string]int
	nextRef   atomic.Uint64
	nextTerm  atomic.Uint64
	stderr    tailBuffer
}

var _ acp.Client = (*runClient)(nil)
var _ acp.ClientFileReader = (*runClient)(nil)
var _ acp.ClientFileWriter = (*runClient)(nil)
var _ acp.ClientTerminal = (*runClient)(nil)

func (c *runClient) RequestPermission(ctx context.Context, req acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error) {
	category := permissionCategory(req.ToolCall)
	name := "the requested tool"
	if req.ToolCall.Title != nil && strings.TrimSpace(*req.ToolCall.Title) != "" {
		name = strings.TrimSpace(*req.ToolCall.Title)
	}
	detail := ""
	if req.ToolCall.RawInput != nil {
		if b, err := json.Marshal(req.ToolCall.RawInput); err == nil {
			detail = string(b)
		}
	}
	if detail == "" && req.ToolCall.Name != nil {
		detail = *req.ToolCall.Name
	}
	options := append([]acp.PermissionOption(nil), req.Options...)
	if len(options) == 0 {
		options = []acp.PermissionOption{{OptionId: "allow", Name: "Allow once", Kind: acp.PermissionOptionKindAllowOnce}, {OptionId: "reject", Name: "Reject", Kind: acp.PermissionOptionKindRejectOnce}}
	}
	ref := c.newQuestionRef()
	p := &permission{ref: ref, category: category, options: options, answer: make(chan string, 1)}
	c.mu.Lock()
	c.questions[ref] = p
	c.mu.Unlock()
	labels := make([]string, 0, len(options))
	for _, option := range options {
		labels = append(labels, option.Name)
	}
	c.owner.Emit(agentapi.Event{Kind: agentapi.KindQuestion, Question: &agentapi.Question{
		Ref: ref, Kind: domain.QuestionApproval, Prompt: "Allow " + name + "?", Context: clip(detail, 1200), Options: labels,
	}})
	select {
	case answer := <-p.answer:
		c.mu.Lock()
		delete(c.questions, ref)
		c.mu.Unlock()
		selected, ok := selectOption(options, answer)
		if !ok {
			return acp.RequestPermissionResponse{Outcome: acp.NewRequestPermissionOutcomeCancelled()}, nil
		}
		if strings.Contains(strings.ToLower(string(selected.Kind)), "allow") {
			c.recordApproval(category)
		}
		return acp.RequestPermissionResponse{Outcome: acp.NewRequestPermissionOutcomeSelected(selected.OptionId)}, nil
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.questions, ref)
		c.mu.Unlock()
		c.owner.Emit(agentapi.Event{Kind: agentapi.KindQuestionClosed, Ref: ref})
		return acp.RequestPermissionResponse{Outcome: acp.NewRequestPermissionOutcomeCancelled()}, nil
	case <-c.owner.ctx.Done():
		c.mu.Lock()
		delete(c.questions, ref)
		c.mu.Unlock()
		c.owner.Emit(agentapi.Event{Kind: agentapi.KindQuestionClosed, Ref: ref})
		return acp.RequestPermissionResponse{Outcome: acp.NewRequestPermissionOutcomeCancelled()}, nil
	}
}

func (c *runClient) SessionUpdate(_ context.Context, n acp.SessionNotification) error {
	u := n.Update
	switch {
	case u.AgentMessageChunk != nil && u.AgentMessageChunk.Content.Text != nil:
		if text := u.AgentMessageChunk.Content.Text.Text; text != "" {
			c.owner.Emit(agentapi.Event{Kind: agentapi.KindOutput, Stream: domain.StreamAssistant, Text: text})
		}
	case u.AgentThoughtChunk != nil:
		c.owner.Emit(agentapi.Event{Kind: agentapi.KindOutput, Stream: domain.StreamSystem, Text: "Thinking…"})
	case u.ToolCall != nil:
		title := "Using a tool"
		if u.ToolCall.Title != "" {
			title = u.ToolCall.Title
		}
		c.owner.Emit(agentapi.Event{Kind: agentapi.KindOutput, Stream: domain.StreamTool, Text: title})
	case u.ToolCallUpdate != nil:
		if u.ToolCallUpdate.Status != nil && string(*u.ToolCallUpdate.Status) == "failed" {
			title := "A tool call failed"
			if u.ToolCallUpdate.Title != nil {
				title = *u.ToolCallUpdate.Title
			}
			c.owner.Emit(agentapi.Event{Kind: agentapi.KindOutput, Stream: domain.StreamSystem, Text: title})
		}
	case u.Plan != nil:
		if b, err := json.Marshal(u.Plan); err == nil {
			c.owner.Emit(agentapi.Event{Kind: agentapi.KindOutput, Stream: domain.StreamSystem, Text: "Plan: " + clip(string(b), 1200)})
		}
	case u.Notice != nil:
		if u.Notice.Title != "" {
			c.owner.Emit(agentapi.Event{Kind: agentapi.KindOutput, Stream: domain.StreamSystem, Text: u.Notice.Title})
		}
	}
	return nil
}

func (c *runClient) ReadTextFile(ctx context.Context, req acp.ReadTextFileRequest) (acp.ReadTextFileResponse, error) {
	if err := ctx.Err(); err != nil {
		return acp.ReadTextFileResponse{}, err
	}
	path, err := c.safePath(req.Path, false)
	if err != nil {
		return acp.ReadTextFileResponse{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return acp.ReadTextFileResponse{}, err
	}
	if !info.Mode().IsRegular() {
		return acp.ReadTextFileResponse{}, fmt.Errorf("only regular workspace files can be read")
	}
	if info.Size() > maxFileBytes {
		return acp.ReadTextFileResponse{}, fmt.Errorf("file is larger than the %d byte read limit", maxFileBytes)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return acp.ReadTextFileResponse{}, err
	}
	if req.Line == nil && req.Limit == nil {
		return acp.ReadTextFileResponse{Content: string(data)}, nil
	}
	lines := strings.Split(string(data), "\n")
	start := 0
	if req.Line != nil {
		if *req.Line == 0 {
			return acp.ReadTextFileResponse{}, fmt.Errorf("line numbers begin at 1")
		}
		start = int(*req.Line - 1)
	}
	if start >= len(lines) {
		return acp.ReadTextFileResponse{Content: ""}, nil
	}
	end := len(lines)
	if req.Limit != nil && *req.Limit > 0 && start+int(*req.Limit) < end {
		end = start + int(*req.Limit)
	}
	return acp.ReadTextFileResponse{Content: strings.Join(lines[start:end], "\n")}, nil
}

func (c *runClient) WriteTextFile(ctx context.Context, req acp.WriteTextFileRequest) (acp.WriteTextFileResponse, error) {
	if len(req.Content) > maxWriteBytes {
		return acp.WriteTextFileResponse{}, fmt.Errorf("file content exceeds the %d byte write limit", maxWriteBytes)
	}
	path, err := c.safePath(req.Path, true)
	if err != nil {
		return acp.WriteTextFileResponse{}, err
	}
	if !c.takeApproval("edit") {
		if err := c.askDirect(ctx, "edit", "Write file?", path); err != nil {
			return acp.WriteTextFileResponse{}, err
		}
	}
	if err := ctx.Err(); err != nil {
		return acp.WriteTextFileResponse{}, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return acp.WriteTextFileResponse{}, err
	}
	path, err = c.safePath(path, true)
	if err != nil {
		return acp.WriteTextFileResponse{}, err
	}
	mode := os.FileMode(0o644)
	if st, statErr := os.Stat(path); statErr == nil {
		mode = st.Mode().Perm() & 0o777
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".werkbord-acp-*")
	if err != nil {
		return acp.WriteTextFileResponse{}, err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return acp.WriteTextFileResponse{}, err
	}
	if _, err := io.Copy(tmp, strings.NewReader(req.Content)); err != nil {
		_ = tmp.Close()
		return acp.WriteTextFileResponse{}, err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return acp.WriteTextFileResponse{}, err
	}
	if err := tmp.Close(); err != nil {
		return acp.WriteTextFileResponse{}, err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return acp.WriteTextFileResponse{}, err
	}
	return acp.WriteTextFileResponse{}, nil
}

func (c *runClient) CreateTerminal(ctx context.Context, req acp.CreateTerminalRequest) (acp.CreateTerminalResponse, error) {
	if !c.takeApproval("terminal") {
		command := strings.TrimSpace(req.Command + " " + strings.Join(req.Args, " "))
		cwd := c.root
		if req.Cwd != nil {
			cwd = *req.Cwd
		}
		if err := c.askDirect(ctx, "terminal", "Run command?", command+"\nDirectory: "+cwd); err != nil {
			return acp.CreateTerminalResponse{}, err
		}
	}
	if strings.TrimSpace(req.Command) == "" || len(req.Command) > 500 {
		return acp.CreateTerminalResponse{}, fmt.Errorf("invalid terminal command")
	}
	cwd := c.root
	if req.Cwd != nil {
		cwd = *req.Cwd
	}
	work, err := c.safePath(cwd, false)
	if err != nil {
		return acp.CreateTerminalResponse{}, fmt.Errorf("terminal directory is outside the run worktree: %w", err)
	}
	info, err := os.Stat(work)
	if err != nil || !info.IsDir() {
		return acp.CreateTerminalResponse{}, fmt.Errorf("terminal working directory is not a workspace directory")
	}
	args := append([]string(nil), req.Args...)
	env := agentapi.SanitizedEnv(os.Environ())
	for _, value := range req.Env {
		if value.Name == "" || strings.ContainsAny(value.Name, "=\x00") || strings.ContainsRune(value.Value, '\x00') {
			return acp.CreateTerminalResponse{}, fmt.Errorf("invalid terminal environment variable")
		}
		env = replaceEnv(env, value.Name, value.Value)
	}
	env = agentapi.SanitizedEnv(env)
	limit := maxTerminalBytes
	if req.OutputByteLimit != nil && *req.OutputByteLimit > 0 && *req.OutputByteLimit < uint64(limit) {
		limit = int(*req.OutputByteLimit)
	}
	termCtx, cancel := context.WithTimeout(c.owner.ctx, maxTerminalRuntime)
	id := acp.TerminalId(fmt.Sprintf("terminal-%d", c.nextTerm.Add(1)))
	t := &terminal{id: id, cancel: cancel, done: make(chan struct{})}
	t.output.limit = limit
	proc, err := stdio.Start(termCtx, stdio.Command{Executable: req.Command, Args: args, Dir: work, Env: env, Stderr: &t.output})
	if err != nil {
		cancel()
		return acp.CreateTerminalResponse{}, fmt.Errorf("start command: %w", err)
	}
	t.proc = proc
	c.mu.Lock()
	c.terminals[string(id)] = t
	c.mu.Unlock()
	go func() {
		_, _ = io.Copy(&t.output, proc.Output())
		waitErr := proc.Wait(context.Background())
		t.mu.Lock()
		if waitErr != nil {
			var exit *exec.ExitError
			if errors.As(waitErr, &exit) {
				if code := exit.ExitCode(); code >= 0 {
					v := uint32(code)
					t.exitCode = &v
				} else {
					s := "terminated"
					t.signal = &s
				}
			} else if termCtx.Err() != nil {
				s := "terminated"
				t.signal = &s
			}
		} else {
			v := uint32(0)
			t.exitCode = &v
		}
		t.mu.Unlock()
		close(t.done)
		cancel()
	}()
	return acp.CreateTerminalResponse{TerminalId: id}, nil
}

func (c *runClient) KillTerminal(_ context.Context, req acp.KillTerminalRequest) (acp.KillTerminalResponse, error) {
	t, err := c.findTerminal(req.TerminalId)
	if err != nil {
		return acp.KillTerminalResponse{}, err
	}
	t.cancel()
	_ = t.proc.Close()
	return acp.KillTerminalResponse{}, nil
}

func (c *runClient) TerminalOutput(_ context.Context, req acp.TerminalOutputRequest) (acp.TerminalOutputResponse, error) {
	t, err := c.findTerminal(req.TerminalId)
	if err != nil {
		return acp.TerminalOutputResponse{}, err
	}
	out, truncated := t.output.snapshot()
	response := acp.TerminalOutputResponse{Output: out, Truncated: truncated}
	select {
	case <-t.done:
		t.mu.Lock()
		exit, signal := t.exitCode, t.signal
		t.mu.Unlock()
		response.ExitStatus = &acp.TerminalExitStatus{ExitCode: exit, Signal: signal}
	default:
	}
	return response, nil
}

func (c *runClient) WaitForTerminalExit(ctx context.Context, req acp.WaitForTerminalExitRequest) (acp.WaitForTerminalExitResponse, error) {
	t, err := c.findTerminal(req.TerminalId)
	if err != nil {
		return acp.WaitForTerminalExitResponse{}, err
	}
	select {
	case <-t.done:
		t.mu.Lock()
		exit, signal := t.exitCode, t.signal
		t.mu.Unlock()
		return acp.WaitForTerminalExitResponse{ExitCode: exit, Signal: signal}, nil
	case <-ctx.Done():
		return acp.WaitForTerminalExitResponse{}, ctx.Err()
	case <-c.owner.ctx.Done():
		return acp.WaitForTerminalExitResponse{}, context.Canceled
	}
}

func (c *runClient) ReleaseTerminal(_ context.Context, req acp.ReleaseTerminalRequest) (acp.ReleaseTerminalResponse, error) {
	t, err := c.findTerminal(req.TerminalId)
	if err != nil {
		return acp.ReleaseTerminalResponse{}, err
	}
	c.mu.Lock()
	delete(c.terminals, string(req.TerminalId))
	c.mu.Unlock()
	select {
	case <-t.done:
	case <-time.After(300 * time.Millisecond):
		_ = t.proc.Close()
		<-t.done
	}
	t.cancel()
	return acp.ReleaseTerminalResponse{}, nil
}

func (c *runClient) askDirect(ctx context.Context, category, prompt, detail string) error {
	options := []acp.PermissionOption{
		{OptionId: "allow", Name: domain.AnswerAllow, Kind: acp.PermissionOptionKindAllowOnce},
		{OptionId: "deny", Name: domain.AnswerDeny, Kind: acp.PermissionOptionKindRejectOnce},
	}
	ref := c.newQuestionRef()
	p := &permission{ref: ref, category: category, options: options, answer: make(chan string, 1)}
	c.mu.Lock()
	c.questions[ref] = p
	c.mu.Unlock()
	c.owner.Emit(agentapi.Event{Kind: agentapi.KindQuestion, Question: &agentapi.Question{Ref: ref, Kind: domain.QuestionApproval, Prompt: prompt, Context: clip(detail, 1200), Options: []string{domain.AnswerAllow, domain.AnswerDeny}}})
	select {
	case answer := <-p.answer:
		c.mu.Lock()
		delete(c.questions, ref)
		c.mu.Unlock()
		selected, ok := selectOption(options, answer)
		if !ok || selected.Kind == acp.PermissionOptionKindRejectOnce {
			return fmt.Errorf("request was not approved")
		}
		return nil
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.questions, ref)
		c.mu.Unlock()
		c.owner.Emit(agentapi.Event{Kind: agentapi.KindQuestionClosed, Ref: ref})
		return ctx.Err()
	case <-c.owner.ctx.Done():
		c.mu.Lock()
		delete(c.questions, ref)
		c.mu.Unlock()
		c.owner.Emit(agentapi.Event{Kind: agentapi.KindQuestionClosed, Ref: ref})
		return context.Canceled
	}
}

func (c *runClient) newQuestionRef() string { return fmt.Sprintf("acp-question-%d", c.nextRef.Add(1)) }

func (c *runClient) respond(ref, answer string) error {
	c.mu.Lock()
	p := c.questions[ref]
	if p == nil {
		c.mu.Unlock()
		return fmt.Errorf("%s: %w", ref, agentapi.ErrUnknownQuestion)
	}
	select {
	case p.answer <- answer:
		c.mu.Unlock()
		return nil
	default:
		c.mu.Unlock()
		return fmt.Errorf("%s: %w", ref, agentapi.ErrUnknownQuestion)
	}
}

func (c *runClient) hasPendingQuestions() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.questions) > 0
}

func (c *runClient) recordApproval(category string) {
	c.mu.Lock()
	c.approvals[category]++
	c.mu.Unlock()
}
func (c *runClient) takeApproval(category string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.approvals[category] == 0 {
		return false
	}
	c.approvals[category]--
	return true
}

func (c *runClient) terminalsSnapshot() []*terminal {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]*terminal, 0, len(c.terminals))
	for _, t := range c.terminals {
		out = append(out, t)
	}
	return out
}

func (c *runClient) findTerminal(id acp.TerminalId) (*terminal, error) {
	c.mu.Lock()
	t := c.terminals[string(id)]
	c.mu.Unlock()
	if t == nil {
		return nil, fmt.Errorf("terminal %q was not found", id)
	}
	return t, nil
}

func permissionCategory(call acp.ToolCallUpdate) string {
	name := ""
	if call.Name != nil {
		name = strings.ToLower(*call.Name)
	}
	if call.Kind != nil {
		switch *call.Kind {
		case acp.ToolKindExecute:
			return "terminal"
		case acp.ToolKindEdit, acp.ToolKindDelete, acp.ToolKindMove:
			return "edit"
		}
	}
	if strings.Contains(name, "bash") || strings.Contains(name, "terminal") || strings.Contains(name, "command") || strings.Contains(name, "shell") {
		return "terminal"
	}
	if strings.Contains(name, "edit") || strings.Contains(name, "write") || strings.Contains(name, "patch") {
		return "edit"
	}
	return "other"
}

func selectOption(options []acp.PermissionOption, answer string) (acp.PermissionOption, bool) {
	answer = strings.TrimSpace(answer)
	for _, option := range options {
		if answer == string(option.OptionId) || strings.EqualFold(answer, option.Name) {
			return option, true
		}
	}
	low := strings.ToLower(answer)
	if low == "allow" || low == "approve" || low == "yes" || low == "ok" {
		for _, option := range options {
			if option.Kind == acp.PermissionOptionKindAllowOnce || option.Kind == acp.PermissionOptionKindAllowAlways {
				return option, true
			}
		}
	}
	if low == "deny" || low == "reject" || low == "no" {
		for _, option := range options {
			if option.Kind == acp.PermissionOptionKindRejectOnce || option.Kind == acp.PermissionOptionKindRejectAlways {
				return option, true
			}
		}
	}
	return acp.PermissionOption{}, false
}

func (c *runClient) safePath(input string, allowMissing bool) (string, error) {
	if strings.TrimSpace(input) == "" || strings.ContainsRune(input, 0) {
		return "", fmt.Errorf("invalid workspace path")
	}
	path, err := filepath.Abs(input)
	if err != nil {
		return "", err
	}
	if !within(c.root, path) {
		return "", fmt.Errorf("path is outside the run worktree")
	}
	root, err := filepath.EvalSymlinks(c.root)
	if err != nil {
		return "", err
	}
	probe := path
	var suffix []string
	for {
		resolved, evalErr := filepath.EvalSymlinks(probe)
		if evalErr == nil {
			if !within(root, resolved) {
				return "", fmt.Errorf("path resolves outside the run worktree")
			}
			for i := len(suffix) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, suffix[i])
			}
			if !allowMissing && len(suffix) > 0 {
				return "", os.ErrNotExist
			}
			if !within(root, resolved) {
				return "", fmt.Errorf("path resolves outside the run worktree")
			}
			return resolved, nil
		}
		if !allowMissing && !errors.Is(evalErr, os.ErrNotExist) {
			return "", evalErr
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			return "", evalErr
		}
		suffix = append(suffix, filepath.Base(probe))
		probe = parent
	}
}

func within(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func replaceEnv(env []string, name, value string) []string {
	prefix := name + "="
	out := make([]string, 0, len(env)+1)
	for _, item := range env {
		if !strings.HasPrefix(item, prefix) {
			out = append(out, item)
		}
	}
	return append(out, prefix+value)
}

type tailBuffer struct {
	mu        sync.Mutex
	b         bytes.Buffer
	limit     int
	truncated bool
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	limit := b.limit
	if limit <= 0 {
		limit = 64 << 10
	}
	if len(p) >= limit {
		p = p[len(p)-limit:]
		b.b.Reset()
		b.truncated = true
	} else if b.b.Len()+len(p) > limit {
		drop := b.b.Len() + len(p) - limit
		old := b.b.Bytes()
		b.b.Reset()
		_, _ = b.b.Write(old[drop:])
		b.truncated = true
	}
	return b.b.Write(p)
}
func (b *tailBuffer) String() string { b.mu.Lock(); defer b.mu.Unlock(); return b.b.String() }

func (b *tailBuffer) snapshot() (string, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String(), b.truncated
}

func commandOutput(ctx context.Context, path, command string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = agentapi.SanitizedEnv(os.Environ())
	cmd.Dir = os.TempDir()
	cmd.Dir = os.TempDir()
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func classifyStart(cfg Config, err error, stderr string) error {
	msg := clip(strings.TrimSpace(err.Error()+" "+stderr), 400)
	low := strings.ToLower(msg)
	switch {
	case strings.Contains(low, "auth_required") || strings.Contains(low, "unauthorized") || strings.Contains(low, "not authenticated") || strings.Contains(low, "authentication"):
		guidance := "Run `gemini` in a terminal and complete its sign-in flow."
		if cfg.ID == "opencode" {
			guidance = "Run `opencode auth login` in a terminal for the selected provider."
		}
		return fmt.Errorf("%s needs authentication: %s (%s)", cfg.Name, msg, guidance)
	case strings.Contains(low, "model") && (strings.Contains(low, "not found") || strings.Contains(low, "not available") || strings.Contains(low, "invalid")):
		return fmt.Errorf("that model is not available in %s: %s", cfg.Name, msg)
	case strings.Contains(low, "connection refused") || strings.Contains(low, "127.0.0.1") || strings.Contains(low, "localhost"):
		return fmt.Errorf("%s could not reach the configured local model endpoint: %s", cfg.Name, msg)
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return err
	default:
		return fmt.Errorf("start %s ACP session: %s", cfg.Name, msg)
	}
}

func clip(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}

var _ io.Writer = (*tailBuffer)(nil)
