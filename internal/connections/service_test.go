package connections

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
	"werkbord/internal/domain"
	"werkbord/internal/store/sqlite"
)

type memorySecrets struct {
	mu     sync.Mutex
	values map[string]string
}

func (m *memorySecrets) Get(k string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.values[k]
	if !ok {
		return "", os.ErrNotExist
	}
	return v, nil
}
func (m *memorySecrets) Set(k, v string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.values[k] = v
	return nil
}
func (m *memorySecrets) Delete(k string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.values, k)
	return nil
}

type auditEntry struct{ actor, operation, outcome, hash, detail string }
type memoryAudit struct {
	mu      sync.Mutex
	entries []auditEntry
}

func (m *memoryAudit) RecordMCPCall(_ context.Context, actor, operation string, _ domain.AuditKind, outcome, hash, detail string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries = append(m.entries, auditEntry{actor, operation, outcome, hash, detail})
	return nil
}

func TestConnectionBrokerKeepsCredentialPrivateAndGatesCalls(t *testing.T) {
	ctx := context.Background()
	db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "connections.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	secrets := &memorySecrets{values: map[string]string{}}
	audit := &memoryAudit{}
	svc := New(db, secrets, audit, nil)
	var calls atomic.Int32
	var seenAuth atomic.Value
	server := mcp.NewServer(&mcp.Implementation{Name: "fixture", Version: "1"}, nil)
	server.AddTool(&mcp.Tool{Name: "search", Title: "Search", Description: "Find records", InputSchema: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}`), Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}}, func(_ context.Context, _ *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		calls.Add(1)
		return &mcp.CallToolResult{StructuredContent: map[string]any{"matches": 1}}, nil
	})
	server.AddResource(&mcp.Resource{URI: "calendar://today", Name: "Today", MIMEType: "text/plain"}, func(_ context.Context, _ *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: "calendar://today", MIMEType: "text/plain", Text: "meeting"}}}, nil
	})
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{JSONResponse: true})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h := r.Header.Get("Authorization"); h != "" {
			seenAuth.Store(h)
		}
		handler.ServeHTTP(w, r)
	}))
	defer upstream.Close()
	c, err := svc.Save(ctx, SaveInput{Name: "Calendar", Transport: TransportHTTP, URL: upstream.URL, AuthMode: "bearer", BearerToken: "do-not-return-this"})
	if err != nil {
		t.Fatal(err)
	}
	if c.Enabled || c.Assistant || c.Coding {
		t.Fatalf("new connection was not disabled by default: %+v", c)
	}
	encoded, _ := json.Marshal(c)
	if strings.Contains(string(encoded), "do-not-return-this") {
		t.Fatalf("credential appeared in API metadata: %s", encoded)
	}
	if got, _ := secrets.Get("connection/" + c.ID + "/bearer"); got != "do-not-return-this" {
		t.Fatal("credential was not kept in the secret store")
	}
	c, err = svc.Test(ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Tools) != 1 || len(c.Resources) != 1 {
		t.Fatalf("discovery incomplete: %+v", c)
	}
	if c.Tools[0].Assistant != toolPolicyAsk || c.Tools[0].Coding != toolPolicyAsk {
		t.Fatalf("tool did not default to confirm: %+v", c.Tools[0])
	}
	c, err = svc.SetEnabled(ctx, c.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	c, err = svc.SetContext(ctx, c.ID, contextAssistant, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.CallTool(ctx, c.ID, "search", contextAssistant, map[string]any{"query": "launch"}, false); err != ErrConfirmationRequired {
		t.Fatalf("default policy call error = %v", err)
	}
	if calls.Load() != 0 {
		t.Fatal("confirmation-required call reached the upstream")
	}
	c, err = svc.SetToolPolicy(ctx, c.ID, "search", contextAssistant, toolPolicyAllow)
	if err != nil {
		t.Fatal(err)
	}
	result, err := svc.CallTool(ctx, c.ID, "search", contextAssistant, map[string]any{"query": "launch"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.(map[string]any)["matches"] != float64(1) {
		t.Fatalf("tool result = %#v", result)
	}
	if seenAuth.Load() != "Bearer do-not-return-this" {
		t.Fatalf("upstream authorization = %v", seenAuth.Load())
	}
	resource, err := svc.ReadResource(ctx, c.ID, contextAssistant, "calendar://today")
	if err != nil {
		t.Fatal(err)
	}
	contents := resource.([]*mcp.ResourceContents)
	if len(contents) != 1 || contents[0].Text != "meeting" {
		t.Fatalf("resource result = %#v", resource)
	}
	if len(audit.entries) < 5 {
		t.Fatalf("calls and outcomes were not all audited: %+v", audit.entries)
	}
	for _, entry := range audit.entries {
		if strings.Contains(entry.detail, "do-not-return-this") || strings.Contains(entry.hash, "do-not-return-this") {
			t.Fatalf("credential in audit: %+v", entry)
		}
	}
}

func TestChangedToolSchemaInvalidatesAllowGrantBeforeCalling(t *testing.T) {
	ctx := context.Background()
	db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "schema.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := New(db, &memorySecrets{values: map[string]string{}}, nil, nil)
	var calls atomic.Int32
	server := mcp.NewServer(&mcp.Implementation{Name: "fixture", Version: "1"}, nil)
	addTool := func(description string) {
		server.AddTool(&mcp.Tool{Name: "write", Description: description, InputSchema: json.RawMessage(`{"type":"object"}`)}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			calls.Add(1)
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "done"}}}, nil
		})
	}
	addTool("first schema")
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{JSONResponse: true})
	upstream := httptest.NewServer(handler)
	defer upstream.Close()
	c, err := svc.Save(ctx, SaveInput{Name: "Writer", Transport: TransportHTTP, URL: upstream.URL})
	if err != nil {
		t.Fatal(err)
	}
	c, err = svc.Test(ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.SetEnabled(ctx, c.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.SetContext(ctx, c.ID, contextCoding, true)
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.SetToolPolicy(ctx, c.ID, "write", contextCoding, toolPolicyAllow)
	if err != nil {
		t.Fatal(err)
	}
	addTool("changed schema")
	if _, err = svc.CallTool(ctx, c.ID, "write", contextCoding, map[string]any{}, false); err == nil || !strings.Contains(err.Error(), "schema changed") {
		t.Fatalf("changed schema call error = %v", err)
	}
	if calls.Load() != 0 {
		t.Fatal("changed tool was called before re-review")
	}
	c, err = svc.get(ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if c.Tools[0].Coding != toolPolicyAsk {
		t.Fatalf("automatic grant survived schema change: %+v", c.Tools[0])
	}
}

func TestOAuthAuthorizationAndRefreshStayInTheSecretStore(t *testing.T) {
	ctx := context.Background()
	db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "oauth.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	secrets := &memorySecrets{values: map[string]string{}}
	var upstream *httptest.Server
	var authorizationServer *httptest.Server
	var codeChallenge string
	var refreshes atomic.Int32
	var lastAccess atomic.Value
	authMux := http.NewServeMux()
	var authURL string
	authMux.HandleFunc("/.well-known/oauth-authorization-server", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(&oauthex.AuthServerMeta{
			Issuer: authURL, AuthorizationEndpoint: authURL + "/authorize", TokenEndpoint: authURL + "/token",
			JWKSURI: authURL + "/jwks", RegistrationEndpoint: authURL + "/register",
			ResponseTypesSupported: []string{"code"}, CodeChallengeMethodsSupported: []string{"S256"},
			TokenEndpointAuthMethodsSupported: []string{"none"}, AuthorizationResponseIssParameterSupported: true,
		})
	})
	authMux.HandleFunc("/register", func(w http.ResponseWriter, r *http.Request) {
		var metadata oauthex.ClientRegistrationMetadata
		if err := json.NewDecoder(r.Body).Decode(&metadata); err != nil {
			t.Errorf("decode dynamic registration: %v", err)
			http.Error(w, "invalid metadata", http.StatusBadRequest)
			return
		}
		if len(metadata.RedirectURIs) != 1 || metadata.RedirectURIs[0] != "http://127.0.0.1:14567/callback" {
			t.Errorf("OAuth registered an unexpected redirect URI: %+v", metadata.RedirectURIs)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"client_id": "werkbord-test-client", "token_endpoint_auth_method": "none"})
	})
	authMux.HandleFunc("/authorize", func(w http.ResponseWriter, r *http.Request) {
		codeChallenge = r.URL.Query().Get("code_challenge")
		if r.URL.Query().Get("code_challenge_method") != "S256" || codeChallenge == "" {
			t.Errorf("OAuth authorization did not use PKCE: %s", r.URL.RawQuery)
		}
		redirect, err := url.Parse(r.URL.Query().Get("redirect_uri"))
		if err != nil {
			t.Errorf("invalid OAuth redirect URI: %v", err)
			http.Error(w, "invalid redirect", http.StatusBadRequest)
			return
		}
		query := redirect.Query()
		query.Set("code", "test-authorization-code")
		query.Set("state", r.URL.Query().Get("state"))
		query.Set("iss", authURL)
		redirect.RawQuery = query.Encode()
		http.Redirect(w, r, redirect.String(), http.StatusFound)
	})
	authMux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse token form: %v", err)
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}
		switch r.Form.Get("grant_type") {
		case "authorization_code":
			sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
			if base64.RawURLEncoding.EncodeToString(sum[:]) != codeChallenge || r.Form.Get("code") != "test-authorization-code" {
				t.Errorf("OAuth token request did not validate the PKCE exchange")
				http.Error(w, "invalid PKCE", http.StatusBadRequest)
				return
			}
			writeOAuthToken(w, "access-initial", "refresh-test")
		case "refresh_token":
			if r.Form.Get("refresh_token") != "refresh-test" {
				t.Errorf("OAuth refresh token was not restored from storage")
				http.Error(w, "invalid refresh", http.StatusBadRequest)
				return
			}
			refreshes.Add(1)
			writeOAuthToken(w, "access-refreshed", "refresh-test")
		default:
			t.Errorf("unexpected OAuth grant type %q", r.Form.Get("grant_type"))
			http.Error(w, "unsupported grant", http.StatusBadRequest)
		}
	})
	authorizationServer = httptest.NewServer(authMux)
	defer authorizationServer.Close()
	authURL = authorizationServer.URL
	upstreamMux := http.NewServeMux()
	var resourceServer *mcp.Server
	resourceServer = mcp.NewServer(&mcp.Implementation{Name: "oauth-fixture", Version: "1"}, nil)
	resourceServer.AddTool(&mcp.Tool{Name: "search", InputSchema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)}, func(_ context.Context, _ *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "ok"}}}, nil
	})
	mcpHandler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return resourceServer }, &mcp.StreamableHTTPOptions{JSONResponse: true})
	upstreamMux.HandleFunc("/.well-known/oauth-protected-resource/mcp", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(&oauthex.ProtectedResourceMetadata{Resource: upstream.URL + "/mcp", AuthorizationServers: []string{authURL}})
	})
	upstreamMux.Handle("/mcp", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		lastAccess.Store(token)
		if token != "access-initial" && token != "access-refreshed" {
			w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+upstream.URL+`/.well-known/oauth-protected-resource/mcp"`)
			http.Error(w, "sign in required", http.StatusUnauthorized)
			return
		}
		mcpHandler.ServeHTTP(w, r)
	}))
	upstream = httptest.NewServer(upstreamMux)
	defer upstream.Close()
	if _, err := oauthex.GetProtectedResourceMetadata(ctx, upstream.URL+"/.well-known/oauth-protected-resource/mcp", upstream.URL+"/mcp", http.DefaultClient); err != nil {
		t.Fatalf("fixture protected resource metadata is invalid: %v", err)
	}

	svc := New(db, secrets, nil, nil)
	svc.SetOAuthRedirectURL("http://127.0.0.1:14567/callback")
	svc.browserOpen = func(raw string) error {
		client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		resp, err := client.Get(raw)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		location, err := resp.Location()
		if err != nil {
			return err
		}
		if !svc.HandleOAuthCallback(location.Query()) {
			t.Error("authorization callback was not matched to the active flow")
		}
		return nil
	}
	connection, err := svc.Save(ctx, SaveInput{Name: "OAuth test", Transport: TransportHTTP, URL: upstream.URL + "/mcp", AuthMode: "oauth"})
	if err != nil {
		t.Fatal(err)
	}
	connection, err = svc.Test(ctx, connection.ID)
	if err != nil {
		t.Fatalf("OAuth connection test failed: %v", err)
	}
	if !connection.SecretStored || lastAccess.Load() != "access-initial" {
		t.Fatalf("initial OAuth token was not stored and used: stored=%v token=%v", connection.SecretStored, lastAccess.Load())
	}
	secret, err := secrets.Get("connection/" + connection.ID + "/oauth")
	if err != nil || strings.Contains(secret, "test-authorization-code") || strings.Contains(secret, "test-code-verifier") {
		t.Fatalf("OAuth credential storage is incomplete or retained temporary data: %s, %v", secret, err)
	}
	var saved savedOAuth
	if err := json.Unmarshal([]byte(secret), &saved); err != nil {
		t.Fatal(err)
	}
	saved.Token.Expiry = time.Now().Add(-time.Hour)
	encodedSaved, _ := json.Marshal(saved)
	if err := secrets.Set("connection/"+connection.ID+"/oauth", string(encodedSaved)); err != nil {
		t.Fatal(err)
	}
	connection, err = svc.Test(ctx, connection.ID)
	if err != nil {
		t.Fatalf("OAuth token refresh failed: %v", err)
	}
	if refreshes.Load() == 0 || lastAccess.Load() != "access-refreshed" {
		t.Fatalf("saved OAuth token was not refreshed: refreshes=%d access=%v", refreshes.Load(), lastAccess.Load())
	}
	secret, _ = secrets.Get("connection/" + connection.ID + "/oauth")
	if err := json.Unmarshal([]byte(secret), &saved); err != nil || saved.Token.AccessToken != "access-refreshed" {
		t.Fatalf("refreshed OAuth access token was not persisted: %+v, %v", saved.Token, err)
	}
}

