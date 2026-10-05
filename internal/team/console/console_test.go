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
	for _, path := range []string{"/", "/console.js", "/console.css"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != 200 || rec.Header().Get("Cache-Control") != "no-cache" {
			t.Errorf("%s: %d %v", path, rec.Code, rec.Header())
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
	// The only link built from server data is a pull request, and only if it is https.
	for _, m := range regexp.MustCompile(`href: ([^,}]+)`).FindAllStringSubmatch(string(js), -1) {
		if !strings.Contains(m[1], "pr.url") && !strings.Contains(m[1], "createObjectURL") {
			t.Errorf("a link is built from %s", m[1])
		}
	}
	if !strings.Contains(string(js), "isHTTPS(pr.url)") {
		t.Error("pull request links are not restricted to https")
	}
}
