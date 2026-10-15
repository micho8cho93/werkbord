package api

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"devboard/internal/netprivate"
	"devboard/internal/phoneaccess"
)

// Phone access: a phone that is enrolled has a credential of its own.
//
// The owner makes a single-use enrollment code (POST /api/phones/codes, shown as a QR code), the phone presents it once
// (POST /api/phones/enroll) and is given a phone token (internal/phoneaccess). The token is accepted only on the routes
// listed below, and only from the doors that are for phones: the private network's listener, and this computer. The
// owner sees every phone and takes any away at once (GET /api/phones, DELETE /api/phones/{id}); taking one away also ends
// whatever it has open, the live event stream included.
//
// Nothing here knows how the phone got to the controller. A different way in (a relay, say) only has to deliver the
// phone's requests to the same handler, as a request that phoneDoor accepts.
//
// A phone is the owner's remote control, not a second owner. It can see everything the Control Deck and the task pages
// show, work with tasks and runs, answer agents and use the Assistant. It cannot change a setting, register or change a
// repository, touch the controller's token, pair or revoke a runner, enroll or revoke phones, or do anything in Git that
// cannot be undone. Those are the owner's, at the computer.

const forbiddenForPhone = "this phone's access to Werkbord does not include that: do it at your computer"

// phoneRoute is one route an enrolled phone may use.
type phoneRoute struct {
	method  string
	pattern string // a path, with {} for any one segment
	// fields are the top-level JSON fields a request body may have; nil means an empty body (or {}). Ignored when
	// anyBody is set, for the routes whose body is the person's own content (a message, a file).
	fields  []string
	anyBody bool
}

