package api

import (
	"crypto/subtle"
	"net"
	"net/http"
	"net/url"
	"strings"

	"werkbord/internal/localaccess"
	"werkbord/internal/phoneaccess"
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
	if s.opt.ClientListener {
		return s.clientAuthenticate(next)
	}
	if !s.opt.AuthRequired {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/api/connections/oauth/callback" && fromThisComputer(r) {
			// A local browser returns without the controller token. A scoped program token must not turn this
			// one-shot browser route into another local-access API endpoint.
			if got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "); got != "" {
				want := []byte(s.currentToken())
				if len(want) == 0 || subtle.ConstantTimeCompare([]byte(got), want) != 1 {
					writeError(w, http.StatusForbidden, "forbidden", "program credentials cannot use the OAuth browser callback")
					return
				}
			}
			next.ServeHTTP(w, r)
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/api/health" {
			next.ServeHTTP(w, r) // the PWA shell itself is public; its data is not
			return
		}
		if r.Method == http.MethodPost && r.URL.Path == "/api/phones/enroll" {
			next.ServeHTTP(w, r) // a phone has no token yet: its single-use code is what it presents (phone.go)
			return
		}
		got := ""
		if v, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
			got = v
		} else if r.URL.Path == "/api/events" {
			got = r.URL.Query().Get("access_token") // EventSource cannot set headers
		}
		want := []byte(s.currentToken())
		if len(want) > 0 && subtle.ConstantTimeCompare([]byte(got), want) == 1 {
			if s.opt.AssistantPhonesOnly && (r.URL.Path == "/api/assistant" || strings.HasPrefix(r.URL.Path, "/api/assistant/")) {
				// On the private network the assistant is for enrolled phones, not for the controller's own token (which
				// has never reached it there).
				writeError(w, http.StatusForbidden, "forbidden", "the assistant is not served on this address to the controller's token")
				return
			}
			next.ServeHTTP(w, r)
			return
		}
		// Another program on this computer, with its own narrow credential (localaccess.go): never from elsewhere.
		if s.opt.LocalAccess != nil && strings.HasPrefix(got, localaccess.TokenPrefix) && fromThisComputer(r) {
			if entry, ok := s.opt.LocalAccess.Authenticate(got); ok {
				s.scoped(entry, next).ServeHTTP(w, r)
				return
			}
		}
		// A phone that was enrolled, with its own credential (phone.go): from the doors that are for phones, and nowhere else.
		if s.opt.Phones != nil && strings.HasPrefix(got, phoneaccess.TokenPrefix) && s.phoneDoor(r) {
			if entry, ok := s.opt.Phones.Authenticate(got); ok {
				s.phoneScoped(entry, next).ServeHTTP(w, r)
				return
			}
		}
		writeError(w, http.StatusUnauthorized, "unauthorized", "missing or invalid token")
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

func (s *Server) currentToken() string {
	if s.opt.TokenSource != nil {
		return s.opt.TokenSource()
	}
	return s.opt.Token
}
