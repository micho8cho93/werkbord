package api

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"werkbord/internal/team/domain"
	"werkbord/internal/team/service"
)

// Comments on tickets and the notifications their mentions make. As everywhere here, a handler decodes, calls one service
// method and encodes: who may comment, whose notification is whose and what a mention does are the service's and the domain's.

// maxCommentBodyBytes is what a request that carries a comment may be: a comment may be 20,000 characters, which does not
// fit in what any other request is given, and the parts it is made of (and an encoder that escapes every character) need room.
const maxCommentBodyBytes = 512 << 10

// bodyLimit is the most a request's body may be.
func bodyLimit(r *http.Request) int64 {
	if strings.HasSuffix(r.URL.Path, "/product-map/proposals") {
		return 600 << 10
	}
	if p := r.URL.Path; strings.HasSuffix(p, "/comments") || strings.Contains(p, "/comments/") {
		return maxCommentBodyBytes
	}
	return maxBodyBytes
}

func intQuery(r *http.Request, name string) (int64, error) {
	v := r.URL.Query().Get(name)
	if v == "" {
		return 0, nil
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("%w: %s is not a number", domain.ErrInvalid, name)
	}
	return n, nil
}

func (s *Server) handleListComments(w http.ResponseWriter, r *http.Request) {
	var q service.CommentListQuery
	var err error
	var limit int64
	if limit, err = intQuery(r, "limit"); err == nil {
		q.Limit = int(limit)
		if q.Before, err = intQuery(r, "before"); err == nil {
			q.After, err = intQuery(r, "after")
			q.HasAfter = r.URL.Query().Has("after")
		}
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	q.Around = r.URL.Query().Get("around")
	list, err := s.opt.Service.ListComments(r.Context(), actorOf(r), r.PathValue("id"), r.PathValue("tid"), q)
	s.respond(w, r, http.StatusOK, list, err)
}

func (s *Server) handlePostComment(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ClientID string           `json:"clientId"`
		Segments []domain.Segment `json:"segments"`
	}
	if !s.decode(w, r, &in) {
		return
	}
	c, created, err := s.opt.Service.PostComment(r.Context(), actorOf(r), r.PathValue("id"), r.PathValue("tid"), service.CommentInput{ClientID: in.ClientID, Segments: in.Segments})
	status := http.StatusCreated
	if !created { // the same comment, posted again: the first one is returned, and nothing is made twice
		status = http.StatusOK
	}
	s.respond(w, r, status, c, err)
}

func (s *Server) handleEditComment(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Version  *int64           `json:"version"`
		Segments []domain.Segment `json:"segments"`
	}
	if !s.decode(w, r, &in) {
		return
	}
	if in.Version == nil || *in.Version < 1 {
		s.fail(w, r, fmt.Errorf("%w: say which version of the comment you edited", domain.ErrInvalid))
		return
	}
	c, err := s.opt.Service.EditComment(r.Context(), actorOf(r), r.PathValue("id"), r.PathValue("tid"), r.PathValue("cid"), *in.Version, in.Segments)
	s.respond(w, r, http.StatusOK, c, err)
}

func (s *Server) handleDeleteComment(w http.ResponseWriter, r *http.Request) {
	var version *int64
	if r.URL.Query().Get("version") != "" {
		v, err := intQuery(r, "version")
		if err != nil {
			s.fail(w, r, err)
			return
		}
		version = &v
	}
	c, err := s.opt.Service.DeleteComment(r.Context(), actorOf(r), r.PathValue("id"), r.PathValue("tid"), r.PathValue("cid"), version)
	s.respond(w, r, http.StatusOK, c, err)
}

func (s *Server) handleListNotifications(w http.ResponseWriter, r *http.Request) {
	limit, err := intQuery(r, "limit")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var all bool
	switch r.URL.Query().Get("state") {
	case "", "unread":
	case "all":
		all = true
	default:
		s.fail(w, r, fmt.Errorf("%w: state is unread or all", domain.ErrInvalid))
		return
	}
	list, err := s.opt.Service.ListNotifications(r.Context(), actorOf(r), service.NotificationListQuery{ProjectID: r.URL.Query().Get("projectId"), IncludeRead: all, Limit: int(limit), Cursor: r.URL.Query().Get("cursor")})
	s.respond(w, r, http.StatusOK, list, err)
}

func (s *Server) handleMarkNotificationRead(w http.ResponseWriter, r *http.Request) {
	n, err := s.opt.Service.MarkNotificationRead(r.Context(), actorOf(r), r.PathValue("nid"))
	s.respond(w, r, http.StatusOK, n, err)
}