var phoneRoutes = []phoneRoute{
	// what this phone is, and signing it out
	{method: "GET", pattern: "/api/phones/self"},
	{method: "PATCH", pattern: "/api/phones/self", fields: []string{"name"}},
	{method: "POST", pattern: "/api/phones/self/signout"},

	// reading: the Control Deck, the task pages, Git's Control Center
	{method: "GET", pattern: "/api/events"},
	{method: "GET", pattern: "/api/onboarding"},
	{method: "GET", pattern: "/api/settings"},
	{method: "GET", pattern: "/api/agents"},
	{method: "GET", pattern: "/api/agents/{}/options"},
	{method: "GET", pattern: "/api/runners"},
	{method: "GET", pattern: "/api/labels"},
	{method: "GET", pattern: "/api/control-center"},
	{method: "GET", pattern: "/api/analytics"},
	{method: "GET", pattern: "/api/workspace/v1/summary"},
	{method: "GET", pattern: "/api/projects"},
	{method: "GET", pattern: "/api/projects/{}"},
	{method: "GET", pattern: "/api/projects/{}/analytics"},
	{method: "GET", pattern: "/api/projects/{}/schedule"},
	{method: "GET", pattern: "/api/projects/{}/orchestration"},
	{method: "GET", pattern: "/api/projects/{}/timeline"},
	{method: "GET", pattern: "/api/projects/{}/tasks"},
	{method: "GET", pattern: "/api/projects/{}/tasks/{}/runs"},
	{method: "GET", pattern: "/api/projects/{}/runs"},
	{method: "GET", pattern: "/api/projects/{}/activity"},
	{method: "GET", pattern: "/api/projects/{}/runs/{}"},
	{method: "GET", pattern: "/api/projects/{}/runs/{}/events"},
	{method: "GET", pattern: "/api/projects/{}/questions"},
	{method: "GET", pattern: "/api/projects/{}/questions/{}"},
	{method: "GET", pattern: "/api/projects/{}/worktrees/{}"},
	{method: "GET", pattern: "/api/projects/{}/git"},
	{method: "GET", pattern: "/api/projects/{}/git/pull-requests"},
	{method: "GET", pattern: "/api/projects/{}/git/commits"},
	{method: "GET", pattern: "/api/projects/{}/git/compare"},
	{method: "GET", pattern: "/api/projects/{}/git/diff"},
	{method: "GET", pattern: "/api/projects/{}/git/changes"},
	{method: "GET", pattern: "/api/projects/{}/git/changes/diff"},
	{method: "GET", pattern: "/api/projects/{}/git/health"},

	// working with tasks and runs. Not their execution settings (agent, model, policy, runner): those are the owner's.
	{method: "POST", pattern: "/api/projects/{}/refresh"},
	{method: "POST", pattern: "/api/projects/{}/tasks", fields: []string{"title", "description", "sourceRef", "workBranch", "baseBranch", "orchestration", "workMode", "labelIds", "plan"}},
	{method: "PATCH", pattern: "/api/projects/{}/tasks/{}", fields: []string{"title", "description", "state", "position", "orchestration", "workMode", "labelIds", "plan", "version", "archived"}},
	{method: "POST", pattern: "/api/projects/{}/tasks/archive-done"},
	{method: "POST", pattern: "/api/projects/{}/tasks/{}/runs", fields: []string{"instructions", "resume"}},
	{method: "POST", pattern: "/api/projects/{}/runs/{}/input", fields: []string{"text"}},
	{method: "POST", pattern: "/api/projects/{}/runs/{}/finish"},
	{method: "POST", pattern: "/api/projects/{}/runs/{}/stop"},
	{method: "POST", pattern: "/api/projects/{}/runs/{}/handoff"},
	{method: "PUT", pattern: "/api/projects/{}/runs/{}/handoff", fields: []string{"version", "handoff"}},
	{method: "POST", pattern: "/api/projects/{}/runs/{}/continue", fields: []string{"purpose", "instructions", "selectedContext"}},
	{method: "PATCH", pattern: "/api/projects/{}/runs/{}/assessment", fields: []string{"acceptance"}},
	{method: "POST", pattern: "/api/projects/{}/questions/{}/answer", fields: []string{"answer"}},

	// Git: looking, fetching (it only updates what Git already knows of the remote) and the repository health list.
	// No merge, push, branch delete, worktree clean or pull request: see phoneRefusedGit.
	{method: "POST", pattern: "/api/projects/{}/git/fetch"},
	{method: "POST", pattern: "/api/projects/{}/git/health/refresh"},
	{method: "POST", pattern: "/api/projects/{}/git/health/findings/{}/dismiss"},
	{method: "POST", pattern: "/api/projects/{}/git/health/findings/{}/reopen"},

	// the Assistant. Not the folders it is given on the computer, nor its audit log.
	{method: "GET", pattern: "/api/assistant/providers"},
	{method: "GET", pattern: "/api/assistant/sessions"},
	{method: "POST", pattern: "/api/assistant/sessions", anyBody: true},
	{method: "GET", pattern: "/api/assistant/sessions/{}"},
	{method: "PATCH", pattern: "/api/assistant/sessions/{}", anyBody: true},
	{method: "DELETE", pattern: "/api/assistant/sessions/{}"},
	{method: "POST", pattern: "/api/assistant/sessions/{}/messages", anyBody: true},
	{method: "POST", pattern: "/api/assistant/sessions/{}/attachments", anyBody: true},
	{method: "DELETE", pattern: "/api/assistant/sessions/{}/attachments/{}"},
	{method: "POST", pattern: "/api/assistant/sessions/{}/cancel"},
	{method: "GET", pattern: "/api/assistant/sessions/{}/events"},
	{method: "POST", pattern: "/api/assistant/sessions/{}/actions/{}", anyBody: true},
}

// phoneRefusedGit is the Git the phone is refused on purpose, with the reason. Nothing reads it but the test that keeps
// it honest: these are the decisions, written down where a change to the list above will be seen.
var phoneRefusedGit = map[string]string{
	"POST /api/projects/{}/git/push":                 "pushes commits to a remote: cannot be undone from here",
	"POST /api/projects/{}/git/merge":                "changes a branch's history",
	"POST /api/projects/{}/git/merge/plan":           "only useful with the merge",
	"POST /api/projects/{}/git/branches/delete":      "deletes a branch (and with it the remote's, when asked)",
	"POST /api/projects/{}/git/branches/delete-plan": "only useful with the delete",
	"POST /api/projects/{}/git/worktrees/clean":      "deletes a worktree's files",
	"POST /api/projects/{}/git/worktrees/clean-plan": "only useful with the clean",
	"POST /api/projects/{}/git/pull-requests":        "publishes to GitHub as the owner",
}

