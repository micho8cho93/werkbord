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
	"net/url"
	"strings"
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

// DesktopFrameAncestor is the one scheme that may show a page of a product inside a frame without being asked twice: the
// Werkbord desktop app's own window, whose pages come from the app and from no network. A web page on the Internet or in a
// browser cannot have it as its address, so allowing it lets the app compose Individual and Team workspaces in one window
// and lets nothing else frame them.
const DesktopFrameAncestor = "wails:"

// MaxEmbedOrigins bounds the extra origins ParseEmbedOrigins accepts.
const MaxEmbedOrigins = 4

// ParseEmbedOrigins reads a comma-separated list of exact loopback origins (http://127.0.0.1:PORT or
// http://localhost:PORT) that may also frame a product's pages. It exists so the desktop shell can be run from source and
// tested in an ordinary browser, where its address is a loopback port rather than the app's scheme; nothing else is
// accepted, so it cannot be used to let another site in.
func ParseEmbedOrigins(list string) ([]string, error) {
	var out []string
	for _, o := range strings.Split(list, ",") {
		o = strings.TrimSpace(o)
		if o == "" {
			continue
		}
		u, err := url.Parse(o)
		if err != nil || u.Scheme != "http" || (u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost") || u.Port() == "" || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil || o != u.Scheme+"://"+u.Host {
			return nil, fmt.Errorf("%q is not an exact loopback origin such as http://127.0.0.1:8080", o)
		}
		out = append(out, o)
	}
	if len(out) > MaxEmbedOrigins {
		return nil, fmt.Errorf("at most %d origins may be listed", MaxEmbedOrigins)
	}
	return out, nil
}

// FramedCSP is csp with frame-ancestors opened to the desktop app's window and to extra (from ParseEmbedOrigins).
func FramedCSP(csp string, extra []string) string {
	ancestors := strings.TrimSpace(DesktopFrameAncestor + " " + strings.Join(extra, " "))
	return strings.Replace(csp, "frame-ancestors 'none'", "frame-ancestors "+ancestors, 1)
}

// SecurityHeadersFramed is SecurityHeaders for a product the desktop app shows in a frame. It sends no X-Frame-Options
// (which cannot name an allowed ancestor); the Content-Security-Policy's frame-ancestors, which csp must carry, says who
// may frame it and every other page is refused as before.
func SecurityHeadersFramed(csp string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy", csp)
		next.ServeHTTP(w, r)
	})
}

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
