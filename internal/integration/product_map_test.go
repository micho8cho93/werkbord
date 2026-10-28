package integration

import "testing"

func TestProductMapSourcePathsExcludeCredentialsAndEscapes(t *testing.T) {
	for _, p := range []string{"/etc/passwd", "../a.go", "a/../b.go", ".env", "src/.env.local", "src/secrets.json", "AGENTS.md", "src/CLAUDE.md", "a\\b.go", "key.pem", "config/credentials", "node_modules/x.js", "sub/\nfoo"} {
		if ProductSourcePath(p) {
			t.Errorf("unsafe source path allowed: %q", p)
		}
	}
	for _, p := range []string{"src/main.go", "web/src/App.svelte", "docs/FEATURES.md"} {
		if !ProductSourcePath(p) {
			t.Errorf("source refused: %s", p)
		}
	}
}
func TestProductMapContentRejectsDanglingEdgesAndEvidence(t *testing.T) {
	c := ProductMapContent{Features: []ProductFeature{{ID: "a", Name: "Feature", Evidence: []ProductEvidence{{Path: "main.go", Start: 1, End: 2}}}}}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	c.Relationships = []ProductRelationship{{From: "a", To: "unknown"}}
	if c.Validate() == nil {
		t.Fatal("dangling relationship accepted")
	}
	c.Relationships = nil
	c.Features[0].Evidence[0].End = 0
	if c.Validate() == nil {
		t.Fatal("invalid source range accepted")
	}
}
