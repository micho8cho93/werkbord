package agentkit

import (
	"context"
	"strings"
)

// CodexDocsURL is where Codex's own documentation lives.
const CodexDocsURL = "https://developers.openai.com/codex/cli"

var codex = base{
	id: "codex", name: "Codex", docs: CodexDocsURL, defaultCommand: "codex",
	install:   "Install Codex (`npm install -g @openai/codex` or `brew install --cask codex`; see " + CodexDocsURL + "), then run `codex login` to sign in with your ChatGPT account.",
	reinstall: "Reinstall Codex: " + CodexDocsURL,
}

func probeCodex(ctx context.Context, command string) Facts {
	f, path, ok := locate(ctx, codex, command, true)
	if !ok {
		return f
	}
	// `codex login status` says "Logged in using ChatGPT" and exits 0 when signed in, and exits non-zero saying "Not
	// logged in" when nobody is. Any other failure (an older version without the command) is not evidence of anything.
	out, err := RunCombined(ctx, path, "login", "status")
	lower := strings.ToLower(out)
	switch {
	case err != nil && strings.Contains(lower, "not logged in"):
		no := false
		f.SignedIn = &no
		signedOut(&f, command, "login", "ChatGPT account")
	case err == nil && strings.Contains(lower, "logged in"):
		yes := true
		f.SignedIn = &yes
		if _, after, found := strings.Cut(out, "using "); found {
			f.Auth = strings.TrimSpace(after)
			f.UsesAPIKey = strings.Contains(strings.ToLower(f.Auth), "api key")
		}
	}
	return f
}
