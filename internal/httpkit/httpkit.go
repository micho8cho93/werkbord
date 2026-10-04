// Package httpkit is the HTTP plumbing shared by every Werkbord product: JSON
// responses and the error envelope, strict request-body decoding, and the
// middleware that logs requests, recovers from panics and sets security headers.
// It holds no routes and no business rules, and imports nothing from any
// product, so each product's API package can build on it.
package httpkit

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// ErrorBody is the JSON envelope of every error response:
// {"error": {"code": "...", "message": "..."}}.
type ErrorBody struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// WriteJSON writes v as the response body. Responses are never cacheable: they
// are per-user and change.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// WriteError writes the error envelope.
func WriteError(w http.ResponseWriter, status int, code, msg string) {
	var b ErrorBody
	b.Error.Code, b.Error.Message = code, msg
	WriteJSON(w, status, b)
}

// DecodeJSON reads a request body that must be exactly one JSON object with no
// unknown fields and at most max bytes.
func DecodeJSON(w http.ResponseWriter, r *http.Request, dst any, max int64) error {
	r.Body = http.MaxBytesReader(w, r.Body, max)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("request body: %v", err)
	}
	if dec.Decode(&struct{}{}) != io.EOF {
		return fmt.Errorf("request body must be a single JSON object")
	}
	return nil
}

// statusRecorder captures the response status for logging. Unwrap lets
// http.ResponseController reach the underlying Flusher for SSE.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	if r.status == 0 {
		r.status = code
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.ResponseWriter.Write(b)
}

func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

// LogRequests logs each request. The query string is never logged: it may carry
// an access token.
func LogRequests(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		level := slog.LevelDebug
		if r.Method != http.MethodGet || rec.status >= 500 {
			level = slog.LevelInfo
		}
		log.Log(r.Context(), level, "http request",
			"method", r.Method, "path", r.URL.Path, "status", rec.status, "duration", time.Since(start).Round(time.Microsecond))
	})
}

// RecoverPanics turns a panic in a handler into a logged 500.
func RecoverPanics(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				log.Error("panic in handler", "path", r.URL.Path, "panic", v)
				WriteError(w, http.StatusInternalServerError, "internal", "internal error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// DefaultCSP is the Content-Security-Policy of a product that serves its own
// static web app and talks only to its own origin.
const DefaultCSP = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; " +
	"connect-src 'self'; manifest-src 'self'; worker-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'"

// SecurityHeaders sets the headers every response carries.
func SecurityHeaders(csp string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Content-Security-Policy", csp)
		next.ServeHTTP(w, r)
	})
}