func phoneRouteFor(method, path string) (phoneRoute, bool) {
	for _, r := range phoneRoutes {
		if r.method == method && matchSegments(r.pattern, path) {
			return r, true
		}
	}
	return phoneRoute{}, false
}

type phoneKey struct{}

// phoneDoor reports whether a request came by a way in that is for phones: the private network's listener, or this
// computer. A phone token is not looked at anywhere else.
func (s *Server) phoneDoor(r *http.Request) bool {
	return netprivate.IsPrivateRequest(r.Context()) || fromThisComputer(r)
}

// phoneScoped lets a request made with a phone token through only if it is on the list, with a body the route allows.
// It ends the request when the phone is revoked, so a stream that is open does not outlive its permission.
func (s *Server) phoneScoped(entry phoneaccess.Entry, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		route, ok := phoneRouteFor(r.Method, r.URL.Path)
		if !ok {
			writeError(w, http.StatusForbidden, "forbidden", forbiddenForPhone)
			return
		}
		if !route.anyBody && r.Method != http.MethodGet && r.Method != http.MethodHead {
			raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes+1))
			if err != nil || len(raw) > maxBodyBytes {
				writeError(w, http.StatusRequestEntityTooLarge, "too_large", "request body too large")
				return
			}
			if msg := checkScopedBody(scopedRoute{fields: route.fields}, raw); msg != "" {
				writeError(w, http.StatusForbidden, "forbidden", strings.Replace(msg, "this program's access", "this phone's access", 1))
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(raw))
		}

		ctx, cancel := context.WithCancel(context.WithValue(r.Context(), phoneKey{}, entry))
		defer cancel()
		revoked, stop := s.opt.Phones.Watch(entry.ID)
		defer stop()
		go func() {
			select {
			case <-revoked:
				cancel()
			case <-ctx.Done():
			}
		}()
		r = r.WithContext(ctx)

		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/phones/self":
			writeJSON(w, http.StatusOK, entry)
		case r.Method == http.MethodPatch && r.URL.Path == "/api/phones/self":
			var req struct {
				Name string `json:"name"`
			}
			if err := decode(w, r, &req); err != nil {
				s.fail(w, r, err)
				return
			}
			s.renamePhone(w, entry.ID, req.Name)
		case r.Method == http.MethodPost && r.URL.Path == "/api/phones/self/signout":
			if _, err := s.opt.Phones.Revoke(entry.ID); err != nil {
				writeError(w, http.StatusInternalServerError, "internal", "could not save")
				return
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			next.ServeHTTP(w, r)
		}
	})
}

// ---- enrollment: the one request a phone makes without a token ----

// attemptLimiter bounds how often enrollment may be tried at all, from anywhere: the codes cannot be guessed, so this is
// about noise, and about not being a way to spend the controller's time.
type attemptLimiter struct {
	mu   sync.Mutex
	max  int
	per  time.Duration
	hits []time.Time
	now  func() time.Time
}

// allow records an attempt and reports whether it may go ahead, or how long to wait.
func (l *attemptLimiter) allow() (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	kept := l.hits[:0]
	for _, h := range l.hits {
		if now.Sub(h) < l.per {
			kept = append(kept, h)
		}
	}
	l.hits = kept
	if len(l.hits) >= l.max {
		return false, l.per - now.Sub(l.hits[0])
	}
	l.hits = append(l.hits, now)
	return true, 0
}

const enrollAttemptsPerMinute = 10

