package httpkit

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWriteErrorUsesTheEnvelopeAndIsNotCacheable(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteError(rec, http.StatusConflict, "conflict", "nope")
	if rec.Code != http.StatusConflict || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("status %d, cache-control %q", rec.Code, rec.Header().Get("Cache-Control"))
	}
	var b ErrorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &b); err != nil || b.Error.Code != "conflict" || b.Error.Message != "nope" {
		t.Fatalf("%s, %v", rec.Body, err)
	}
}

func TestDecodeJSONIsStrict(t *testing.T) {
	type in struct {
		Name string `json:"name"`
	}
	for name, body := range map[string]string{
		"unknown field": `{"name":"a","extra":1}`,
		"two objects":   `{"name":"a"}{"name":"b"}`,
		"not json":      `name`,
		"too large":     `{"name":"` + strings.Repeat("x", 100) + `"}`,
	} {
		r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		var v in
		if err := DecodeJSON(httptest.NewRecorder(), r, &v, 64); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"a"}`))
	var v in
	if err := DecodeJSON(httptest.NewRecorder(), r, &v, 64); err != nil || v.Name != "a" {
		t.Fatalf("%v %+v", err, v)
	}
}

func TestMiddlewareRecoversLogsAndSetsHeaders(t *testing.T) {
	var logs bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	var h http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { panic("boom") })
	h = SecurityHeaders(DefaultCSP, h)
	h = LogRequests(log, h)
	h = RecoverPanics(log, h)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x?access_token=secret", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("a panic should be a 500, got %d", rec.Code)
	}
	if rec.Header().Get("Content-Security-Policy") != DefaultCSP || rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("security headers missing")
	}
	if strings.Contains(logs.String(), "secret") {
		t.Fatalf("the query string reached the log: %s", logs.String())
	}
}
