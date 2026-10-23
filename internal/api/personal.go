package api

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"unicode/utf8"

	"devboard/internal/domain"
)

const personalMaxBody = 64 << 10

func (s *Server) personalReady(w http.ResponseWriter, r *http.Request) bool {
	want := s.currentToken()
	got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !s.opt.AuthRequired || !fromThisComputer(r) || want == "" || subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
		writeError(w, 403, "forbidden", "personal workspaces require the local controller owner token")
		return false
	}
	if s.opt.Personal == nil {
		writeError(w, 503, "unavailable", "personal workspaces are unavailable on this listener")
		return false
	}
	return true
}
func (s *Server) personalRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/personal/v1/workspaces/{id}/deletion-preview", s.handlePersonalLifecycle)
	mux.HandleFunc("GET /api/personal/v1/workspaces/{id}/history", s.handlePersonalLifecycle)
	mux.HandleFunc("GET /api/personal/v1/workspaces/{id}/revisions/{revision}", s.handlePersonalLifecycle)
	mux.HandleFunc("POST /api/personal/v1/workspaces/{id}/rollback", s.handlePersonalLifecycle)
	mux.HandleFunc("GET /api/personal/v1/collections/{id}/usage", s.handlePersonalLifecycle)
	for _, resource := range []struct {
		path   string
		handle http.HandlerFunc
	}{
		{"/api/personal/v1/workspaces", s.handlePersonalWorkspaces},
		{"/api/personal/v1/collections", s.handlePersonalCollections},
		{"/api/personal/v1/collections/{cid}/records", s.handlePersonalRecords},
	} {
		for _, method := range []string{"GET", "POST"} {
			mux.HandleFunc(method+" "+resource.path, resource.handle)
		}
		for _, method := range []string{"GET", "PUT", "DELETE"} {
			mux.HandleFunc(method+" "+resource.path+"/{id}", resource.handle)
		}
	}
	mux.HandleFunc("GET /api/personal/v1/capabilities", func(w http.ResponseWriter, r *http.Request) {
		if !s.personalReady(w, r) {
			return
		}
		if len(r.URL.RawQuery) != 0 {
			s.fail(w, r, domain.ErrInvalid)
			return
		}
		writeJSON(w, 200, map[string]any{"uiSchemaVersion": domain.PersonalSchemaVersion, "collectionSchemaVersion": 1, "icons": domain.PersonalIcons(), "readBindingKinds": []string{"individual.project", "individual.tasks"}, "maxDocumentBytes": domain.PersonalMaxDocumentBytes, "maxBodyBytes": personalMaxBody, "maxPageSize": domain.PersonalMaxPage})
	})
}

// Reject duplicate keys before decoding: json.Unmarshal's last-key-wins rule
// must never turn a reviewed schema or revision into a different request.
func personalJSONTokens(dec *json.Decoder, depth int) error {
	if depth > 32 {
		return errors.New("JSON is too deep")
	}
	token, err := dec.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for dec.More() {
			key, err := dec.Token()
			if err != nil {
				return err
			}
			k, ok := key.(string)
			if !ok || seen[strings.ToLower(k)] {
				return errors.New("duplicate JSON key")
			}
			seen[strings.ToLower(k)] = true
			if err = personalJSONTokens(dec, depth+1); err != nil {
				return err
			}
		}
	case '[':
		for dec.More() {
			if err = personalJSONTokens(dec, depth+1); err != nil {
				return err
			}
		}
	default:
		return errors.New("invalid JSON")
	}
	_, err = dec.Token()
	return err
}

