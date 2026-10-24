package api

import (
	"devboard/internal/domain"
	"encoding/json"
	"fmt"
	"testing"
)

func TestPersonalRuntimeRequiresOwnerAndSavedDefinitions(t *testing.T) {
	ts := newTestServer(t, personalAPIOptions)
	base := ts.URL + "/api/personal/v1"
	var c domain.PersonalCollection
	personalAPICall(t, "POST", base+"/collections", personalAPICollection, 201, &c)
	spec := fmt.Sprintf(`{"name":"Actions","icon":"auto","uiSchemaVersion":1,"visibility":{"individual":true,"universal":false,"teamWorkspaceIds":[]},"collectionIds":[%q],"readBindings":[],"definition":{"views":[{"id":"records","name":"Records","kind":"table","collectionId":%q,"fields":["title","due"],"operations":["create","update"],"actions":[{"id":"rename","name":"Rename","steps":[{"operation":"update","values":{"title":"Renamed"}}]}]}],"layout":{"type":"view","viewId":"records"}}}`, c.ID, c.ID)
	var w domain.PersonalWorkspace
	personalAPICall(t, "POST", base+"/workspaces", spec, 201, &w)
	var record domain.PersonalRecord
	personalAPICall(t, "POST", base+"/collections/"+c.ID+"/records", `{"values":{"title":"Keep"}}`, 201, &record)
	action := base + "/workspaces/" + w.ID + "/views/records/actions/rename"
	body := fmt.Sprintf(`{"workspaceRevision":1,"collectionRevision":1,"records":[{"id":%q,"revision":1}],"confirmed":true}`, record.ID)
	for _, token := range []string{"", "scoped-access", "phone-not-enrolled"} {
		status, _ := call(t, "POST", action, token, body)
		if status != 401 && status != 403 {
			t.Fatal("runtime route escaped owner authentication", status)
		}
	}
	personalAPICall(t, "POST", action, body[:len(body)-1]+`,"values":{"title":"Injected"}}`, 400, nil)
	personalAPICall(t, "POST", action, `{"workspaceRevision":1,"collectionRevision":1,"confirmed":false}`, 400, nil)
	personalAPICall(t, "POST", action, body, 200, nil)
	personalAPICall(t, "POST", action, body, 409, nil)
	personalAPICall(t, "GET", base+"/collections/"+c.ID+"/records/"+record.ID, "", 200, &record)
	var title string
	_ = json.Unmarshal(record.Values["title"], &title)
	if title != "Renamed" {
		t.Fatal(title)
	}
	personalAPICall(t, "GET", base+"/workspaces/"+w.ID+"/bindings/undeclared", "", 404, nil)
}
