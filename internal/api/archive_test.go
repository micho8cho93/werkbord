package api

import (
	"fmt"
	"testing"

	"devboard/internal/domain"
	"devboard/internal/service"
)

func TestArchiveAPIPreservesTaskAndRejectsStaleRestore(t *testing.T) {
	ts := newTestServer(t, nil)
	var p service.ProjectDetail
	if code := do(t, "POST", ts.URL+"/api/projects", `{"path":`+jsonString(gitRepo(t))+`}`, &p); code != 201 {
		t.Fatal(code)
	}
	base := ts.URL + "/api/projects/" + p.ID + "/tasks"
	var k domain.Task
	if code := do(t, "POST", base, `{"title":"Keep the details","description":"History survives"}`, &k); code != 201 {
		t.Fatal(code)
	}
	if code := do(t, "PATCH", base+"/"+k.ID, fmt.Sprintf(`{"version":%d,"state":"done"}`, k.Version), &k); code != 200 {
		t.Fatal(code)
	}
	var batch struct {
		Tasks []domain.Task `json:"tasks"`
	}
	if code := do(t, "POST", base+"/archive-done", "{}", &batch); code != 200 || len(batch.Tasks) != 1 {
		t.Fatalf("clear = %d, %+v", code, batch)
	}
	closed := batch.Tasks[0]
	if closed.ArchivedAt == nil || closed.Description != k.Description || closed.State != domain.TaskDone {
		t.Fatalf("closed = %+v", closed)
	}
	if code := do(t, "PATCH", base+"/"+k.ID, fmt.Sprintf(`{"version":%d,"archived":false}`, k.Version), nil); code != 409 {
		t.Fatalf("stale restore: %d", code)
	}
	if code := do(t, "PATCH", base+"/"+k.ID, fmt.Sprintf(`{"version":%d,"archived":false}`, closed.Version), &k); code != 200 || k.ArchivedAt != nil {
		t.Fatalf("restore = %d %+v", code, k)
	}
}