// Go's JSON decoder accepts case-insensitive field names and null scalars.
// The persisted contract uses exact keys and typed non-null objects/scalars.
// A null optional list has the same meaning as an omitted/empty list.
func personalJSONShape(raw json.RawMessage, typ reflect.Type) error {
	if typ == reflect.TypeFor[json.RawMessage]() {
		return nil
	} // field values are checked against the collection schema
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		if typ.Kind() == reflect.Slice {
			return nil
		}
		return errors.New("null objects and scalar fields are not allowed")
	}
	switch typ.Kind() {
	case reflect.Struct:
		var object map[string]json.RawMessage
		if err := json.Unmarshal(raw, &object); err != nil {
			return err
		}
		fields := map[string]reflect.Type{}
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			name := strings.Split(field.Tag.Get("json"), ",")[0]
			if name == "" {
				name = field.Name
			}
			fields[name] = field.Type
		}
		for key, value := range object {
			fieldType, ok := fields[key]
			if !ok {
				return fmt.Errorf("unknown field %q", key)
			}
			if err := personalJSONShape(value, fieldType); err != nil {
				return err
			}
		}
	case reflect.Slice:
		var list []json.RawMessage
		if err := json.Unmarshal(raw, &list); err != nil {
			return err
		}
		for _, item := range list {
			if err := personalJSONShape(item, typ.Elem()); err != nil {
				return err
			}
		}
	case reflect.Map:
		var object map[string]json.RawMessage
		if err := json.Unmarshal(raw, &object); err != nil {
			return err
		}
		for _, item := range object {
			if err := personalJSONShape(item, typ.Elem()); err != nil {
				return err
			}
		}
	}
	return nil
}
func personalDecode(w http.ResponseWriter, r *http.Request, dst any) bool {
	content, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || content != "application/json" {
		writeError(w, 415, "invalid_content_type", "use application/json")
		return false
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, personalMaxBody))
	if err != nil {
		var size *http.MaxBytesError
		if errors.As(err, &size) {
			writeError(w, 413, "too_large", "request exceeds 64 KiB")
		} else {
			writeError(w, 400, "invalid", "cannot read JSON")
		}
		return false
	}
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] != '{' || !utf8.Valid(raw) {
		writeError(w, 400, "invalid", "body must be a JSON object")
		return false
	}
	tokens := json.NewDecoder(bytes.NewReader(raw))
	if err = personalJSONTokens(tokens, 0); err == nil {
		_, err = tokens.Token()
		if errors.Is(err, io.EOF) {
			err = nil
		} else if err == nil {
			err = errors.New("multiple JSON values")
		}
	}
	if err != nil {
		writeError(w, 400, "invalid", "malformed JSON or duplicate keys")
		return false
	}
	if err = personalJSONShape(raw, reflect.TypeOf(dst)); err != nil {
		writeError(w, 400, "invalid", fmt.Sprintf("invalid request: %v", err))
		return false
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err = dec.Decode(dst); err != nil {
		writeError(w, 400, "invalid", fmt.Sprintf("invalid request: %v", err))
		return false
	}
	return true
}
func personalQuery(r *http.Request, prefix string) (domain.PersonalQuery, error) {
	q := domain.PersonalQuery{Limit: 50}
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return q, fmt.Errorf("%w: malformed query", domain.ErrInvalid)
	}
	for key, v := range values {
		if len(v) != 1 {
			return q, domain.ErrInvalid
		}
		switch key {
		case "after":
			q.After = v[0]
		case "limit":
			q.Limit, err = strconv.Atoi(v[0])
			if err != nil {
				return q, domain.ErrInvalid
			}
		default:
			return q, domain.ErrInvalid
		}
	}
	return q, domain.ValidatePersonalQuery(q, prefix)
}
func personalPage[T any](items []T, q domain.PersonalQuery, lastID func(T) string) any {
	next := ""
	if len(items) == q.Limit {
		next = lastID(items[len(items)-1])
	}
	return struct {
		Items     []T    `json:"items"`
		NextAfter string `json:"nextAfter,omitempty"`
	}{items, next}
}
func (s *Server) handlePersonalWorkspaces(w http.ResponseWriter, r *http.Request) {
	if !s.personalReady(w, r) {
		return
	}
	id := r.PathValue("id")
	if r.Method != "GET" && r.URL.RawQuery != "" || id != "" && r.URL.RawQuery != "" {
		s.fail(w, r, domain.ErrInvalid)
		return
	}
	var out any
	var err error
	status := 200
	switch r.Method {
	case "GET":
		if id != "" {
			out, err = s.opt.Personal.GetWorkspace(r.Context(), id)
		} else {
			var q domain.PersonalQuery
			q, err = personalQuery(r, domain.PrefixPersonalWorkspace)
			if err == nil {
				var list []domain.PersonalWorkspace
				list, err = s.opt.Personal.ListWorkspaces(r.Context(), q)
				out = personalPage(list, q, func(v domain.PersonalWorkspace) string { return v.ID })
			}
		}
	case "POST":
		var spec domain.PersonalWorkspaceSpec
		if !personalDecode(w, r, &spec) {
			return
		}
		out, err = s.opt.Personal.CreateWorkspace(r.Context(), spec)
		status = 201
	case "PUT":
		var in struct {
			Revision  int64                        `json:"revision"`
			Workspace domain.PersonalWorkspaceSpec `json:"workspace"`
		}
		if !personalDecode(w, r, &in) {
			return
		}
		out, err = s.opt.Personal.UpdateWorkspace(r.Context(), id, in.Revision, in.Workspace)
	case "DELETE":
		var in domain.PersonalDeleteRequest
		if !personalDecode(w, r, &in) {
			return
		}
		out, err = s.opt.Personal.DeleteWorkspaceReviewed(r.Context(), id, in)
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if status == 204 {
		w.WriteHeader(status)
	} else {
		writeJSON(w, status, out)
	}
}
func (s *Server) handlePersonalCollections(w http.ResponseWriter, r *http.Request) {
	if !s.personalReady(w, r) {
		return
	}
	id := r.PathValue("id")
	if r.Method != "GET" && r.URL.RawQuery != "" || id != "" && r.URL.RawQuery != "" {
		s.fail(w, r, domain.ErrInvalid)
		return
	}
	var out any
	var err error
	status := 200
	switch r.Method {
	case "GET":
		if id != "" {
			out, err = s.opt.Personal.GetCollection(r.Context(), id)
		} else {
			var q domain.PersonalQuery
			q, err = personalQuery(r, domain.PrefixPersonalCollection)
			if err == nil {
				var list []domain.PersonalCollection
				list, err = s.opt.Personal.ListCollections(r.Context(), q)
				out = personalPage(list, q, func(v domain.PersonalCollection) string { return v.ID })
			}
		}
	case "POST":
		var spec domain.PersonalCollectionSpec
		if !personalDecode(w, r, &spec) {
			return
		}
		out, err = s.opt.Personal.CreateCollection(r.Context(), spec)
		status = 201
	case "PUT":
		var in struct {
			Revision   int64                         `json:"revision"`
			Collection domain.PersonalCollectionSpec `json:"collection"`
		}
		if !personalDecode(w, r, &in) {
			return
		}
		out, err = s.opt.Personal.UpdateCollection(r.Context(), id, in.Revision, in.Collection)
	case "DELETE":
		var in struct {
			Revision int64 `json:"revision"`
		}
		if !personalDecode(w, r, &in) {
			return
		}
		err = s.opt.Personal.DeleteCollection(r.Context(), id, in.Revision)
		status = 204
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if status == 204 {
		w.WriteHeader(status)
	} else {
		writeJSON(w, status, out)
	}
}
func (s *Server) handlePersonalRecords(w http.ResponseWriter, r *http.Request) {
	if !s.personalReady(w, r) {
		return
	}
	id, collection := r.PathValue("id"), r.PathValue("cid")
	if r.Method != "GET" && r.URL.RawQuery != "" || id != "" && r.URL.RawQuery != "" {
		s.fail(w, r, domain.ErrInvalid)
		return
	}
	var out any
	var err error
	status := 200
	switch r.Method {
	case "GET":
		if id != "" {
			out, err = s.opt.Personal.GetRecord(r.Context(), collection, id)
		} else {
			var q domain.PersonalQuery
			q, err = personalQuery(r, domain.PrefixPersonalRecord)
			if err == nil {
				var list []domain.PersonalRecord
				list, err = s.opt.Personal.ListRecords(r.Context(), collection, q)
				out = personalPage(list, q, func(v domain.PersonalRecord) string { return v.ID })
			}
		}
	case "POST":
		var in struct {
			Values map[string]json.RawMessage `json:"values"`
		}
		if !personalDecode(w, r, &in) {
			return
		}
		out, err = s.opt.Personal.CreateRecord(r.Context(), collection, in.Values)
		status = 201
	case "PUT":
		var in struct {
			Revision int64                      `json:"revision"`
			Values   map[string]json.RawMessage `json:"values"`
		}
		if !personalDecode(w, r, &in) {
			return
		}
		out, err = s.opt.Personal.UpdateRecord(r.Context(), collection, id, in.Revision, in.Values)
	case "DELETE":
		var in struct {
			Revision int64 `json:"revision"`
		}
		if !personalDecode(w, r, &in) {
			return
		}
		err = s.opt.Personal.DeleteRecord(r.Context(), collection, id, in.Revision)
		status = 204
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if status == 204 {
		w.WriteHeader(status)
	} else {
		writeJSON(w, status, out)
	}
}

func (s *Server) handlePersonalLifecycle(w http.ResponseWriter, r *http.Request) {
	if !s.personalReady(w, r) {
		return
	}
	if r.URL.RawQuery != "" {
		s.fail(w, r, domain.ErrInvalid)
		return
	}
	id := r.PathValue("id")
	var out any
	var err error
	switch {
	case strings.HasSuffix(r.URL.Path, "/deletion-preview"):
		var in struct {
			Revision int64  `json:"revision"`
			Mode     string `json:"mode"`
		}
		if !personalDecode(w, r, &in) {
			return
		}
		out, err = s.opt.Personal.PreviewDeletion(r.Context(), id, in.Revision, in.Mode)
	case strings.HasSuffix(r.URL.Path, "/history"):
		out, err = s.opt.Personal.WorkspaceHistory(r.Context(), id)
	case strings.HasSuffix(r.URL.Path, "/usage"):
		out, err = s.opt.Personal.CollectionUsage(r.Context(), id)
	case strings.HasSuffix(r.URL.Path, "/rollback"):
		var in struct {
			Revision       int64 `json:"revision"`
			TargetRevision int64 `json:"targetRevision"`
		}
		if !personalDecode(w, r, &in) {
			return
		}
		out, err = s.opt.Personal.RollbackWorkspace(r.Context(), id, in.Revision, in.TargetRevision)
	default:
		var revision int64
		revision, err = strconv.ParseInt(r.PathValue("revision"), 10, 64)
		if err != nil || revision < 1 {
			s.fail(w, r, domain.ErrInvalid)
			return
		}
		out, err = s.opt.Personal.Snapshot(r.Context(), id, revision)
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, out)
}
