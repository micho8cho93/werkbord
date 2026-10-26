package agentkit

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
)

// ClaudeDocsURL is where Claude Code's own installation instructions live.
const ClaudeDocsURL = "https://docs.anthropic.com/en/docs/claude-code/setup"

var claude = base{
	id: "claude-code", name: "Claude Code", docs: ClaudeDocsURL, defaultCommand: "claude",
	install:   "Install Claude Code (https://claude.ai/install.sh, or `npm install -g @anthropic-ai/claude-code`; see " + ClaudeDocsURL + "), then run `claude auth login` to sign in.",
	reinstall: "Reinstall Claude Code: " + ClaudeDocsURL,
}

func probeClaude(ctx context.Context, command string) Facts {
	f, path, ok := locate(ctx, claude, command, false)
	if !ok {
		return f
	}
	// Older versions have no `auth status`; then nothing is known and the agent is assumed usable, so that a real failure
	// shows up when a session starts.
	out, err := Run(ctx, path, "auth", "status")
	if err != nil {
		return f
	}
	var st struct {
		LoggedIn     *bool  `json:"loggedIn"`
		AuthMethod   string `json:"authMethod"`
		APIKeySource string `json:"apiKeySource"`
	}
	if json.Unmarshal([]byte(out), &st) != nil || st.LoggedIn == nil {
		return f
	}
	f.SignedIn, f.Auth, f.UsesAPIKey = st.LoggedIn, st.AuthMethod, st.APIKeySource != ""
	if !*st.LoggedIn {
		signedOut(&f, command, "auth login", "Claude account")
	}
	return f
}

// ClaudeModels are Claude Code's own aliases. An alias always means the latest model of that family, so the list does
// not go stale the way model names do; a full model name can still be typed in.
func ClaudeModels() []Option {
	return []Option{
		{ID: "sonnet", Name: "Sonnet", Description: "Claude Sonnet, latest"},
		{ID: "opus", Name: "Opus", Description: "Claude Opus, latest"},
		{ID: "haiku", Name: "Haiku", Description: "Claude Haiku, latest"},
	}
}

// Option is a model or reasoning level that can be chosen.
type Option struct{ ID, Name, Description string }

// effortRE finds the reasoning levels in the CLI's own help for --effort, e.g.
// "--effort <level>  Effort level for the current session (low, medium, high)".
var effortRE = regexp.MustCompile(`(?s)--effort\s+<[^>]+>.*?\(([a-z, ]+)\)`)

// EffortLevels reads the reasoning levels --effort takes out of Claude Code's `--help`. nil means it has no such flag
// (an older version): the CLI knows, and its list changes, so it is asked rather than hard-coded.
func EffortLevels(help string) []string {
	m := effortRE.FindStringSubmatch(help)
	if m == nil {
		return nil
	}
	var out []string
	for _, l := range strings.Split(m[1], ",") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}
