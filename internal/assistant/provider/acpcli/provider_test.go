package acpcli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"werkbord/internal/assistant/provider"
)

func TestOpenCodeModelsKeepConfiguredProviderQualifiedIDs(t *testing.T) {
	p := New(Config{ID: "opencode", Name: "OpenCode", Command: filepath.Join(t.TempDir(), "missing"), Models: []provider.Model{{ID: "openrouter/anthropic/claude-sonnet-4", Name: "Claude Sonnet"}}, ListModels: true, CustomModels: true})
	models := p.Models(context.Background())
	if models.Source != "configured" || len(models.Models) != 1 || models.Models[0].ID != "openrouter/anthropic/claude-sonnet-4" || models.Note == "" || !models.Custom {
		t.Fatalf("models = %+v", models)
	}
}

func TestGeminiAssistantEnvironmentDisablesRuntimeToolsAndExternalConnections(t *testing.T) {
	work := t.TempDir()
	t.Setenv("GEMINI_CLI_SYSTEM_SETTINGS_PATH", filepath.Join(t.TempDir(), "user-settings.json"))
	p := New(Config{ID: "gemini-cli", Name: "Gemini CLI"})
	env, err := p.assistantEnv(work)
	if err != nil {
		t.Fatal(err)
	}
	var value string
	for _, entry := range env {
		if strings.HasPrefix(entry, "GEMINI_CLI_SYSTEM_SETTINGS_PATH=") {
			value = strings.TrimPrefix(entry, "GEMINI_CLI_SYSTEM_SETTINGS_PATH=")
		}
	}
	if value == "" || value == os.Getenv("GEMINI_CLI_SYSTEM_SETTINGS_PATH") {
		t.Fatalf("isolated system settings path = %q", value)
	}
	data, err := os.ReadFile(value)
	if err != nil {
		t.Fatal(err)
	}
	var settings struct {
		Admin struct {
			MCP struct {
				Enabled bool `json:"enabled"`
			} `json:"mcp"`
			Extensions struct {
				Enabled bool `json:"enabled"`
			} `json:"extensions"`
			Skills struct {
				Enabled bool `json:"enabled"`
			} `json:"skills"`
		} `json:"admin"`
		Tools struct {
			Core []string `json:"core"`
		} `json:"tools"`
		Security struct {
			DisableYoloMode bool `json:"disableYoloMode"`
		} `json:"security"`
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatal(err)
	}
	if settings.Admin.MCP.Enabled || settings.Admin.Extensions.Enabled || settings.Admin.Skills.Enabled || settings.Tools.Core == nil || len(settings.Tools.Core) != 0 || !settings.Security.DisableYoloMode {
		t.Fatalf("assistant settings did not fail closed: %s", data)
	}
}

func TestOpenCodeAssistantEnvironmentReplacesUserConfiguration(t *testing.T) {
	work := t.TempDir()
	t.Setenv("OPENCODE_CONFIG_CONTENT", `{"mcp":{"personal":{"url":"https://example.invalid"}}}`)
	p := New(Config{ID: "opencode", Name: "OpenCode"})
	env, err := p.assistantEnv(work)
	if err != nil {
		t.Fatal(err)
	}
	var content string
	for _, entry := range env {
		if strings.HasPrefix(entry, "OPENCODE_CONFIG_CONTENT=") {
			content = strings.TrimPrefix(entry, "OPENCODE_CONFIG_CONTENT=")
		}
	}
	if content == "" || strings.Contains(content, "personal") {
		t.Fatalf("assistant inherited OpenCode configuration: %q", content)
	}
	var settings map[string]any
	if err := json.Unmarshal([]byte(content), &settings); err != nil {
		t.Fatal(err)
	}
	if settings["permission"] != "deny" || settings["mcp"] == nil || settings["plugin"] == nil {
		t.Fatalf("isolated OpenCode settings = %#v", settings)
	}
}
