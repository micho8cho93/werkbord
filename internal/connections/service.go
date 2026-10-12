// Package connections owns user-configured upstream MCP connections for the Individual controller.
// The agent never receives an upstream credential or transport; calls pass through this broker.
package connections

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"devboard/internal/browser"
	"devboard/internal/domain"
	"devboard/internal/store"
	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
	"github.com/zalando/go-keyring"
	"golang.org/x/oauth2"
)

const (
	settingKey          = "mcp_connections_v1"
	maxTools            = 500
	maxResources        = 500
	maxResultBytes      = 1 << 20
	maxEventBytes       = 1 << 20
	maxInputSchemaBytes = 64 << 10
	connectTimeout      = 30 * time.Second
	callTimeout         = 30 * time.Second
	resourceTimeout     = 15 * time.Second
	oauthTestTimeout    = 5 * time.Minute
	toolPolicyDeny      = "deny"
	toolPolicyAsk       = "confirm"
	toolPolicyAllow     = "allow"
	contextAssistant    = "assistant"
	contextCoding       = "coding"
)

var (
	envNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)
	validIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,80}$`)
)

// PermissionAudit records only a digest of arguments and a bounded outcome; it must never receive a credential.
type PermissionAudit interface {
	RecordMCPCall(context.Context, string, string, domain.AuditKind, string, string, string) error
}

// SecretStore is the OS credential-storage boundary.
type SecretStore interface {
	Get(string) (string, error)
	Set(string, string) error
	Delete(string) error
}

type Keyring struct{}

func (Keyring) Get(key string) (string, error) { return keyring.Get("Werkbord MCP", key) }
func (Keyring) Set(key, value string) error    { return keyring.Set("Werkbord MCP", key, value) }
func (Keyring) Delete(key string) error        { return keyring.Delete("Werkbord MCP", key) }

// Transport names the upstream MCP transport.
type Transport string

const (
	TransportStdio Transport = "stdio"
	TransportHTTP  Transport = "streamable_http"
)

// Connection contains metadata only. Credentials are kept by SecretStore or referenced by environment variable name.
type Connection struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Transport    Transport         `json:"transport"`
	Command      string            `json:"command,omitempty"`
	Args         []string          `json:"args,omitempty"`
	URL          string            `json:"url,omitempty"`
	Environment  map[string]string `json:"environment,omitempty"` // child variable => controller variable name
	AuthMode     string            `json:"authMode,omitempty"`    // none, bearer, environment, oauth
	AuthEnv      string            `json:"authEnv,omitempty"`
	SecretStored bool              `json:"secretStored,omitempty"`
	Enabled      bool              `json:"enabled"`
	Assistant    bool              `json:"assistantEnabled"`
	Coding       bool              `json:"codingEnabled"`
	Tools        []Tool            `json:"tools,omitempty"`
	Resources    []Resource        `json:"resources,omitempty"`
	TestedAt     time.Time         `json:"testedAt,omitempty"`
	LastError    string            `json:"lastError,omitempty"`
}

type Tool struct {
	Name        string          `json:"name"`
	Title       string          `json:"title,omitempty"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"inputSchema,omitempty"`
	Fingerprint string          `json:"fingerprint"`
	Assistant   string          `json:"assistantPolicy"`
	Coding      string          `json:"codingPolicy"`
	// These are untrusted hints from the server. They are shown as information only.
	ReadOnlyHint bool `json:"readOnlyHint,omitempty"`
}

type Resource struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	MIMEType    string `json:"mimeType,omitempty"`
}

type state struct {
	Connections []Connection `json:"connections"`
}

// SaveInput is accepted by the settings API. The secret is write-only and never appears in Connection.
type SaveInput struct {
	ID          string            `json:"id,omitempty"`
	Name        string            `json:"name"`
	Transport   Transport         `json:"transport"`
	Command     string            `json:"command,omitempty"`
	Args        []string          `json:"args,omitempty"`
	URL         string            `json:"url,omitempty"`
	Environment map[string]string `json:"environment,omitempty"`
	AuthMode    string            `json:"authMode,omitempty"`
	AuthEnv     string            `json:"authEnv,omitempty"`
	BearerToken string            `json:"bearerToken,omitempty"`
}

// Service stores connection metadata, owns credentials and creates short-lived broker sessions.
type Service struct {
	store         store.Store
	secrets       SecretStore
	audit         PermissionAudit
	log           *slog.Logger
	mu            sync.Mutex
	oauthMu       sync.Mutex
	oauthFlows    map[string]chan oauthResult
	oauthRedirect string
	browserOpen   func(string) error
}

type oauthResult struct{ code, state, issuer, err string }

type savedOAuth struct {
	ClientID     string           `json:"clientId"`
	ClientSecret string           `json:"clientSecret,omitempty"`
	AuthURL      string           `json:"authUrl"`
	TokenURL     string           `json:"tokenUrl"`
	RedirectURL  string           `json:"redirectUrl,omitempty"`
	Scopes       []string         `json:"scopes,omitempty"`
	AuthStyle    oauth2.AuthStyle `json:"authStyle,omitempty"`
	Token        oauth2.Token     `json:"token"`
}

type tokenPersistSource struct {
	source oauth2.TokenSource
	save   func(*oauth2.Token)
}

func (s tokenPersistSource) Token() (*oauth2.Token, error) {
	t, err := s.source.Token()
	if err == nil {
		s.save(t)
	}
	return t, err
}

func New(st store.Store, secrets SecretStore, audit PermissionAudit, log *slog.Logger) *Service {
	if secrets == nil {
		secrets = Keyring{}
	}
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Service{store: st, secrets: secrets, audit: audit, log: log, oauthFlows: map[string]chan oauthResult{}, browserOpen: browser.Open}
}

// SetOAuthRedirectURL sets the loopback callback handled by the controller's API listener.
func (s *Service) SetOAuthRedirectURL(rawURL string) {
	s.oauthMu.Lock()
	s.oauthRedirect = rawURL
	s.oauthMu.Unlock()
}

// HandleOAuthCallback forwards an in-flight authorization response only when its SDK-generated state is active.
func (s *Service) HandleOAuthCallback(values url.Values) bool {
	state := values.Get("state")
	if state == "" {
		return false
	}
	s.oauthMu.Lock()
	ch := s.oauthFlows[state]
	s.oauthMu.Unlock()
	if ch == nil {
		return false
	}
	result := oauthResult{code: values.Get("code"), state: state, issuer: values.Get("iss"), err: values.Get("error")}
	select {
	case ch <- result:
		return true
	default:
		return false
	}
}

func (s *Service) List(ctx context.Context) ([]Connection, error) {
	var v state
	if err := s.store.View(ctx, func(tx store.Tx) error {
		err := tx.Settings().Get(ctx, settingKey, &v)
		if errors.Is(err, domain.ErrNotFound) {
			return nil
		}
		return err
	}); err != nil {
		return nil, err
	}
	return cloneConnections(v.Connections), nil
}

func (s *Service) Save(ctx context.Context, in SaveInput) (Connection, error) {
	if err := validateInput(in); err != nil {
		return Connection{}, err
	}
	if in.AuthMode == "" {
		in.AuthMode = "none"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	items, err := s.listLocked(ctx)
	if err != nil {
		return Connection{}, err
	}
	idx := -1
	if in.ID != "" {
		for i := range items {
			if items[i].ID == in.ID {
				idx = i
				break
			}
		}
		if idx < 0 {
			return Connection{}, domain.ErrNotFound
		}
	}
	var old Connection
	if idx >= 0 {
		old = items[idx]
	}
	id := in.ID
	if id == "" {
		id = domain.NewID("mcp")
	}
	c := Connection{ID: id, Name: strings.TrimSpace(in.Name), Transport: in.Transport, Command: strings.TrimSpace(in.Command), Args: append([]string(nil), in.Args...), URL: strings.TrimSpace(in.URL), Environment: cloneMap(in.Environment), AuthMode: in.AuthMode, AuthEnv: in.AuthEnv,
		Assistant: old.Assistant, Coding: old.Coding, Enabled: old.Enabled, Tools: old.Tools, Resources: old.Resources, TestedAt: old.TestedAt, LastError: old.LastError, SecretStored: old.SecretStored}
	if c.Transport == TransportStdio {
		c.URL = ""
	} else {
		c.Command = ""
		c.Args = nil
		c.Environment = nil
	}
	if idx >= 0 && (!sameTarget(old, in) || in.BearerToken != "") {
		// A changed endpoint or identity must be rediscovered and reviewed before a model can use it again.
		c.Enabled, c.Assistant, c.Coding = false, false, false
		c.Tools, c.Resources, c.TestedAt, c.LastError = nil, nil, time.Time{}, ""
		if !sameTarget(old, in) && in.BearerToken == "" {
			c.SecretStored = false
		}
	}
	secretKey := "connection/" + id + "/bearer"
	previousSecret, previousErr := s.secrets.Get(secretKey)
	if in.BearerToken != "" {
		if c.AuthMode != "bearer" {
			return Connection{}, errors.New("a bearer token requires bearer authentication")
		}
		if err := s.secrets.Set(secretKey, in.BearerToken); err != nil {
			return Connection{}, fmt.Errorf("save bearer credential to the OS credential store: %w", err)
		}
		c.SecretStored = true
	}
	if c.AuthMode != "bearer" && c.AuthMode != "oauth" {
		c.SecretStored = false
	} else if c.AuthMode == "oauth" {
		c.SecretStored = s.hasOAuthCredential(id)
	}
	if idx >= 0 {
		items[idx] = c
	} else {
		items = append(items, c)
	}
	if err := s.saveLocked(ctx, items); err != nil {
		if in.BearerToken != "" {
			if previousErr == nil {
				_ = s.secrets.Set(secretKey, previousSecret)
			} else {
				_ = s.secrets.Delete(secretKey)
			}
		}
		return Connection{}, err
	}
	if idx >= 0 && old.SecretStored && (old.AuthMode != c.AuthMode || !sameTarget(old, in)) {
		oldKey := "connection/" + id + "/" + old.AuthMode
		if err := s.secrets.Delete(oldKey); err != nil && !errors.Is(err, keyring.ErrNotFound) {
			s.log.Warn("could not remove old MCP credential from OS storage", "connection", id, "err", err)
		}
	}
	return c, nil
}

func (s *Service) Delete(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	items, err := s.listLocked(ctx)
	if err != nil {
		return err
	}
	out := items[:0]
	found := false
	for _, c := range items {
		if c.ID == id {
			found = true
			continue
		}
		out = append(out, c)
	}
	if !found {
		return domain.ErrNotFound
	}
	if err := s.saveLocked(ctx, out); err != nil {
		return err
	}
	for _, kind := range []string{"bearer", "oauth"} {
		if err := s.secrets.Delete("connection/" + id + "/" + kind); err != nil && !errors.Is(err, keyring.ErrNotFound) {
			s.log.Warn("could not remove MCP credential from OS storage", "connection", id, "err", err)
		}
	}
	return nil
}

func (s *Service) SetEnabled(ctx context.Context, id string, enabled bool) (Connection, error) {
	return s.mutate(ctx, id, func(c *Connection) { c.Enabled = enabled })
}

func (s *Service) SetContext(ctx context.Context, id, kind string, enabled bool) (Connection, error) {
	if kind != contextAssistant && kind != contextCoding {
		return Connection{}, errors.New("context must be assistant or coding")
	}
	return s.mutate(ctx, id, func(c *Connection) {
		if kind == contextAssistant {
			c.Assistant = enabled
		} else {
			c.Coding = enabled
		}
	})
}

func (s *Service) SetToolPolicy(ctx context.Context, id, name, kind, policy string) (Connection, error) {
	if kind != contextAssistant && kind != contextCoding {
		return Connection{}, errors.New("context must be assistant or coding")
	}
	if policy != toolPolicyDeny && policy != toolPolicyAsk && policy != toolPolicyAllow {
		return Connection{}, errors.New("policy must be deny, confirm or allow")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	items, err := s.listLocked(ctx)
	if err != nil {
		return Connection{}, err
	}
	for i := range items {
		if items[i].ID != id {
			continue
		}
		for j := range items[i].Tools {
			if items[i].Tools[j].Name != name {
				continue
			}
			if kind == contextAssistant {
				items[i].Tools[j].Assistant = policy
			} else {
				items[i].Tools[j].Coding = policy
			}
			if err := s.saveLocked(ctx, items); err != nil {
				return Connection{}, err
			}
			return items[i], nil
		}
		return Connection{}, domain.ErrNotFound
	}
	return Connection{}, domain.ErrNotFound
}

func (s *Service) mutate(ctx context.Context, id string, fn func(*Connection)) (Connection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items, err := s.listLocked(ctx)
	if err != nil {
		return Connection{}, err
	}
	for i := range items {
		if items[i].ID == id {
			fn(&items[i])
			if err := s.saveLocked(ctx, items); err != nil {
				return Connection{}, err
			}
			return items[i], nil
		}
	}
	return Connection{}, domain.ErrNotFound
}

func (s *Service) Test(ctx context.Context, id string) (Connection, error) {
	c, err := s.get(ctx, id)
	if err != nil {
		return Connection{}, err
	}
	timeout := connectTimeout
	if c.AuthMode == "oauth" {
		timeout = oauthTestTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	session, closeSession, err := s.connect(ctx, c)
	var tools []Tool
	var resources []Resource
	if err == nil {
		tools, err = readTools(ctx, session)
		if err == nil {
			resources, err = readResources(ctx, session)
			if isMethodUnsupported(err) {
				err = nil
			}
		}
	}
	if closeSession != nil {
		closeSession()
	}
	if err != nil {
		err = s.redact(c, err)
		c.LastError = safeError(err)
		c.TestedAt = time.Now().UTC()
		_, _ = s.mutate(context.Background(), id, func(stored *Connection) { stored.LastError, stored.TestedAt = c.LastError, c.TestedAt })
		return c, err
	}
	old := map[string]Tool{}
	for _, tool := range c.Tools {
		old[tool.Name] = tool
	}
	for i := range tools {
		if prior, ok := old[tools[i].Name]; ok {
			tools[i].Assistant, tools[i].Coding = prior.Assistant, prior.Coding
			if prior.Fingerprint != tools[i].Fingerprint {
				if tools[i].Assistant == toolPolicyAllow {
					tools[i].Assistant = toolPolicyAsk
				}
				if tools[i].Coding == toolPolicyAllow {
					tools[i].Coding = toolPolicyAsk
				}
			}
		}
	}
	c.Tools, c.Resources, c.LastError, c.TestedAt = tools, resources, "", time.Now().UTC()
	if c.AuthMode == "oauth" {
		c.SecretStored = s.hasOAuthCredential(id)
	}
	c, err = s.mutate(ctx, id, func(stored *Connection) {
		stored.Tools, stored.Resources, stored.LastError, stored.TestedAt, stored.SecretStored = c.Tools, c.Resources, "", c.TestedAt, c.SecretStored
	})
	return c, err
}

// Reauthorize clears saved OAuth credentials and immediately starts a fresh sign-in and discovery.
func (s *Service) Reauthorize(ctx context.Context, id string) (Connection, error) {
	c, err := s.get(ctx, id)
	if err != nil {
		return Connection{}, err
	}
	if c.AuthMode != "oauth" {
		return Connection{}, errors.New("this connection does not use OAuth")
	}
	if err := s.secrets.Delete("connection/" + id + "/oauth"); err != nil && !errors.Is(err, keyring.ErrNotFound) && !errors.Is(err, os.ErrNotExist) {
		return Connection{}, errors.New("could not remove the saved OAuth credential from OS storage")
	}
	if _, err := s.mutate(ctx, id, func(stored *Connection) { stored.SecretStored = false; stored.LastError = "" }); err != nil {
		return Connection{}, err
	}
	return s.Test(ctx, id)
}

// CallTool executes through the broker. confirmed is set only by an explicit Werkbord approval action.
func (s *Service) CallTool(ctx context.Context, id, name, kind string, args map[string]any, confirmed bool) (any, error) {
	raw, err := json.Marshal(args)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(raw)
	argsHash := hex.EncodeToString(digest[:])
	c, err := s.get(ctx, id)
	if err != nil {
		s.record(ctx, id, name, "denied", argsHash, "connection was not found")
		return nil, err
	}
	if !c.Enabled {
		s.record(ctx, id, name, "denied", argsHash, "connection is disabled")
		return nil, errors.New("this MCP connection is disabled")
	}
	contextOn := (kind == contextAssistant && c.Assistant) || (kind == contextCoding && c.Coding)
	if !contextOn {
		s.record(ctx, id, name, "denied", argsHash, "connection is not enabled for this context")
		return nil, errors.New("this MCP connection is not enabled for this context")
	}
	var tool *Tool
	for i := range c.Tools {
		if c.Tools[i].Name == name {
			tool = &c.Tools[i]
			break
		}
	}
	if tool == nil {
		s.record(ctx, id, name, "denied", argsHash, "tool was not in the reviewed discovery")
		return nil, errors.New("this tool is not in the last reviewed discovery; test the connection again")
	}
	policy := tool.Assistant
	if kind == contextCoding {
		policy = tool.Coding
	}
	if policy == toolPolicyDeny {
		s.record(ctx, id, name, "denied", argsHash, "tool policy denies this call")
		return nil, errors.New("this MCP tool is denied")
	}
	if policy != toolPolicyAllow && !confirmed {
		s.record(ctx, id, name, "confirmation_required", argsHash, "tool call awaits an explicit Werkbord approval")
		return nil, ErrConfirmationRequired
	}
	if err := s.record(ctx, id, name, "requested", argsHash, "tool call requested through broker"); err != nil {
		return nil, err
	}
	callCtx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	session, closeSession, err := s.connect(callCtx, c)
	if err == nil {
		defer closeSession()
		liveTools, discoveryErr := readTools(callCtx, session)
		if discoveryErr != nil {
			err = discoveryErr
		} else {
			var live *Tool
			for i := range liveTools {
				if liveTools[i].Name == name {
					live = &liveTools[i]
					break
				}
			}
			if live == nil || live.Fingerprint != tool.Fingerprint {
				closeSession()
				_, _ = s.Test(context.WithoutCancel(ctx), id)
				return nil, errors.New("the upstream tool schema changed; review the updated discovery and permissions before calling it")
			}
		}
	}
	if err == nil {
		res, callErr := session.CallTool(callCtx, &mcp.CallToolParams{Name: name, Arguments: args})
		err = callErr
		if err == nil {
			data, marshalErr := json.Marshal(res)
			if marshalErr != nil {
				err = marshalErr
			} else if len(data) > maxResultBytes {
				err = ErrResultTooLarge
			} else if res.IsError {
				err = fmt.Errorf("upstream MCP tool returned an error: %s", clippedContent(res.Content))
			} else {
				err = s.record(ctx, id, name, "ok", argsHash, "tool call completed")
			}
			if err == nil {
				if res.StructuredContent != nil {
					return res.StructuredContent, nil
				}
				return res.Content, nil
			}
		}
	}
	if closeSession != nil {
		closeSession()
	}
	if err != nil {
		message := "tool call failed; its outcome may be unknown. Check the destination before retrying."
		if auditErr := s.record(context.Background(), id, name, "failed", argsHash, message); auditErr != nil {
			return nil, auditErr
		}
		return nil, fmt.Errorf("%s: %w", message, s.redact(c, err))
	}
	return nil, nil
}

func (s *Service) ReadResource(ctx context.Context, id, kind, uri string) (any, error) {
	c, err := s.get(ctx, id)
	if err != nil {
		return nil, err
	}
	if !c.Enabled || (kind == contextAssistant && !c.Assistant) || (kind == contextCoding && !c.Coding) {
		s.recordKind(ctx, id, "resources/read", domain.AuditRead, "denied", "", "connection is not enabled for this context")
		return nil, errors.New("this MCP connection is not enabled for this context")
	}
	found := false
	for _, r := range c.Resources {
		if r.URI == uri {
			found = true
			break
		}
	}
	if !found {
		s.recordKind(ctx, id, "resources/read", domain.AuditRead, "denied", "", "resource was not in the reviewed discovery")
		return nil, errors.New("resource was not discovered; test the connection again")
	}
	if err := s.recordKind(ctx, id, "resources/read", domain.AuditRead, "requested", "", "resource read requested through broker"); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, resourceTimeout)
	defer cancel()
	session, closeSession, err := s.connect(ctx, c)
	if err != nil {
		return nil, err
	}
	defer closeSession()
	res, err := session.ReadResource(ctx, &mcp.ReadResourceParams{URI: uri})
	if err != nil {
		_ = s.recordKind(context.Background(), id, "resources/read", domain.AuditRead, "failed", "", "resource read failed")
		return nil, s.redact(c, err)
	}
	data, err := json.Marshal(res)
	if err != nil {
		return nil, err
	}
	if len(data) > maxResultBytes {
		return nil, ErrResultTooLarge
	}
	if s.audit != nil {
		if err := s.recordKind(ctx, id, "resources/read", domain.AuditRead, "ok", "", "resource read"); err != nil {
			return nil, err
		}
	}
	return res.Contents, nil
}

func (s *Service) record(ctx context.Context, id, operation, outcome, argsHash, detail string) error {
	return s.recordKind(ctx, id, operation, domain.AuditMutation, outcome, argsHash, detail)
}

func (s *Service) recordKind(ctx context.Context, id, operation string, kind domain.AuditKind, outcome, argsHash, detail string) error {
	if s.audit == nil {
		return nil
	}
	return s.audit.RecordMCPCall(ctx, "mcp:"+id, operation, kind, outcome, argsHash, detail)
}

var (
	ErrConfirmationRequired = errors.New("this MCP tool needs confirmation in Werkbord before it runs")
	ErrResultTooLarge       = errors.New("the MCP result exceeded the 1 MiB limit")
)

func (s *Service) get(ctx context.Context, id string) (Connection, error) {
	items, err := s.List(ctx)
	if err != nil {
		return Connection{}, err
	}
	for _, c := range items {
		if c.ID == id {
			return c, nil
		}
	}
	return Connection{}, domain.ErrNotFound
}

func (s *Service) listLocked(ctx context.Context) ([]Connection, error) {
	var v state
	err := s.store.View(ctx, func(tx store.Tx) error {
		e := tx.Settings().Get(ctx, settingKey, &v)
		if errors.Is(e, domain.ErrNotFound) {
			return nil
		}
		return e
	})
	return v.Connections, err
}

func (s *Service) saveLocked(ctx context.Context, items []Connection) error {
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	return s.store.Update(ctx, func(tx store.Tx) error {
		return tx.Settings().Set(ctx, settingKey, state{Connections: items}, time.Now().UTC())
	})
}

func (s *Service) connect(ctx context.Context, c Connection) (*mcp.ClientSession, func(), error) {
	client := mcp.NewClient(&mcp.Implementation{Name: "werkbord-connection-broker", Version: "1"}, &mcp.ClientOptions{Capabilities: &mcp.ClientCapabilities{}})
	var tr mcp.Transport
	if c.Transport == TransportStdio {
		path, err := exec.LookPath(c.Command)
		if err != nil {
			return nil, nil, fmt.Errorf("MCP command %q is not installed: %w", c.Command, err)
		}
		cmd := exec.Command(path, c.Args...)
		cmd.Env = safeEnvironment(c.Environment)
		tr = &mcp.CommandTransport{Command: cmd, TerminateDuration: 2 * time.Second}
	} else {
		u, err := url.Parse(c.URL)
		if err != nil {
			return nil, nil, err
		}
		hc := &http.Client{Timeout: connectTimeout, CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return errors.New("too many redirects")
			}
			if !sameOrigin(u, req.URL) {
				return errors.New("MCP redirect changed origin; credentials were not forwarded")
			}
			return nil
		}}
		if c.AuthMode == "bearer" || c.AuthMode == "environment" {
			secret, err := s.authValue(c)
			if err != nil {
				return nil, nil, err
			}
			hc.Transport = bearerTransport{next: http.DefaultTransport, token: secret}
		}
		var oauthHandler auth.OAuthHandler
		if c.AuthMode == "oauth" {
			oauthHandler, err = s.oauthHandler(ctx, c)
			if err != nil {
				return nil, nil, err
			}
		}
		tr = &mcp.StreamableClientTransport{Endpoint: c.URL, HTTPClient: hc, OAuthHandler: oauthHandler, DisableStandaloneSSE: true, MaxRetries: 0, MaxEventSize: maxEventBytes}
	}
	session, err := client.Connect(ctx, tr, nil)
	if err != nil {
		return nil, nil, err
	}
	return session, func() { _ = session.Close() }, nil
}

func (s *Service) oauthHandler(ctx context.Context, c Connection) (auth.OAuthHandler, error) {
	s.oauthMu.Lock()
	redirect := s.oauthRedirect
	s.oauthMu.Unlock()
	if redirect == "" {
		return nil, errors.New("Werkbord's local OAuth callback is not ready; restart the controller and try again")
	}
	redirect = strings.TrimSuffix(redirect, "/")
	var saved *savedOAuth
	if raw, err := s.secrets.Get("connection/" + c.ID + "/oauth"); err == nil {
		var stored savedOAuth
		if json.Unmarshal([]byte(raw), &stored) != nil || stored.ClientID == "" || stored.Token.AccessToken == "" {
			return nil, errors.New("saved OAuth credentials could not be read from the OS credential store")
		}
		saved = &stored
	} else if !errors.Is(err, keyring.ErrNotFound) && !errors.Is(err, os.ErrNotExist) {
		return nil, errors.New("saved OAuth credentials are unavailable in the OS credential store")
	}
	config := &auth.AuthorizationCodeHandlerConfig{
		RedirectURL: redirect,
		Client:      &http.Client{Timeout: connectTimeout},
		DynamicClientRegistrationConfig: &auth.DynamicClientRegistrationConfig{Metadata: &oauthex.ClientRegistrationMetadata{
			RedirectURIs: []string{redirect}, TokenEndpointAuthMethod: "none", GrantTypes: []string{"authorization_code", "refresh_token"}, ResponseTypes: []string{"code"},
			ClientName: "Werkbord", ApplicationType: "native", SoftwareID: "werkbord", SoftwareVersion: "1",
		}},
		RequestRefreshToken: true,
		AuthorizationCodeFetcher: func(flowCtx context.Context, args *auth.AuthorizationArgs) (*auth.AuthorizationResult, error) {
			authURL, err := url.Parse(args.URL)
			if err != nil {
				return nil, errors.New("authorization URL was invalid")
			}
			state := authURL.Query().Get("state")
			if state == "" {
				return nil, errors.New("authorization request had no state")
			}
			result := make(chan oauthResult, 1)
			s.oauthMu.Lock()
			s.oauthFlows[state] = result
			s.oauthMu.Unlock()
			defer func() { s.oauthMu.Lock(); delete(s.oauthFlows, state); s.oauthMu.Unlock() }()
			if s.browserOpen == nil {
				return nil, errors.New("no browser is available for OAuth sign-in")
			}
			if err := s.browserOpen(args.URL); err != nil {
				return nil, fmt.Errorf("could not open the sign-in page: %w", err)
			}
			select {
			case response := <-result:
				if response.err != "" {
					return nil, fmt.Errorf("sign-in was declined (%s)", clip(response.err, 80))
				}
				if response.code == "" {
					return nil, errors.New("sign-in returned no authorization code")
				}
				return &auth.AuthorizationResult{Code: response.code, State: response.state, Iss: response.issuer}, nil
			case <-flowCtx.Done():
				return nil, errors.New("sign-in was cancelled before the callback arrived")
			}
		},
		NewTokenSource: func(tokenCtx context.Context, oauthConfig *oauth2.Config, token *oauth2.Token) (oauth2.TokenSource, error) {
			if oauthConfig == nil || token == nil {
				return nil, errors.New("OAuth returned no token configuration")
			}
			persist := func(updated *oauth2.Token) {
				if updated == nil {
					return
				}
				entry := savedOAuth{ClientID: oauthConfig.ClientID, ClientSecret: oauthConfig.ClientSecret, AuthURL: oauthConfig.Endpoint.AuthURL, TokenURL: oauthConfig.Endpoint.TokenURL, RedirectURL: oauthConfig.RedirectURL, Scopes: append([]string(nil), oauthConfig.Scopes...), AuthStyle: oauthConfig.Endpoint.AuthStyle, Token: *updated}
				raw, err := json.Marshal(entry)
				if err == nil {
					if err = s.secrets.Set("connection/"+c.ID+"/oauth", string(raw)); err != nil {
						s.log.Warn("could not save MCP OAuth token", "connection", c.ID, "err", err)
					}
				}
			}
			persist(token)
			return tokenPersistSource{source: oauthConfig.TokenSource(tokenCtx, token), save: persist}, nil
		},
	}
	if saved != nil {
		client := &oauthex.ClientCredentials{ClientID: saved.ClientID}
		if saved.ClientSecret != "" {
			client.ClientSecretAuth = &oauthex.ClientSecretAuth{ClientSecret: saved.ClientSecret}
		}
		config.PreregisteredClient = client
		prior := &oauth2.Config{ClientID: saved.ClientID, ClientSecret: saved.ClientSecret, RedirectURL: redirect, Scopes: append([]string(nil), saved.Scopes...), Endpoint: oauth2.Endpoint{AuthURL: saved.AuthURL, TokenURL: saved.TokenURL, AuthStyle: saved.AuthStyle}}
		config.InitialTokenSource = tokenPersistSource{source: prior.TokenSource(context.WithoutCancel(ctx), &saved.Token), save: func(updated *oauth2.Token) {
			if updated == nil {
				return
			}
			saved.Token = *updated
			raw, err := json.Marshal(saved)
			if err == nil {
				if err = s.secrets.Set("connection/"+c.ID+"/oauth", string(raw)); err != nil {
					s.log.Warn("could not refresh MCP OAuth credential", "connection", c.ID, "err", err)
				}
			}
		}}
	}
	return auth.NewAuthorizationCodeHandler(config)
}

func (s *Service) hasOAuthCredential(id string) bool {
	raw, err := s.secrets.Get("connection/" + id + "/oauth")
	if err != nil {
		return false
	}
	var saved savedOAuth
	return json.Unmarshal([]byte(raw), &saved) == nil && saved.Token.AccessToken != ""
}

func (s *Service) authValue(c Connection) (string, error) {
	switch c.AuthMode {
	case "bearer":
		v, err := s.secrets.Get("connection/" + c.ID + "/bearer")
		if err != nil {
			return "", errors.New("the saved bearer credential is unavailable in the OS credential store")
		}
		return v, nil
	case "environment":
		v := os.Getenv(c.AuthEnv)
		if v == "" {
			return "", fmt.Errorf("environment variable %s is empty", c.AuthEnv)
		}
		return v, nil
	default:
		return "", nil
	}
}

type bearerTransport struct {
	next  http.RoundTripper
	token string
}

func (b bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	c := req.Clone(req.Context())
	c.Header = c.Header.Clone()
	c.Header.Set("Authorization", "Bearer "+b.token)
	n := b.next
	if n == nil {
		n = http.DefaultTransport
	}
	return n.RoundTrip(c)
}

func safeEnvironment(refs map[string]string) []string {
	allowed := map[string]bool{"PATH": true, "HOME": true, "TMPDIR": true, "TMP": true, "TEMP": true, "SYSTEMROOT": true, "USERPROFILE": true}
	out := make([]string, 0, len(allowed)+len(refs))
	for _, kv := range os.Environ() {
		k, v, ok := strings.Cut(kv, "=")
		if ok && allowed[strings.ToUpper(k)] {
			out = append(out, k+"="+v)
		}
	}
	for child, source := range refs {
		if !envNamePattern.MatchString(child) || !envNamePattern.MatchString(source) {
			continue
		}
		if val := os.Getenv(source); val != "" {
			out = append(out, child+"="+val)
		}
	}
	sort.Strings(out)
	return out
}

func readTools(ctx context.Context, session *mcp.ClientSession) ([]Tool, error) {
	var out []Tool
	cursor := ""
	for page := 0; page < 20; page++ {
		res, err := session.ListTools(ctx, &mcp.ListToolsParams{Cursor: cursor})
		if err != nil {
			return nil, err
		}
		for _, t := range res.Tools {
			if len(out) >= maxTools {
				return nil, fmt.Errorf("MCP server returned more than %d tools", maxTools)
			}
			raw, err := json.Marshal(t.InputSchema)
			if err != nil {
				return nil, err
			}
			if len(raw) > maxInputSchemaBytes {
				return nil, fmt.Errorf("MCP tool %q has an input schema larger than %d KB", clip(t.Name, 80), maxInputSchemaBytes>>10)
			}
			if len(raw) == 0 || string(raw) == "null" {
				raw = json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)
			}
			annotations := t.Annotations
			tool := Tool{Name: t.Name, Title: t.Title, Description: clip(t.Description, 2000), InputSchema: raw, Assistant: toolPolicyAsk, Coding: toolPolicyAsk}
			if annotations != nil {
				tool.ReadOnlyHint = annotations.ReadOnlyHint
			}
			tool.Fingerprint = fingerprint(tool)
			out = append(out, tool)
		}
		cursor = res.NextCursor
		if cursor == "" {
			break
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func readResources(ctx context.Context, session *mcp.ClientSession) ([]Resource, error) {
	var out []Resource
	cursor := ""
	for page := 0; page < 20; page++ {
		res, err := session.ListResources(ctx, &mcp.ListResourcesParams{Cursor: cursor})
		if err != nil {
			return nil, err
		}
		for _, r := range res.Resources {
			if len(out) >= maxResources {
				return nil, fmt.Errorf("MCP server returned more than %d resources", maxResources)
			}
			out = append(out, Resource{URI: r.URI, Name: r.Name, Title: r.Title, Description: clip(r.Description, 2000), MIMEType: r.MIMEType})
		}
		cursor = res.NextCursor
		if cursor == "" {
			break
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].URI < out[j].URI })
	return out, nil
}

func fingerprint(t Tool) string {
	b, _ := json.Marshal(struct {
		Name, Title, Description string
		Schema                   json.RawMessage
		ReadOnly                 bool
	}{t.Name, t.Title, t.Description, t.InputSchema, t.ReadOnlyHint})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
func safeError(err error) string { return clip(err.Error(), 400) }

func (s *Service) redact(c Connection, err error) error {
	if err == nil {
		return nil
	}
	message := err.Error()
	if c.AuthMode == "bearer" {
		if token, e := s.secrets.Get("connection/" + c.ID + "/bearer"); e == nil && token != "" {
			message = strings.ReplaceAll(message, token, "[credential redacted]")
		}
	}
	if c.AuthMode == "environment" {
		if token := os.Getenv(c.AuthEnv); token != "" {
			message = strings.ReplaceAll(message, token, "[credential redacted]")
		}
	}
	if c.AuthMode == "oauth" {
		if raw, e := s.secrets.Get("connection/" + c.ID + "/oauth"); e == nil {
			var saved savedOAuth
			if json.Unmarshal([]byte(raw), &saved) == nil {
				for _, secret := range []string{saved.Token.AccessToken, saved.Token.RefreshToken, saved.ClientSecret} {
					if secret != "" {
						message = strings.ReplaceAll(message, secret, "[credential redacted]")
					}
				}
			}
		}
	}
	for _, source := range c.Environment {
		if token := os.Getenv(source); token != "" {
			message = strings.ReplaceAll(message, token, "[environment value redacted]")
		}
	}
	return errors.New(message)
}
func clip(v string, n int) string {
	if len(v) > n {
		return v[:n] + "…"
	}
	return v
}
func cloneMap(v map[string]string) map[string]string {
	if v == nil {
		return nil
	}
	o := map[string]string{}
	for k, x := range v {
		o[k] = x
	}
	return o
}
func cloneConnections(v []Connection) []Connection {
	o := make([]Connection, len(v))
	copy(o, v)
	for i := range o {
		o[i].Args = append([]string(nil), v[i].Args...)
		o[i].Environment = cloneMap(v[i].Environment)
		o[i].Tools = append([]Tool(nil), v[i].Tools...)
		o[i].Resources = append([]Resource(nil), v[i].Resources...)
	}
	return o
}
func sameOrigin(a, b *url.URL) bool {
	return strings.EqualFold(a.Scheme, b.Scheme) && strings.EqualFold(a.Host, b.Host)
}

func sameTarget(old Connection, in SaveInput) bool {
	if old.Transport != in.Transport || old.Command != strings.TrimSpace(in.Command) || old.URL != strings.TrimSpace(in.URL) || old.AuthMode != in.AuthMode || old.AuthEnv != in.AuthEnv {
		return false
	}
	if len(old.Args) != len(in.Args) {
		return false
	}
	for i := range old.Args {
		if old.Args[i] != in.Args[i] {
			return false
		}
	}
	if len(old.Environment) != len(in.Environment) {
		return false
	}
	for k, v := range old.Environment {
		if in.Environment[k] != v {
			return false
		}
	}
	return true
}
func isMethodUnsupported(err error) bool {
	return err != nil && (strings.Contains(strings.ToLower(err.Error()), "method not found") || strings.Contains(strings.ToLower(err.Error()), "-32601"))
}
func clippedContent(c []mcp.Content) string {
	var parts []string
	for _, x := range c {
		if t, ok := x.(*mcp.TextContent); ok {
			parts = append(parts, t.Text)
		}
	}
	return clip(strings.Join(parts, " "), 300)
}

func validateInput(in SaveInput) error {
	if name := strings.TrimSpace(in.Name); name == "" || len(name) > 100 {
		return errors.New("connection name must contain 1 to 100 characters")
	}
	if in.ID != "" && !validIDPattern.MatchString(in.ID) {
		return errors.New("invalid connection id")
	}
	switch in.Transport {
	case TransportStdio:
		if in.AuthMode != "" && in.AuthMode != "none" {
			return errors.New("local MCP processes use explicit environment references for authentication")
		}
		if strings.TrimSpace(in.Command) == "" || len(in.Command) > 512 {
			return errors.New("local MCP command is required")
		}
		if len(in.Args) > 64 {
			return errors.New("a local MCP command may have at most 64 arguments")
		}
		for _, a := range in.Args {
			if len(a) > 4096 {
				return errors.New("MCP command argument exceeds 4096 characters")
			}
		}
	case TransportHTTP:
		u, err := url.ParseRequestURI(strings.TrimSpace(in.URL))
		if err != nil || u.Host == "" || u.User != nil || u.Fragment != "" || u.RawQuery != "" {
			return errors.New("remote MCP URL must be an http(s) URL without credentials, query or fragment")
		}
		if u.Scheme != "https" && !(u.Scheme == "http" && isLoopbackHost(u.Hostname())) {
			return errors.New("remote MCP URLs must use HTTPS; HTTP is allowed only for loopback")
		}
	default:
		return errors.New("transport must be stdio or streamable_http")
	}
	if in.AuthMode == "" {
		in.AuthMode = "none"
	}
	if in.AuthMode != "none" && in.AuthMode != "bearer" && in.AuthMode != "environment" && in.AuthMode != "oauth" {
		return errors.New("authMode must be none, bearer, environment or oauth")
	}
	if in.AuthMode == "oauth" && in.Transport != TransportHTTP {
		return errors.New("OAuth authentication requires Streamable HTTP")
	}
	if in.AuthMode == "environment" && !envNamePattern.MatchString(in.AuthEnv) {
		return errors.New("authEnv must name an environment variable")
	}
	if in.AuthMode != "environment" && in.AuthEnv != "" {
		return errors.New("authEnv is only used with environment authentication")
	}
	if len(in.Environment) > 32 {
		return errors.New("a local MCP connection may use at most 32 environment references")
	}
	for child, source := range in.Environment {
		if !envNamePattern.MatchString(child) || !envNamePattern.MatchString(source) {
			return errors.New("environment references must use valid environment variable names")
		}
	}
	return nil
}

func isLoopbackHost(host string) bool {
	ip := net.ParseIP(host)
	if ip != nil {
		return ip.IsLoopback()
	}
	return strings.EqualFold(host, "localhost") || strings.HasSuffix(strings.ToLower(host), ".localhost")
}
