package domain

import "testing"

func TestPersonalLibraryIconsSemanticSelectionAndBrowserParity(t *testing.T) {
	for _, sample := range []struct{ name, old, want string }{
		{"Notes", "auto", "lucide-notebook-pen"},
		{"Demo · Focus planner", "workspace-1297741", "lucide-target"},
		{"Demo · Reading journal", "folder", "lucide-book-open"},
		{"Demo · Launch room", "projects", "lucide-rocket"},
		{"Demo · Research hub", "people", "lucide-microscope"},
		{"Weekly planner", "check", "lucide-calendar-days"},
	} {
		got := DisplayPersonalIcon(sample.old, sample.name)
		if got != sample.want || !LibraryPersonalIcon(got) {
			t.Fatalf("library icon parity for %q: %q != %q", sample.name, got, sample.want)
		}
		if DisplayPersonalIcon(got, "Renamed") != got {
			t.Fatal("rename changed saved library icon")
		}
	}
	for _, bad := range []string{"lucide-not-real", "lucide-rocket<script>", "<svg/>", "https://example.com/icon.svg", "workspace-0000000", "workspace-1ffffff", "workspace-fffffff"} {
		if ValidPersonalIcon(bad) {
			t.Fatalf("unsafe or unknown icon accepted: %q", bad)
		}
	}
	for _, old := range []string{"auto", "folder", "workspace-1297741"} {
		if !ValidPersonalIcon(old) {
			t.Fatalf("legacy icon cannot be read: %q", old)
		}
	}
	seen := map[string]bool{}
	for _, icon := range PersonalIcons() {
		if seen[icon] || len(icon) > 32 {
			t.Fatalf("duplicate/oversized catalogue entry %q", icon)
		}
		seen[icon] = true
	}
	if len(seen) < 100 {
		t.Fatal("workspace catalogue unexpectedly small")
	}
	if got := PersonalIconCandidates("Untitled", "Research interviews")[0]; got != "lucide-microscope" {
		t.Fatalf("view hints were ignored: %q", got)
	}
}
