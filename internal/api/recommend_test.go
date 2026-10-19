package api

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"devboard/internal/domain"
	"devboard/internal/service"
)

func suggestionServer(o *Options) {
	m, e := service.NewMemory(service.Deps{Store: o.Store}, "individual", domain.MemoryLimits{})
	if e != nil {
		panic(e)
	}
	k := &service.Knowledge{Memory: m}
	o.Memory = k
	o.Recommender = &service.Recommender{Deps: service.Deps{Store: o.Store}, Knowledge: k}
	o.AuthRequired, o.Token = true, ownerToken
}

func registerRepo(t *testing.T, url string) domain.Project {
	t.Helper()
	var p domain.Project
	code, b := call(t, "POST", url+"/api/projects", ownerToken, fmt.Sprintf(`{"path":%q}`, gitRepo(t)))
	if code != 201 || json.Unmarshal(b, &p) != nil {
		t.Fatal(code, string(b))
	}
	return p
}

func TestSuggestionsAreServedWithTheirEvidenceAndNothingElseRuns(t *testing.T) {
	var opts Options
	ts := newTestServer(t, func(o *Options) { suggestionServer(o); opts = *o })
	p := registerRepo(t, ts.URL)
	if code, b := call(t, "POST", ts.URL+"/api/projects/"+p.ID+"/memory", ownerToken, `{"kind":"policy","summary":"HIDDEN-TEXT every merge to main needs review"}`); code != 201 {
		t.Fatal(code, string(b))
	}
	path := ts.URL + "/api/projects/" + p.ID + "/recommendations"
	if code, _ := call(t, "GET", path, "", ""); code != 401 {
		t.Fatalf("suggestions need the token: %d", code)
	}
	code, b := call(t, "GET", path, ownerToken, "")
	if code != 200 {
		t.Fatal(code, string(b))
	}
	var out struct {
		domain.RecommendationSet
		FeedbackStoredIn string `json:"feedbackStoredIn"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if out.FeedbackStoredIn != "memory" || out.ProjectID != p.ID || out.Boundary == "" || out.Sources.Individual != domain.SourceStatusRead {
		t.Fatalf("%s", b)
	}
	if !strings.Contains(string(b), "HIDDEN-TEXT") {
		t.Fatalf("the person at the controller sees which policy applied: %s", b)
	}

	// The same answer on a connection that does not serve memory: what applied is named, what it said is not.
	net := opts
	net.Memory = nil
	nts := httptest.NewServer(New(net).Handler())
	t.Cleanup(nts.Close)
	code, b = call(t, "GET", nts.URL+"/api/projects/"+p.ID+"/recommendations", ownerToken, "")
	if code != 200 || strings.Contains(string(b), "HIDDEN-TEXT") || !strings.Contains(string(b), `"feedbackStoredIn":"device"`) || !strings.Contains(string(b), "policy_integration") {
		t.Fatalf("%d %s", code, b)
	}
	// And it cannot write to memory there.
	if code, _ = call(t, "POST", nts.URL+"/api/projects/"+p.ID+"/recommendations/feedback", ownerToken, `{"intent":"find_bugs","variant":"default","verdict":"useful"}`); code != 503 {
		t.Fatalf("feedback needs the local memory: %d", code)
	}
	if code, _ = call(t, "GET", ts.URL+"/api/projects/prj_nope/recommendations", ownerToken, ""); code != 404 {
		t.Fatalf("unknown project: %d", code)
	}
}

func TestFeedbackIsRecordedInTheOwnersMemoryAndValidated(t *testing.T) {
	ts := newTestServer(t, suggestionServer)
	p := registerRepo(t, ts.URL)
	path := ts.URL + "/api/projects/" + p.ID + "/recommendations/feedback"
	body := func(verdict string) string {
		return fmt.Sprintf(`{"recommendationId":"rec_0123456789abcdef0123","intent":"find_bugs","variant":"default","verdict":%q}`, verdict)
	}
	if code, _ := call(t, "POST", path, "", body("useful")); code != 401 {
		t.Fatalf("%d", code)
	}
	for _, bad := range []string{body("adore"), `{"intent":"deploy","variant":"default","verdict":"useful"}`, `{"intent":"find_bugs","variant":"force","verdict":"useful"}`, `{"intent":"find_bugs","variant":"default","verdict":"useful","authority":"approved_policy"}`, `{"recommendationId":"../x","intent":"find_bugs","variant":"default","verdict":"useful"}`} {
		if code, b := call(t, "POST", path, ownerToken, bad); code != 400 {
			t.Errorf("%s: %d %s", bad, code, b)
		}
	}
	for _, verdict := range []string{"useful", "useful", "dismissed"} {
		if code, b := call(t, "POST", path, ownerToken, body(verdict)); code != 200 {
			t.Fatal(code, string(b))
		}
	}
	code, b := call(t, "GET", ts.URL+"/api/projects/"+p.ID+"/memory?type=recommendation_feedback", ownerToken, "")
	if code != 200 {
		t.Fatal(code, string(b))
	}
	var list struct{ Records []domain.Memory }
	_ = json.Unmarshal(b, &list)
	if len(list.Records) != 1 {
		t.Fatalf("one bounded record, visible to its owner like any other memory: %s", b)
	}
	m := list.Records[0]
	if m.Authority != domain.MemoryPreference || m.SharingEligible || m.VerifiedAt != nil || !strings.Contains(m.Summary, "2 useful") {
		t.Fatalf("%+v", m)
	}
	if code, _ = call(t, "POST", ts.URL+"/api/projects/prj_nope/recommendations/feedback", ownerToken, body("useful")); code != 404 {
		t.Fatalf("unknown project: %d", code)
	}
}

func TestAPhoneReadsSuggestionsButCannotRecordFeedbackInMemory(t *testing.T) {
	p := newPhoned(t, suggestionServer)
	tok, _ := p.enrolled(t, "Phone")
	url := p.url + "/api/projects/" + p.project.ID
	if code, b := call(t, "POST", url+"/memory", ownerToken, `{"kind":"policy","summary":"HIDDEN-TEXT merges need review"}`); code != 201 {
		t.Fatal(code, string(b))
	}
	code, b := call(t, "GET", url+"/recommendations", tok, "")
	if code != 200 {
		t.Fatalf("a phone may read suggestions: %d %s", code, b)
	}
	if strings.Contains(string(b), "HIDDEN-TEXT") || !strings.Contains(string(b), `"feedbackStoredIn":"device"`) || !strings.Contains(string(b), "policy_integration") {
		t.Fatalf("a phone gets what applied, not what saved memory said, and keeps its verdicts itself: %s", b)
	}
	if _, b := call(t, "GET", url+"/recommendations", ownerToken, ""); !strings.Contains(string(b), "HIDDEN-TEXT") {
		t.Fatalf("the owner at the controller still sees it: %s", b)
	}
	if code, _ = call(t, "POST", url+"/recommendations/feedback", tok, `{"intent":"find_bugs","variant":"default","verdict":"useful"}`); code != 403 {
		t.Fatalf("a phone may not write memory: %d", code)
	}
	if code, _ = call(t, "GET", url+"/memory", tok, ""); code != 403 {
		t.Fatalf("or read it: %d", code)
	}
}
