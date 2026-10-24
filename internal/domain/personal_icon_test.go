package domain

import "testing"

func TestPersonalGlyphClosedGrammarAndBrowserParity(t *testing.T) {
	for _, sample := range []struct{ name, old, want string }{
		{"Notes", "auto", "workspace-1297741"},
		{"Weekly planner", "check", "workspace-101a1aa"},
		{"Café 🌿", "calendar", "workspace-0c9f71f"},
	} {
		got := DisplayPersonalIcon(sample.old, sample.name)
		if got != sample.want || !GeneratedPersonalIcon(got) {
			t.Fatalf("glyph parity: %q != %q", got, sample.want)
		}
	}
	for _, bad := range []string{"check", "calendar", "workspace-0000000", "workspace-1ffffff", "workspace-fffffff", "workspace-1297741<script>", "<svg/>", "https://example.com/icon.svg"} {
		if GeneratedPersonalIcon(bad) {
			t.Fatalf("invalid glyph accepted: %q", bad)
		}
	}
}
