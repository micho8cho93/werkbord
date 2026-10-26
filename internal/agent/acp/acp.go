// Package acp adapts ACP-compatible command-line coding agents to the
// controller's interactive agent contract.
package acp

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	agentapi "werkbord/internal/agent"
	"werkbord/internal/domain"

	acp "github.com/caelis-labs/acp-go-sdk"
	"github.com/caelis-labs/acp-go-sdk/transport/stdio"
)

// Config is the safe, product-level configuration shared by Gemini CLI and
// OpenCode. Provider-specific defaults are supplied by the constructors in
// their own packages.
type Config struct {
	ID, Name, Command, DocsURL string
	Args                       []string
	ModelFlag                  string
	SessionOptions             bool
	Model                      string
	Models                     []domain.AgentOption
	Reasoning                  []string
	ListModels                 bool
	Guidance                   string
	DiscoverOptions            bool
}

type Adapter struct {
	cfg      Config
	mu       sync.Mutex
	detected domain.Agent
	detectAt time.Time
	optMu    sync.Mutex
	options  domain.AgentOptions
	optAt    time.Time
}

var _ agentapi.Adapter = (*Adapter)(nil)

func New(cfg Config) *Adapter { return &Adapter{cfg: cfg} }
func (a *Adapter) ID() string { return a.cfg.ID }

var versionRE = regexp.MustCompile(`\d+\.\d+\.\d+[^\s]*`)
var modelIDRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/\[\]+@-]{0,99}$`)

func (a *Adapter) Detect(ctx context.Context) domain.Agent {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.detectAt.IsZero() && time.Since(a.detectAt) < 30*time.Second {
		return a.detected
	}
	info := domain.Agent{ID: a.cfg.ID, Name: a.cfg.Name, DocsURL: a.cfg.DocsURL, SignIn: domain.SignInUnknown}
	path, err := exec.LookPath(a.cfg.Command)
	if err != nil {
		info.Detail = fmt.Sprintf("%q was not found on PATH", a.cfg.Command)
		info.Guidance = a.guidance()
		a.detected, a.detectAt = info, time.Now()
		return info
	}
	out, err := commandOutput(ctx, path, a.cfg.Command, "--version")
	if err != nil {
		info.Detail = "could not run `" + a.cfg.Command + " --version`: " + clip(err.Error(), 240)
		info.Guidance = "Reinstall or update " + a.cfg.Name + ": " + a.cfg.DocsURL
		a.detected, a.detectAt = info, time.Now()
		return info
	}
	info.Installed, info.Available = true, true
	info.Version = versionRE.FindString(out)
	a.detected, a.detectAt = info, time.Now()
	return info
}

func (a *Adapter) guidance() string {
	if a.cfg.Guidance != "" {
		return a.cfg.Guidance + " " + a.cfg.DocsURL
	}
	if a.cfg.ID == "gemini-cli" {
		return "Install Gemini CLI and complete sign-in from a terminal. Werkbord reuses its existing configuration: " + a.cfg.DocsURL
	}
	return "Install OpenCode and run `opencode auth login` for a configured provider. Werkbord reuses its existing configuration: " + a.cfg.DocsURL
}

func (a *Adapter) Options(ctx context.Context) domain.AgentOptions {
	a.optMu.Lock()
	defer a.optMu.Unlock()
	if !a.optAt.IsZero() && time.Since(a.optAt) < 30*time.Second {
		return cloneOptions(a.options)
	}
	o := domain.AgentOptions{AgentID: a.cfg.ID, Models: append([]domain.AgentOption(nil), a.cfg.Models...), Reasoning: stringOptions(a.cfg.Reasoning), CustomModels: true}
	if a.cfg.ListModels {
		if path, err := exec.LookPath(a.cfg.Command); err == nil {
			if out, err := commandOutput(ctx, path, a.cfg.Command, "models"); err == nil {
				if listed := parseModels(out); len(listed) > 0 {
					o.Models = listed
					o.ModelsSource = domain.OptionsFromAgent
				}
			} else {
				o.Note = "OpenCode could not list models just now; configured and typed model names are still available."
			}
		} else {
			o.Note = "OpenCode is not installed; configured and typed model names are still available."
		}
	}
	if o.ModelsSource == "" {
		if len(a.cfg.Models) > 0 {
			o.ModelsSource = domain.OptionsConfigured
		} else {
			o.ModelsSource = domain.OptionsBuiltIn
		}
	}
	if len(a.cfg.Reasoning) > 0 {
		o.ReasoningSource = domain.OptionsConfigured
	} else {
		o.ReasoningSource = domain.OptionsFromAgent
	}
	if a.cfg.DiscoverOptions {
		if discovered, err := a.discover(ctx); err == nil {
			for i := range o.Models {
				o.Models[i].ReasoningKnown = true
				for _, model := range discovered.Models {
					if o.Models[i].ID == model.ID {
						o.Models[i].Reasoning = append([]string(nil), model.Reasoning...)
					}
				}
			}
			if len(discovered.Models) > 0 && len(a.cfg.Models) == 0 {
				o.Models = discovered.Models
				o.ModelsSource = domain.OptionsFromAgent
			}
			if len(a.cfg.Reasoning) == 0 {
				o.Reasoning = discovered.Reasoning
				o.ReasoningSource = domain.OptionsFromAgent
			}
		} else {
			o.Note = "Model discovery unavailable: " + clip(err.Error(), 180) + ". Agent default remains available."
		}
	}
	o = o.WithAgentDefault()
	a.options, a.optAt = cloneOptions(o), time.Now()
	return o
}

func cloneOptions(o domain.AgentOptions) domain.AgentOptions {
	o.Models = append([]domain.AgentOption(nil), o.Models...)
	o.Reasoning = append([]domain.AgentOption(nil), o.Reasoning...)
	for i := range o.Models {
		o.Models[i].Reasoning = append([]string(nil), o.Models[i].Reasoning...)
	}
	return o
}

func stringOptions(in []string) []domain.AgentOption {
	out := make([]domain.AgentOption, 0, len(in))
	for _, id := range in {
		out = append(out, domain.AgentOption{ID: id, Name: id})
	}
	return out
}

func parseModels(out string) []domain.AgentOption {
	seen := map[string]bool{}
	var models []domain.AgentOption
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		id := strings.Trim(fields[0], "`*•")
		if !strings.Contains(id, "/") || !modelIDRE.MatchString(id) || seen[id] {
			continue
		}
		seen[id] = true
		models = append(models, domain.AgentOption{ID: id, Name: id})
	}
	return models
}

