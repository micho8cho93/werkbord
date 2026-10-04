package api

import (
	"crypto/subtle"
	"net"
	"net/http"
	"net/url"
	"strings"
)

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
