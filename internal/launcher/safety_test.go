package launcher

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDesktopUpdateSafetyChecksRealRunsAndRunnerJournals(t *testing.T) {
	for _, tc := range []struct {
		name, state       string
		remote, wantError bool
	}{
		{"active", "running", false, true}, {"waiting", "waiting_for_user", false, true}, {"blocked", "blocked", false, true}, {"complete", "completed", false, false}, {"remote", "running", true, false}, {"unknown", "new_state", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			token := "disposable-safety-token"
			os.WriteFile(filepath.Join(dir, "token"), []byte(token), 0600)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/health" {
					w.Write([]byte(`{"status":"ok","version":"v1.8.0"}`))
					return
				}
				if r.Header.Get("Authorization") != "Bearer "+token {
					t.Error("safety check unauthenticated")
					w.WriteHeader(401)
					return
				}
				remote := "false"
				if tc.remote {
					remote = "true"
				}
				w.Write([]byte(`{"runs":[{"run":{"state":"` + tc.state + `","remote":` + remote + `}}]}`))
			}))
			defer srv.Close()
			os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"addr":"`+strings.TrimPrefix(srv.URL, "http://")+`"}`), 0600)
			l := New(Options{DataDir: dir})
			err := l.UpdateSafety(context.Background())
			if (err != nil) != tc.wantError {
				t.Fatalf("safety %s: %v", tc.name, err)
			}
			os.MkdirAll(filepath.Join(dir, "runner", "runs"), 0700)
			os.WriteFile(filepath.Join(dir, "runner", "runs", "run.json"), []byte(`{"phase":"active"}`), 0600)
			if err = l.UpdateSafety(context.Background()); err == nil {
				t.Fatal("runner's unresolved work ignored")
			}
		})
	}
}
