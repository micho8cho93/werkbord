package agentcatalog

import (
	"net/url"
	"strings"
	"testing"
)

func TestReviewedCatalogueHasUniqueCLIEntriesAndSetupEvidence(t *testing.T) {
	seen := map[string]bool{}
	for _, e := range Entries() {
		if seen[e.ID] || e.ID == "" || e.Command == "" || e.Setup == "" {
			t.Fatalf("invalid entry: %+v", e)
		}
		seen[e.ID] = true
		u, err := url.Parse(e.DocsURL)
		if err != nil || u.Scheme != "https" || u.Host == "" {
			t.Fatalf("no source for %s", e.ID)
		}
		for _, arg := range e.Args {
			if strings.Contains(arg, "yolo") || strings.Contains(arg, "trust-all") || strings.Contains(arg, "allow-all") {
				t.Fatalf("catalogue may not bypass permissions: %s %s", e.ID, arg)
			}
		}
	}
	for _, id := range []string{"cursor", "github-copilot-cli", "cline", "kilo", "amp-acp", "auggie", "factory-droid", "goose", "kimi", "qwen-code", "mistral-vibe", "junie", "qoder", "pi-acp", "antigravity-acp", "devin", "kiro"} {
		if !seen[id] {
			t.Errorf("missing %s", id)
		}
	}
}