func writeOAuthToken(w http.ResponseWriter, access, refresh string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"access_token": access, "token_type": "Bearer", "expires_in": 3600, "refresh_token": refresh})
}

func TestStdioMCPUsesOnlyConfiguredEnvironmentReferences(t *testing.T) {
	if os.Getenv("WERKBORD_CONNECTIONS_STDIO_HELPER") == "1" {
		server := mcp.NewServer(&mcp.Implementation{Name: "stdio-fixture", Version: "1"}, nil)
		server.AddTool(&mcp.Tool{Name: "inspect", InputSchema: json.RawMessage(`{"type":"object"}`)}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "ok"}}}, nil
		})
		_ = server.Run(context.Background(), &mcp.StdioTransport{})
		os.Exit(0)
	}
	t.Setenv("WERKBORD_CONNECTIONS_STDIO_HELPER_SOURCE", "1")
	t.Setenv("MCP_UNREFERENCED_SECRET", "must-not-be-inherited")
	ctx := context.Background()
	db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "stdio.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := New(db, &memorySecrets{values: map[string]string{}}, nil, nil)
	c, err := svc.Save(ctx, SaveInput{Name: "Local", Transport: TransportStdio, Command: os.Args[0], Args: []string{"-test.run=^TestStdioMCPUsesOnlyConfiguredEnvironmentReferences$"}, Environment: map[string]string{"WERKBORD_CONNECTIONS_STDIO_HELPER": "WERKBORD_CONNECTIONS_STDIO_HELPER_SOURCE"}})
	if err != nil {
		t.Fatal(err)
	}
	c, err = svc.Test(ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Tools) != 1 || c.Tools[0].Name != "inspect" {
		t.Fatalf("stdio discovery = %+v", c.Tools)
	}
}
