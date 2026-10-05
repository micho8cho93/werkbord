package main

import (
	"context"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"devboard/internal/team/config"
	"devboard/internal/team/server"
)

func TestTeamRestartPreservesReviewMetadataCredentialsAndSync(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	db, svc, err := server.Open(context.Background(), cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(server.Handler(db, svc, nil, "before"))
	t.Cleanup(func() { ts.Close(); _ = db.Close() })
	created, err := svc.CreateWorkspace(context.Background(), "Existing workspace", "Owner", "")
	if err != nil {
		t.Fatal(err)
	}
	owner := person{t, ts.URL, created.Token}
	p := owner.ok("POST", "/projects", map[string]any{"name": "Existing project", "repository": "https://github.com/acme/shop"})
	projectPath := "/projects/" + text(p, "id")
	invite := owner.ok("POST", projectPath+"/invites", map[string]any{"role": "reviewer"})
	joined := (person{t, ts.URL, ""}).ok("POST", "/invites/redeem", map[string]any{"code": text(invite, "code"), "name": "Reviewer"})
	reviewer := person{t, ts.URL, text(joined, "token")}
	k := owner.ok("POST", projectPath+"/tickets", map[string]any{"title": "Existing ticket", "status": "available"})
	ticketPath := projectPath + "/tickets/" + text(k, "id")
	k = owner.ok("POST", ticketPath+"/claim", nil)
	sha := "0123456789abcdef0123456789abcdef01234567"
	owner.ok("PUT", ticketPath+"/git", map[string]any{"commits": []map[string]any{{"sha": sha, "subject": "Existing commit", "author": "Owner", "committedAt": time.Now().UTC().Format(time.RFC3339)}}, "pullRequest": map[string]any{"number": 9, "url": "https://github.com/acme/shop/pull/9", "state": "merged"}, "state": map[string]any{"headSha": sha, "baseBranch": "main", "ahead": 1, "behind": 0, "files": []string{"preserved.go"}}})
	owner.ok("POST", ticketPath+"/submit", map[string]any{})
	before := owner.ok("GET", "/sync?wait=0", nil)
	ts.Close()
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, svc, err = server.Open(context.Background(), cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ts = httptest.NewServer(server.Handler(db, svc, nil, "after"))
	defer ts.Close()
	owner.base, reviewer.base = ts.URL, ts.URL
	saved := owner.ok("GET", ticketPath, nil)
	commits := list(saved, "commits")
	if text(saved, "status") != "review" || text(saved, "branch") != text(k, "branch") || text(obj(saved, "pullRequest"), "state") != "merged" || len(commits) != 1 || text(commits[0], "sha") != sha {
		t.Fatalf("review evidence lost: %v", saved)
	}
	unchanged := owner.ok("GET", fmt.Sprintf("/sync?since=%.0f&after=%.0f&wait=0", before["revision"], before["cursor"]), nil)
	if unchanged["changed"] != false || unchanged["revision"] != before["revision"] {
		t.Fatalf("restart changed sync contract: %v", unchanged)
	}
	queue := reviewer.ok("GET", "/reviews", nil)
	if len(list(queue, "items")) != 1 || list(queue, "items")[0]["canComplete"] != true {
		t.Fatalf("reviewer credential/permissions lost: %v", queue)
	}
	done := reviewer.ok("POST", ticketPath+"/complete", nil)
	if text(done, "status") != "done" {
		t.Fatal(done)
	}
	reconciled := owner.ok("GET", fmt.Sprintf("/sync?since=%.0f&after=%.0f&wait=0", before["revision"], before["cursor"]), nil)
	if reconciled["changed"] != true || reconciled["revision"].(float64) <= before["revision"].(float64) {
		t.Fatalf("pre-restart cursor did not reconcile: %v", reconciled)
	}
}
