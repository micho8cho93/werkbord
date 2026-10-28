package api

import "testing"

// Confirming that a revoked machine stopped is the owner's decision alone: no phone, desktop client or program may list the
// held work or release it. (The desktop client policy is covered in internal/clientpolicy.)
func TestOnlyTheOwnerOnThisComputerSeesAndReleasesHeldRunnerWork(t *testing.T) {
	p := newPhoned(t)
	phone, _ := p.enrolled(t, "A")
	for name, c := range map[string]struct{ method, path, token, body string }{
		"phone lists held work":     {"GET", "/api/runners/held-runs", phone, ""},
		"phone confirms a stop":     {"POST", "/api/runners/rnr_x/runs/run_x/resolve", phone, `{"confirmStopped":true}`},
		"no credential lists":       {"GET", "/api/runners/held-runs", "", ""},
		"wrong credential lists":    {"GET", "/api/runners/held-runs", "not-the-token", ""},
		"no credential confirms":    {"POST", "/api/runners/rnr_x/runs/run_x/resolve", "", `{"confirmStopped":true}`},
		"wrong credential confirms": {"POST", "/api/runners/rnr_x/runs/run_x/resolve", "not-the-token", `{"confirmStopped":true}`},
	} {
		if code, _ := call(t, c.method, p.url+c.path, c.token, c.body); code != 401 && code != 403 {
			t.Errorf("%s: %d", name, code)
		}
	}
	// The owner reaches the handler: with no runner service in this fixture it says so rather than refusing.
	if code, _ := call(t, "GET", p.url+"/api/runners/held-runs", ownerToken, ""); code != 503 {
		t.Errorf("owner: %d", code)
	}
	// A confirmation must be explicit.
	if code, _ := call(t, "POST", p.url+"/api/runners/rnr_x/runs/run_x/resolve", ownerToken, `{}`); code != 400 && code != 422 {
		t.Errorf("an unconfirmed release was not refused as invalid: %d", code)
	}
}
