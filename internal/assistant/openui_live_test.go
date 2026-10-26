package assistant

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"werkbord/internal/assistant/provider"
	"werkbord/internal/assistant/provider/claude"
	"werkbord/internal/assistant/provider/codex"
)

var liveOpenUIFence = regexp.MustCompile("(?s)```openui-lang\\s*\\n(.*?)\\n```")

// TestLiveOpenUIResponsesUseRegisteredComponents asks the installed providers to produce a small UI using the prompt
// generated from the real Svelte registry, then validates their output with OpenUI's installed parser and schema.
//
// It uses one short turn per available provider and runs only when explicitly requested:
//
//	WERKBORD_LIVE_ASSISTANT=1 go test ./internal/assistant -run TestLiveOpenUIResponsesUseRegisteredComponents -v
func TestLiveOpenUIResponsesUseRegisteredComponents(t *testing.T) {
	if os.Getenv("WERKBORD_LIVE_ASSISTANT") == "" {
		t.Skip("set WERKBORD_LIVE_ASSISTANT=1 to run against the real CLIs")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	prompt := generatedOpenUIPrompt(t, ctx)

	for _, p := range []provider.Provider{claude.New(claude.Config{}), codex.New(codex.Config{})} {
		t.Run(p.ID(), func(t *testing.T) {
			if info := p.Detect(ctx); !info.Available {
				t.Skipf("%s is not available here: %s", p.ID(), info.Detail)
			}

			model := ""
			if p.ID() == claude.ID {
				model = "haiku"
			} else {
				for _, option := range p.Models(ctx).Models {
					if option.Default {
						model = option.ID
					}
				}
			}

			var raw strings.Builder
			_, err := p.Run(ctx, provider.Turn{
				System:  prompt,
				Prompt:  "Create a compact project status dashboard. Use a heading titled Sprint snapshot, one metric labeled Open with value 4, and a two-column table with Task and State and one row Review API / Review. Include one short prose introduction outside the interface fence. These details are provided for this example; do not look up board data.",
				Model:   model,
				WorkDir: t.TempDir(),
			}, func(e provider.Event) {
				if e.Kind == provider.EventText {
					raw.WriteString(e.Text)
				}
			})
			if err != nil {
				t.Fatalf("provider turn: %v", err)
			}

			match := liveOpenUIFence.FindStringSubmatch(raw.String())
			if match == nil {
				t.Fatalf("provider did not return a fenced OpenUI interface; raw reply: %q", raw.String())
			}
			if err := parseGeneratedOpenUI(ctx, match[1]); err != nil {
				t.Fatalf("provider returned OpenUI that does not parse with the registered schema: %v\nraw reply: %q", err, raw.String())
			}
			t.Logf("validated %s OpenUI output (%d response bytes): %s", p.ID(), raw.Len(), strings.TrimSpace(raw.String()))
		})
	}
}

func generatedOpenUIPrompt(t *testing.T, ctx context.Context) string {
	t.Helper()
	const script = `import { createServer } from "vite";
const server = await createServer({ configFile: "vite.config.ts", logLevel: "silent", server: { middlewareMode: true }, appType: "custom" });
try {
  const mod = await server.ssrLoadModule("/src/lib/assistant/openui/library.ts");
  process.stdout.write(mod.openUISystemPrompt);
} finally { await server.close(); }`
	cmd := exec.CommandContext(ctx, "node", "--input-type=module", "-e", script)
	cmd.Dir = filepath.Join("..", "..", "web")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("load generated OpenUI prompt from web/src/lib/assistant/openui/library.ts: %v", err)
	}
	return string(out)
}

func parseGeneratedOpenUI(ctx context.Context, source string) error {
	const script = `import { createServer } from "vite";
const server = await createServer({ configFile: "vite.config.ts", logLevel: "silent", server: { middlewareMode: true }, appType: "custom" });
try {
  const mod = await server.ssrLoadModule("/src/lib/assistant/openui/library.ts");
  const source = Buffer.from(process.env.WERKBORD_OPENUI_SOURCE, "base64").toString("utf8");
  const result = mod.parseOpenUI(source);
  const summary = { root: result.root?.typeName, errors: result.meta.errors, unresolved: result.meta.unresolved, incomplete: result.meta.incomplete };
  process.stdout.write(JSON.stringify(summary));
  if (!summary.root || summary.errors.length || summary.unresolved.length || summary.incomplete) process.exitCode = 1;
} finally { await server.close(); }`
	cmd := exec.CommandContext(ctx, "node", "--input-type=module", "-e", script)
	cmd.Dir = filepath.Join("..", "..", "web")
	cmd.Env = append(os.Environ(), "WERKBORD_OPENUI_SOURCE="+base64.StdEncoding.EncodeToString([]byte(source)))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("parser validation failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