func (a *Adapter) Start(ctx context.Context, req agentapi.StartRequest) (agentapi.Session, error) {
	info := a.Detect(ctx)
	if !info.Available {
		return nil, fmt.Errorf("%s is unavailable: %s. %s", a.cfg.Name, info.Detail, info.Guidance)
	}
	if req.WorkDir == "" {
		return nil, fmt.Errorf("%s requires a run worktree", a.cfg.Name)
	}
	root, err := filepath.EvalSymlinks(req.WorkDir)
	if err != nil {
		return nil, fmt.Errorf("resolve run worktree: %w", err)
	}
	path, err := exec.LookPath(a.cfg.Command)
	if err != nil {
		return nil, fmt.Errorf("%q was not found on PATH", a.cfg.Command)
	}
	args := append([]string(nil), a.cfg.Args...)
	model := req.Model
	if model == domain.AgentDefault {
		model = ""
	}
	if model == "" {
		model = a.cfg.Model
	}
	if a.cfg.ModelFlag != "" && model != "" {
		args = append(args, a.cfg.ModelFlag, model)
	}
	processCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	c := &runClient{root: root, questions: map[string]*permission{}, terminals: map[string]*terminal{}, approvals: map[string]int{}}
	child, err := stdio.StartClient(processCtx, c, stdio.Command{Executable: path, Args: args, Dir: root, Env: agentapi.SanitizedEnv(os.Environ()), Stderr: &c.stderr}, acp.ConnectionOptions{})
	if err != nil {
		cancel()
		return nil, classifyStart(a.cfg, err, c.stderr.String())
	}
	s := &session{adapter: a, root: root, cfg: a.cfg, proc: child, client: c, ctx: processCtx, cancel: cancel, stream: agentapi.NewEventStream(), sessionID: "", busy: make(chan struct{}, 1), done: make(chan struct{})}
	c.owner = s
	conn := child.Connection
	startCtx, startCancel := context.WithTimeout(ctx, 60*time.Second)
	defer startCancel()
	initialized, err := conn.Initialize(startCtx, acp.InitializeRequest{
		ProtocolVersion: acp.ProtocolVersion(acp.WireProtocolVersion),
		ClientInfo:      &acp.Implementation{Name: "Werkbord", Version: "1"},
		ClientCapabilities: acp.ClientCapabilities{
			Fs:       acp.FileSystemCapabilities{ReadTextFile: true, WriteTextFile: true},
			Terminal: true,
		},
	})
	if err != nil {
		s.stopFailedStart()
		return nil, classifyStart(a.cfg, err, c.stderr.String())
	}
	if initialized.ProtocolVersion != acp.ProtocolVersion(acp.WireProtocolVersion) {
		s.stopFailedStart()
		return nil, fmt.Errorf("%s selected an unsupported ACP protocol version", a.cfg.Name)
	}
	mcpServers := []acp.McpServer{}
	var sessionID acp.SessionId
	var options []acp.SessionConfigOption
	if req.ResumeRef != "" {
		if !initialized.AgentCapabilities.LoadSession {
			s.stopFailedStart()
			return nil, fmt.Errorf("%s does not support session recovery", a.cfg.Name)
		}
		loaded, loadErr := conn.LoadSession(startCtx, acp.LoadSessionRequest{Cwd: root, SessionId: acp.SessionId(req.ResumeRef), McpServers: mcpServers})
		if loadErr != nil {
			s.stopFailedStart()
			return nil, classifyStart(a.cfg, loadErr, c.stderr.String())
		}
		sessionID, options = acp.SessionId(req.ResumeRef), loaded.ConfigOptions
	} else {
		created, createErr := conn.NewSession(startCtx, acp.NewSessionRequest{Cwd: root, McpServers: mcpServers})
		if createErr != nil {
			s.stopFailedStart()
			return nil, classifyStart(a.cfg, createErr, c.stderr.String())
		}
		sessionID, options = created.SessionId, created.ConfigOptions
	}
	if sessionID == "" {
		s.stopFailedStart()
		return nil, fmt.Errorf("%s returned no session id", a.cfg.Name)
	}
	if a.cfg.SessionOptions {
		if model != "" {
			if err := setOption(startCtx, conn, sessionID, &options, "model", model); err != nil {
				s.stopFailedStart()
				return nil, fmt.Errorf("%s does not support model %q: %w", a.cfg.Name, model, err)
			}
		}
		if req.Reasoning != "" && req.Reasoning != domain.AgentDefault {
			if err := setOption(startCtx, conn, sessionID, &options, "effort", req.Reasoning); err != nil {
				s.stopFailedStart()
				return nil, fmt.Errorf("%s does not support reasoning level %q: %w", a.cfg.Name, req.Reasoning, err)
			}
		}
	}
	s.sessionID = sessionID
	s.Emit(agentapi.Event{Kind: agentapi.KindSessionRef, SessionRef: string(sessionID)})
	go s.supervise()
	if err := s.beginPrompt(ctx, req.Prompt); err != nil {
		s.stopFailedStart()
		return nil, fmt.Errorf("send the first message to %s: %w", a.cfg.Name, err)
	}
	return s, nil
}