func (s *Server) handlePhoneEnroll(w http.ResponseWriter, r *http.Request) {
	if s.opt.Phones == nil || !s.phoneDoor(r) {
		writeError(w, http.StatusForbidden, "forbidden", "phones cannot be enrolled here")
		return
	}
	if ok, wait := s.enrollLimit.allow(); !ok {
		w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many tries: wait a minute, then scan again")
		return
	}
	var req struct {
		Code string `json:"code"`
		Name string `json:"name"`
	}
	if err := decode(w, r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	e, token, err := s.opt.Phones.Enroll(req.Code, req.Name)
	switch {
	case err == phoneaccess.ErrInvalidCode:
		writeError(w, http.StatusForbidden, "forbidden", "that code does not work: make a new one on your computer")
	case err == phoneaccess.ErrTooMany:
		writeError(w, http.StatusConflict, "conflict", err.Error())
	case err != nil:
		writeError(w, http.StatusBadRequest, "invalid", err.Error())
	default:
		writeJSON(w, http.StatusCreated, struct {
			phoneaccess.Entry
			Token string `json:"token"`
		}{Entry: e, Token: token})
	}
}

// ---- the owner's side: make a code, see the phones, take one away ----

// handlePhoneCode makes an enrollment code and the link and QR code that carry it. The link is the address a phone opens
// and the code in its fragment, which a browser never sends to a server. With no address to open (the private network is
// not connected) there is no link or QR code, only the code: a client that needs one says so.
func (s *Server) handlePhoneCode(w http.ResponseWriter, r *http.Request) {
	if s.opt.Phones == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "phone access is not available here")
		return
	}
	code, expires, err := s.opt.Phones.NewCode()
	if err != nil {
		if err == phoneaccess.ErrTooMany {
			writeError(w, http.StatusConflict, "conflict", err.Error())
			return
		}
		s.fail(w, r, err)
		return
	}
	out := map[string]any{"code": code, "expiresAt": expires, "link": "", "qrSvg": ""}
	if base := s.phoneBaseURL(); base != "" {
		link := strings.TrimSuffix(base, "/") + "/#enroll=" + code
		svg, err := netprivate.QRSVG(link)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		out["link"], out["qrSvg"] = link, svg
	}
	writeJSON(w, http.StatusCreated, out)
}

// phoneBaseURL is the address a phone opens, or "" when there is none yet.
func (s *Server) phoneBaseURL() string {
	if s.opt.Network == nil {
		return ""
	}
	st := s.opt.Network.Status()
	if st.State != netprivate.StateConnected {
		return ""
	}
	return st.URL
}

func (s *Server) handlePhoneList(w http.ResponseWriter, r *http.Request) {
	list := []phoneaccess.Entry{}
	if s.opt.Phones != nil {
		list = s.opt.Phones.List()
	}
	writeJSON(w, http.StatusOK, map[string]any{"phones": list})
}

func (s *Server) renamePhone(w http.ResponseWriter, id, name string) {
	e, ok, err := s.opt.Phones.Rename(id, name)
	switch {
	case err != nil && !strings.HasPrefix(err.Error(), "phoneaccess: a phone's name"):
		writeError(w, http.StatusInternalServerError, "internal", "could not save")
	case err != nil:
		writeError(w, http.StatusBadRequest, "invalid", err.Error())
	case !ok:
		writeError(w, http.StatusNotFound, "not_found", "no such phone")
	default:
		writeJSON(w, http.StatusOK, e)
	}
}

func (s *Server) handlePhoneRename(w http.ResponseWriter, r *http.Request) {
	if s.opt.Phones == nil {
		writeError(w, http.StatusNotFound, "not_found", "no such phone")
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := decode(w, r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	s.renamePhone(w, r.PathValue("id"), req.Name)
}

func (s *Server) handlePhoneRevoke(w http.ResponseWriter, r *http.Request) {
	if s.opt.Phones == nil {
		writeError(w, http.StatusNotFound, "not_found", "no such phone")
		return
	}
	ok, err := s.opt.Phones.Revoke(r.PathValue("id"))
	switch {
	case err != nil:
		writeError(w, http.StatusInternalServerError, "internal", "could not save")
	case !ok:
		writeError(w, http.StatusNotFound, "not_found", "no such phone")
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// handlePhoneSelfRefused answers /api/phones/self made with the controller's own token: that is not a phone, so there is
// nothing to say about it. (A phone's own token is answered before it gets here.)
func (s *Server) handlePhoneSelfRefused(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotFound, "not_found", "this is the controller's own token, not a phone's")
}
