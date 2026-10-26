package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"werkbord/internal/domain"
	"werkbord/internal/service"
)

func personalAPIOptions(o *Options) {
	o.AuthRequired = true
	o.Token = ownerToken
	o.Personal = &service.Personal{Deps: service.Deps{Store: o.Store}}
}
func personalAPICall(t *testing.T, method, path, body string, status int, out any) {
	t.Helper()
	code, b := call(t, method, path, ownerToken, body)
	if code != status {
		t.Fatalf("%s %s: %d (want %d): %s", method, path, code, status, b)
	}
	if out != nil {
		if err := json.Unmarshal(b, out); err != nil {
			t.Fatal(err, string(b))
		}
	}
}

const personalAPICollection = `{"name":"Content calendar","schemaVersion":1,"fields":[{"key":"title","name":"Title","type":"text","required":true},{"key":"due","name":"Due","type":"date"}]}`

func personalAPIWorkspace(collection string) string {
	return fmt.Sprintf(`{"name":"Campaign","icon":"calendar","uiSchemaVersion":1,"visibility":{"universal":true,"individual":true,"teamWorkspaceIds":["team:design"]},"collectionIds":[%q],"readBindings":[],"definition":{"views":[{"id":"calendar","name":"Calendar","kind":"calendar","collectionId":%q,"fields":["title"],"dateField":"due"},{"id":"table","name":"Table","kind":"table","collectionId":%q,"fields":["title","due"]}],"layout":{"type":"stack","children":[{"type":"view","viewId":"calendar"},{"type":"view","viewId":"table"}]}}}`, collection, collection, collection)
}
func TestPersonalAPICompleteCRUDAndIsolation(t *testing.T) {
	ts := newTestServer(t, personalAPIOptions)
	base := ts.URL + "/api/personal/v1"
	var c, other domain.PersonalCollection
	personalAPICall(t, "POST", base+"/collections", personalAPICollection, 201, &c)
	personalAPICall(t, "POST", base+"/collections", personalAPICollection, 201, &other)
	spec := personalAPIWorkspace(c.ID)
	var w domain.PersonalWorkspace
	personalAPICall(t, "POST", base+"/workspaces", spec, 201, &w)
	if w.Revision != 1 || w.ID == "" || w.CreatedAt.IsZero() || w.UpdatedAt != w.CreatedAt {
		t.Fatal(w)
	}
	personalAPICall(t, "GET", base+"/workspaces/"+w.ID, "", 200, nil)
	var page struct {
		Items     []domain.PersonalWorkspace
		NextAfter string
	}
	personalAPICall(t, "GET", base+"/workspaces?limit=1", "", 200, &page)
	if len(page.Items) != 1 || page.NextAfter != w.ID {
		t.Fatal(page)
	}
	personalAPICall(t, "GET", base+"/workspaces?after="+page.NextAfter, "", 200, &page)
	if len(page.Items) != 0 {
		t.Fatal(page)
	}
	personalAPICall(t, "PUT", base+"/workspaces/"+w.ID, `{"revision":1,"workspace":`+strings.Replace(spec, "Campaign", "Renamed", 1)+`}`, 200, &w)
	if w.Name != "Renamed" || w.Revision != 2 {
		t.Fatal(w)
	}
	personalAPICall(t, "PUT", base+"/workspaces/"+w.ID, `{"revision":1,"workspace":`+spec+`}`, 409, nil)
	personalAPICall(t, "DELETE", base+"/workspaces/"+w.ID, `{"revision":1}`, 400, nil)
	records := base + "/collections/" + c.ID + "/records"
	var record domain.PersonalRecord
	personalAPICall(t, "POST", records, `{"values":{"title":"Draft","due":"2026-10-23"}}`, 201, &record)
	personalAPICall(t, "GET", records+"/"+record.ID, "", 200, nil)
	personalAPICall(t, "GET", records, "", 200, nil)
	personalAPICall(t, "PUT", records+"/"+record.ID, `{"revision":1,"values":{"title":"Ready"}}`, 200, &record)
	if record.Revision != 2 {
		t.Fatal(record)
	}
	personalAPICall(t, "PUT", records+"/"+record.ID, `{"revision":1,"values":{"title":"Stale"}}`, 409, nil)
	personalAPICall(t, "DELETE", records+"/"+record.ID, `{"revision":1}`, 409, nil)
	for _, method := range []string{"GET", "PUT", "DELETE"} {
		body := `{"revision":2,"values":{"title":"Other"}}`
		if method == "DELETE" {
			body = `{"revision":2}`
		}
		personalAPICall(t, method, base+"/collections/"+other.ID+"/records/"+record.ID, body, 404, nil)
	}
	// No personal row is an existing project/task and existing IDs are rejected.
	personalAPICall(t, "GET", ts.URL+"/api/projects/"+w.ID, "", 404, nil)
	personalAPICall(t, "GET", records+"/"+domain.NewID("tsk"), "", 400, nil)
	personalAPICall(t, "DELETE", base+"/collections/"+c.ID, `{"revision":1}`, 409, nil)
	var preview domain.PersonalDeletion
	personalAPICall(t, "POST", base+"/workspaces/"+w.ID+"/deletion-preview", `{"revision":2,"mode":"keep_data"}`, 200, &preview)
	body, _ := json.Marshal(domain.PersonalDeleteRequest{Revision: 2, Mode: "keep_data", Fingerprint: preview.Fingerprint, Confirmed: true})
	personalAPICall(t, "DELETE", base+"/workspaces/"+w.ID, string(body), 200, nil)
	personalAPICall(t, "GET", records+"/"+record.ID, "", 200, nil)
	personalAPICall(t, "DELETE", base+"/collections/"+c.ID, `{"revision":1}`, 409, nil)
	personalAPICall(t, "DELETE", records+"/"+record.ID, `{"revision":2}`, 204, nil)
	personalAPICall(t, "GET", records+"/"+record.ID, "", 404, nil)
	personalAPICall(t, "PUT", base+"/collections/"+c.ID, `{"revision":1,"collection":`+strings.Replace(personalAPICollection, "Content calendar", "Renamed", 1)+`}`, 200, &c)
	if c.Revision != 2 || c.Name != "Renamed" {
		t.Fatal(c)
	}
	personalAPICall(t, "PUT", base+"/collections/"+c.ID, `{"revision":1,"collection":`+personalAPICollection+`}`, 409, nil)
	personalAPICall(t, "GET", base+"/collections/"+c.ID, "", 200, nil)
	personalAPICall(t, "GET", base+"/collections?limit=1", "", 200, nil)
	personalAPICall(t, "DELETE", base+"/collections/"+c.ID, `{"revision":1}`, 409, nil)
	personalAPICall(t, "DELETE", base+"/collections/"+c.ID, `{"revision":2}`, 204, nil)
}
func TestPersonalAPIStrictSchemasAndBounds(t *testing.T) {
	ts := newTestServer(t, personalAPIOptions)
	base := ts.URL + "/api/personal/v1"
	var c domain.PersonalCollection
	personalAPICall(t, "POST", base+"/collections", personalAPICollection, 201, &c)
	records := base + "/collections/" + c.ID + "/records"
	for _, body := range []string{`null`, `[]`, `{}`, `{"values":null}`, `{"values":{"title":null}}`, `{"values":{"title":4}}`, `{"values":{"title":"x","due":"2026-02-29"}}`, `{"values":{"title":"x","unknown":true}}`, `{"values":{"title":"x"},"id":"injected"}`, `{"values":{"title":"x"},"revision":1}`, `{"values":{"title":"x","title":"y"}}`, `{"values":{"title":"x"},"Values":{"title":"y"}}`, `{"values":{"title":"x"}} {}`, `{"values":{"title":"x"}} junk`, `{"values":{"title":{},"due":"2026-01-01"}}`} {
		personalAPICall(t, "POST", records, body, 400, nil)
	}
	for _, body := range []string{
		strings.Replace(personalAPICollection, `"schemaVersion":1`, `"schemaVersion":2`, 1),
		strings.Replace(personalAPICollection, `"schemaVersion":1`, `"SchemaVersion":1`, 1),
		strings.Replace(personalAPICollection, `"required":true`, `"required":null`, 1),

		strings.Replace(personalAPICollection, `"name":"Content calendar"`, `"name":"Content calendar","sql":"SELECT * FROM tasks"`, 1),
		strings.Replace(personalAPICollection, `"type":"text"`, `"type":"text","script":"alert(1)"`, 1),
		strings.Replace(personalAPICollection, `"name":"Content calendar"`, `"name":"Content calendar","name":"other"`, 1),
		strings.Replace(personalAPICollection, `"type":"text"`, `"type":"html"`, 1),
	} {
		personalAPICall(t, "POST", base+"/collections", body, 400, nil)
	}
	for _, body := range []string{
		strings.Replace(personalAPIWorkspace(c.ID), `"type":"stack"`, `"type":"script"`, 1),
		strings.Replace(personalAPIWorkspace(c.ID), `"individual":true`, `"individual":null`, 1),
		strings.Replace(personalAPIWorkspace(c.ID), `"type":"stack"`, `"type":"stack","columns":null`, 1),

		strings.Replace(personalAPIWorkspace(c.ID), `"type":"stack"`, `"type":"stack","html":"<b>unsafe</b>"`, 1),
		strings.Replace(personalAPIWorkspace(c.ID), `"icon":"calendar"`, `"icon":"https://example.com/x.svg"`, 1),
		strings.Replace(personalAPIWorkspace(c.ID), `"icon":"calendar"`, `"icon":"<svg/>"`, 1),
		strings.Replace(personalAPIWorkspace(c.ID), `"viewId":"calendar"`, `"viewId":"unknown"`, 1),
	} {
		personalAPICall(t, "POST", base+"/workspaces", body, 400, nil)
	}
	personalAPICall(t, "POST", records, `{"values":{"title":"`+strings.Repeat("x", personalMaxBody)+`"}}`, 413, nil)
	personalAPICall(t, "POST", records, `{"values":{"title":"`+strings.Repeat("x", 16001)+`"}}`, 400, nil)
	for _, path := range []string{"collections", "workspaces", "collections/" + c.ID + "/records"} {
		for _, query := range []string{"limit=0", "limit=101", "limit=-1", "limit=x", "limit=1&limit=2", "after=bad", "sort=sql", "limit=%zz"} {
			personalAPICall(t, "GET", base+"/"+path+"?"+query, "", 400, nil)
		}
	}
	for _, id := range []string{"bad", "pws_0000000000000000", c.ID} {
		personalAPICall(t, "GET", base+"/workspaces/"+id, "", 400, nil)
	}
	personalAPICall(t, "DELETE", base+"/collections/"+c.ID, `{}`, 400, nil)
	personalAPICall(t, "GET", base+"/capabilities", "", 200, nil)
	req := httptest.NewRequest("POST", "http://localhost/api/personal/v1/collections", strings.NewReader(personalAPICollection))
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("Authorization", "Bearer "+ownerToken)
	req.Header.Set("Content-Type", "text/plain")
	rec := httptest.NewRecorder()
	New(Options{AuthRequired: true, Token: ownerToken, Personal: &service.Personal{}}).Handler().ServeHTTP(rec, req)
	if rec.Code != 415 {
		t.Fatal(rec.Code, rec.Body.String())
	}
}
func TestPersonalAPIRequiresLocalOwnerAndExplicitWiring(t *testing.T) {
	sc := newScoped(t)
	tok := sc.mint(t, "program")
	// Exercise every verb through actual granted credentials, not fabricated tokens.
	for _, method := range []string{"GET", "POST", "PUT", "DELETE"} {
		if code, _ := call(t, method, sc.url+"/api/personal/v1/collections", tok, personalAPICollection); code != 403 {
			t.Fatal(method, code)
		}
	}
	phone := newPhoned(t)
	ptok, _ := phone.enrolled(t, "phone")
	// A phone is served personal data only where the service is wired (the private listener does, see the controller) and
	// only on the routes it was reviewed for; on an unwired listener it gets nothing.
	if code, _ := call(t, "GET", phone.url+"/api/personal/v1/workspaces", ptok, ""); code != 503 {
		t.Fatal(code)
	}
	ts := newTestServer(t, personalAPIOptions)
	if code, _ := call(t, "GET", ts.URL+"/api/personal/v1/workspaces", "", ""); code != 401 {
		t.Fatal(code)
	}
	for _, tc := range []struct {
		name, remote, token string
		auth, enabled       bool
		want                int
	}{
		{"remote owner", "100.64.0.2:1234", ownerToken, true, true, 403},
		{"no auth mode", "127.0.0.1:1234", ownerToken, false, true, 403},
		{"unwired listener", "127.0.0.1:1234", ownerToken, true, false, 503},
		{"no credential", "127.0.0.1:1234", "", true, true, 401},
		{"query credential", "127.0.0.1:1234", "", true, true, 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := Options{AuthRequired: tc.auth, Token: ownerToken}
			if tc.enabled {
				o.Personal = &service.Personal{}
			}
			req := httptest.NewRequest(http.MethodGet, "http://localhost/api/personal/v1/workspaces?access_token="+ownerToken, nil)
			req.RemoteAddr = tc.remote
			if tc.token != "" {
				req.Header.Set("Authorization", "Bearer "+tc.token)
			}
			w := httptest.NewRecorder()
			New(o).Handler().ServeHTTP(w, req)
			if w.Code != tc.want {
				t.Fatal(w.Code, w.Body.String())
			}
		})
	}
}
