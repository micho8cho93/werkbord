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

func TestOnlyExactLoopbackOriginsMayBeAddedAsFramingParents(t *testing.T) {
	got, err := ParseEmbedOrigins("http://127.0.0.1:5173, http://localhost:8080")
	if err != nil || len(got) != 2 || got[0] != "http://127.0.0.1:5173" {
		t.Fatalf("%v %v", got, err)
	}
	for _, bad := range []string{"https://evil.example", "http://evil.example:80", "http://127.0.0.1", "http://127.0.0.1:80/", "http://127.0.0.1:80/x", "*", "http://*:80", "http://127.0.0.1.evil.example:80", "http://user@127.0.0.1:80", "wails:", "http://127.0.0.1:1,http://127.0.0.1:2,http://127.0.0.1:3,http://127.0.0.1:4,http://127.0.0.1:5"} {
		if _, err := ParseEmbedOrigins(bad); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
}

func TestAFramedPageNamesTheDesktopWindowAndNoOneElse(t *testing.T) {
	csp := FramedCSP(DefaultCSP, []string{"http://127.0.0.1:9"})
	if !strings.Contains(csp, "frame-ancestors wails: http://127.0.0.1:9;") || strings.Contains(csp, "frame-ancestors 'none'") || strings.Contains(csp, "*") {
		t.Fatalf("csp = %s", csp)
	}
	if !strings.Contains(FramedCSP(DefaultCSP, nil), "frame-ancestors wails;") && !strings.Contains(FramedCSP(DefaultCSP, nil), "frame-ancestors wails:;") {
		t.Fatalf("csp = %s", FramedCSP(DefaultCSP, nil))
	}
	rec := httptest.NewRecorder()
	SecurityHeadersFramed(csp, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Header().Get("X-Frame-Options") != "" || rec.Header().Get("Content-Security-Policy") != csp || rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("headers = %v", rec.Header())
	}
	// A product that does not opt in still refuses every frame.
	rec = httptest.NewRecorder()
	SecurityHeaders(DefaultCSP, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Header().Get("X-Frame-Options") != "DENY" || !strings.Contains(rec.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") {
		t.Fatalf("headers = %v", rec.Header())
	}
}
