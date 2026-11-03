package api

import (
	"fmt"
	"testing"

	"werkbord/internal/domain"
	"werkbord/internal/service"
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

func TestTrashAPIDeletesOnlyWhatIsArchivedAndErasesOnlyWhatIsDeleted(t *testing.T) {
	ts := newTestServer(t, nil)
	var p service.ProjectDetail
	if code := do(t, "POST", ts.URL+"/api/projects", `{"path":`+jsonString(gitRepo(t))+`}`, &p); code != 201 {
		t.Fatal(code)
	}
	base := ts.URL + "/api/projects/" + p.ID + "/tasks"
	var k domain.Task
	if code := do(t, "POST", base, `{"title":"Finished"}`, &k); code != 201 {
		t.Fatal(code)
	}
	if code := do(t, "PATCH", base+"/"+k.ID, fmt.Sprintf(`{"version":%d,"state":"done"}`, k.Version), &k); code != 200 {
		t.Fatal(code)
	}
	if code := do(t, "POST", base+"/"+k.ID+"/trash", fmt.Sprintf(`{"version":%d}`, k.Version), nil); code != 409 {
		t.Fatalf("deleting a task that is not archived: %d", code)
	}
	if code := do(t, "PATCH", base+"/"+k.ID, fmt.Sprintf(`{"version":%d,"archived":true}`, k.Version), &k); code != 200 {
		t.Fatal(code)
	}
	if code := do(t, "DELETE", fmt.Sprintf("%s/%s?version=%d", base, k.ID, k.Version), "", nil); code != 409 {
		t.Fatalf("erasing a task that is not deleted: %d", code)
	}
	if code := do(t, "POST", base+"/"+k.ID+"/trash", fmt.Sprintf(`{"version":%d}`, k.Version), &k); code != 200 || k.DeletedAt == nil {
		t.Fatalf("trash = %d %+v", code, k)
	}
	var back domain.Task
	if code := do(t, "POST", base+"/"+k.ID+"/untrash", fmt.Sprintf(`{"version":%d}`, k.Version), &back); code != 200 || back.DeletedAt != nil || back.ArchivedAt == nil {
		t.Fatalf("untrash = %d %+v", code, back)
	}
	k = back
	var again domain.Task
	if code := do(t, "POST", base+"/"+k.ID+"/trash", fmt.Sprintf(`{"version":%d}`, k.Version), &again); code != 200 {
		t.Fatal(code)
	}
	k = again
	if code := do(t, "DELETE", fmt.Sprintf("%s/%s?version=%d", base, k.ID, k.Version), "", nil); code != 204 {
		t.Fatalf("erase = %d", code)
	}
	if code := do(t, "GET", base, "", nil); code != 200 {
		t.Fatal(code)
	}

	var settings domain.ArchiveSettings
	if code := do(t, "PUT", ts.URL+"/api/projects/"+p.ID+"/archive-settings", `{"afterDays":30}`, &settings); code != 200 || settings.AfterDays != 30 {
		t.Fatalf("settings = %d %+v", code, settings)
	}
	if code := do(t, "PUT", ts.URL+"/api/projects/"+p.ID+"/archive-settings", `{"afterDays":5}`, nil); code != 400 {
		t.Fatalf("a period that is not offered: %d", code)
	}

	if code := do(t, "DELETE", ts.URL+"/api/projects/"+p.ID, "", nil); code != 409 {
		t.Fatalf("erasing an active project: %d", code)
	}
	if code := do(t, "PUT", ts.URL+"/api/projects/"+p.ID+"/lifecycle", `{"state":"deleted"}`, nil); code != 409 {
		t.Fatalf("deleting a project that is not archived: %d", code)
	}
	for _, state := range []string{"archived", "deleted"} {
		if code := do(t, "PUT", ts.URL+"/api/projects/"+p.ID+"/lifecycle", `{"state":"`+state+`"}`, nil); code != 200 {
			t.Fatalf("%s: %d", state, code)
		}
	}
	if code := do(t, "DELETE", ts.URL+"/api/projects/"+p.ID, "", nil); code != 204 {
		t.Fatalf("erase project = %d", code)
	}
	if code := do(t, "GET", ts.URL+"/api/projects/"+p.ID, "", nil); code != 404 {
		t.Fatalf("an erased project: %d", code)
	}
}
