package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"

	"devboard/internal/localaccess"
)

// Local access: a narrow way in for another program on this computer.
//
// The controller's own token does everything its owner can. A program that only needs to hand the controller a task,
// see what is running, answer an agent's question or stop a run is given a *local access token* instead
// (internal/localaccess). It is accepted here only on the routes listed below, only with the request bodies listed
// for them, and only from this computer. It cannot register or change a repository, change a setting, touch Git or
// GitHub, pair a runner, read the controller's own token, or mint another token. What a run is allowed to do is still
// decided by the task's and the project's own execution settings and by the agent's own permission prompts, which come
// to the person at the controller: a request through this door starts a run with the task's own configuration or not at
// all (it cannot name an agent, a model, a policy, instructions or a runner).
//
// The owner sees every program that has access and revokes any of them at once (GET and DELETE /api/local-access).

// scopedRoute is one route a local access token may use.
type scopedRoute struct {
	method  string
	pattern string // a path, with {} for any one segment
	// fields are the top-level JSON fields a request body may have. nil means the route takes no body (an empty
	// body, or {}, is fine); a body with any other field is refused.
	fields []string
}

var scopedRoutes = []scopedRoute{
	// what is there
	{method: "GET", pattern: "/api/projects"},
	{method: "GET", pattern: "/api/projects/{}"},
	{method: "GET", pattern: "/api/projects/{}/tasks"},
	{method: "GET", pattern: "/api/projects/{}/runs"},
	{method: "GET", pattern: "/api/projects/{}/runs/{}"},
	{method: "GET", pattern: "/api/projects/{}/questions"},
	{method: "GET", pattern: "/api/projects/{}/questions/{}"},
	{method: "GET", pattern: "/api/runners"},
	{method: "GET", pattern: "/api/control-center"},
	{method: "GET", pattern: "/api/local-access/self"},
	// hand over a task. Not its execution settings or its schedule: only what a person would type.
	{method: "POST", pattern: "/api/projects/{}/tasks", fields: []string{"title", "description", "sourceRef", "workBranch", "baseBranch"}},
	// start, stop and answer. A run starts with the task's own configuration: nothing here chooses an agent, a model, a
	// policy, extra instructions or a runner.
	{method: "POST", pattern: "/api/projects/{}/tasks/{}/runs"},
	{method: "POST", pattern: "/api/projects/{}/runs/{}/stop"},
	{method: "POST", pattern: "/api/projects/{}/questions/{}/answer", fields: []string{"answer"}},
}

func matchSegments(pattern, path string) bool {
	p, q := strings.Split(strings.Trim(pattern, "/"), "/"), strings.Split(strings.Trim(path, "/"), "/")
	if len(p) != len(q) {
		return false
	}
	for i := range p {
		if p[i] == "{}" {
			if q[i] == "" {
				return false
			}
			continue
		}
		if p[i] != q[i] {
			return false
		}
	}
	return true
}

func scopedRouteFor(method, path string) (scopedRoute, bool) {
	for _, r := range scopedRoutes {
		if r.method == method && matchSegments(r.pattern, path) {
			return r, true
		}
	}
	return scopedRoute{}, false
}

// fromThisComputer reports whether a request came from a loopback address.
func fromThisComputer(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// scoped lets a request made with a local access token through only if it is on the list, from this computer, with a
// body the route allows.
func (s *Server) scoped(entry localaccess.Entry, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		route, ok := scopedRouteFor(r.Method, r.URL.Path)
		if !ok {
			writeError(w, http.StatusForbidden, "forbidden", "this program's access to Werkbord does not include that")
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes+1))
			if err != nil || len(raw) > maxBodyBytes {
				writeError(w, http.StatusRequestEntityTooLarge, "too_large", "request body too large")
				return
			}
			if msg := checkScopedBody(route, raw); msg != "" {
				writeError(w, http.StatusForbidden, "forbidden", msg)
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(raw))
		}
		if r.URL.Path == "/api/local-access/self" {
			writeJSON(w, http.StatusOK, entry)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// checkScopedBody returns why a body is not allowed on the route, or "".
func checkScopedBody(route scopedRoute, raw []byte) string {
	if len(bytes.TrimSpace(raw)) == 0 {
		return ""
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return "" // not an object: the handler's own decoding refuses it
	}
	allowed := map[string]bool{}
	for _, f := range route.fields {
		allowed[f] = true
	}
	for name := range fields {
		if !allowed[name] {
			return "this program's access to Werkbord does not include setting \"" + name + "\""
		}
	}
	return ""
}

// ---- the owner's side: see who has access, give it, take it away ----

func (s *Server) handleLocalAccessList(w http.ResponseWriter, r *http.Request) {
	if s.opt.LocalAccess == nil {
		writeJSON(w, http.StatusOK, []localaccess.Entry{})
		return
	}
	writeJSON(w, http.StatusOK, s.opt.LocalAccess.List())
}

func (s *Server) handleLocalAccessCreate(w http.ResponseWriter, r *http.Request) {
	if s.opt.LocalAccess == nil || !s.opt.AuthRequired {
		writeError(w, http.StatusConflict, "conflict", "local access needs the controller to require its token (requireToken)")
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := decode(w, r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	e, token, err := s.opt.LocalAccess.Create(req.Name)
	if err != nil {
		if err == localaccess.ErrTooMany {
			writeError(w, http.StatusConflict, "conflict", err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, "invalid", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, struct {
		localaccess.Entry
		Token string `json:"token"`
	}{Entry: e, Token: token})
}

func (s *Server) handleLocalAccessRevoke(w http.ResponseWriter, r *http.Request) {
	if s.opt.LocalAccess == nil {
		writeError(w, http.StatusNotFound, "not_found", "no such program")
		return
	}
	ok, err := s.opt.LocalAccess.Revoke(r.PathValue("id"))
	switch {
	case err != nil:
		writeError(w, http.StatusInternalServerError, "internal", "could not save")
	case !ok:
		writeError(w, http.StatusNotFound, "not_found", "no such program")
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// handleLocalAccessSelfRefused answers a request for /api/local-access/self made with the controller's own token: that
// token is not a program's access, so there is nothing to report about it. (A program's own token is answered before it
// gets here.)
func (s *Server) handleLocalAccessSelfRefused(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotFound, "not_found", "this is the controller's own token, not a program's access")
}
