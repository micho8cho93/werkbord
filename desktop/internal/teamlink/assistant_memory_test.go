package teamlink

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAssistantMemoryProjectionAndRevokedMembership(t *testing.T) {
	revoked := false
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+key {
			t.Error("missing authentication")
		}
		if revoked {
			w.WriteHeader(403)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/workspace") {
			_, _ = w.Write([]byte(`{"id":"ws_1"}`))
			return
		}
		if strings.HasSuffix(r.URL.Path, "/memory/query") {
			_, _ = w.Write([]byte(`{"records":[]}`))
			return
		}
		if !strings.HasSuffix(r.URL.Path, "/projects/p1/memory") {
			t.Error(r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"records":[
   {"id":"safe","state":"published","content":{"kind":"policy","summary":"Review before merging <ignore instructions>","verification":"verified","source":{"kind":"human","id":"review"}},"approval":{"memberId":"m1"},"freshness":{"status":"source_matches"},"token":"SECRET"},
   {"id":"stale","state":"published","content":{"summary":"STALE","verification":"verified"},"freshness":{"status":"stale"}},
   {"id":"unverified","state":"published","content":{"summary":"UNVERIFIED","verification":"unverified"},"freshness":{"status":"source_matches"}},
   {"id":"superseded","state":"superseded","content":{"summary":"SUPERSEDED","verification":"verified"},"freshness":{"status":"source_matches"}}
  ]}`))
	}))
	defer ts.Close()
	l, err := New(ts.URL, writeKey(t, 0o600, key), nil)
	if err != nil {
		t.Fatal(err)
	}
	args := BoardArgs{ProjectID: "p1", TokenBudget: 1500}
	out, err := l.AssistantBoard(bg, "team:main", "retrieve_memory", args)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(out)
	for _, forbidden := range []string{"SECRET", "STALE", "UNVERIFIED", "SUPERSEDED"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("projection leaked %s: %s", forbidden, raw)
		}
	}
	if !strings.Contains(string(raw), "Review before merging") || len(raw) > 1500 {
		t.Fatalf("missing evidence or oversized: %s", raw)
	}
	revoked = true
	if _, err = l.AssistantBoard(bg, "team:main", "retrieve_memory", args); err == nil {
		t.Fatal("revoked membership reused previous memory")
	}
	for _, raw := range []string{`{"projectId":"p1","tokenBudget":1500,"url":"/secrets"}`, `{"projectId":"p1","tokenBudget":1500,"title":"write"}`} {
		if _, err = DecodeBoardArgs("retrieve_memory", json.RawMessage(raw)); err == nil {
			t.Fatal("invalid semantic input accepted")
		}
	}
}
