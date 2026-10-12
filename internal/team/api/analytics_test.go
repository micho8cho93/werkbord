package api_test

import (
	"net/http"
	"strings"
	"testing"
)

func TestAnalyticsHTTPPermissions(t *testing.T) {
	ts := newServer(t)
	base := ts.URL + "/api/team/v1"
	owner := client{t, base, ownerToken}
	anonymous := client{t, base, ""}
	p := owner.want(201, "POST", "/projects", map[string]any{"name": "Private"})
	id := p["id"].(string)
	ticket := owner.want(201, "POST", "/projects/"+id+"/tickets", map[string]any{"title": "private ticket"})
	for _, path := range []string{"/analytics", "/projects/" + id + "/analytics"} {
		anonymous.want(http.StatusUnauthorized, "GET", path, nil)
		status, m, raw := owner.do("GET", path, nil)
		if status != 200 || m["created"] != float64(1) {
			t.Fatalf("%s: %d ticketCreatedAt=%v %s", path, status, ticket["createdAt"], raw)
		}
		if strings.Contains(string(raw), "private ticket") {
			t.Fatalf("title leaked: %s", raw)
		}
	}
	owner.want(404, "GET", "/projects/"+id+"/analytics?project=other", nil)
	for _, rangeQuery := range []string{"days=0", "days=5", "days=abc"} {
		owner.want(400, "GET", "/analytics?"+rangeQuery, nil)
	}
	owner.want(404, "GET", "/analytics?project=missing", nil)
}
