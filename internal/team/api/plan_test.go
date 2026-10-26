package api_test

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"werkbord/internal/team/config"
	"werkbord/internal/team/server"
)

// A server that applies the plan rules, with a key check that needs no signatures (the key format has its own tests).
func newPlanServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	db, svc, err := server.Open(context.Background(), cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	svc.EnforcePlan(func(key string) (string, error) {
		if id, ok := strings.CutPrefix(key, "good-"); ok && id != "" {
			return id, nil
		}
		return "", errors.New("not an unlock key")
	})
	ts := httptest.NewServer(server.Handler(db, svc, nil, "v9.9.9"))
	t.Cleanup(ts.Close)
	c, err := svc.CreateWorkspace(context.Background(), "Acme", "Ada", "")
	if err != nil {
		t.Fatal(err)
	}
	return ts, c.Token
}

func TestThePlanIsReportedEnforcedAndPaymentIsRequiredPastIt(t *testing.T) {
	ts, token := newPlanServer(t)
	owner := client{t: t, base: ts.URL, token: token}
	st := owner.want(200, "GET", "/api/team/v1/plan", nil)
	if st["enforced"] != true || st["freeLimit"] != float64(2) || st["people"] != float64(1) || st["premium"] != false || st["readOnly"] != false {
		t.Fatalf("plan = %v", st)
	}
	owner.want(201, "POST", "/api/team/v1/members", map[string]any{"name": "Partner", "role": "member"})
	got, body, raw := owner.do("POST", "/api/team/v1/members", map[string]any{"name": "Third", "role": "member"})
	if got != 402 {
		t.Fatalf("a third person on the Free plan: %d %s", got, raw)
	}
	e, _ := body["error"].(map[string]any)
	if e["code"] != "premium_required" || !strings.Contains(e["message"].(string), "Premium") {
		t.Fatalf("error = %v", e)
	}
	owner.want(400, "PUT", "/api/team/v1/me/premium", map[string]any{"key": "nonsense"})
	st = owner.want(200, "PUT", "/api/team/v1/me/premium", map[string]any{"key": "good-owner"})
	if st["premium"] != true || st["ownerPremium"] != true {
		t.Fatalf("plan after the key = %v", st)
	}
	owner.want(201, "POST", "/api/team/v1/members", map[string]any{"name": "Third", "role": "member"})
}

func TestAReadOnlyMemberMakesOnlyTheRoutesThePlanLists(t *testing.T) {
	ts, token := newPlanServer(t)
	owner := client{t: t, base: ts.URL, token: token}
	owner.want(201, "POST", "/api/team/v1/members", map[string]any{"name": "Partner", "role": "admin"})
	owner.want(200, "PUT", "/api/team/v1/me/premium", map[string]any{"key": "good-owner"})
	third := owner.want(201, "POST", "/api/team/v1/members", map[string]any{"name": "Third", "role": "admin"})
	thirdToken, _ := third["token"].(string)
	free := client{t: t, base: ts.URL, token: thirdToken}

	st := free.want(200, "GET", "/api/team/v1/plan", nil)
	if st["readOnly"] != true || st["premium"] != false || st["people"] != float64(3) {
		t.Fatalf("plan = %v", st)
	}
	// Reading is never refused.
	free.want(200, "GET", "/api/team/v1/members", nil)
	free.want(200, "GET", "/api/team/v1/projects", nil)
	// Changing the team's coordination is.
	for _, c := range []struct {
		method, path string
		body         any
	}{
		{"POST", "/api/team/v1/projects", map[string]any{"name": "X"}},
		{"PATCH", "/api/team/v1/workspace", map[string]any{"name": "Renamed"}},
	} {
		got, body, raw := free.do(c.method, c.path, c.body)
		e, _ := body["error"].(map[string]any)
		if got != 402 || e["code"] != "premium_required" {
			t.Fatalf("%s %s: %d %s", c.method, c.path, got, raw)
		}
	}
	// What keeps the team safe still works: a read-only administrator can replace a leaked credential.
	fresh := free.want(200, "POST", "/api/team/v1/members/"+third["member"].(map[string]any)["id"].(string)+"/token", nil)
	free.token, _ = fresh["token"].(string)
	// Presenting a key is how they stop being read-only.
	st = free.want(200, "PUT", "/api/team/v1/me/premium", map[string]any{"key": "good-third"})
	if st["premium"] != true || st["readOnly"] != false {
		t.Fatalf("plan after the key = %v", st)
	}
}
