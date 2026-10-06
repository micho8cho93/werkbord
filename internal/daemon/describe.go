package daemon

import (
	"context"
	"regexp"
	"strings"
)

// Definition is what an installed controller service says about itself: which
// executable it runs, and where it keeps its data.
type Definition struct {
	// Binary is the executable the service starts, as `werkbord setup` wrote it.
	Binary string
	// DataDir is the data directory it was given (WERKBORD_DATA_DIR in its
	// environment), or "" when it was left to the default.
	DataDir string
}

var (
	plistProgram = regexp.MustCompile(`(?s)<key>ProgramArguments</key>\s*<array>\s*<string>(.*?)</string>`)
	plistEnvKeys = []string{"WERKBORD_DATA_DIR", "DEVBOARD_DATA_DIR"}
)

// Describe reads the controller service's definition, without starting, stopping
// or changing anything. A program that did not start from a terminal (the desktop
// app, which the Finder starts with almost no environment) uses it to join the
// installation that is already there instead of making a second one next to it: the
// service says which executable it runs and which data directory it was given, and
// `werkbord setup` was the one that decided both.
//
// It reads launchd's definition (macOS, which is where such a program runs). On
// another system, and when nothing is installed, ok is false.
func Describe(ctx context.Context, o Options) (Definition, bool) {
	m := Detect(ctx, o)
	if _, isLaunchd := m.(*Launchd); !isLaunchd {
		return Definition{}, false
	}
	body, ok := definition(ctx, m)
	if !ok {
		return Definition{}, false
	}
	return parsePlist(body)
}

func parsePlist(body string) (Definition, bool) {
	var d Definition
	if m := plistProgram.FindStringSubmatch(body); m != nil {
		d.Binary = xmlUnescape(strings.TrimSpace(m[1]))
	}
	for _, key := range plistEnvKeys {
		re := regexp.MustCompile(`(?s)<key>` + key + `</key>\s*<string>(.*?)</string>`)
		if m := re.FindStringSubmatch(body); m != nil {
			d.DataDir = xmlUnescape(strings.TrimSpace(m[1]))
			break
		}
	}
	return d, d.Binary != ""
}

var xmlUnescaper = strings.NewReplacer("&lt;", "<", "&gt;", ">", "&quot;", `"`, "&apos;", "'", "&#39;", "'", "&#34;", `"`, "&amp;", "&")

func xmlUnescape(s string) string { return xmlUnescaper.Replace(s) }
