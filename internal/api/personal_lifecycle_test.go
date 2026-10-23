package api

import (
	"devboard/internal/domain"
	"encoding/json"
	"fmt"
	"testing"
)

func TestPersonalLifecycleAPIConfirmationProtectionAndHistory(t *testing.T) {
	ts := newTestServer(t, personalAPIOptions)
	base := ts.URL + "/api/personal/v1"
	var c domain.PersonalCollection
	var w domain.PersonalWorkspace
	personalAPICall(t, "POST", base+"/collections", personalAPICollection, 201, &c)
	personalAPICall(t, "POST", base+"/workspaces", personalAPIWorkspace(c.ID), 201, &w)
	path := base + "/workspaces/" + w.ID
	personalAPICall(t, "POST", base+"/collections/"+c.ID+"/records", `{"values":{"title":"Private"}}`, 201, nil)
	personalAPICall(t, "GET", base+"/collections/"+c.ID+"/usage", "", 200, nil)
	personalAPICall(t, "GET", path+"/history", "", 200, nil)
	personalAPICall(t, "GET", path+"/revisions/1", "", 200, nil)
	personalAPICall(t, "POST", path+"/rollback", `{"revision":1,"targetRevision":1}`, 200, &w)
	if w.Revision != 2 {
		t.Fatal(w)
	}
	var preview domain.PersonalDeletion
	personalAPICall(t, "POST", path+"/deletion-preview", `{"revision":1,"mode":"delete_data"}`, 409, nil)
	personalAPICall(t, "POST", path+"/deletion-preview", `{"revision":2,"mode":"everything"}`, 400, nil)
	personalAPICall(t, "POST", path+"/deletion-preview", `{"revision":2,"mode":"delete_data","confirmed":true}`, 400, nil)
	personalAPICall(t, "POST", path+"/deletion-preview", `{"revision":2,"mode":"delete_data"}`, 200, &preview)
	if !preview.Collections[0].Delete || preview.Collections[0].RecordCount != 1 {
		t.Fatal(preview)
	}
	personalAPICall(t, "DELETE", path, `{"revision":2}`, 400, nil)
	request := domain.PersonalDeleteRequest{Revision: 2, Mode: "delete_data", Fingerprint: preview.Fingerprint}
	raw, _ := json.Marshal(request)
	personalAPICall(t, "DELETE", path, string(raw), 400, nil)
	// The new dependency invalidates a previously reviewed destructive plan.
	var shared domain.PersonalWorkspace
	personalAPICall(t, "POST", base+"/workspaces", personalAPIWorkspace(c.ID), 201, &shared)
	request.Confirmed = true
	raw, _ = json.Marshal(request)
	personalAPICall(t, "DELETE", path, string(raw), 409, nil)
	personalAPICall(t, "POST", path+"/deletion-preview", `{"revision":2,"mode":"delete_data"}`, 200, &preview)
	if preview.Collections[0].Delete || preview.Collections[0].ReferenceCount != 2 {
		t.Fatal(preview)
	}
	request.Fingerprint = preview.Fingerprint
	raw, _ = json.Marshal(request)
	personalAPICall(t, "DELETE", path, string(raw), 200, nil)
	personalAPICall(t, "GET", path, "", 404, nil)
	personalAPICall(t, "GET", path+"/history", "", 404, nil)
	personalAPICall(t, "GET", base+"/collections/"+c.ID, "", 200, nil)
	personalAPICall(t, "GET", base+"/workspaces/"+shared.ID, "", 200, nil)
	// All lifecycle routes retain the local owner boundary.
	for _, endpoint := range []struct{ method, path, body string }{
		{"GET", "/workspaces/" + shared.ID + "/history", ""},
		{"POST", "/workspaces/" + shared.ID + "/deletion-preview", `{"revision":1,"mode":"keep_data"}`},
		{"POST", "/workspaces/" + shared.ID + "/rollback", `{"revision":1,"targetRevision":1}`},
		{"GET", "/collections/" + c.ID + "/usage", ""},
	} {
		if code, _ := call(t, endpoint.method, base+endpoint.path, "", endpoint.body); code != 401 {
			t.Fatal(fmt.Sprint(endpoint), code)
		}
	}
}
