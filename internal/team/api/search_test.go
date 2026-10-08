package api_test

import (
	"net/url"
	"strings"
	"testing"
)

func TestSearchOverHTTPAuthorizesAndReturnsOnlyNavigationMetadata(t *testing.T) {
	ts := newServer(t)
	owner := client{t: t, base: ts.URL, token: ownerToken}
	project := owner.want(201, "POST", v1+"/projects", `{"name":"Launch portal"}`)
	pid := str(project, "id")
	k := owner.want(201, "POST", v1+"/projects/"+pid+"/tickets", `{"title":"Welcome screen","description":"Private body with searchable wording","requirements":"A long requirement","status":"available"}`)
	boData := owner.want(201, "POST", v1+"/members", `{"name":"Bo"}`)
	bo := client{t: t, base: ts.URL, token: str(boData, "token")}
	query := v1 + "/search?q=searchable"
	if r := bo.want(200, "GET", query, nil); len(r["tickets"].([]any)) != 0 || len(r["projects"].([]any)) != 0 {
		t.Fatalf("search reveals a project before joining: %v", r)
	}
	owner.want(204, "PUT", v1+"/projects/"+pid+"/members/"+str(sub(boData, "member"), "id"), `{}`)
	r := bo.want(200, "GET", query, nil)
	tickets := r["tickets"].([]any)
	if len(tickets) != 1 || str(tickets[0].(map[string]any), "id") != str(k, "id") {
		t.Fatalf("joined member cannot search ticket context: %v", r)
	}
	for _, field := range []string{"description", "requirements", "commits", "pullRequest", "token"} {
		if _, ok := tickets[0].(map[string]any)[field]; ok {
			t.Errorf("search unnecessarily returns %s", field)
		}
	}
	bo.want(400, "GET", v1+"/search?q="+url.QueryEscape(strings.Repeat("a", 161)), nil)
	anon := client{t: t, base: ts.URL}
	anon.want(401, "GET", query, nil)
	owner.want(204, "DELETE", v1+"/members/"+str(sub(boData, "member"), "id"), nil)
	bo.want(401, "GET", query, nil)
}
