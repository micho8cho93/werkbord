package api

import (
	"bytes"
	"net/http"
	"strings"
	"testing"

	"devboard/internal/workspace"
)

// The desktop shell reads Personal's summary with the controller's own credential, in the neutral words, and no program
// that was given narrow access can.
func TestThePersonalSummaryIsForTheControllersOwnCredentialAndIsReadByTheShellsStrictReader(t *testing.T) {
	sc := newScoped(t)
	if code, _ := call(t, "GET", sc.url+"/api/workspace/v1/summary", "", ""); code != 401 {
		t.Fatalf("no credential: %d", code)
	}
	tok := sc.mint(t, "A program")
	if code, _ := call(t, "GET", sc.url+"/api/workspace/v1/summary", tok, ""); code != 403 {
		t.Fatalf("a program's narrow access read the shell's summary: %d", code)
	}
	code, body := call(t, "GET", sc.url+"/api/workspace/v1/summary", sc.owner, "")
	if code != 200 {
		t.Fatalf("summary: %d %s", code, body)
	}
	got, dropped, err := workspace.Decode(bytes.NewReader(body), workspace.PersonalID)
	if err != nil || dropped != 0 {
		t.Fatalf("the shell's reader refused the controller's summary: %v (dropped %d)\n%s", err, dropped, body)
	}
	if got.Workspace.Kind != workspace.KindPersonal || got.Workspace.State != workspace.StateReady {
		t.Fatalf("%+v", got.Workspace)
	}
}

// The desktop app's window may show the web app in a frame; no web page can.
func TestTheWebAppMayBeFramedByTheDesktopWindowAndNoOneElse(t *testing.T) {
	sc := newScoped(t)
	res, err := http.Get(sc.url + "/api/health")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	csp := res.Header.Get("Content-Security-Policy")
	if !strings.Contains(csp, "frame-ancestors wails:;") || strings.Contains(csp, "'none'; base") && strings.Contains(csp, "frame-ancestors 'none'") {
		t.Fatalf("csp = %s", csp)
	}
	if res.Header.Get("X-Frame-Options") != "" {
		t.Fatalf("X-Frame-Options would block the desktop window: %q", res.Header.Get("X-Frame-Options"))
	}
	for _, directive := range []string{"default-src 'self'", "script-src 'self'", "connect-src 'self'", "base-uri 'none'"} {
		if !strings.Contains(csp, directive) {
			t.Errorf("the framed policy lost %q: %s", directive, csp)
		}
	}
}
