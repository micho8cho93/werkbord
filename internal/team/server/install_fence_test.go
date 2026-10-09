package server

import (
	"devboard/internal/team/config"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestInstallFenceBlocksEnrollmentUntilVerified(t *testing.T) {
	root := t.TempDir()
	h := &Hub{o: DaemonOptions{Config: config.Config{DataDir: filepath.Join(root, "data")}}}
	called := 0
	handler := h.installationFence(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called++; w.WriteHeader(200) }))
	for _, phase := range []string{"prepared", "verified", "rolled_back", "corrupt"} {
		os.WriteFile(filepath.Join(root, "install-transaction.json"), []byte(`{"phase":"`+phase+`"}`), 0600)
		for _, method := range []string{"GET", "POST", "DELETE"} {
			before := called
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, httptest.NewRequest(method, "/api/device/v1/workspaces", nil))
			pending := phase == "prepared" || phase == "corrupt"
			if (w.Header().Get("X-Werkbord-Installation-Pending") != "") != pending {
				t.Fatal("activation cannot detect an interrupted healthy installation")
			}
			blocked := method != "GET" && (phase == "prepared" || phase == "corrupt")
			if blocked && (w.Code != 503 || called != before) {
				t.Fatal("mutation raced service verification")
			}
			if !blocked && w.Code != 200 {
				t.Fatal("safe request refused")
			}
		}
	}
}