func setOption(ctx context.Context, c *acp.ClientSideConnection, sessionID acp.SessionId, options *[]acp.SessionConfigOption, id, value string) error {
	found := false
	for _, option := range *options {
		v := option.Select
		if v == nil {
			continue
		}
		category := string(v.Id)
		if v.Category != nil {
			category = string(*v.Category)
		}
		matches := string(v.Id) == id || category == id || id == "effort" && (category == "thought_level" || category == "reasoning" || category == "reasoning_effort" || strings.Contains(strings.ToLower(string(v.Id)), "reasoning"))
		if matches {
			id = string(v.Id)
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("ACP session did not advertise a %s choice", id)
	}
	response, err := c.SetSessionConfigOption(ctx, acp.SetSessionConfigOptionRequest{ValueId: &acp.SetSessionConfigOptionValueId{SessionId: sessionID, ConfigId: acp.SessionConfigId(id), Value: acp.SessionConfigValueId(value)}})
	if err == nil {
		*options = response.ConfigOptions
	}
	return err
}

type session struct {
	adapter   *Adapter
	root      string
	cfg       Config
	proc      *stdio.ClientProcess
	client    *runClient
	ctx       context.Context
	cancel    context.CancelFunc
	stream    *agentapi.EventStream
	sessionID acp.SessionId
	busy      chan struct{}
	done      chan struct{}
	finish    sync.Once
	result    agentapi.Result
	resultMu  sync.Mutex
}

func (s *session) Events() <-chan agentapi.Event { return s.stream.Events() }
func (s *session) Emit(ev agentapi.Event)        { s.stream.Emit(ev) }
func (s *session) Process() agentapi.ProcessInfo { return agentapi.ProcessInfo{} }

func (s *session) beginPrompt(ctx context.Context, text string) error {
	select {
	case s.busy <- struct{}{}:
	default:
		return fmt.Errorf("%s is already processing a prompt", s.cfg.Name)
	}
	go func() {
		defer func() { <-s.busy }()
		_, err := s.proc.Connection.Prompt(s.ctx, acp.PromptRequest{SessionId: s.sessionID, Prompt: []acp.ContentBlock{acp.TextBlock(text)}})
		if err != nil && s.ctx.Err() == nil {
			s.Emit(agentapi.Event{Kind: agentapi.KindOutput, Stream: domain.StreamSystem, Text: s.cfg.Name + " could not finish the turn: " + clip(err.Error(), 300)})
		}
		s.Emit(agentapi.Event{Kind: agentapi.KindTurnEnd})
	}()
	return nil
}

func (s *session) Send(ctx context.Context, text string) error {
	if err := s.checkAlive(); err != nil {
		return err
	}
	return s.beginPrompt(ctx, text)
}

func (s *session) Respond(_ context.Context, ref, answer string) error {
	return s.client.respond(ref, answer)
}

func (s *session) Close(ctx context.Context) error {
	if s.checkAlive() != nil {
		return agentapi.ErrEnded
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if s.client.hasPendingQuestions() {
		return fmt.Errorf("cannot close %s while a permission request is waiting", s.cfg.Name)
	}
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	return s.proc.Shutdown(shutdownCtx)
}

func (s *session) Stop(ctx context.Context) error {
	if s.checkAlive() != nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	_ = s.proc.Connection.Cancel(context.WithoutCancel(ctx), acp.CancelNotification{SessionId: s.sessionID})
	for _, t := range s.client.terminalsSnapshot() {
		_ = t.proc.Close()
	}
	stopCtx := ctx
	if stopCtx == nil {
		stopCtx = context.Background()
	}
	err := s.proc.Shutdown(stopCtx)
	s.cancel()
	select {
	case <-s.done:
	case <-ctx.Done():
		return ctx.Err()
	}
	return err
}

func (s *session) Wait() agentapi.Result {
	<-s.done
	s.resultMu.Lock()
	defer s.resultMu.Unlock()
	return s.result
}

func (s *session) checkAlive() error {
	select {
	case <-s.done:
		return agentapi.ErrEnded
	default:
		return nil
	}
}

func (s *session) supervise() {
	err := s.proc.Wait(context.Background())
	for _, t := range s.client.terminalsSnapshot() {
		_ = t.proc.Close()
	}
	res := agentapi.Result{State: domain.RunCompleted, ExitCode: 0}
	if err != nil {
		res.State, res.ExitCode, res.Reason = domain.RunFailed, -1, clip(err.Error(), 500)
		if strings.Contains(strings.ToLower(err.Error()), "exit status 0") {
			res.State, res.ExitCode, res.Reason = domain.RunCompleted, 0, ""
		}
	}
	s.finish.Do(func() {
		s.resultMu.Lock()
		s.result = res
		s.resultMu.Unlock()
		if res.State == domain.RunFailed {
			s.Emit(agentapi.Event{Kind: agentapi.KindOutput, Stream: domain.StreamSystem, Text: s.cfg.Name + " ended: " + res.Reason})
		}
		close(s.done)
		s.stream.Close()
		s.cancel()
	})
}

func (s *session) stopFailedStart() {
	s.cancel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = s.proc.Shutdown(ctx)
}
