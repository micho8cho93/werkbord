package console

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

func TestTheConsoleIsServedAndReadOnlyForOtherMethods(t *testing.T) {
	h := Handler()
	for _, path := range []string{"/", "/console.js", "/console.css", "/work.css", "/motion.js", "/theme.js", "/search.js", "/ui/planning.js", "/ui/tokens.css", "/ui/controls.css", "/mentions.js", "/comments.js", "/comments.css", "/mark-dark.svg"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != 200 || rec.Header().Get("Cache-Control") != "no-cache" {
			t.Errorf("%s: %d %v", path, rec.Code, rec.Header())
		}
	}
	for _, path := range []string{"/timeline.js", "/ui/timeline.js"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("retired asset %s: %d", path, rec.Code)
		}
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST: %d", rec.Code)
	}
}

// Everything the server sends (ticket text, names, branch names, pull request
// addresses) reaches the page as text. The console must never build HTML from it.
func TestTheConsoleNeverBuildsHTMLFromData(t *testing.T) {
	root, _ := fs.Sub(static, "static")
	js, err := fs.ReadFile(root, "console.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, banned := range []string{"innerHTML", "outerHTML", "insertAdjacentHTML", "document.write", "eval(", "new Function", "setTimeout('", `setTimeout("`} {
		if strings.Contains(string(js), banned) {
			t.Errorf("console.js uses %s", banned)
		}
	}
	search, err := fs.ReadFile(root, "search.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, banned := range []string{"innerHTML", "outerHTML", "insertAdjacentHTML", "document.write", "eval(", "new Function"} {
		if strings.Contains(string(search), banned) {
			t.Errorf("search.js uses %s", banned)
		}
	}
	// Comments are written by other members and shown to everyone on the project: they are text, in the box, the list and the rows.
	for _, name := range []string{"comments.js", "mentions.js"} {
		src, err := fs.ReadFile(root, name)
		if err != nil {
			t.Fatal(err)
		}
		for _, banned := range []string{"innerHTML", "outerHTML", "insertAdjacentHTML", "document.write", "eval(", "new Function", "setTimeout('", `setTimeout("`, "DOMParser", "createContextualFragment", "srcdoc", "href:"} {
			if strings.Contains(string(src), banned) {
				t.Errorf("%s uses %s", name, banned)
			}
		}
		if regexp.MustCompile(`setAttribute\(\s*['"]style['"]|\bstyle:\s*['"]|\.style\.cssText`).Match(src) {
			t.Errorf("%s builds a style attribute from a string", name)
		}
	}
	// A link built from server data is only ever followed if it is https: a pull
	// request's address through isHTTPS, anything else (a branch, a commit, the
	// repository) through safeHref.
	for _, m := range regexp.MustCompile(`href: ([^,}]+)`).FindAllStringSubmatch(string(js), -1) {
		if !strings.Contains(m[1], "pr.url") && !strings.Contains(m[1], "createObjectURL") && !strings.Contains(m[1], "safeHref(") {
			t.Errorf("a link is built from %s", m[1])
		}
	}
	if !strings.Contains(string(js), "isHTTPS(pr.url)") {
		t.Error("pull request links are not restricted to https")
	}
	if !regexp.MustCompile(`function safeHref\(u\) \{ return isHTTPS\(u\) \? u : null; \}`).MatchString(string(js)) {
		t.Error("safeHref must return a link only if it is https")
	}
	// Every address handed to extLink is shown only if it is https (extLink checks it).
	if !regexp.MustCompile(`function extLink\(u, label\) \{ return isHTTPS\(u\)`).MatchString(string(js)) {
		t.Error("extLink must check isHTTPS")
	}
}

// Calendar dates and labels put server data on the page: names, colours, dates. Names are text, and a colour is only ever
// used if it is #rrggbb, and only through the element's style object, never built into markup.
func TestPlanningAndLabelsNeverBuildMarkupFromData(t *testing.T) {
	root, _ := fs.Sub(static, "static")
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ui/planning.js", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("shared planning utilities: %d", rec.Code)
	}
	planning := rec.Body.Bytes()
	for _, banned := range []string{"innerHTML", "outerHTML", "insertAdjacentHTML", "document.write", "eval(", "new Function"} {
		if strings.Contains(string(planning), banned) {
			t.Errorf("planning.js uses %s", banned)
		}
	}
	if !regexp.MustCompile(`safeColor = \(c\) => \(typeof c === 'string' && /\^#\[0-9a-f\]\{6\}\$/i\.test\(c\)`).Match(planning) {
		t.Error("a label colour must be checked to be #rrggbb before it is used")
	}
	js, _ := fs.ReadFile(root, "console.js")
	desktop, _ := fs.ReadFile(root, "desktop.js")
	for name, src := range map[string]string{"console.js": string(js), "desktop.js": string(desktop)} {
		if regexp.MustCompile(`setAttribute\(\s*['"]style['"]|\bstyle:\s*['"]`).MatchString(src) {
			t.Errorf("%s builds a style attribute from a string: set colours through element.style", name)
		}
	}
	index, _ := fs.ReadFile(root, "index.html")
	if strings.Index(string(index), "planning.js") < 0 || strings.Index(string(index), "planning.js") > strings.Index(string(index), "console.js") {
		t.Error("index.html must load planning.js before console.js")
	}
}

// The first-visit tour is plain text on a page that holds a member's work: it builds no HTML, is loaded by the console, and is
// served like the rest.
func TestTheTourIsLoadedAndBuildsNoHTML(t *testing.T) {
	root, _ := fs.Sub(static, "static")
	tour, err := fs.ReadFile(root, "tour.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, banned := range []string{"innerHTML", "outerHTML", "insertAdjacentHTML", "document.write", "eval(", "new Function"} {
		if strings.Contains(string(tour), banned) {
			t.Errorf("tour.js uses %s", banned)
		}
	}
	index, _ := fs.ReadFile(root, "index.html")
	if !strings.Contains(string(index), `src="tour.js"`) {
		t.Error("index.html must load tour.js")
	}
}

// The comment box and its list are loaded in the order they need each other, and the pure part is a module of its own so that
// scripts/team-mentions.test.cjs can run it under node.
func TestCommentsAreLoadedAfterWhatTheyUse(t *testing.T) {
	root, _ := fs.Sub(static, "static")
	index, _ := fs.ReadFile(root, "index.html")
	page := string(index)
	at := func(name string) int { return strings.Index(page, `src="`+name+`"`) }
	if at("mentions.js") < 0 || at("comments.js") < 0 || !(at("mentions.js") < at("comments.js")) || !(at("console.js") < at("comments.js")) {
		t.Error("index.html must load mentions.js, then console.js, then comments.js")
	}
	if !strings.Contains(page, `href="comments.css"`) {
		t.Error("index.html must load comments.css")
	}
	mentions, _ := fs.ReadFile(root, "mentions.js")
	if !strings.Contains(string(mentions), "module.exports") {
		t.Error("mentions.js must export for node as well as the page, to be testable on its own")
	}
	if strings.Contains(string(mentions), "document.") || strings.Contains(string(mentions), "window.") && !strings.Contains(string(mentions), "typeof window") {
		t.Error("mentions.js must stay free of the page: it is the pure part")
	}
}
