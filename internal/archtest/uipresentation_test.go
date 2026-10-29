package archtest

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The shared presentation library must remain usable by either frontend without
// reaching a product API or carrying a second set of layout calculations.
func TestFrontendsSharePresentationWithoutProductDependencies(t *testing.T) {
	read := func(path string) string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(moduleRoot(t), path))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	for _, path := range []string{"web/src/app.css", "internal/team/console/static/console.css"} {
		if regexp.MustCompile(`--(?:bg|surface|surface-2|border|text|text-2|accent):\s*#`).MatchString(read(path)) {
			t.Errorf("%s duplicates the palette: use internal/uipresentation/static/tokens.css", path)
		}
	}
	for _, path := range []string{"web/src/lib/planning-dates.ts", "internal/team/console/static/console.js"} {
		source := read(path)
		if !strings.Contains(source, "WerkbordPlanning") {
			t.Errorf("%s does not use the shared planning library", path)
		}
		if regexp.MustCompile(`function\s+(?:daysBetween|spanOf|localToday|inkFor)\s*\(`).MatchString(source) {
			t.Errorf("%s duplicates shared planning utilities", path)
		}
	}
	entries, err := os.ReadDir(filepath.Join(moduleRoot(t), "internal/uipresentation/static"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".js") {
			continue
		}
		source := read("internal/uipresentation/static/" + entry.Name())
		for _, forbidden := range []string{"fetch(", "XMLHttpRequest", "WebSocket(", "/api/", "internal/team", "web/src"} {
			if strings.Contains(source, forbidden) {
				t.Errorf("shared %s contains product or network access %q", entry.Name(), forbidden)
			}
		}
	}
}
