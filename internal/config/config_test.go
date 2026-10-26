package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadPrecedence(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("WERKBORD_DATA_DIR", dir)
	t.Setenv("WERKBORD_LOG_LEVEL", "debug")
	if err := os.WriteFile(filepath.Join(dir, "config.json"),
		[]byte(`{"addr":"127.0.0.1:9999","logLevel":"warn","shutdownTimeout":"3s"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Addr != "127.0.0.1:9999" || c.LogLevel != "debug" || c.ShutdownTimeout.Duration != 3*time.Second || c.DataDir != dir {
		t.Fatalf("unexpected config: %+v", c)
	}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
}

var (
	loopbackAddrs    = []string{"127.0.0.1:7420", "localhost:7420", "[::1]:7420"}
	nonLoopbackAddrs = []string{"0.0.0.0:7420", "192.168.1.2:80", ":7420"}
)

// The API can start processes on this computer, so it is authenticated unless
// someone deliberately turns that off.
func TestTokenIsRequiredByDefault(t *testing.T) {
	if !Default().RequireToken {
		t.Error("Default().RequireToken = false")
	}
	for _, addr := range append(append([]string{}, loopbackAddrs...), nonLoopbackAddrs...) {
		c := Default()
		c.Addr = addr
		if !c.AuthRequired() {
			t.Errorf("AuthRequired(%s) = false with the default configuration", addr)
		}
	}
}

// Opting out is for loopback only: a controller reachable from the network
// is never unauthenticated, whatever the setting says.
func TestOptingOutNeverAppliesOffLoopback(t *testing.T) {
	for _, addr := range loopbackAddrs {
		c := Default()
		c.Addr, c.RequireToken = addr, false
		if c.AuthRequired() {
			t.Errorf("AuthRequired(%s) = true after opting out", addr)
		}
	}
	for _, addr := range nonLoopbackAddrs {
		c := Default()
		c.Addr, c.RequireToken = addr, false
		if !c.AuthRequired() {
			t.Errorf("AuthRequired(%s) = false: opting out must not expose the network", addr)
		}
	}
}

func TestRequireTokenSources(t *testing.T) {
	cases := []struct {
		name    string
		file    string // contents of config.json, "" for none
		env     string
		want    bool
		wantErr bool
	}{
		{"nothing set", "", "", true, false},
		{"file false", `{"requireToken": false}`, "", false, false},
		{"file true", `{"requireToken": true}`, "", true, false},
		{"file without the key", `{"logLevel": "warn"}`, "", true, false},
		{"env false", "", "false", false, false},
		{"env 0", "", "0", false, false},
		{"env true", "", "true", true, false},
		{"env 1", "", "1", true, false},
		{"env beats file: on", `{"requireToken": false}`, "true", true, false},
		{"env beats file: off", `{"requireToken": true}`, "false", false, false},
		// Not understanding a setting must never quietly mean "unauthenticated".
		{"env nonsense", "", "off", true, true},
		{"env nonsense 2", "", "disabled", true, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("WERKBORD_DATA_DIR", dir)
			t.Setenv("WERKBORD_REQUIRE_TOKEN", c.env)
			if c.file != "" {
				if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(c.file), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			cfg, err := Load()
			if c.wantErr {
				if err == nil || !strings.Contains(err.Error(), "WERKBORD_REQUIRE_TOKEN") {
					t.Fatalf("Load err = %v, want an error naming WERKBORD_REQUIRE_TOKEN", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if cfg.RequireToken != c.want {
				t.Errorf("RequireToken = %v, want %v", cfg.RequireToken, c.want)
			}
		})
	}
}

func TestClientAddr(t *testing.T) {
	c := Default()
	c.Addr = "0.0.0.0:7420"
	if got := c.ClientAddr(); got != "127.0.0.1:7420" {
		t.Fatalf("ClientAddr = %s", got)
	}
}

func TestResolveTokenGeneratesOnce(t *testing.T) {
	c := Default()
	c.DataDir = t.TempDir()
	if tok, err := c.ResolveToken(false); err != nil || tok != "" {
		t.Fatalf("without create: %q, %v", tok, err)
	}
	a, err := c.ResolveToken(true)
	if err != nil || len(a) != 64 {
		t.Fatalf("generated %q, %v", a, err)
	}
	b, _ := c.ResolveToken(true)
	if a != b {
		t.Fatal("token should be stable once generated")
	}
	info, _ := os.Stat(c.TokenPath())
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("token file mode = %v", info.Mode().Perm())
	}
}

func TestAgentSettingsAreLoadedFromTheConfigFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("WERKBORD_DATA_DIR", dir)
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{
		"worktreesDir": "/srv/worktrees",
		"agents": {
			"claude-code": {"command": "/opt/claude", "model": "opus", "permissionMode": "plan"},
			"codex": {"approvalPolicy": "untrusted", "sandbox": "read-only"}
		}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	cl, cx := c.Agents[AgentClaudeCode], c.Agents[AgentCodex]
	if cl.Command != "/opt/claude" || cl.Model != "opus" || cl.PermissionMode != "plan" || cx.ApprovalPolicy != "untrusted" || cx.Sandbox != "read-only" {
		t.Fatalf("agents = %+v", c.Agents)
	}
	if c.WorktreesPath() != "/srv/worktrees" {
		t.Fatalf("worktrees = %s", c.WorktreesPath())
	}
}

func TestWorktreesDefaultToTheDataDirectory(t *testing.T) {
	c := Default()
	c.DataDir = "/data/devboard"
	if got := c.WorktreesPath(); got != "/data/devboard/worktrees" {
		t.Fatalf("WorktreesPath = %s", got)
	}
	t.Setenv("WERKBORD_WORKTREES_DIR", "/elsewhere")
	t.Setenv("WERKBORD_DATA_DIR", t.TempDir())
	loaded, err := Load()
	if err != nil || loaded.WorktreesPath() != "/elsewhere" {
		t.Fatalf("env override: %v, %v", loaded.WorktreesPath(), err)
	}
}

func TestInvalidAgentSettingsAreRejected(t *testing.T) {
	cases := map[string]Config{
		"unknown agent":           {Agents: map[string]AgentConfig{"gemini": {}}},
		"bad claude mode":         {Agents: map[string]AgentConfig{AgentClaudeCode: {PermissionMode: "yolo"}}},
		"codex setting on claude": {Agents: map[string]AgentConfig{AgentClaudeCode: {Sandbox: "read-only"}}},
		"claude setting on codex": {Agents: map[string]AgentConfig{AgentCodex: {PermissionMode: "plan"}}},
		"bad codex approval":      {Agents: map[string]AgentConfig{AgentCodex: {ApprovalPolicy: "sometimes"}}},
		"bad codex sandbox":       {Agents: map[string]AgentConfig{AgentCodex: {Sandbox: "open"}}},
		"Gemini reasoning":        {Agents: map[string]AgentConfig{AgentGeminiCLI: {Reasoning: []string{"low"}}}},
	}
	for name, extra := range cases {
		t.Run(name, func(t *testing.T) {
			c := Default()
			c.Agents = extra.Agents
			if err := c.Validate(); err == nil {
				t.Fatal("expected an error: a typo in a safety setting must not be silently ignored")
			}
		})
	}
	c := Default()
	c.WorktreesDir = "relative/path"
	if err := c.Validate(); err == nil {
		t.Fatal("a relative worktrees directory must be rejected")
	}
}

func TestRiskyAgentSettingsAreReported(t *testing.T) {
	c := Default()
	if got := c.RiskyAgentSettings(); len(got) != 0 {
		t.Fatalf("defaults are not risky: %v", got)
	}
	c.Agents = map[string]AgentConfig{
		AgentClaudeCode: {PermissionMode: "bypassPermissions"},
		AgentCodex:      {ApprovalPolicy: "never", Sandbox: "danger-full-access"},
	}
	if got := c.RiskyAgentSettings(); len(got) != 3 {
		t.Fatalf("risky = %v", got)
	}
	c.Agents = map[string]AgentConfig{AgentClaudeCode: {PermissionMode: "acceptEdits"}, AgentCodex: {ApprovalPolicy: "on-request", Sandbox: "workspace-write"}}
	if got := c.RiskyAgentSettings(); len(got) != 0 {
		t.Fatalf("risky = %v", got)
	}
}

// The environment overrides the defaults.
func TestWerkbordVariablesOverrideTheDefaults(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("WERKBORD_DATA_DIR", dir)
	t.Setenv("WERKBORD_LOG_LEVEL", "warn")
	t.Setenv("WERKBORD_ADDR", "127.0.0.1:7999")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.DataDir != dir || c.LogLevel != "warn" || c.Addr != "127.0.0.1:7999" {
		t.Fatalf("got data %q, level %q, addr %q", c.DataDir, c.LogLevel, c.Addr)
	}
	t.Setenv("WERKBORD_REQUIRE_TOKEN", "maybe")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "WERKBORD_REQUIRE_TOKEN") {
		t.Fatalf("err = %v, want one naming the variable that was set", err)
	}
}

func TestTheDataDirectoryIsWerkbordUnderTheConfigDirectory(t *testing.T) {
	base := t.TempDir()
	if got := DataDirIn(base); got != filepath.Join(base, "werkbord") {
		t.Fatalf("data dir: %s", got)
	}
}

// LoadIn is for a program that was not started from a terminal: it knows where the
// installed service keeps its data, and that wins over the environment.
func TestLoadInUsesTheGivenDataDirAndStillReadsItsConfig(t *testing.T) {
	envDir, dir := t.TempDir(), t.TempDir()
	t.Setenv("WERKBORD_DATA_DIR", envDir)
	t.Setenv("WERKBORD_LOG_LEVEL", "debug")
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"addr":"127.0.0.1:7431"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(envDir, "config.json"), []byte(`{"addr":"127.0.0.1:9999"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := LoadIn(dir)
	if err != nil {
		t.Fatal(err)
	}
	if c.DataDir != dir || c.Addr != "127.0.0.1:7431" || c.LogLevel != "debug" {
		t.Fatalf("LoadIn = %+v: want the given data dir and its config.json, with the rest of the environment applied", c)
	}
	if c.DBPath() != filepath.Join(dir, "devboard.db") {
		t.Fatalf("the database is at %s: a second independent install", c.DBPath())
	}
}

func TestLoadInIgnoresADataDirWrittenInTheConfigFile(t *testing.T) {
	dir, other := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"dataDir":"`+other+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := LoadIn(dir)
	if err != nil || c.DataDir != dir {
		t.Fatalf("LoadIn = %+v, %v: the data dir is where the config was found", c, err)
	}
}

func TestSignInURLCarriesTheTokenInTheFragmentOnly(t *testing.T) {
	dir := t.TempDir()
	c := Default()
	c.DataDir, c.Addr = dir, "127.0.0.1:7421"
	// No token yet: the link is the address, and nothing is created by asking.
	u, err := c.SignInURL()
	if err != nil || u != "http://127.0.0.1:7421/" {
		t.Fatalf("SignInURL without a token = %q, %v", u, err)
	}
	if _, err := os.Stat(c.TokenPath()); err == nil {
		t.Fatal("SignInURL created a token")
	}
	tok, err := c.ResolveToken(true)
	if err != nil {
		t.Fatal(err)
	}
	u, err = c.SignInURL()
	if err != nil || u != "http://127.0.0.1:7421/#token="+tok {
		t.Fatalf("SignInURL = %q, %v", u, err)
	}
	if i := strings.IndexByte(u, '?'); i >= 0 {
		t.Fatalf("the token must never be in a query string, which a server would see: %s", u)
	}
	// A controller that does not ask for a token is not given one.
	c.RequireToken = false
	if u, _ = c.SignInURL(); u != "http://127.0.0.1:7421/" {
		t.Fatalf("with authentication off the link still carries a token: %q", u)
	}
}

func TestNoUpdateCheckFromFileAndEnvironment(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("WERKBORD_DATA_DIR", dir)
	if c, err := Load(); err != nil || c.NoUpdateCheck {
		t.Fatalf("default: %+v, %v: looking for updates is on unless turned off", c.NoUpdateCheck, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"noUpdateCheck":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if c, err := Load(); err != nil || !c.NoUpdateCheck {
		t.Fatalf("config.json: %v, %v", c.NoUpdateCheck, err)
	}
	t.Setenv("WERKBORD_NO_UPDATE_CHECK", "maybe")
	if _, err := Load(); err == nil {
		t.Fatal("a value that is not a boolean was accepted for a setting that stops something")
	}
}

func TestEmbedOriginsAreExactLoopbackOriginsFromTheEnvironmentOrTheFile(t *testing.T) {
	t.Setenv("WERKBORD_EMBED_ORIGINS", "http://127.0.0.1:5173")
	c, err := Load()
	if err != nil || len(c.EmbedOrigins) != 1 || c.EmbedOrigins[0] != "http://127.0.0.1:5173" {
		t.Fatalf("%+v %v", c.EmbedOrigins, err)
	}
	t.Setenv("WERKBORD_EMBED_ORIGINS", "https://evil.example")
	if _, err := Load(); err == nil {
		t.Fatal("a non-loopback origin was accepted from the environment")
	}
	t.Setenv("WERKBORD_EMBED_ORIGINS", "")
	c = Default()
	c.EmbedOrigins = []string{"https://evil.example"}
	if err := c.Validate(); err == nil {
		t.Fatal("a non-loopback origin was accepted from config.json")
	}
}

func TestAssistantSettingsAreLoadedAndChecked(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("WERKBORD_DATA_DIR", dir)
	write := func(body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(`{"assistant": {"readOnly": true, "projects": ["prj_abc123"], "turnTimeoutSeconds": 120, "idleTimeoutSeconds": 30, "auditRetentionDays": 90}}`)
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	a := c.Assistant
	if !a.ReadOnly || a.Disabled || len(a.Projects) != 1 || a.TurnTimeoutSeconds != 120 || a.IdleTimeoutSeconds != 30 || a.AuditRetentionDays != 90 {
		t.Fatalf("assistant = %+v", a)
	}
	if d := Default().Assistant; d.Disabled || d.ReadOnly || len(d.Projects) != 0 {
		t.Fatalf("by default the assistant is on, can propose changes, and sees every project: %+v", d)
	}
	for name, body := range map[string]string{
		"not a project id":     `{"assistant": {"projects": ["../../etc"]}}`,
		"project with a space": `{"assistant": {"projects": ["prj_a b"]}}`,
		"negative timeout":     `{"assistant": {"turnTimeoutSeconds": -1}}`,
		"huge timeout":         `{"assistant": {"idleTimeoutSeconds": 100000}}`,
		"negative retention":   `{"assistant": {"attachmentRetentionMinutes": -5}}`,
		"retention over a day": `{"assistant": {"attachmentRetentionMinutes": 1441}}`,
	} {
		write(body)
		c, err := Load()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "assistant.") {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestAuditRetentionForEverIsAllowed(t *testing.T) {
	c := Default()
	c.Assistant.AuditRetentionDays = -1
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestAttachmentSettingsAreLoadedAndDefaultToHalfAnHourWithFilesOn(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("WERKBORD_DATA_DIR", dir)
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"assistant": {"attachmentRetentionMinutes": 10, "attachmentsDisabled": true}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if c.Assistant.AttachmentRetentionMinutes != 10 || !c.Assistant.AttachmentsDisabled {
		t.Fatalf("assistant = %+v", c.Assistant)
	}
	if d := Default().Assistant; d.AttachmentRetentionMinutes != 0 || d.AttachmentsDisabled {
		t.Fatalf("by default files are accepted and the engine's own 30 minutes applies: %+v", d)
	}
}

func TestRelayNeedsBothAddressesAndHttps(t *testing.T) {
	for _, tc := range []struct {
		name string
		r    RelayConfig
		ok   bool
	}{
		{"neither", RelayConfig{}, true},
		{"both", RelayConfig{URL: "https://relay.example.workers.dev", ConnectorURL: "https://connect.example.pages.dev"}, true},
		{"localhost for trying it", RelayConfig{URL: "http://localhost:8787", ConnectorURL: "http://127.0.0.1:8788"}, true},
		{"only the relay", RelayConfig{URL: "https://relay.example.workers.dev"}, false},
		{"only the connector", RelayConfig{ConnectorURL: "https://connect.example.pages.dev"}, false},
		{"plain http elsewhere", RelayConfig{URL: "http://relay.example.com", ConnectorURL: "https://connect.example.pages.dev"}, false},
		{"a path", RelayConfig{URL: "https://relay.example.com/v1", ConnectorURL: "https://connect.example.pages.dev"}, false},
		{"a query", RelayConfig{URL: "https://relay.example.com/?a=1", ConnectorURL: "https://connect.example.pages.dev"}, false},
	} {
		c := Default()
		c.Relay = tc.r
		if err := c.Validate(); (err == nil) != tc.ok {
			t.Errorf("%s: %v", tc.name, err)
		}
	}
	if (RelayConfig{URL: "https://a.example"}).Enabled() {
		t.Error("a relay with no connector cannot make a link")
	}
}

func TestTheRelayThatShipsIsUsedUnlessYouSetOneOrTurnItOff(t *testing.T) {
	own := RelayConfig{URL: "https://relay.example.workers.dev", ConnectorURL: "https://connect.example.pages.dev"}
	for _, tc := range []struct {
		name   string
		in     RelayConfig
		inTest bool
		want   RelayConfig
	}{
		{"nothing set", RelayConfig{}, false, RelayConfig{URL: DefaultRelayURL, ConnectorURL: DefaultConnectorURL}},
		{"your own is kept", own, false, own},
		{"turned off", RelayConfig{Disabled: true}, false, RelayConfig{Disabled: true}},
		{"half a pair is left for validation to refuse", RelayConfig{URL: own.URL}, false, RelayConfig{URL: own.URL}},
		{"a test never gets it", RelayConfig{}, true, RelayConfig{}},
	} {
		if got := tc.in.builtIn(tc.inTest); got != tc.want {
			t.Errorf("%s: %+v, want %+v", tc.name, got, tc.want)
		}
	}
	if !(RelayConfig{URL: DefaultRelayURL, ConnectorURL: DefaultConnectorURL}).Enabled() {
		t.Error("the built-in pair makes a relay")
	}
	if (RelayConfig{URL: DefaultRelayURL, ConnectorURL: DefaultConnectorURL, Disabled: true}).Enabled() {
		t.Error("disabled wins over addresses")
	}
	c := Default()
	c.Relay = RelayConfig{URL: DefaultRelayURL, ConnectorURL: DefaultConnectorURL}
	if err := c.Validate(); err != nil {
		t.Errorf("the built-in addresses must validate: %v", err)
	}
}
