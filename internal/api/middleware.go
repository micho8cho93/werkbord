package api

import (
	"crypto/subtle"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

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

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		level := slog.LevelDebug
		if r.Method != http.MethodGet || rec.status >= 500 {
			level = slog.LevelInfo
		}
		// The query string is never logged: it may carry the access token.
		s.log.Log(r.Context(), level, "http request",
			"method", r.Method, "path", r.URL.Path, "status", rec.status, "duration", time.Since(start).Round(time.Microsecond))
	})
}

func (s *Server) recoverPanics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				s.log.Error("panic in handler", "path", r.URL.Path, "panic", v)
				writeError(w, http.StatusInternalServerError, "internal", "internal error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Content-Security-Policy",
			"default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; "+
				"connect-src 'self'; manifest-src 'self'; worker-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		next.ServeHTTP(w, r)
	})
}

// checkHost defends against DNS rebinding when the API is unauthenticated:
// a malicious page cannot reach 127.0.0.1 through a hostname it controls.
// When a token is required the token is the defence instead.
func (s *Server) checkHost(next http.Handler) http.Handler {
	if s.opt.AuthRequired {
		return next
	}
	allowed := map[string]bool{}
	for _, h := range s.opt.AllowedHosts {
		allowed[strings.ToLower(h)] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isLocalHost(r.Host) && !allowed[strings.ToLower(hostOnly(r.Host))] {
			writeError(w, http.StatusForbidden, "forbidden_host", "host not allowed")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// checkOrigin rejects cross-site state-changing requests.
func (s *Server) checkOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
		default:
			if origin := r.Header.Get("Origin"); origin != "" {
				u, err := url.Parse(origin)
				if err != nil || !strings.EqualFold(u.Host, r.Host) {
					writeError(w, http.StatusForbidden, "forbidden_origin", "cross-origin request rejected")
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) authenticate(next http.Handler) http.Handler {
	if !s.opt.AuthRequired {
		return next
	}
	want := []byte(s.opt.Token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/api/health" {
			next.ServeHTTP(w, r) // the PWA shell itself is public; its data is not
			return
		}
		got := ""
		if v, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
			got = v
		} else if r.URL.Path == "/api/events" {
			got = r.URL.Query().Get("access_token") // EventSource cannot set headers
		}
		if len(want) == 0 || subtle.ConstantTimeCompare([]byte(got), want) != 1 {
			writeError(w, http.StatusUnauthorized, "unauthorized", "missing or invalid token")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func hostOnly(hostport string) string {
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		return strings.Trim(h, "[]")
	}
	return strings.Trim(hostport, "[]")
}

func isLocalHost(hostport string) bool {
	h := strings.ToLower(hostOnly(hostport))
	if h == "localhost" || strings.HasSuffix(h, ".localhost") {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}
