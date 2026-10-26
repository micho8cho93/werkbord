package api

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"werkbord/internal/connections"
	"werkbord/internal/store/sqlite"
)

func TestUpstreamMCPControlsAreAvailableOnlyOnTheControllerListener(t *testing.T) {
	db, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "api.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	management := New(Options{Store: db, Connections: connections.New(db, nil, nil, nil), AuthRequired: true, Token: "owner-token"}).Handler()
	private := New(Options{Store: db, AuthRequired: true, Token: "owner-token"}).Handler()
	request := func(handler http.Handler) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/api/connections", nil)
		r.Header.Set("Authorization", "Bearer owner-token")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	if got := request(management); got.Code != http.StatusOK {
		t.Fatalf("controller listener status = %d, body=%s", got.Code, got.Body.String())
	}
	if got := request(private); got.Code != http.StatusServiceUnavailable {
		t.Fatalf("private listener exposed connection controls: status=%d body=%s", got.Code, got.Body.String())
	}
}
